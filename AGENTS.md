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

Currently open upstream PRs (update as PRs merge):
- #167 `feat: support executable file mode in FileSet` — adds `commitViaGitDataAPI`
- #169 `fix: skip commit when GitHub normalizes content to existing tree` — stacked on #167

## Key code notes

- **`commitViaGitDataAPI`** (added in PR #167): only exists on `dev` and the
  `wpfleger96/feat/executable-files` branch; not yet in `origin/main`. Any fix
  targeting this function must be stacked on #167.
- **`commitViaGraphQL`**: the default commit path for non-executable files; exists
  in `origin/main`. The empty-commit noop bug (issue #168) also affects this path
  but requires an extra API round-trip to detect — left for a follow-up.

## No commit trailers

Git is already configured with the maintainer's identity. Do NOT add
`Co-authored-by` or `Signed-off-by` trailers to commits in this repo.

## Worktree naming

Branch `wpfleger96/fix/empty-commit-on-noop` → worktree `.worktrees/wpfleger96-fix-empty-commit-on-noop`
(replace `/`, `\`, `:` with `-`).
