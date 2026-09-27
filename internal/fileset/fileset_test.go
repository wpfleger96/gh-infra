package fileset

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"testing"

	"github.com/babarot/gh-infra/internal/gh"
	"github.com/babarot/gh-infra/internal/manifest"
	"github.com/babarot/gh-infra/internal/ui"
)

func init() {
	ui.DisableStyles()
}

// helper: build a GitHub Contents API JSON response
func contentsJSON(content, sha string) []byte {
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	resp := struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
		SHA      string `json:"sha"`
	}{
		Content:  encoded,
		Encoding: "base64",
		SHA:      sha,
	}
	b, _ := json.Marshal(resp)
	return b
}

// helper: build a mock key for the contents API fetch call
func contentsKey(repo, path string) string {
	return fmt.Sprintf("api repos/%s/contents/%s", repo, path)
}

func makeFileSet(owner, repo string, files []manifest.FileEntry) []*manifest.FileSet {
	return []*manifest.FileSet{
		{
			Metadata: manifest.FileSetMetadata{Owner: owner},
			Spec: manifest.FileSetSpec{
				Repositories: []manifest.FileSetRepository{{Name: repo}},
				Files:        files,
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Plan tests
// ---------------------------------------------------------------------------

func TestPlan_NewFile(t *testing.T) {
	mock := &gh.MockRunner{
		Responses: map[string][]byte{},
		Errors: map[string]error{
			contentsKey("owner/repo", ".github/ci.yml"): gh.ErrNotFound,
		},
	}
	p := NewProcessor(mock, ui.NewStandardPrinterWith(&bytes.Buffer{}, &bytes.Buffer{}))
	fileSets := makeFileSet("owner", "repo", []manifest.FileEntry{
		{Path: ".github/ci.yml", Content: "name: CI"},
	})

	changes, _ := p.Plan(context.Background(), fileSets, "", nil)

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Type != ChangeCreate {
		t.Errorf("expected ChangeCreate, got %s", changes[0].Type)
	}
	if changes[0].Desired != "name: CI" {
		t.Errorf("unexpected desired content: %q", changes[0].Desired)
	}
}

func TestPlan_NoChange(t *testing.T) {
	mock := &gh.MockRunner{
		Responses: map[string][]byte{
			contentsKey("owner/repo", ".github/ci.yml"): contentsJSON("name: CI", "abc123"),
		},
		Errors: map[string]error{},
	}
	p := NewProcessor(mock, ui.NewStandardPrinterWith(&bytes.Buffer{}, &bytes.Buffer{}))
	fileSets := makeFileSet("owner", "repo", []manifest.FileEntry{
		{Path: ".github/ci.yml", Content: "name: CI"},
	})

	changes, _ := p.Plan(context.Background(), fileSets, "", nil)

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Type != ChangeNoOp {
		t.Errorf("expected ChangeNoOp, got %s", changes[0].Type)
	}
}

func TestPlan_ContentDiffers(t *testing.T) {
	mock := &gh.MockRunner{
		Responses: map[string][]byte{
			contentsKey("owner/repo", ".github/ci.yml"): contentsJSON("old content", "sha1"),
		},
		Errors: map[string]error{},
	}
	p := NewProcessor(mock, ui.NewStandardPrinterWith(&bytes.Buffer{}, &bytes.Buffer{}))
	fileSets := makeFileSet("owner", "repo", []manifest.FileEntry{
		{Path: ".github/ci.yml", Content: "new content"},
	})

	changes, _ := p.Plan(context.Background(), fileSets, "", nil)

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	c := changes[0]
	if c.Type != ChangeUpdate {
		t.Errorf("expected ChangeUpdate, got %s", c.Type)
	}
	if c.SHA != "sha1" {
		t.Errorf("expected SHA=sha1, got %s", c.SHA)
	}
}

func TestPlan_CreateOnly_FileNotExists(t *testing.T) {
	mock := &gh.MockRunner{
		Responses: map[string][]byte{},
		Errors: map[string]error{
			contentsKey("owner/repo", "VERSION"): gh.ErrNotFound,
		},
	}
	p := NewProcessor(mock, ui.NewStandardPrinterWith(&bytes.Buffer{}, &bytes.Buffer{}))
	fileSets := makeFileSet("owner", "repo", []manifest.FileEntry{
		{Path: "VERSION", Content: "0.1.0", Reconcile: manifest.ReconcileCreateOnly},
	})

	changes, _ := p.Plan(context.Background(), fileSets, "", nil)

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Type != ChangeCreate {
		t.Errorf("expected ChangeCreate, got %s", changes[0].Type)
	}
}

func TestPlan_CreateOnly_FileExists(t *testing.T) {
	mock := &gh.MockRunner{
		Responses: map[string][]byte{
			contentsKey("owner/repo", "VERSION"): contentsJSON("0.2.0", "sha1"),
		},
		Errors: map[string]error{},
	}
	p := NewProcessor(mock, ui.NewStandardPrinterWith(&bytes.Buffer{}, &bytes.Buffer{}))
	fileSets := makeFileSet("owner", "repo", []manifest.FileEntry{
		{Path: "VERSION", Content: "0.1.0", Reconcile: manifest.ReconcileCreateOnly},
	})

	changes, _ := p.Plan(context.Background(), fileSets, "", nil)

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Type != ChangeNoOp {
		t.Errorf("expected ChangeNoOp (file exists, create_only ignores), got %s", changes[0].Type)
	}
}

func TestPlan_MultipleFilesDiffer(t *testing.T) {
	mock := &gh.MockRunner{
		Responses: map[string][]byte{
			contentsKey("owner/repo", "a.txt"): contentsJSON("old a", "sha-a"),
			contentsKey("owner/repo", "b.txt"): contentsJSON("old b", "sha-b"),
		},
		Errors: map[string]error{},
	}
	p := NewProcessor(mock, ui.NewStandardPrinterWith(&bytes.Buffer{}, &bytes.Buffer{}))

	fileSets := makeFileSet("owner", "repo", []manifest.FileEntry{
		{Path: "a.txt", Content: "new a"},
		{Path: "b.txt", Content: "new b"},
	})

	changes, err := p.Plan(context.Background(), fileSets, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(changes))
	}

	// Both files differ → ChangeUpdate
	if changes[0].Type != ChangeUpdate {
		t.Errorf("a.txt: expected ChangeUpdate, got %s", changes[0].Type)
	}
	if changes[1].Type != ChangeUpdate {
		t.Errorf("b.txt: expected ChangeUpdate, got %s", changes[1].Type)
	}
}

// ---------------------------------------------------------------------------
// Apply tests
// ---------------------------------------------------------------------------

// WildcardMockRunner extends MockRunner to return responses for unmatched keys.
// Resolution order: Responses (exact key) → ContainsResponses (substring) → DefaultResponse.
type WildcardMockRunner struct {
	gh.MockRunner
	// ContainsResponses maps a substring to a response; first match wins.
	ContainsResponses map[string][]byte
	DefaultResponse   []byte
}

func (m *WildcardMockRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	key := strings.Join(args, " ")
	m.Called = append(m.Called, args)
	if err, ok := m.Errors[key]; ok {
		return nil, err
	}
	if resp, ok := m.Responses[key]; ok {
		return resp, nil
	}
	for substr, resp := range m.ContainsResponses {
		if strings.Contains(key, substr) {
			return resp, nil
		}
	}
	return m.DefaultResponse, nil
}

// setupGraphQLMock creates a WildcardMockRunner for the GraphQL-based commit path.
// The noop check (isContentNoop) creates a proposed tree and compares SHAs;
// the mock returns a different new tree SHA so the noop guard does NOT fire and
// the GraphQL mutation is reached.
func setupGraphQLMock(repo string) *WildcardMockRunner {
	const baseTreeSHA = "tree-sha-aaa"
	return &WildcardMockRunner{
		MockRunner: gh.MockRunner{
			Responses: map[string][]byte{
				// Get default branch
				fmt.Sprintf("repo view %s --json defaultBranchRef --jq .defaultBranchRef.name", repo): []byte("main"),
				// Get HEAD SHA
				fmt.Sprintf("api repos/%s/git/ref/heads/main --jq .object.sha", repo): []byte("head123"),
				// isContentNoop: fetch current tree SHA
				fmt.Sprintf("api repos/%s/git/commits/head123 --jq .tree.sha", repo): []byte(baseTreeSHA),
			},
			Errors: map[string]error{},
		},
		// isContentNoop tree creation returns a different SHA → noop guard does not fire.
		ContainsResponses: map[string][]byte{
			"git/trees": []byte("tree-sha-bbb"),
		},
		// Default response for the GraphQL mutation (and any other unmatched dynamic calls).
		DefaultResponse: []byte(`{"data":{"createCommitOnBranch":{"commit":{"oid":"new-sha-456"}}}}`),
	}
}

func TestApply_CreateFile(t *testing.T) {
	mock := setupGraphQLMock("owner/repo")
	p := NewProcessor(mock, ui.NewStandardPrinterWith(&bytes.Buffer{}, &bytes.Buffer{}))

	changes := []Change{
		{
			FileSetID: "ci-files",
			Target:    "owner/repo",
			Path:      ".github/ci.yml",
			Type:      ChangeCreate,
			Desired:   "name: CI",
		},
	}

	results := p.Apply(context.Background(), changes, ApplyOptions{FileSetID: "test"}, ui.NoopReporter{})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Errorf("unexpected error: %v", results[0].Err)
	}

	// Verify GraphQL mutation was called
	callLog := strings.Join(flattenCalls(mock.Called), " | ")
	if !strings.Contains(callLog, "api graphql") {
		t.Errorf("expected GraphQL mutation call, got: %s", callLog)
	}
}

func TestApply_UpdateFile(t *testing.T) {
	mock := setupGraphQLMock("owner/repo")
	p := NewProcessor(mock, ui.NewStandardPrinterWith(&bytes.Buffer{}, &bytes.Buffer{}))

	changes := []Change{
		{
			FileSetID: "ci-files",
			Target:    "owner/repo",
			Path:      ".github/ci.yml",
			Type:      ChangeUpdate,
			Desired:   "name: CI v2",
			SHA:       "old123",
		},
	}

	results := p.Apply(context.Background(), changes, ApplyOptions{FileSetID: "test"}, ui.NoopReporter{})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Errorf("unexpected error: %v", results[0].Err)
	}
}

func flattenCalls(calls [][]string) []string {
	var flat []string
	for _, call := range calls {
		flat = append(flat, strings.Join(call, " "))
	}
	return flat
}

func TestApply_NoOpNotApplied(t *testing.T) {
	mock := &gh.MockRunner{
		Responses: map[string][]byte{},
		Errors:    map[string]error{},
	}
	p := NewProcessor(mock, ui.NewStandardPrinterWith(&bytes.Buffer{}, &bytes.Buffer{}))

	changes := []Change{
		{Type: ChangeNoOp, Target: "owner/repo", Path: "a.txt"},
		{Type: ChangeNoOp, Target: "owner/repo", Path: "b.txt"},
	}

	results := p.Apply(context.Background(), changes, ApplyOptions{FileSetID: "test"}, ui.NoopReporter{})

	if len(results) != 0 {
		t.Errorf("expected 0 results for noop, got %d", len(results))
	}
	if len(mock.Called) != 0 {
		t.Errorf("expected no runner calls, got %d", len(mock.Called))
	}
}

// setupGitDataAPIMock creates a WildcardMockRunner for the Git Data API commit path.
// newTreeSHA controls what POST /git/trees returns; set it equal to the base tree SHA
// to simulate the noop case (GitHub normalized content to what was already stored).
func setupGitDataAPIMock(repo, newTreeSHA string) *WildcardMockRunner {
	const baseTreeSHA = "tree-sha-aaa"
	return &WildcardMockRunner{
		MockRunner: gh.MockRunner{
			Responses: map[string][]byte{
				fmt.Sprintf("repo view %s --json defaultBranchRef --jq .defaultBranchRef.name", repo): []byte("main"),
				fmt.Sprintf("api repos/%s/git/ref/heads/main --jq .object.sha", repo):                 []byte("head123"),
				fmt.Sprintf("api repos/%s/git/commits/head123 --jq .tree.sha", repo):                  []byte(baseTreeSHA),
			},
			Errors: map[string]error{},
		},
		DefaultResponse: []byte(newTreeSHA),
	}
}

// setupGraphQLNoopMock creates a WildcardMockRunner for the GraphQL path where
// isContentNoop returns true (proposed tree SHA == current tree SHA).
func setupGraphQLNoopMock(repo string) *WildcardMockRunner {
	const baseTreeSHA = "tree-sha-aaa"
	return &WildcardMockRunner{
		MockRunner: gh.MockRunner{
			Responses: map[string][]byte{
				fmt.Sprintf("repo view %s --json defaultBranchRef --jq .defaultBranchRef.name", repo): []byte("main"),
				fmt.Sprintf("api repos/%s/git/ref/heads/main --jq .object.sha", repo):                 []byte("head123"),
				fmt.Sprintf("api repos/%s/git/commits/head123 --jq .tree.sha", repo):                  []byte(baseTreeSHA),
			},
			Errors: map[string]error{},
		},
		// Same SHA as baseTreeSHA → noop guard fires.
		ContainsResponses: map[string][]byte{
			"git/trees": []byte(baseTreeSHA),
		},
		DefaultResponse: []byte(`{"data":{"createCommitOnBranch":{"commit":{"oid":"new-sha-456"}}}}`),
	}
}

func TestApply_GraphQL_SkipsCommitWhenTreeUnchanged(t *testing.T) {
	mock := setupGraphQLNoopMock("owner/repo")
	p := NewProcessor(mock, ui.NewStandardPrinterWith(&bytes.Buffer{}, &bytes.Buffer{}))

	changes := []Change{
		{
			FileSetID: "ci-files",
			Target:    "owner/repo",
			Path:      ".github/ci.yml",
			Type:      ChangeCreate,
			Desired:   "name: CI",
		},
	}

	results := p.Apply(context.Background(), changes, ApplyOptions{FileSetID: "test"}, ui.NoopReporter{})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Errorf("unexpected error: %v", results[0].Err)
	}

	// Noop guard fired: GraphQL mutation must not have been called.
	callLog := strings.Join(flattenCalls(mock.Called), " | ")
	if strings.Contains(callLog, "api graphql") {
		t.Errorf("expected no GraphQL mutation (noop), but it was called: %s", callLog)
	}
}

