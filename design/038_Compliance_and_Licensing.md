# 038: Compliance and Licensing

Status: Proposed
Owners: Project Maintainers (Governance), Provisioning Working Group
Last updated: 2025-11-05

Summary

This document defines the compliance and licensing policy for the Shoal repository and all artifacts produced by the provisioning system. Shoal is released under the GNU Affero General Public License v3.0 (AGPLv3). This policy specifies:
- What licenses are compatible with Shoal
- How to add and track dependencies (code and containers)
- Mandatory source file headers and notices
- Required compliance automation in CI
- Release and distribution obligations (including AGPL “network copyleft”)
- Governance (approvals, exceptions, audits) and acceptance criteria

Scope

In scope:
- Source code, build scripts, documentation, example configs
- Controller and dispatcher binaries
- Maintenance OS image/ISO
- Tool container images
- Example/test artifacts included or distributed with the project
- Embedded web assets and third-party code copied into the repo

Out of scope:
- Users’ own OS images/WIMs and proprietary vendor firmware payloads (operators are responsible for licensing their own artifacts)
- Legal advice (this is an engineering policy; consult counsel for legal interpretations)

License Overview and Policy

- Project license: AGPLv3 (root LICENSE file)
- Compatible inbound licenses (dependencies):
  - Permissive: MIT, BSD (2/3 clause), ISC, Apache-2.0
  - Copyleft: GPLv3-only or GPLv3-or-later may be compatible for separate programs linked by process boundaries; avoid linking GPL-only code into AGPL code unless compatibility is confirmed
- Incompatible inbound licenses (reject):
  - Proprietary licenses, EULAs, or “non-commercial” terms
  - Copyleft with additional restrictions incompatible with AGPLv3 (e.g., SSPL, Commons Clause)
  - GPLv2-only libraries without the “or later” clause (incompatible with AGPLv3)
- Outbound license:
  - All new source contributions are under AGPLv3 by default
  - Do not dual-license without a governance decision and a LICENSES/NOTICE update

AGPLv3 and Network Copyleft Obligations

AGPLv3 §13 requires that when users interact with Shoal over a network, you must offer the complete Corresponding Source to those users. Operational guidance:
- Always publish source code for the exact version in use, including local modifications (patches) and build/packaging scripts
- Provide easy access:
  - Link to the public repository (commit hash/semantic version) used for the running service
  - Include a “Source and Licenses” page in the controller UI or an endpoint (e.g., GET /about/licenses) that returns:
    - Current version and commit
    - Link(s) to source
    - List of third-party notices (licenses and attributions)
- If distributing modified binaries/ISOs outside the public internet, accompany them with the source (tapes/USBs/links per AGPLv3)
- Operators deploying Shoal in a product must ensure their distribution and network-use obligations are fulfilled

Source File Headers (Mandatory)

All new source files MUST include the standard AGPLv3 header (see AGENTS.md for templates). Apply to:
- .go, .py, .js, .ts, .css, .sh, Dockerfile/Containerfile (as comments), systemd unit templates (as comments at top)
- Generated files should say “Code generated—do not edit” and reference the generator; add a short license header where feasible
- Externally vendored or copied code must retain original license headers and include attribution in THIRD_PARTY_NOTICES.md

Dependency Management and Approval Workflow

Before adding any dependency (Go module, container base image, CLI tool, library):
1) License vetting
   - Confirm license is compatible with AGPLv3
   - Record license, version, source URL, and rationale
2) Minimality principle
   - Prefer the Go standard library or existing dependencies over adding new ones
   - Justify any non-trivial dependency with a clear value/cost articulation
3) Security standing
   - Check for known CVEs; ensure project is maintained
4) Governance approval
   - Open a PR with a “Dependency introduction” section showing:
     - License, link, transitive licenses summary
     - Security/CVE check results
     - Alternative analysis
   - A maintainer with compliance role must approve
5) Lock and track
   - Pin versions (go.mod) and vendor if required by build/release policy
   - Update THIRD_PARTY_NOTICES.md with license text or link and attribution

Transitive Dependencies

- The PR introducing a new dependency is responsible for verifying that transitive dependencies are license compatible
- CI generates a dependency license report; PR must include a summary (new licenses introduced, counts)
- If a transitive dependency introduces an incompatible license, either:
  - Replace it
  - Constrain versions to avoid it
  - Remove the parent dependency

Container Images and Artifacts Compliance

- Base images (for maintenance OS, tool containers) must be from reputable sources with clear licenses (e.g., Fedora, UBI, Debian) and documented EULAs; confirm redistribution terms
- Containerfile labels:
  - org.opencontainers.image.licenses
  - org.opencontainers.image.source
  - org.opencontainers.image.revision
  - org.opencontainers.image.title/description
