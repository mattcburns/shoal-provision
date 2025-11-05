# 034: CI/CD Pipelines and Artifacts

Status: Proposed
Owners: Provisioning Working Group
Last updated: 2025-11-05

Summary

This document defines the CI/CD pipelines and artifact management for the Layer 3 bare-metal provisioner. It specifies build stages, verification gates, artifact types and naming, versioning and release channels, reproducibility requirements, SBOM and provenance, signing, registry publication, retention/GC policies, and acceptance criteria. It aligns with repository conventions (feature branches, AGPLv3 headers, and go run build.go validate) and supports both the single-binary, embedded-registry deployment model and external registry use.

Scope

- In scope:
  - Pipelines for controller binary, dispatcher binary, bootc maintenance OS image/ISO, tool container images, and test OS artifacts for CI
  - Verification gates: formatting, linting, unit/integration tests, coverage, license checks, reproducible/deterministic outputs
  - Publication: pushing artifacts to an embedded or external OCI registry and attaching release assets
  - Versioning and promotion: semver, channels, and provenance
  - SBOMs, signatures, attestations, and artifact retention/GC
- Out of scope:
  - Environment-specific deployment automation (handled in deployment/runbooks)
  - Full enterprise controls (SSO, secret managers) beyond CI integration hooks

Definitions and Artifact Types

- Controller (binary)
  - Single Go binary implementing: user API, webhook, Redfish orchestration, media serving, and optional embedded OCI registry (/v2)
  - Release asset: tarball containing the binary, example config, and sample systemd unit
- Dispatcher (binary)
  - Static Go binary used inside the maintenance OS to mount/validate task.iso and hand off to systemd
  - Release asset: binary for integration into bootc image build
- Maintenance OS image and ISO
  - bootc-based OCI image containing dispatcher, systemd units, and Quadlet units
  - Bootable ISO produced via bootc-image-builder (or equivalent)
- Tool Containers (images)
  - Container images for steps like partitioning, imaging (oras+tar), wimapply, bootloader, config-drive, vendor firmwares
- Test OS Artifacts (optional CI inputs)
  - Small sample rootfs tarball or minimized WIM for CI integration tests (non-production)
- Documentation and Metadata
  - SBOMs for binaries, images, and ISO
  - Signatures and attestations
  - Checksums and provenance metadata
  - Change logs and release notes

Branching, Triggers, and Environments

