---
name: caw-fix
description: >-
  Commits a quick fix directly to develop, compiles with make build, pushes to develop, deploys the built binary to ~/caw/caw via atomic swap, and restarts systemctl caw without opening a PR. Use when the user requests a direct fix or hotfix to develop.
---

# Caw Fix Workflow

Commits a fix directly to develop, pushes, deploys the built binary to `~/caw/caw`, and restarts systemctl. No PR created. No branch rename needed.

## Workflow

1. **Read the task**: The user's prompt or argument describes the fix to implement.
2. **Ensure on develop**: Confirm the current branch is `develop` (`git branch --show-current`). If not, switch to develop first.
3. **Explore the codebase**: Use search tools to understand the relevant code before making changes.
4. **Implement the change**: Write the code following existing conventions and patterns.
5. **Build**: Run `make build` and verify it compiles successfully before committing.
6. **Stage changes**: `git add` the relevant files.
7. **Commit**: Write a commit message in English following [CONTRIBUTING.md](CONTRIBUTING.md) conventions:
   - Format: `type: brief description`
   - Types: `feat`, `fix`, `chore`, `docs`, `refactor`, `style`
   - Example: `fix: make settings sidebar scrollable`
8. **Push**: `git push` to push directly to develop.
9. **Deploy**: Swap the freshly built binary into `~/caw/caw`:
   - `cp <build-output> /tmp/caw-new && chmod +x /tmp/caw-new && mv ~/caw/caw ~/caw/caw.old && mv /tmp/caw-new ~/caw/caw`
10. **Restart**: Run `systemctl restart caw` to apply changes.

## Constraints

- Do NOT rename the branch — stay on `develop`
- Do NOT create a PR
- Must run `make build` before committing
- Do NOT run `make test`
- Deploy the built binary to `~/caw/caw` via atomic swap
- Must run `systemctl restart caw` after deploying
