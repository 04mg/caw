# Conventions

## Commits
```
type: brief description
```

### Types
- `feat` — new feature
- `feat!` — breaking change (triggers major version bump)
- `fix` — bug fix
- `chore` — maintenance, restructuring, tooling
- `docs` — documentation only
- `refactor` — code change with no behavior change
- `style` — formatting, whitespace, etc.

### Versioning
Versions are auto-bumped from commits using semver:
- `feat!:` or `BREAKING CHANGE` → **major** (e.g. `1.0.0`)
- `feat:` → **minor** (e.g. `0.2.0`)
- `fix:`, `chore:`, `docs:`, `refactor:`, `style:` → **patch** (e.g. `0.1.1`)

### Examples
```
feat!: redesign terminal grid API
feat: add workspace emoji picker
fix: emoji grid not stretching to full width
chore: restructure repo into src/
docs: add build instructions
refactor: extract dialog components
style: format with oxlint
```

## Branches
```
type/description-in-kebab-case
```

### Examples
```
feat/workspace-emoji
fix/emoji-grid-width
chore/repo-restructure
docs/build-instructions
```
