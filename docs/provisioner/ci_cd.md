# CI/CD Pipelines and Artifacts

This document describes the CI/CD pipelines for the Shoal provisioner, including build processes, artifact management, and release workflows.

## Overview

The Shoal project uses GitHub Actions for continuous integration and delivery. The CI/CD system ensures code quality, builds artifacts across multiple platforms, generates SBOMs, signs releases, and automates the release process.

## Pipelines

### PR Validation Pipeline

**Trigger**: Pull requests to `master` or `release/**` branches

**Workflow**: `.github/workflows/pr-validation.yml`

**Steps**:
1. **License header check** - Verifies AGPLv3 headers on new/changed files
2. **Dependency license scan** - Checks for AGPLv3-compatible dependencies
3. **Deadcode analysis** - Identifies unreachable code (informational)
4. **Full validation** - Runs `go run build.go validate`
5. **Coverage threshold** - Ensures minimum test coverage (60%)
6. **Recipe schema tests** - Validates provisioner recipe schemas
7. **Redfish mock tests** - Verifies Redfish client integration
8. **Smoke builds** - Builds controller and dispatcher binaries

**Quality Gates**:
- All tests must pass
- Code must be properly formatted (`gofmt`)
- Static analysis must pass (go vet, golangci-lint if installed)
- Security checks must pass (gosec if installed)
- Coverage must meet threshold
- New files must have AGPLv3 license headers

**Optional Checks**:
- Container builds (only if labeled `containers` or container files changed)
- Dockerfile linting with hadolint
- Maintenance OS image build verification

### Release Pipeline

**Trigger**: 
- Tag push matching `v*` pattern (e.g., `v1.0.0`)
- Manual workflow dispatch

**Workflow**: `.github/workflows/release.yml`

**Artifacts Built**:
1. **Controller binaries** (all platforms):
   - `shoal-linux-amd64`
   - `shoal-linux-arm64`
   - `shoal-darwin-amd64`
   - `shoal-darwin-arm64`
   - `shoal-windows-amd64.exe`

2. **Dispatcher binaries** (Linux only, static):
   - `dispatcher-linux-amd64`
   - `dispatcher-linux-arm64`

3. **Software Bill of Materials (SBOM)**:
   - `shoal-sbom.spdx.json` (SPDX format)
   - `shoal-sbom.cyclonedx.json` (CycloneDX format)
   - `shoal-sbom.txt` (human-readable)

4. **Security artifacts**:
   - `SHA256SUMS` - Checksums for all binaries
   - `SHA256SUMS.sig` - Cosign signature (keyless OIDC)
   - `SHA256SUMS.pem` - Certificate for signature verification

**Steps**:
1. Checkout code with full history
2. Install Go and build tools (golangci-lint, gosec, syft, cosign)
3. Run full validation pipeline
4. Build binaries for all platforms
5. Build dispatcher binaries
6. Generate SBOMs using syft
7. Generate SHA256 checksums
8. Sign checksums with cosign (keyless signing via OIDC)
9. Create GitHub release with all artifacts
10. Generate comprehensive release notes

**Release Types**:
- **Pre-release**: Tags containing `-alpha`, `-beta`, `-rc`, `-test`, or `-dev`
- **Stable release**: All other version tags
- **Manual release**: Workflow dispatch with custom version

### Maintenance OS Build Pipeline

**Trigger**: Manual workflow dispatch only (resource-intensive)

**Workflow**: `.github/workflows/build-maintenance-iso.yml`

**Purpose**: Build bootable ISO for the maintenance OS used in provisioning workflows

**Inputs**:
- `image_tag`: Container image tag (default: `shoal-maintenance:dev`)
- `upload_artifact`: Whether to upload ISO as workflow artifact

**Steps**:
1. Free up disk space (ISO builds are large)
2. Install podman and pull bootc-image-builder
3. Build maintenance OS bootc container image
4. Generate bootable ISO using bootc-image-builder
5. Verify ISO artifact
6. Optionally upload ISO as workflow artifact (7 day retention)

