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

> These are just message **style** hints for humans/reviewers. What actually
> drives the version bump is the keyword logic in `.github/workflows/release.yml`
> (see Versioning below) — not the `type` prefix alone, and **not** the `!` suffix.

## Versioning (driven by `.github/workflows/release.yml`)
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

Key corrections to older notes:
- ❌ `feat!` does **not** trigger a major bump — this action ignores the `!` suffix.
  Use `BREAKING CHANGE` in the body or `#major` / `major:` in the message instead.
- ❌ `chore:`, `docs:`, `refactor:`, `style:` do **not** explicitly map to patch.
  They simply match no keyword, so they fall through to the default **patch** bump.
- Tags are prefixed with `v` (`TAG_PREFIX: v`), e.g. `v1.3.0`.
- To skip a release on a non-important commit, include `[skip ci]` in the message.

### Examples
```
# major (breaking change)
git commit -m "redesign terminal grid API\n\nBREAKING CHANGE: removed legacy grid config"
git commit -m "major: drop Node 18 support"

# minor (new feature)
feat: add workspace emoji picker
feature: add command palette

# patch (bug fix or anything else)
fix: emoji grid not stretching to full width
chore: restructure repo into src/        # -> patch via DEFAULT_BUMP

# no release
docs: add build instructions [skip ci]
```

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
