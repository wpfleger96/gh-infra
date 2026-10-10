# gh-infra Agent Notes

This file is **dev-branch only** — it must never be committed to `main` or any branch
opened as a PR to `babarot/gh-infra` upstream.

## Fork workflow

Two remotes:
- `origin` → `wpfleger96/gh-infra` (personal fork)
- `upstream` → `babarot/gh-infra` (canonical)

Every fix or feature follows this pattern:

1. Create a worktree from `upstream/main` (not dev):
   ```
   git worktree add .worktrees/<sanitized-branch> -b wpfleger96/<type>/<slug> upstream/main
   ```
2. Implement and commit in the worktree.
3. Push to fork and open a PR: `wpfleger96:<branch>` → `babarot/gh-infra:main`.
4. **Also merge into `dev`** so the fix is available for dogfooding immediately:
   ```
   git checkout dev
   git merge wpfleger96/<type>/<slug> --no-ff -m "chore: merge <slug> into dev"
   git push origin dev
   ```

All four CI workflows in `wpfleger96/github-config` build gh-infra from the `dev`
commit SHA pinned in that repo's `.gh-infra-ref`. After every push to `dev` (and
especially after a rebuild/force-push), bump `.gh-infra-ref` to the new `dev` HEAD —
the old SHA is no longer reachable from any branch.

## Stacking PRs

When a fix targets code that only exists in an open upstream PR (not yet in
`upstream/main`), base the fix branch on that PR's branch instead of `upstream/main`:

```
git worktree add .worktrees/<sanitized-branch> -b wpfleger96/fix/<slug> <base-branch>
```

Still open the upstream PR targeting `babarot/gh-infra:main`. GitHub recomputes
the diff dynamically — once the base PR merges, the stacked PR's diff shrinks to
just the new changes.

Note the stacking relationship in both PR descriptions and cross-reference with
`Related: #N` or `Stacked on #N`.

Currently open upstream PRs merged into `dev` (update as PRs merge):
- #160 `when:` clause / `conditional_spec` for conditional RepositorySet settings, incl. `defaults`
- #164 commit message templates (`<% .Source.URL %>` via `GH_INFRA_SOURCE_URL`, headline/body split)
- #203 apply matches FileSet changes by the originating FileSet, not `Identity()` (which
  unnamed FileSets with the same repo list, or `File` manifests for one repo, share), and
  `ParseResult.Validate()` rejects `via: pull_request` FileSets that would share a PR branch
- `wpfleger96/fix/actions-disabled-payload` (upstream PR not yet opened): with `actions.enabled: false`,
  apply sends only `enabled` (GitHub 409s on `allowed_actions`/`sha_pinning_required` otherwise) and
  diff skips those fields and `selected_actions`. Cherry-picked onto `dev` (plus a `reflect` test import
  `dev` lacks) instead of merged, so `dev` stays on its pre-v0.15.0 upstream base

Merged upstream (no longer carried separately on `dev`):
- #161 `plan --diff` and #167 executable file mode (merged 2026-10-10, after
  v0.14.1). The maintainer rebuilt #167 before merging: plan diffs file modes,
  omitting `executable` keeps the current mode, and only mode-changing commits use
  the (unsigned) Git Data API
- #152, #158 (released in v0.13.1)
- #159 silent-accept detection, #163 retry on HEAD conflict, #170 `${ENV_*}`
  secret resolution, #176 `allow_auto_merge` through `merge_strategy` overrides
  (released in v0.14.0)
- #169 was closed; upstream re-landed it as #177 (merge commit title/message pairs
  sent together + validated) and #178 (skip FileSet commits that would not change
  the HEAD tree), both released in v0.14.0

## Rebuilding dev

`dev` = `upstream/main` + every open PR branch merged in with `--no-ff` + this
`AGENTS.md`. When upstream merges PRs, rebuild `dev` from `upstream/main` rather than
keeping pre-review copies of merged work on `dev`:

```
git tag dev-backup-<date> origin/dev
git -C .worktrees/dev checkout -B dev upstream/main   # or `git worktree add -B dev .worktrees/dev upstream/main`
git merge --no-ff -m "chore: merge <slug> into dev" <branch>   # per open PR, stack order
git checkout dev-backup-<date> -- AGENTS.md   # then update and commit
```

Rebase open PR branches onto `upstream/main` first so `dev` merges resolve against
current upstream code. `rerere` is enabled, so a rebase may silently reuse a
resolution recorded against an older `dev`; diff every "Resolved ... using previous
resolution" file against the original commit before continuing. Run `make test`
after each merge, then force-push the result to `origin/dev` and bump
`.gh-infra-ref` in github-config.

## Key code notes

- **`applyToRepo`** (`internal/fileset/gitapi.go`): upstream's orchestration —
  `isNoopCommit` guard (#178), optional PR branch creation, the #163 retry loop
  around the commit call, PR open. It picks `commitViaGraphQL` or
  `commitViaGitDataAPI` as a `commitFunc` before the loop; `needsGitDataAPI` is true
  only when a change has `ModeChanged()`.
- **`isHeadConflict`**: matches the GraphQL `Expected branch to point to ... but it
  did not` error and the Git Data API update-ref `Update is not a fast forward`.
- **Commit messages** (#164): `resolveCommitMessage` renders templates once in
  `applyToRepo`; GraphQL splits the result into headline/body, the Git Data API uses
  the full multi-line message as-is.

## No commit trailers

Git is already configured with the maintainer's identity. Do NOT add
`Co-authored-by` or `Signed-off-by` trailers to commits in this repo.

## Worktree naming

Branch `wpfleger96/feat/executable-files` → worktree `.worktrees/wpfleger96-feat-executable-files`
(replace `/`, `\`, `:` with `-`).
