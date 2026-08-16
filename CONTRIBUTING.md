# Conventions

## Commits
```
type: brief description
```

### Types (Conventional Commits)
- `feat` — new feature
- `fix` — bug fix
- `chore` — maintenance, restructuring, tooling
- `docs` — documentation only
- `refactor` — code change with no behavior change
- `style` — formatting, whitespace, etc.

## Versioning
Releases are produced on every push to `main`.
The next version is computed from the **git tags** in the repo. The action scans the commit messages since the last tag and applies the highest-priority keyword it finds:

| In commit message (or body)        | Bump  | From `v1.2.3` |
|------------------------------------|-------|---------------|
| `#major`, `major:`                 | **major** | `v2.0.0` |
| `#minor`, `minor:`                 | **minor** | `v1.3.0` |
| `default`                          | **patch** | `v1.2.4` |

> Note: only the explicit keywords above trigger a bump. Conventional commit types such as `feat:`/`feature:` do not bump minor, and `BREAKING CHANGE` does not bump major.

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