**Notes**:
- Requires privileged containers
- Uses rootful podman for bootc-image-builder
- ISOs are not compressed during upload (already compressed)
- Default serial console enabled (`console=ttyS0,115200`)

## Artifact Management

### Versioning

Shoal follows [Semantic Versioning 2.0.0](https://semver.org/):

```
MAJOR.MINOR.PATCH[-PRERELEASE][+BUILD]
```

Examples:
- `v1.0.0` - Stable release
- `v1.0.1` - Patch release
- `v2.0.0` - Major release (breaking changes)
- `v1.1.0-rc.1` - Release candidate
- `v1.1.0-beta.1` - Beta release

### Container Image Tags

For maintenance OS and tool containers:

- `vX.Y.Z` - Specific version tag
- `vX.Y` - Major.minor tag (tracks latest patch)
- `latest` - Latest stable release
- `stable` - Alias for latest stable
- `candidate` - Latest release candidate
- `<git-sha>` - Specific commit (for development)

### SBOM Formats

SBOMs are generated in multiple formats for compatibility:

1. **SPDX JSON** (`*.spdx.json`)
   - Industry standard format
   - Best for automated compliance tools
   - Machine-readable

2. **CycloneDX JSON** (`*.cyclonedx.json`)
   - Popular in security scanning tools
   - Includes vulnerability data
   - Machine-readable

3. **Text Summary** (`*.txt`)
   - Human-readable format
   - Quick review of dependencies
   - Suitable for documentation

### Signature Verification

Releases are signed using [cosign](https://github.com/sigstore/cosign) with keyless signing (OIDC):

**Verify a release**:
```bash
# Download release artifacts
wget https://github.com/mattcburns/shoal/releases/download/v1.0.0/SHA256SUMS
wget https://github.com/mattcburns/shoal/releases/download/v1.0.0/SHA256SUMS.sig
wget https://github.com/mattcburns/shoal/releases/download/v1.0.0/SHA256SUMS.pem

# Verify signature
cosign verify-blob \
  --certificate SHA256SUMS.pem \
  --signature SHA256SUMS.sig \
  SHA256SUMS

# Verify checksums
sha256sum -c SHA256SUMS --ignore-missing
```

## Build Commands

### Local Development

```bash
# Full validation pipeline (required before PR)
go run build.go validate

# Build controller binary
go run build.go build

# Build dispatcher binaries
go run build.go build-dispatcher

# Build all platform binaries
go run build.go build-all

# Run tests with coverage
go run build.go coverage

# Install development tools
go run build.go install-tools
```

### Development Tools

Install required tools:
```bash
go run build.go install-tools
```

This installs:
- **golangci-lint** - Comprehensive Go linter
- **gosec** - Security vulnerability scanner
- **deadcode** - Unreachable code detector

Additional tools for releases:
```bash
# SBOM generation
go install github.com/anchore/syft/cmd/syft@latest

# Binary signing
go install github.com/sigstore/cosign/v2/cmd/cosign@latest

# License checking
go install github.com/google/go-licenses@latest
```

## Quality Standards

### Code Quality

- **Formatting**: All code must pass `gofmt`
- **Linting**: Must pass `go vet` and ideally `golangci-lint`
- **Security**: Must pass `gosec` security scan
- **Deadcode**: Unreachable code should be removed or documented in allowlist

### Testing

- **Minimum coverage**: 60% (enforced in PR validation)
- **Unit tests**: All new features and bug fixes
- **Integration tests**: Redfish client, recipe validation, webhook handling
- **Security tests**: Credential redaction, auth enforcement, rate limiting

### License Compliance

- **Project license**: AGPLv3
- **Dependency licenses**: Must be AGPLv3-compatible
  - ✅ Compatible: MIT, Apache 2.0, BSD, ISC
  - ❌ Incompatible: GPL-2.0, LGPL, proprietary
- **License headers**: All new source files must include AGPLv3 header

See `AGENTS.md` section 1.4 for license header format.

## Container Registry

### Embedded Registry

The provisioner controller includes an embedded OCI registry at `/v2`:

**Usage**:
```bash
# Push to embedded registry
podman push localhost:8080/v2/tools/sgdisk:latest

# Pull from embedded registry
podman pull localhost:8080/v2/maintenance/os:latest
```

### External Registry (Production)

For production deployments, use an external registry:

**Recommended registries**:
- GitHub Container Registry (ghcr.io)
- Docker Hub
- Self-hosted registry (Harbor, Quay)

**Tag strategy**:
```bash
# Stable releases
registry.example.com/shoal/maintenance-os:v1.0.0
registry.example.com/shoal/maintenance-os:v1.0
registry.example.com/shoal/maintenance-os:latest

# Development builds
registry.example.com/shoal/maintenance-os:main-abc1234
registry.example.com/shoal/maintenance-os:pr-123
```

## Retention Policies

### Artifacts

- **Release binaries**: Retained indefinitely
- **SBOMs**: Retained with each release
- **Signatures**: Retained with each release
- **Workflow artifacts**: 7 days (maintenance ISOs)

### Container Images

- **Stable releases**: Retain last 5 versions per major.minor
- **Release candidates**: Retain last 3 per version
- **Development builds**: 30 days or until merged
- **CI-only test images**: 7 days

### Logs and Build Metadata

- **CI logs**: Per GitHub organization policy
- **Build info**: Embedded in binaries
- **Git history**: Permanent

## Reproducible Builds

### Deterministic Builds

The task ISO builder produces deterministic output:
```bash
# Same inputs always produce same hash
go test ./internal/provisioner/iso -run TestTaskISOBuilder_Deterministic
```

### Build Reproducibility

For controller and dispatcher:
- Use `SOURCE_DATE_EPOCH` for timestamps
- Pin Go version and module versions
- Use identical build flags

**Example**:
```bash
# Reproducible build
export SOURCE_DATE_EPOCH=1234567890
go build -trimpath -ldflags="-s -w -buildid=" -o shoal ./cmd/shoal
```

## CI/CD Maintenance

### Updating Go Version

1. Update `.github/workflows/*.yml` files:
   ```yaml
   - name: Set up Go
     uses: actions/setup-go@v5
     with:
       go-version: '1.23'  # Update version
   ```

2. Update `go.mod`:
   ```
   go 1.23
   ```

3. Test locally:
   ```bash
   go run build.go validate
   ```

### Adding New Platforms

1. Update `build.go` platforms list:
   ```go
   platforms := []SupportedPlatform{
       {"linux", "amd64"},
       {"linux", "arm64"},
       {"linux", "riscv64"},  // New platform
   }
   ```

2. Test build:
   ```bash
   go run build.go --platform linux/riscv64 build
   ```

3. Update release workflow if needed

### Adding New Workflows

1. Create workflow file in `.github/workflows/`
2. Follow existing patterns for consistency
3. Add appropriate triggers and permissions
4. Test with workflow dispatch before enabling on PR/push
5. Document in this file

## Troubleshooting

### Build Failures

**Coverage below threshold**:
```bash
# Check current coverage
go run build.go coverage
open coverage.html
```

**License header failures**:
```bash
# Add AGPLv3 header to new files
# See AGENTS.md section 1.4 for template
```

**Dependency license issues**:
```bash
# Scan dependencies
go-licenses csv ./... > licenses.csv
cat licenses.csv
```

### Release Issues

**Signature verification fails**:
- Ensure cosign is installed
- Check certificate validity period
- Verify you're using the correct certificate file

**SBOM generation fails**:
- Install syft: `go install github.com/anchore/syft/cmd/syft@latest`
- Check for network issues (syft may download metadata)
- Verify Go module cache is accessible

**ISO build fails**:
- Check disk space (ISOs are 1-2 GB)
- Verify podman is running in rootful mode
- Check bootc-image-builder version compatibility

## References

- [Design 034: CI/CD Pipelines and Artifacts](../../design/034_CI_CD_Pipelines_and_Artifacts.md)
- [AGENTS.md](../../AGENTS.md) - AI agent development protocol
- [GitHub Actions Documentation](https://docs.github.com/en/actions)
- [Cosign Documentation](https://docs.sigstore.dev/cosign/overview/)
- [SBOM Formats](https://www.cisa.gov/sbom)
