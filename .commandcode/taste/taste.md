# Taste

## Workflow
- Prefers a plan-first workflow: expects a written implementation plan (design, exact file paths, code sketches, verification steps) to review and explicitly approve before any code changes begin. Implementation only proceeds after approval. Confidence: 0.8
- Follows the project's AGENTS.md verification workflow — for the Caw repo this means verifying with `make build` rather than running `make test`. Confidence: 0.7
- Expects full delivery through the repo's git workflow: create a feature branch from `develop`, commit with a conventional commit message, push, open a PR into `develop` via `gh` (following the repo's PR template), then check back out to the base branch. Confidence: 0.6

## Code style
- Prefers new features to mirror existing codebase mechanisms (e.g., modeling a provider disable toggle on the existing agent disable toggle, reusing existing WS sync/prefs patterns) rather than introducing novel abstractions. Confidence: 0.7
