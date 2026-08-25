---
name: caw-merge
description: >-
  Merges all (or user-specified) open PRs in Caw into develop, pulls latest develop, validates with make build, and cleans up merged local branches and stale worktrees. Use when the user asks to merge open PRs or consolidate branches into develop.
---

# Caw Merge Workflow

Merges open Pull Requests into `develop` both on the remote (via GitHub) and locally, validates with `make build`, and cleans up leftover local branches.

## Workflow

1. **Select PRs to merge**:
   - If the user provided PR numbers or branch names, target only those.
   - Otherwise, list every open PR: `gh pr list --state open`.
2. **Merge each PR on the remote**:
   - For each selected PR: `gh pr merge <num> --merge --delete-branch` (delete the remote branch on merge).
   - If conflicts occur on the remote, ask the user rather than forcing.
3. **Checkout and update develop**:
   - `git checkout develop`
   - `git pull origin develop`
4. **Validate**: Run `make build` from the repo root to ensure everything compiles.
5. **Cleanup local branches**:
   - List local branches that have been merged into develop: `git branch --merged develop`.
   - Skip `develop` (and `main`/`master` if present).
   - Delete each leftover merged branch: `git branch -d <branch>`.
   - Remove any stale worktrees for those branches: `git worktree prune` and `git worktree remove <path>` where applicable.
6. **Report**: Summarize which PRs were merged, the build result, and which local branches were removed.

## Conflict Resolution Strategy

- **Prefer remote merge**: Let GitHub merge PRs when possible to keep history linear.
- **Prioritize features**: When conflicts arise, keep both sides' functionality and reconcile logic rather than discarding either.
- **Code changes vs formatting**: Prefer the code change over pure formatting diffs.
- **When in doubt**: Ask the user rather than silently discarding either side.

## Constraints

- Do NOT skip `make build` — it must pass before cleanup.
- Do NOT run `make test`
- Do NOT delete `develop`, `main`, or `master`.
- Do NOT force-merge PRs with unresolved conflicts without asking the user.
- Always pull `develop` after remote merges and before building.