- Bundle third-party license files within images where required by license terms (e.g., Apache NOTICE)
- SBOMs:
  - Generate SBOMs for controller/dispatcher binaries, tool images, and maintenance OS
- Distribution:
  - Respect upstream trademark/redistribution policies (e.g., avoid shipping trademarked vendor assets without permission)
- Example/test artifacts:
  - Do not check in proprietary OS images or WIMs; use minimal open samples or document how to produce them

Compliance Automation (CI Requirements)

On every PR and main/release builds:
- License header check:
  - Verify new/changed source files include AGPL headers (language-appropriate comment)
- License scan:
  - Generate dependency license report for Go modules; detect incompatible licenses
  - For container builds, scan base images’ license manifests where available
- SBOM generation:
  - Controller and dispatcher binaries
  - Tool images and maintenance OS image (on main/release as capacity allows)
- Third-party notices:
  - Update THIRD_PARTY_NOTICES.md if new dependencies are added
- Security scans:
  - Dependency CVE scan; fail on high severity with no mitigation
- Determinism:
  - Ensure reproducible builds where applicable; record SOURCE_DATE_EPOCH
- PR checklist:
  - Confirm “Compliance and Licensing” section completed when adding deps/artifacts

Third-Party Notices and Attributions

- Maintain THIRD_PARTY_NOTICES.md at repo root that includes:
  - Name, version, license, and link for each third-party component
  - Required attribution text (e.g., Apache NOTICE)
  - For copied code snippets (not vendored), include file headers with original copyright/notice and link to upstream
- For container images, include license files under /usr/share/licenses/<component> where relevant

Modifications and Patches

- When modifying third-party code under a permissive license:
  - Retain original copyright and license
  - Add “Modifications Copyright (C) <year> Matthew Burns”
  - Clearly separate local changes or maintain patches in a /patches directory with apply scripts
- Do not remove required notices or trademark attributions

Distribution, Binaries, and ISOs

- Releases MUST include:
  - LICENSE (AGPLv3)
  - Source reference (commit; repo URL)
  - SBOMs and checksums/signatures
  - THIRD_PARTY_NOTICES.md (or a link in the release notes to the exact version)
- Maintenance ISO distribution:
  - Include /usr/share/licenses or equivalent location with license bundles if required by included software
  - Publish the exact source for any changes to bootc image composition or unit files

Use of External Services and APIs

- Only use services with compatible ToS for redistribution and CI pulling
- For embedded assets (fonts, JS libs): prefer vendored copies with clear licenses; avoid dynamic CDN pulls that can change license posture
- Do not embed keys/tokens; never commit secrets

Governance and Exceptions

- Compliance Maintainers:
  - Review dependency additions, license scans, and release artifacts
  - Own THIRD_PARTY_NOTICES.md accuracy
- Exceptions process:
  - File an “Exception Request” issue with:
    - Component, version, license, use case, justification, risk analysis, alternatives tried
  - Approval requires at least two maintainers, including one compliance maintainer
  - Time-bound exceptions must include a remediation plan and deadline
- Periodic audits:
  - Quarterly license/NOTICE audit and SBOM regeneration
  - Annual policy review (this document) and tooling updates

Recordkeeping

- Keep CI-generated license reports and SBOMs as artifacts for each release
- Maintain a /compliance directory (optional) with:
  - Latest SBOMs
  - License scan outputs
  - Exception decisions
  - Audit checklists

Developer Checklist (Before Merge)

- License header added to all new source files
- Dependency licenses verified; report attached to PR
- THIRD_PARTY_NOTICES.md updated if needed
- No incompatible licenses introduced
- SBOM and license scans pass in CI
- Release notes (if applicable) list new third-party components

Operator Guidance (Network Use)

- If you offer Shoal as a network service, ensure users can obtain corresponding source code
- Provide a discoverable link or API endpoint exposing source, version, and licenses
- Propagate notices and attributions to downstream users per third-party license terms

Acceptance Criteria

- CI enforces license headers, dependency license scans, SBOM generation, and basic CVE checks
- THIRD_PARTY_NOTICES.md is present, accurate, and updated on dependency changes
- All releases include LICENSE, SBOMs, checksums/signatures, and third-party notices
- No incompatible licenses are introduced; exceptions (if any) are documented and time-bound
- Controller exposes a source/license disclosure (UI link or API endpoint) suitable for AGPLv3 compliance
- Maintenance OS image includes required notices for bundled components
- go run build.go validate passes including compliance gates

Open Questions

- Should we mandate a machine-readable license manifest (e.g., DEPENDENCIES.json) per release?
- Should we adopt SPDX license identifiers across all files and SBOMs as the single source of truth?
- Do we require signature verification (cosign) of all third-party images at build time?

Change Log

- v0.1 (2025-11-05): Initial compliance and licensing policy covering AGPLv3 obligations, dependency vetting, headers, notices, CI automation, and governance.