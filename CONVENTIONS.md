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
Releases are produced by `anothrNick/github-tag-action` on every push to `main`.
The next version is computed from the **git tags** in the repo (there is **no**
`VERSION` file — the version lives only as a `vX.Y.Z` tag). The action scans the
commit messages since the last tag and applies the highest-priority keyword it
finds:

| In commit message (or body)        | Bump  | From `v1.2.3` |
|------------------------------------|-------|---------------|
| `#major`, `major:`, `BREAKING CHANGE` | **major** | `v2.0.0` |
| `#minor`, `minor:`, `feature:`, `feat:` | **minor** | `v1.3.0` |
| `#patch`, `patch:`, `fix:`          | **patch** | `v1.2.4` |
| nothing recognized → `DEFAULT_BUMP: patch` | **patch** | `v1.2.4` |
| `[skip ci]`                         | **no tag / no release** | — |

## Branches
```
type/description-in-kebab-case
```

> Note: releases are cut from `main` (not from feature branches). Merge to `main`
> and the version bump is decided by the commit messages landed there.

### Examples
```
feat/workspace-emoji
fix/emoji-grid-width
chore/repo-restructure
docs/build-instructions
```
