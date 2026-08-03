# Agent Workflow Guidelines

Every AI agent working on this repository must follow this workflow upon task completion:

1. **Branch**: Work on a custom branch named according to the branch convention in [CONTRIBUTING.md](CONTRIBUTING.md) (`type/description-in-kebab-case`). A branch for the current task may already exist — if so, check it out and continue working on it instead of creating a new one.
2. **Rebuild**: Run `make build` to ensure both the frontend and backend compile successfully without errors. Do NOT run `make test`.
3. **Commit**: Commit the changes with a commit message in **English**, following the commit conventions in [CONTRIBUTING.md](CONTRIBUTING.md).
4. **Push**: Do **not** push by default. Only push when explicitly instructed to do so.