func TestApply_GraphQL_CommitsWhenTreeChanged(t *testing.T) {
	mock := setupGraphQLMock("owner/repo")
	p := NewProcessor(mock, ui.NewStandardPrinterWith(&bytes.Buffer{}, &bytes.Buffer{}))

	changes := []Change{
		{
			FileSetID: "ci-files",
			Target:    "owner/repo",
			Path:      ".github/ci.yml",
			Type:      ChangeCreate,
			Desired:   "name: CI",
		},
	}

	results := p.Apply(context.Background(), changes, ApplyOptions{FileSetID: "test"}, ui.NoopReporter{})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Errorf("unexpected error: %v", results[0].Err)
	}

	// Noop guard did not fire: GraphQL mutation must have been called.
	callLog := strings.Join(flattenCalls(mock.Called), " | ")
	if !strings.Contains(callLog, "api graphql") {
		t.Errorf("expected GraphQL mutation call, got: %s", callLog)
	}
}

func TestApply_GitDataAPI_SkipsCommitWhenTreeUnchanged(t *testing.T) {
	// newTreeSHA == baseTreeSHA: GitHub normalized content to what was already stored.
	mock := setupGitDataAPIMock("owner/repo", "tree-sha-aaa")
	p := NewProcessor(mock, ui.NewStandardPrinterWith(&bytes.Buffer{}, &bytes.Buffer{}))

	changes := []Change{
		{
			FileSetID:  "ci-files",
			Target:     "owner/repo",
			Path:       "bin/tool",
			Type:       ChangeCreate,
			Desired:    "#!/bin/sh\necho hello",
			Executable: true,
		},
	}

	results := p.Apply(context.Background(), changes, ApplyOptions{FileSetID: "test"}, ui.NoopReporter{})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Errorf("unexpected error: %v", results[0].Err)
	}

	// The noop guard should have fired: no ref update means no commit was created.
	callLog := strings.Join(flattenCalls(mock.Called), " | ")
	if strings.Contains(callLog, "git/refs") {
		t.Errorf("expected no ref update (noop), but git/refs was called: %s", callLog)
	}
}

