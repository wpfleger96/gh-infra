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
- #161 `plan --diff` — inline FileSet diffs in plan output
- #164 commit message templates (`<% .Source.URL %>` via `GH_INFRA_SOURCE_URL`, headline/body split)
- #167 `feat: support executable file mode in FileSet` — adds `commitViaGitDataAPI`.
  Awaiting the maintainer's redesign: detect file mode drift at plan time, use the
  Git Data API only for mode changes, and `createCommitOnBranch` otherwise. Expect
  the current implementation to be replaced rather than merged as-is.

Merged upstream (no longer carried separately on `dev`):
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
git worktree add -B dev .worktrees/dev upstream/main
git merge --no-ff -m "chore: merge <slug> into dev" <branch>   # per open PR, stack order
git checkout dev-backup-<date> -- AGENTS.md   # then update and commit
```

Rebase open PR branches onto `upstream/main` first so `dev` merges resolve against
current upstream code. Run `make test` after each merge, then force-push the result
to `origin/dev` and bump `.gh-infra-ref` in github-config.

## Key code notes

- **`applyToRepo`** (`internal/fileset/gitapi.go`): upstream's orchestration —
  `isNoopCommit` guard (#178), optional PR branch creation, the #163 retry loop
  around the commit call, PR open. On `dev` it picks `commitViaGraphQL` or
  `commitViaGitDataAPI` (via `needsGitDataAPI`) as a `commitFunc` before the loop,
  so retry and the noop recheck on retry cover both commit paths.
- **`commitViaGitDataAPI`** (#167): only exists on `dev` and the
  `wpfleger96/feat/executable-files` branch. Any fix targeting it must be stacked
  on #167 (or wait for the redesign).
- **`isHeadConflict`**: matches the GraphQL `Expected branch to point to ... but it
  did not` error; #167 adds the Git Data API update-ref `Update is not a fast
  forward` (HTTP 422).
- **`isNoopCommit`**: builds a tree on top of the HEAD tree and compares SHAs. #167
  makes it write mode `100755` for executable files so a mode-only change is not
  treated as a noop.
- **Commit messages** (#164): `resolveCommitMessage` renders templates once in
  `applyToRepo`; GraphQL splits the result into headline/body, the Git Data API uses
  the full multi-line message as-is.

## No commit trailers

Git is already configured with the maintainer's identity. Do NOT add
`Co-authored-by` or `Signed-off-by` trailers to commits in this repo.

## Worktree naming

Branch `wpfleger96/feat/executable-files` → worktree `.worktrees/wpfleger96-feat-executable-files`
(replace `/`, `\`, `:` with `-`).
