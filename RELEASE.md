# Release Process

## Creating Releases

Push a version tag to automatically create a GitHub release:

```bash
git tag v1.0.0
git push origin v1.0.0
```

**Tag naming:**
- `v1.0.0` - Stable release
- `v1.0.0-beta` - Pre-release (alpha, beta, rc, test, dev)

## What Happens

The release workflow will:
- Run full validation (`python build.py validate`)
- Build binaries for all platforms
- Generate SHA256 checksums
- Create GitHub release with binaries and release notes

## Testing Releases

Use the "Test Release Process" workflow in GitHub Actions to test the release automation without creating actual releases.