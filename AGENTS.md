# gh-infra Agent Notes

This file is **dev-branch only** — it must never be committed to `main` or any branch
opened as a PR to `babarot/gh-infra` upstream.

## Fork workflow

Two remotes:
- `origin` → `wpfleger96/gh-infra` (personal fork)
- `upstream` → `babarot/gh-infra` (canonical)

Every fix or feature follows this pattern:

1. Create a worktree from `origin/main` (upstream's main, not dev):
   ```
   git worktree add .worktrees/<sanitized-branch> -b wpfleger96/<type>/<slug> origin/main
   ```
2. Implement and commit in the worktree.
3. Push to fork and open a PR: `wpfleger96:<branch>` → `babarot/gh-infra:main`.
4. **Also merge into `dev`** so the fix is available for dogfooding immediately:
   ```
   git checkout dev
   git merge wpfleger96/<type>/<slug> --no-ff -m "chore: merge <slug> into dev"
   git push origin dev
   ```

All four CI workflows in `wpfleger96/github-config` install gh-infra from
`wpfleger96/gh-infra -b dev`, so `dev` is always the running version.

## Stacking PRs

When a fix targets code that only exists in an open upstream PR (not yet in
`origin/main`), base the fix branch on that PR's branch instead of `origin/main`:

```
git worktree add .worktrees/<sanitized-branch> -b wpfleger96/fix/<slug> wpfleger96/<base-branch>
```

Still open the upstream PR targeting `babarot/gh-infra:main`. GitHub recomputes
the diff dynamically — once the base PR merges, the stacked PR's diff shrinks to
just the new changes.

Note the stacking relationship in both PR descriptions and cross-reference with
`Related: #N` or `Stacked on #N`.

Currently open upstream PRs merged into `dev` (update as PRs merge):
- #160 `when:` clause / `conditional_spec` for conditional RepositorySet settings, incl. `defaults`
- #161 `plan --diff` — inline FileSet diffs in plan output
- #163 retry `createCommitOnBranch` on HEAD conflict
- #164 commit message templates (`<% .Source.URL %>` via `GH_INFRA_SOURCE_URL`, headline/body split)
- #167 `feat: support executable file mode in FileSet` — adds `commitViaGitDataAPI`
- #169 `fix: skip commit when GitHub normalizes content to existing tree` + paired
  squash/merge commit fields — stacked on #167
- #176 `fix(manifest)`: carry `allow_auto_merge` through `merge_strategy` overrides (was dropped by `mergeMergeStrategy`)

Merged upstream (no longer carried separately on `dev`):
- #152, #158 (released in v0.13.1)
- #159 silent-accept detection (merge strategy PATCH verification)
- #170 `${ENV_*}` expansion (merged 2026-09-27, unreleased as of v0.13.1)

## Rebuilding dev

`dev` = `upstream/main` + this `AGENTS.md` + every open PR branch merged in with
`--no-ff`. When upstream merges PRs, rebuild `dev` from `upstream/main` rather than
keeping pre-review copies of merged work on `dev`:

```
git worktree add .worktrees/dev-rebuild -b dev-rebuild upstream/main
git checkout origin/dev -- AGENTS.md && git commit -m "chore: add AGENTS.md with fork workflow notes (dev-branch only)"
git merge --no-ff -m "chore: merge <slug> into dev" origin/<branch>   # per open PR, stack order
```

Run `make test` after each merge, then force-push the result to `origin/dev`.

## Key code notes

- **`commitViaGitDataAPI`** (added in PR #167): only exists on `dev` and the
  `wpfleger96/feat/executable-files` branch; not yet in `upstream/main`. Any fix
  targeting this function must be stacked on #167.
- **`commitViaGraphQL`**: the default commit path for non-executable files; exists
  in `upstream/main`. `applyToRepo` picks it or `commitViaGitDataAPI` (via
  `needsGitDataAPI`) as a `commitFunc` and hands off to `applyViaCommitFunc`.
- **`applyViaCommitFunc`** (#169): shared orchestration — PR branch creation,
  `isContentNoop` guard, commit, PR open. On `dev` the #163 retry loop lives here
  around the noop check + `commit(...)` call, so it covers both commit paths; the
  noop check re-runs against the refetched HEAD on retry. `isHeadConflict` matches
  both the GraphQL `Expected branch to point to ... but it did not` error and the
  Git Data API update-ref `Update is not a fast forward` (HTTP 422).
- **Commit messages** (#164): `resolveCommitMessage` renders templates once in
  `applyViaCommitFunc`; GraphQL splits it into headline/body, the Git Data API uses
  the full multi-line message as-is.

## No commit trailers

Git is already configured with the maintainer's identity. Do NOT add
`Co-authored-by` or `Signed-off-by` trailers to commits in this repo.

## Worktree naming

Branch `wpfleger96/fix/empty-commit-on-noop` → worktree `.worktrees/wpfleger96-fix-empty-commit-on-noop`
(replace `/`, `\`, `:` with `-`).
