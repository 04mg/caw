---
name: caw-feature
description: >-
  Implements a feature or fix in the current git worktree, renames the branch for the feature, runs make lint and make build, deploys the caw-{feature} binary and the develop-merged binary to ~/caw, commits, pushes, and opens a PR following the PULL_REQUEST_TEMPLATE. Use when implementing a feature or non-trivial fix in Caw that requires a PR.
---

# Caw Feature Workflow

Implements a fix or feature in the current git worktree, renames it for the feature, runs lint and `make build`, builds and deploys `caw-{feature}` (only this feature) to `~/caw`, then builds the result of merging all open worktrees/branches into `develop` and uses it to REPLACE `~/caw/caw` — then commits, pushes, and opens a PR.

## Workflow

1. **Read the task**: The user's prompt or argument describes the fix or feature to implement.
2. **Rename the worktree branch**: Rename the current worktree's branch to a slug derived from the feature description:
   - `git branch -m <old-name> <feat-slug>`
   - Slug format: `feat/<short-kebab-description>` (e.g. `feat/fix-emoji-grid-overflow`).
   - Keep it concise, lowercase, kebab-case, no spaces or accents.
3. **Explore the codebase**: Use search tools to understand the relevant code before making changes.
4. **Implement the change**: Write the code following existing conventions and patterns.
5. **Lint**: Run `make lint` and fix any issues the linter reports before committing. Repeat until lint passes.
6. **Build**: Run `make build` and verify it compiles successfully. The binary is produced in the current worktree's own directory as `./caw` — note the worktree lives in a different folder, NOT in `~/caw`, so the deploy step below is required.
7. **Deploy the feature binary to `~/caw/caw-{feature}`**: Copy the freshly built binary from this worktree into `~/caw` under a name derived from the feature slug. Use a temp file then move it into place, and `chmod +x` it:
   - `cp ./caw /tmp/caw-feature && chmod +x /tmp/caw-feature`
   - `mv /tmp/caw-feature ~/caw/caw-{feature}` where `{feature}` is the short kebab-case feature name (e.g. `caw-fix-emoji-grid-overflow`), without the `feat/` prefix.
8. **Stage changes**: `git add` the relevant files.
9. **Commit**: Write a commit message following [CONTRIBUTING.md](CONTRIBUTING.md) conventions:
   - Format: `type: brief description`
   - Types: `feat`, `fix`, `chore`, `docs`, `refactor`, `style`
   - Example: `fix: resolve emoji grid overflow on mobile`
10. **Build the merged binary and replace `~/caw/caw` with it**: Generate a binary that reflects what merging all open worktrees/branches into `develop` would produce — WITHOUT touching `develop` itself. This step must run after the feature is committed (step 9) so its branch can be merged:
    - Enumerate the branches to merge: list all current feature branches (see `git branch` / `git worktree list`). Include every branch except `develop`, `main`, and `master`. The branch created for the current feature IS included. Only merge branches that have COMMITTED work — i.e. branches whose tip is not identical to `develop` (skip branches whose HEAD equals the develop ref; uncommitted work in a worktree is not part of any branch and is not merged).
    - Create a throwaway worktree checked out to a detached HEAD at `develop` (or `main` if no `develop`): `git -C /root/caw worktree add --detach /tmp/caw-merge <develop-ref>`. This does not move `develop`.
    - For each branch with committed work (in a sensible order), merge it into that detached worktree: `git -C /tmp/caw-merge merge <branch>`.
    - Resolve any conflicts, keeping both sides' functionality (prioritize feature code over pure formatting; reconcile logic rather than discarding either side).
    - Run `make build` inside `/tmp/caw-merge` and verify it compiles. The binary is produced as `/tmp/caw-merge/caw`.
    - Deploy it by REPLACING the main service binary: `cp /tmp/caw-merge/caw /tmp/caw-new && chmod +x /tmp/caw-new && mv /tmp/caw-new ~/caw/caw`. Do NOT create any `caw-merged` file.
    - Clean up the throwaway worktree: `git -C /root/caw worktree remove /tmp/caw-merge`, then `git -C /root/caw worktree prune`. If a merge fails or cannot be resolved, ask the user before proceeding.
11. **Push**: `git push -u origin HEAD` to push the renamed branch to the remote.
12. **Open a Pull Request against `develop`**: Create the PR targeting `develop` using `gh pr create --base develop` with a body filled from [`.github/PULL_REQUEST_TEMPLATE.md`](.github/PULL_REQUEST_TEMPLATE.md):
    - Fill in the Summary, Related Issue (if known), Type of Change, and Notes for Reviewer sections.
    - In the Testing checklist, tick `make lint` passes and `make build` passes (only check `make test` if you actually ran and verified it).
    - Return the PR URL to the user.

## Constraints

- Must run `make lint` — it must pass before committing
- Do NOT run `make test`
- Must run `make build` — it must compile successfully before committing
- Deploy two binaries to `~/caw`, both `chmod +x`: `caw-{feature}`, and the merged build which must REPLACE `~/caw/caw`
- Never run `cp` directly onto `~/caw/caw` (ETXTBSY while running) — always temp file + `mv`, which atomically swaps the directory entry; do not restart the `caw` service
- Do NOT move `develop` or modify it in any way — merge into a detached throwaway worktree only
- Resolve all merge conflicts in the throwaway worktree; ask the user if a conflict cannot be resolved
- Clean up the throwaway worktree and prune after replacing `~/caw/caw`
- Do NOT create extra branches — rename the current worktree's branch in place
- Only commit when the implementation is complete and lint/build pass
- Always push and open a PR before finishing