func TestApply_GitDataAPI_CommitsWhenTreeChanged(t *testing.T) {
	// newTreeSHA != baseTreeSHA: content genuinely changed, commit should proceed.
	mock := setupGitDataAPIMock("owner/repo", "tree-sha-bbb")
	p := NewProcessor(mock, ui.NewStandardPrinterWith(&bytes.Buffer{}, &bytes.Buffer{}))

	changes := []Change{
		{
			FileSetID:  "ci-files",
			Target:     "owner/repo",
			Path:       "bin/tool",
			Type:       ChangeCreate,
			Desired:    "#!/bin/sh\necho hello",
			Executable: true,
		},
	}

	results := p.Apply(context.Background(), changes, ApplyOptions{FileSetID: "test"}, ui.NoopReporter{})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Errorf("unexpected error: %v", results[0].Err)
	}

	// The guard should NOT have fired: ref update confirms the commit was created.
	callLog := strings.Join(flattenCalls(mock.Called), " | ")
	if !strings.Contains(callLog, "git/refs") {
		t.Errorf("expected ref update after commit, but git/refs was not called: %s", callLog)
	}
}

// ---------------------------------------------------------------------------
// HasChanges tests
// ---------------------------------------------------------------------------

func TestHasChanges_AllNoOp(t *testing.T) {
	changes := []Change{
		{Type: ChangeNoOp},
		{Type: ChangeNoOp},
		{Type: ChangeNoOp},
	}
	if HasChanges(changes) {
		t.Error("expected HasChanges=false for all noop")
	}
}

