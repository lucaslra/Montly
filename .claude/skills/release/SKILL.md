---
name: release
description: Cut a new montly release — bump version, update CHANGELOG, commit, tag, and push to Gitea.
argument-hint: [version bump: patch | minor | major | x.y.z]
---

Cut a new release for the montly project. Follow these steps exactly, in order.

## 1. Check preconditions

Run these checks before doing anything else:

```bash
git status          # must be clean (or only have staged/unstaged changes to release)
git log --oneline -3
git tag --sort=-v:refname | head -1   # current latest tag
```

If there are uncommitted changes unrelated to the release, stop and tell the user.

## 2. Determine the next version

- Read the latest tag (e.g. `v0.9.0`) with `git tag --sort=-v:refname | head -1`
- If the user passed a version argument (e.g. `1.0.0` or `patch`/`minor`/`major`), use it. Otherwise default to **minor** bump.
- Strip the leading `v` for use in CHANGELOG and commit messages; use the full `vX.Y.Z` form for the git tag.

Version bump rules:
- `patch` → increment Z (0.9.0 → 0.9.1)
- `minor` → increment Y, reset Z (0.9.0 → 0.10.0)
- `major` → increment X, reset Y and Z (0.9.0 → 1.0.0)

## 3. Run the test suite

```bash
cd backend && go test ./...
cd frontend && npm test -- --run
```

If either fails, stop and report the failures. Do not proceed.

## 4. Summarise unreleased changes

```bash
git log <previous-tag>..HEAD --oneline
```

Use these commits to write the CHANGELOG entry. Group them into Added / Changed / Fixed / Removed as appropriate. Skip pure chore commits (dependency bumps, release commits) unless they're notable.

## 5. Update CHANGELOG.md

Insert a new section at the top (just after the format line), following the existing style exactly:

```markdown
## [X.Y.Z] — YYYY-MM-DD

### Added / Changed / Fixed / Removed
- **Feature name** — one-line description.
```

Use today's date. Keep descriptions concise — match the voice and length of existing entries.

## 6. Commit everything

Stage the CHANGELOG and any other modified files that belong in the release, then commit:

```
git add CHANGELOG.md [other files if needed]
git commit -m "chore: release X.Y.Z

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

## 7. Tag and push to Gitea

```bash
git tag vX.Y.Z
git push origin main
git push origin vX.Y.Z
```

Always push to `origin` (Gitea). Never push to any other remote.

## 8. Confirm

Report: version released, tag pushed, and a one-line summary of what was in the release.
