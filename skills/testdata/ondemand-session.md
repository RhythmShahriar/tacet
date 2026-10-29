---
name: bump-version-go-write
description: "Use when asked to bump the version in version.go and write the changelog entry for the release."
---
<!-- tacet · drafted from this session -->

# Bump version go write

What was asked:

- fix the typo in the readme please
- Bump the version in version.go and write the changelog entry for the release

## Steps

1. Edit `README.md`
2. Read `CHANGELOG.md`
3. Edit `version.go`, `CHANGELOG.md`
4. Run `make check`
5. Run `GH_TOKEN=[masked] gh release create v1.2.0`