- Branch strategy
  - feature/* branches for development
  - main (or master) protected with required checks
  - release/* branches for stabilization when needed
- Triggers
  - Pull Requests: run validation, unit/integration tests, linters, schema checks, deadcode scan, and doc checks
  - Push to main: full CI + publish to staging registry and pre-release artifacts with a pre-release tag (e.g., -rc.N)
  - Tag push (vX.Y.Z): full release build, final signing, SBOM attach, publish to stable channels, and create release notes
- Environments
  - CI ephemeral runners with caching (Go modules, container layers)
  - Staging registry (internal) for RC and development images
  - Production registry and release distribution for stable artifacts

Pipelines Overview

PR Validation Pipeline (required)
- Checkout and bootstrap
- License header check (AGPLv3) for new/changed source files
- go run build.go fmt; go run build.go lint; go run build.go test; go run build.go coverage (enforce minimum coverage thresholds)
- Recipe schema tests and golden tests (022)
- Task ISO builder determinism tests (023)
- Redfish client mock integration tests (028)
- Webhook handler tests (032)
- Deadcode scan; evaluate allowlist (AGENTS)
- Security checks: dependency license scan (AGPL-compatible), basic CVE scan on Go modules and Dockerfiles
- Artifact smoke builds:
  - Controller binary build
  - Dispatcher static build
  - Tool container images (build only, no push)
  - Optional: maintenance OS image layer compile, but skip heavy ISO build on PR unless labeled

Main Branch Pipeline (merge)
- All PR checks
- Build artifacts:
  - Controller binary for linux/amd64 and linux/arm64
  - Dispatcher static binary (linux/amd64 and linux/arm64)
  - Tool container images (tagged with git SHA and moving tag latest)
  - Maintenance OS bootc image (tagged with git SHA; optional ISO if capacity allows)
- SBOMs:
  - Generate SBOMs (e.g., syft) for controller, dispatcher, tool containers, and maintenance OS image
- Signing and Attestations:
  - Sign binaries and container images; attach attestations (e.g., SLSA-style provenance)
- Publish (staging):
  - Push tool images to the staging registry
  - Push maintenance OS image to staging
  - Upload controller and dispatcher binaries to CI artifacts or draft release (pre-release tag, e.g., vX.Y.Z-rc.N)
  - Publish SBOMs and checksums alongside artifacts
- Smoke tests:
  - Basic end-to-end flow with a mock Redfish server and staged registry
  - Deterministic task.iso build verification (same inputs -> same hash)

Release Pipeline (tag vX.Y.Z)
- Versioning and tagging:
  - Annotated tag vX.Y.Z; bump module version if required
- Rebuild from clean state:
  - Rebuild controller, dispatcher, maintenance OS, and tool images from scratch with SOURCE_DATE_EPOCH pinned
  - Verify reproducible hashes match main-branch pre-release where applicable
- SBOM and signatures:
  - Generate SBOMs for all deliverables
  - Sign binaries, ISOs, and images; produce attestations (build provenance)
- Publish (production):
  - Push tool images with tags: vX.Y.Z, major.minor, and latest (if stable), plus digest references
  - Push maintenance OS image similarly
  - Produce maintenance ISO and upload to release (if part of release scope)
  - Upload controller binary tarballs (amd64/arm64), dispatcher binary, checksums, SBOMs, and signatures to the release
- Channel promotion:
  - Promote registry tags from candidate to stable (e.g., :vX.Y.Z → :stable for major line), or set channel tags: stable, candidate
- Documentation:
  - Generate release notes: merged PRs, notable changes, upgrade notes, schema changes, breaking changes, security notes
- Post-release verification:
  - Pull-by-digest checks to verify integrity
  - Quick smoke on minimal hardware or emulator

Build Stages and Requirements

Controller (Go)
- Build flags:
  - Static or mostly-static; embed version info (git SHA, date, version)
  - Hardened build flags as appropriate
- Targets:
  - linux/amd64, linux/arm64
- Verification:
  - Unit/integration tests
  - Binary checksum and signature
  - SBOM (package list and licenses)
- Packaging:
  - Tarball with binary, example config, and example systemd unit
  - Checksums file (sha256sum)

Dispatcher (Go)
- Build:
  - CGO_ENABLED=0, static build for linux/amd64 and linux/arm64
- Verification:
  - Unit/integration tests (mount and schema validator mocked)
  - Binary checksum and signature
  - SBOM
- Delivery:
  - Tarball or raw binary artifact for inclusion in the bootc image build

Tool Containers (Quadlet tools)
- Images:
  - tools/sgdisk
  - tools/linux-imager
  - tools/wimapply
  - tools/linux-bootloader
  - tools/windows-bootloader
  - tools/config-drive
  - tools/vendor-firmware (e.g., supermicro-flasher)
- Build:
  - Build each Dockerfile/Containerfile with labels: org.opencontainers.image.revision, version, created, source
- Tests:
  - Lint Dockerfiles
  - Minimal container unit tests (wrappers dry-run or small integration tests)
- Publish:
  - Push to staging on main; push to production on release
  - Tags: vX.Y.Z, major.minor, latest, git SHA
  - Digests: record and publish

Maintenance OS (bootc image and ISO)
- Build:
  - Multi-stage: compile dispatcher, assemble systemd units and Quadlet, configure CA trust, optionally bind tool images
  - Tag with version and git SHA
- ISO:
  - Produce bootable ISO via bootc-image-builder in release pipeline (may be skipped on main merges for capacity)
- Verification:
  - Basic boot smoke (VM-based) in CI where feasible
  - SBOM for the image
  - ISO checksum and signature
- Publish:
  - Push bootc image to registry with vX.Y.Z, major.minor, and latest
  - Upload ISO and checksums to the release

Test OS Artifacts (optional)
- Small rootfs tarball and minimized WIM for CI integration tests
- Stored in a separate CI-only registry namespace and clearly labeled as non-production
- Never promoted to production channels

Versioning, Tagging, and Channels

- Semantic Versioning for releases: MAJOR.MINOR.PATCH
- Pre-release tags: vX.Y.Z-rc.N for candidates
- Moving tags:
  - latest: current stable release (or current for branch)
  - stable: alias for the latest stable; candidate: latest RC
  - major.minor: tracks latest patch of a minor line
- Image tagging:
  - Always push by semver tag and by digest; optionally maintain moving tags
- Provenance:
  - Record build metadata (commit SHA, date, builder identity) as OCI labels/annotations
- Compatibility:
  - recipe.schema.json changes trigger minor or major bumps depending on backward compatibility

Reproducibility and Determinism

- SOURCE_DATE_EPOCH
  - Pin for ISO builder, controller packaging, and any tarball creation
- Task ISO (023):
  - Deterministic build validated with golden tests on CI
- Dispatcher/builds:
  - Stable flags and module versions; vendor modules when practical
- Images:
  - Avoid non-deterministic layers (timestamps/ordering); prefer explicit times and sorted inputs where possible
- Verification:
  - Compare hashes between main-branch build and release rebuilds for identical inputs

SBOMs, Signing, and Attestations

- SBOMs:
  - Generate for controller and dispatcher binaries (Go module graph)
  - Generate for container images and maintenance OS image
  - Attach SBOM files to release assets and optionally publish as oras artifacts referenced via referrers
- Signing:
  - Sign binaries and ISOs; sign container images (and/or attach signatures via OCI referrers)
  - Publish signatures alongside artifacts; store in registry as referrers for images
- Attestations:
  - Produce build provenance attestations (who built, when, source commit) and attach to release/registy artifacts
- Verification:
  - Document how to verify signatures and attestations for operators

Registries and Publication

- Embedded registry (controller)
  - Used for air-gapped deployments and local testing
  - CI pushes to staging controller instances during integration testing
- External registry (recommended for production release)
  - Push final images to a hosted registry with retention/GC and access controls
- oras and podman compatible publication:
  - Tool containers and maintenance OS: container images
  - OS artifacts (e.g., rootfs tarball/WIM for testing): oras artifacts with appropriate media types
- Access control:
  - Authenticated push; read access per environment
- Retention and GC:
  - tools/*: retain last N tagged releases (e.g., last 5); GC unreferenced blobs after grace period
  - maintenance/*: retain last N releases per major.minor
  - CI-only repos periodically pruned aggressively

Quality Gates and Security

- Mandatory validation:
  - go run build.go validate must pass on PR and before release tagging
  - Unit/integration coverage thresholds enforced
- Dependency hygiene:
  - License scan for AGPLv3 compatibility
  - Vulnerability scan for Go modules and container base images; block high severity with no available mitigation
- Dead code and linting:
  - Deadcode scan; adhere to allowlist and cleanup policy
  - Lint across Go and Dockerfiles
- Schema stability:
  - Test that current and N-1 schema validate representative recipes unless breaking change is intentional
- Supply chain:
  - Verify SBOM generation; maintain signing keys securely in CI secret store

Release Management and Promotion

- Candidates:
  - Tag vX.Y.Z-rc.N, publish to candidate channel, solicit testing
- Promotion:
  - Promote RC to stable by re-tag or rebuild with same commit and SOURCE_DATE_EPOCH; sign and attach final notes
- Rollback:
  - Operators can pin previous tags; controller is single binary, so rollback is a binary swap
  - Registry tags allow downgrading maintenance OS and tools
- Release notes:
  - Summaries include breaking changes, schema diffs, migration steps, and security fixes

Artifact Retention and Cleanup

- Registries:
  - Implement GC job to prune unreferenced blobs post-retention windows
  - Maintain channel tags and semver tags consistently
- Releases:
  - Keep SBOMs, checksums, signatures, attestations, and binaries indefinitely for each stable release
- CI cache:
  - Prune module caches and layer caches periodically to control storage
- Logs and build artifacts:
  - Retain CI logs per organization policy; avoid storing secrets

Operator Consumption

- Controller
  - Download tarball for target arch; verify checksum and signature; deploy via provided systemd unit example
- Maintenance OS
  - Download ISO; verify checksum and signature; host via controller or external web for Redfish mounts
  - Or pull bootc image by tag/digest to rebuild ISO for site-specific variants
- Tools
  - Pull container images from registry in Quadlet units (or use bound images in maintenance OS for air-gapped)
- Provenance and SBOM
  - Operators can verify SBOMs and signatures; document verification steps in deployment docs

Acceptance Criteria

- CI
  - PR validation enforces formatting, linting, tests, schema validation, determinism tests, deadcode scan, and basic security scans
- Artifacts
  - Controller and dispatcher binaries build for amd64/arm64 with checksums, signatures, and SBOMs
  - Tool images and maintenance OS image are built, tagged, signed, and pushed; SBOMs attached
  - Maintenance ISO produced during release; checksums and signatures published
- Publication
  - Staging pushes on main; production pushes and release assets on tags
  - Channel tags and semver tags updated consistently
- Reproducibility
  - Deterministic task.iso validated; reproducible builds compared where feasible
- Documentation
  - Release notes include schema changes, upgrade notes, and security items
- Governance
  - License compliance and vulnerability thresholds enforced; builds fail on violations

Open Questions

- Should we mandate reproducible controller binaries across builders (hermetic toolchain) or keep best-effort with SOURCE_DATE_EPOCH?
- Do we adopt cosign attestations and policy enforcement (verify before pull) in the maintenance OS pull path?
- How aggressively should we prune CI-only artifacts to minimize storage while preserving testability?
- Do we promote a minimal reference rootfs/WIM artifact in an example registry for quickstarts, or require users to provide their own artifacts?

Change Log

- v0.1 (2025-11-05): Initial CI/CD and artifacts design, covering builds, pushes, versions, SBOMs, signing, and retention.