func TestHasChanges_WithCreateOrUpdate(t *testing.T) {
	tests := []struct {
		name    string
		changes []Change
		want    bool
	}{
		{
			name:    "with create",
			changes: []Change{{Type: ChangeNoOp}, {Type: ChangeCreate}},
			want:    true,
		},
		{
			name:    "with update",
			changes: []Change{{Type: ChangeUpdate}},
			want:    true,
		},
		{
			name:    "empty",
			changes: []Change{},
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasChanges(tt.changes)
			if got != tt.want {
				t.Errorf("HasChanges() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CountChanges tests
// ---------------------------------------------------------------------------

func TestCountChanges(t *testing.T) {
	changes := []Change{
		{Type: ChangeCreate},
		{Type: ChangeCreate},
		{Type: ChangeUpdate},
		{Type: ChangeNoOp},
	}

	creates, updates, deletes := CountChanges(changes)

	if creates != 2 {
		t.Errorf("creates: got %d, want 2", creates)
	}
	if updates != 1 {
		t.Errorf("updates: got %d, want 1", updates)
	}
	if deletes != 0 {
		t.Errorf("deletes: got %d, want 0", deletes)
	}
}

// ---------------------------------------------------------------------------
// Authoritative mode tests
// ---------------------------------------------------------------------------

// dirContentsJSON builds a GitHub Contents API JSON response for a directory listing.
func dirContentsJSON(files []struct{ Path, Type string }) []byte {
	type item struct {
		Path string `json:"path"`
		Type string `json:"type"`
	}
	var items []item
	for _, f := range files {
		items = append(items, item{Path: f.Path, Type: f.Type})
	}
	b, _ := json.Marshal(items)
	return b
}

func TestPlan_MirrorDetectsOrphans(t *testing.T) {
	// file1.yml is declared in YAML, file2.yml is NOT → file2.yml should be ChangeDelete
	dirFiles := []struct{ Path, Type string }{
		{Path: "config/file1.yml", Type: "file"},
		{Path: "config/file2.yml", Type: "file"},
	}

	mock := &gh.MockRunner{
		Responses: map[string][]byte{
			// file1.yml exists in repo with same content → NoOp
			contentsKey("owner/repo", "config/file1.yml"): contentsJSON("content1", "sha1"),
			// directory listing for authoritative orphan detection
			contentsKey("owner/repo", "config"): dirContentsJSON(dirFiles),
		},
		Errors: map[string]error{},
	}
	p := NewProcessor(mock, ui.NewStandardPrinterWith(&bytes.Buffer{}, &bytes.Buffer{}))

	fileSets := []*manifest.FileSet{
		{
			Metadata: manifest.FileSetMetadata{Owner: "owner"},
			Spec: manifest.FileSetSpec{
				Repositories: []manifest.FileSetRepository{{Name: "repo"}},
				Files: []manifest.FileEntry{
					{
						Path:      "config/file1.yml",
						Content:   "content1",
						Reconcile: manifest.ReconcileAuthoritative,
						DirScope:  "config",
					},
				},
			},
		},
	}

	changes, err := p.Plan(context.Background(), fileSets, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect 2 changes: NoOp for file1.yml, Delete for file2.yml
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d: %+v", len(changes), changes)
	}

	var foundDelete bool
	for _, c := range changes {
		if c.Path == "config/file2.yml" && c.Type == ChangeDelete {
			foundDelete = true
		}
	}
	if !foundDelete {
		t.Errorf("expected ChangeDelete for config/file2.yml, changes: %+v", changes)
	}
}

func TestPlan_AdditiveIgnoresOrphans(t *testing.T) {
	// Same setup but with additive mode — no deletes should be generated
	dirFiles := []struct{ Path, Type string }{
		{Path: "config/file1.yml", Type: "file"},
		{Path: "config/file2.yml", Type: "file"},
	}

	mock := &gh.MockRunner{
		Responses: map[string][]byte{
			contentsKey("owner/repo", "config/file1.yml"): contentsJSON("content1", "sha1"),
			// directory listing should NOT be called for additive mode, but include it to be safe
			contentsKey("owner/repo", "config"): dirContentsJSON(dirFiles),
		},
		Errors: map[string]error{},
	}
	p := NewProcessor(mock, ui.NewStandardPrinterWith(&bytes.Buffer{}, &bytes.Buffer{}))

	fileSets := []*manifest.FileSet{
		{
			Metadata: manifest.FileSetMetadata{Owner: "owner"},
			Spec: manifest.FileSetSpec{
				Repositories: []manifest.FileSetRepository{{Name: "repo"}},
				Files: []manifest.FileEntry{
					{
						Path:      "config/file1.yml",
						Content:   "content1",
						Reconcile: manifest.ReconcileAdditive,
						DirScope:  "config",
					},
				},
			},
		},
	}

	changes, err := p.Plan(context.Background(), fileSets, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, c := range changes {
		if c.Type == ChangeDelete {
			t.Errorf("additive mode should not generate ChangeDelete, got delete for %s", c.Path)
		}
	}
}

func TestCountChanges_WithDeletes(t *testing.T) {
	changes := []Change{
		{Type: ChangeCreate},
		{Type: ChangeUpdate},
		{Type: ChangeDelete},
		{Type: ChangeDelete},
		{Type: ChangeNoOp},
	}

	creates, updates, deletes := CountChanges(changes)

	if creates != 1 {
		t.Errorf("creates: got %d, want 1", creates)
	}
	if updates != 1 {
		t.Errorf("updates: got %d, want 1", updates)
	}
	if deletes != 2 {
		t.Errorf("deletes: got %d, want 2", deletes)
	}
}

func TestHasChanges_ChangeDelete(t *testing.T) {
	changes := []Change{
		{Type: ChangeNoOp},
		{Type: ChangeDelete},
	}
	if !HasChanges(changes) {
		t.Error("expected HasChanges=true when ChangeDelete is present")
	}
}

func TestHasChanges_OnlyDeletes(t *testing.T) {
	changes := []Change{
		{Type: ChangeDelete},
	}
	if !HasChanges(changes) {
		t.Error("expected HasChanges=true when only ChangeDelete changes exist")
	}
}

// ---------------------------------------------------------------------------
// isHeadConflict and retry tests
// ---------------------------------------------------------------------------

// headConflictMsg is the exact createCommitOnBranch error GitHub returns on a HEAD conflict.
const headConflictMsg = `Expected branch to point to "1d13718b1408525401981e76c82e25c8c1d6dc67" but it did not.  Pull and try again.`

func TestIsHeadConflict(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"real GitHub error wrapped by commitViaGraphQL", fmt.Errorf("graphql mutation: %w", errors.New(headConflictMsg)), true},
		{"real GitHub error from graphql errors payload", fmt.Errorf("graphql: %s", headConflictMsg), true},
		{"git data API non-fast-forward wrapped by commitViaGitDataAPI", fmt.Errorf("update ref: %w", errors.New("gh: Update is not a fast forward (HTTP 422)")), true},
		{"unrelated graphql error", fmt.Errorf("graphql: some other error"), false},
		{"partial match", errors.New("Expected branch to point to main"), false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isHeadConflict(tt.err); got != tt.want {
				t.Errorf("isHeadConflict(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// SequenceMockRunner returns different responses for successive calls matching a prefix.
type SequenceMockRunner struct {
	gh.MockRunner
	DefaultResponse []byte
	sequences       map[string][]sequenceEntry
	callCounts      map[string]int
	mu              sync.Mutex
}

type sequenceEntry struct {
	response []byte
	err      error
}

func (m *SequenceMockRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	key := strings.Join(args, " ")
	m.mu.Lock()
	m.Called = append(m.Called, args)
	m.CalledStdin = append(m.CalledStdin, nil)
	for prefix, entries := range m.sequences {
		if strings.HasPrefix(key, prefix) {
			idx := m.callCounts[prefix]
			m.callCounts[prefix]++
			m.mu.Unlock()
			if idx < len(entries) {
				e := entries[idx]
				return e.response, e.err
			}
			return m.DefaultResponse, nil
		}
	}
	m.mu.Unlock()
	if err, ok := m.Errors[key]; ok {
		return nil, err
	}
	if resp, ok := m.Responses[key]; ok {
		return resp, nil
	}
	return m.DefaultResponse, nil
}

// repeatEntry returns n copies of a successful sequence entry.
func repeatEntry(response string, n int) []sequenceEntry {
	entries := make([]sequenceEntry, n)
	for i := range entries {
		entries[i] = sequenceEntry{response: []byte(response)}
	}
	return entries
}

// newHeadConflictMock returns a runner whose first createCommitOnBranch call fails with
// a HEAD conflict and whose second succeeds. extraSequences adds per-prefix responses.
// Tree lookups and tree creation return distinct SHAs so the noop guard does not fire.
func newHeadConflictMock(repo string, extraSequences map[string][]sequenceEntry) *SequenceMockRunner {
	// The Git Data API path fetches and creates a tree twice per attempt (noop check + commit).
	const treeCalls = 2 * maxCommitRetries
	sequences := map[string][]sequenceEntry{
		"api graphql": {
			{err: errors.New(headConflictMsg)},
			{response: []byte(`{"data":{"createCommitOnBranch":{"commit":{"oid":"final-sha"}}}}`)},
		},
		fmt.Sprintf("api repos/%s/git/commits/", repo): repeatEntry("base-tree", treeCalls),
		fmt.Sprintf("api repos/%s/git/trees ", repo):   repeatEntry("new-tree", treeCalls),
	}
	maps.Copy(sequences, extraSequences)
	return &SequenceMockRunner{
		MockRunner: gh.MockRunner{
			Responses: map[string][]byte{
				fmt.Sprintf("repo view %s --json defaultBranchRef --jq .defaultBranchRef.name", repo): []byte("main"),
			},
			Errors: map[string]error{},
		},
		DefaultResponse: []byte(`{"data":{"createCommitOnBranch":{"commit":{"oid":"new-sha"}}}}`),
		sequences:       sequences,
		callCounts:      make(map[string]int),
	}
}

func applyOneChange(t *testing.T, runner gh.Runner, repo string, opts ApplyOptions, executable bool) {
	t.Helper()
	p := NewProcessor(runner, ui.NewStandardPrinterWith(&bytes.Buffer{}, &bytes.Buffer{}))
	changes := []Change{
		{
			FileSetID:  opts.FileSetID,
			Target:     repo,
			Path:       ".github/ci.yml",
			Type:       ChangeUpdate,
			Desired:    "name: CI",
			Executable: executable,
		},
	}

	results := p.Apply(context.Background(), changes, opts, ui.NoopReporter{})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("expected success after retry, got error: %v", results[0].Err)
	}
}

func TestApply_RetryOnHeadConflict_Push(t *testing.T) {
	repo := "owner/repo"
	mainRef := "api repos/owner/repo/git/ref/heads/main "
	mock := newHeadConflictMock(repo, map[string][]sequenceEntry{
		mainRef: {{response: []byte("head123")}, {response: []byte("head456")}},
	})

	applyOneChange(t, mock, repo, ApplyOptions{FileSetID: "test", Via: manifest.ViaPush}, false)

	if got := mock.callCounts["api graphql"]; got != 2 {
		t.Errorf("expected 2 graphql calls (1 conflict + 1 retry), got %d", got)
	}
	if got := mock.callCounts[mainRef]; got != 2 {
		t.Errorf("expected 2 default branch HEAD fetches (initial + retry), got %d", got)
	}
}

func TestApply_RetryOnHeadConflict_PullRequestRefetchesPRBranch(t *testing.T) {
	repo := "owner/repo"
	mainRef := "api repos/owner/repo/git/ref/heads/main "
	prRef := "api repos/owner/repo/git/ref/heads/gh-infra/sync-test "
	mock := newHeadConflictMock(repo, map[string][]sequenceEntry{
		mainRef: {{response: []byte("head123")}},
		prRef:   {{response: []byte("pr-head456")}},
	})

	applyOneChange(t, mock, repo, ApplyOptions{FileSetID: "test", Via: manifest.ViaPullRequest}, false)

	if got := mock.callCounts["api graphql"]; got != 2 {
		t.Errorf("expected 2 graphql calls (1 conflict + 1 retry), got %d", got)
	}
	if got := mock.callCounts[mainRef]; got != 1 {
		t.Errorf("expected default branch HEAD fetched once (initial only), got %d", got)
	}
	if got := mock.callCounts[prRef]; got != 1 {
		t.Errorf("expected PR branch HEAD fetched once (retry), got %d", got)
	}
}

func TestApply_RetryOnHeadConflict_GitDataAPI(t *testing.T) {
	repo := "owner/repo"
	mainRef := "api repos/owner/repo/git/ref/heads/main "
	updateRef := "api repos/owner/repo/git/refs/heads/main --method PATCH"
	mock := newHeadConflictMock(repo, map[string][]sequenceEntry{
		mainRef:   {{response: []byte("head123")}, {response: []byte("head456")}},
		updateRef: {{err: errors.New("gh: Update is not a fast forward (HTTP 422)")}, {response: []byte("{}")}},
	})

	applyOneChange(t, mock, repo, ApplyOptions{FileSetID: "test", Via: manifest.ViaPush}, true)

	if got := mock.callCounts[updateRef]; got != 2 {
		t.Errorf("expected 2 update-ref calls (1 conflict + 1 retry), got %d", got)
	}
	if got := mock.callCounts[mainRef]; got != 2 {
		t.Errorf("expected 2 default branch HEAD fetches (initial + retry), got %d", got)
	}
	if got := mock.callCounts["api graphql"]; got != 0 {
		t.Errorf("expected no graphql calls for executable files, got %d", got)
	}
}

func TestApply_RetryOnHeadConflict_SkipsWhenConcurrentCommitMatches(t *testing.T) {
	repo := "owner/repo"
	mainRef := "api repos/owner/repo/git/ref/heads/main "
	mock := newHeadConflictMock(repo, map[string][]sequenceEntry{
		mainRef: {{response: []byte("head123")}, {response: []byte("head456")}},
		// Second noop check (after refetching HEAD) yields the base tree: the
		// concurrent commit already contains our content.
		"api repos/owner/repo/git/trees ": {{response: []byte("new-tree")}, {response: []byte("base-tree")}},
	})

	applyOneChange(t, mock, repo, ApplyOptions{FileSetID: "test", Via: manifest.ViaPush}, false)

	if got := mock.callCounts["api graphql"]; got != 1 {
		t.Errorf("expected 1 graphql call (conflict only, retry skipped as noop), got %d", got)
	}
}
