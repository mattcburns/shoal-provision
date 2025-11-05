# 020: Provisioner Architecture and Design Index

Status: In Progress (Phase 1 complete)
Owners: Provisioning Working Group
Last updated: 2025-11-05

Summary

This document provides the top-level architecture and an indexed breakdown of implementable design documents for the Layer 3 Bare Metal Provisioning System described in “Shoal Provisioner Design Combined.md”. It defines clear component boundaries, interfaces, data models, milestones, and acceptance criteria so AI agents can implement the system incrementally while adhering to Shoal repository protocols.

Scope and Non-Goals

- In scope:
  - Provisioner controller service (jobs API, webhook, Redfish orchestration)
  - Embedded OCI registry (optional addendum, single-binary deployment)
  - Task ISO generation and lifecycle
  - Maintenance OS (bootc-based) composition
  - On-host dispatcher (Go binary) and systemd/Quadlet orchestration
  - Linux, Windows, ESXi workflows
  - Error handling, telemetry, and test strategy
- Out of scope:
  - General Redfish aggregator features unrelated to provisioning
  - Full-blown external registry UIs or vulnerability scanning
  - Hardware onboarding/inventory service outside provisioning’s minimal needs

Repository and Packaging Conventions

- Code layout (proposed):
  - cmd/
    - provisioner-controller (HTTP API + Redfish + optional /v2/ registry)
    - provisioner-dispatcher (static Go binary embedded in maintenance OS)
  - internal/provisioner/
    - api (HTTP handlers, request/response models)
    - jobs (job orchestration, state machine, persistence)
    - redfish (virtual media operations, boot override, power control)
    - iso (task ISO builder, recipe packaging, schema bundling)
    - oci (embedded registry adapter, artifact I/O helpers)
    - schema (recipe schema validation + versions)
    - webhooks (status processing, reconciliation triggers)
    - logging (structured logger adapters)
    - config (service configuration, env/flags)
    - store (SQLite accessors, migrations)
  - pkg/provisioner/ (shared models and constants if needed by both cmds)
  - docs/ and design/02x_*.md (this series)
- Compliance:
  - All new dependencies must be AGPLv3-compatible
  - All new source files include the AGPLv3 header (see AGENTS.md)
  - Always develop on a feature branch and run: go run build.go validate
  - Write tests for all new functionality

High-Level Architecture

- Control plane (Layer 3 only, Redfish-driven):
  - Provisioner Controller receives a recipe via POST /api/v1/jobs
  - Validates recipe against recipe.schema.json
  - Builds a minimal task.iso embedding recipe and any config assets
  - Uses Redfish to:
    - Mount maintenance.iso (CD1)
    - Mount task.iso (CD2)
    - Set one-time boot to CD and reboot
  - Awaits webhook from maintenance OS (success/failure) and performs cleanup (unmount, reboot)
- Execution plane (on server under maintenance OS):
  - Static bootc-based maintenance.iso includes:
    - provisioner-dispatcher (Go)
    - systemd targets and services
    - Quadlet unit files to run privileged containers (tools)
    - oras/podman and pre-bound images where applicable
  - Dispatcher mounts task.iso, validates recipe, writes /run/provision/*, starts systemd target from recipe.task_target
  - Quadlet containers perform partitioning, imaging, bootloader install, config drive, etc.
  - Systemd OnSuccess/OnFailure call controller webhook with final status

Core Components and Responsibilities

- Controller Service
  - API: POST /api/v1/jobs, GET /api/v1/jobs/{id}, POST /api/v1/status-webhook/{serial}
  - State machine: queued → provisioning → {succeeded|failed} → complete
  - Task ISO builder
  - Redfish client orchestration (mount/unmount media, boot override, reboot)
  - Optional: embedded OCI Distribution API (/v2/…) with filesystem backend
- Maintenance OS (bootc-maintenance.iso)
  - Immutable bootc image with podman, oras, systemd, Quadlet, dispatcher, and unit definitions
  - Bound tool images (or pull from controller’s /v2/)
- Dispatcher (Go)
  - Mounts /dev/sr1 (task.iso)
  - Validates recipe.json against recipe.schema.json
  - Writes normalized data to /run/provision/
  - Starts systemd task target (recipe.task_target)
- Tooling via Quadlet
  - Partitioning, imaging, bootloader, config-drive, firmware update, etc.
  - Each tool encapsulated as a container image and executed by a .container unit
- Embedded OCI Registry (optional addendum)
  - Co-hosted /v2/ endpoints for artifacts and tool images
  - Backed by an OCI Layout on disk

Primary Data Models (conceptual)

- Job
  - id: uuid
  - server_serial: string
  - status: enum[queued, provisioning, succeeded, failed, complete]
  - failed_step: string (optional)
  - recipe: json (opaque to controller after validation)
  - created_at, updated_at: timestamps
- Recipe (validated against schema)
  - task_target: string (systemd target)
  - target_disk: string
  - oci_url: string (controller:port/repo:tag, when embedded registry enabled)
  - firmware_url: string
  - partition_layout: array of partitions (size, type_guid, format, label?)
  - user_data, unattend_xml, ks.cfg (as strings or references)
- Artifact
  - name: string (e.g., os-images/ubuntu-rootfs:22.04)
  - media_type: string (for oras)
  - digest/size metadata if cached

Cross-Cutting Concerns

- Security
  - Controller auth (basic/JWT) for API and optional /v2/ registry
  - Webhook shared secret or mTLS for authenticity
  - No secrets in logs; redact sensitive fields in recipes
- Logging
  - Structured, with job_id and server_serial correlation
- Observability
  - Metrics: job durations, step failures, Redfish call latencies, /v2/ I/O
- Configuration
  - Flags/env for ports, DB path, storage roots, registry enablement, webhook secret, concurrency, timeouts
- Error Handling
  - Consistent mapping from systemd failed unit to Job.failed_step
  - Retry policies for Redfish transient failures
- Testing
  - Unit tests for API, ISO builder, Redfish client, schema validation
  - Integration tests with mock Redfish and fake /v2/ registry
  - Golden tests for generated ISO contents and unit files

Implementation Roadmap

- Phase 1: Controller minimal viable
  - Jobs API, SQLite store, schema validation, task ISO builder
  - Redfish: mount media (stubs or mock), boot override, reboot
  - Webhook handling and job state transitions
- Phase 2: Maintenance OS and dispatcher
  - Dispatcher binary, systemd/Quadlet units, minimal Linux workflow
  - Success/failure webhooks wired end-to-end (mock network OK)
- Phase 3: Full Linux workflow
  - Partition + image + bootloader + config-drive containers
  - E2E with real oras artifacts (small test images)
- Phase 4: Windows and ESXi workflows
  - wimapply, bootloader-windows, unattend integration
  - ESXi installer + kickstart flow
- Phase 5: Embedded registry (optional addendum)
  - Co-host /v2/ endpoints, layout-backed storage, auth
  - CI pushes artifacts directly to controller
- Phase 6: Hardening and ops
  - Concurrency controls, retries, timeout policies, metrics, docs

Design Document Index (proposed series)

- 021_Provisioner_Controller_Service.md
  - API contract (POST /api/v1/jobs, GET /api/v1/jobs/{id}, POST /api/v1/status-webhook/{serial})
  - Job state machine, SQLite schema, migration plan
  - Concurrency model and worker pool
  - Acceptance criteria:
    - Create/list job
    - Transition on webhook
    - Persist and reload on restart
- 022_Recipe_Schema_and_Validation.md
  - recipe.schema.json (draft-07), fields and constraints, compatibility policy
  - Validation library choice and error reporting format
  - Acceptance criteria:
    - Invalid recipe returns 400 with field errors
    - Schema backward-compat tests
- 023_Task_ISO_Builder.md
  - ISO generation pipeline, file layout:
    - /recipe.json, /recipe.schema.json, /user-data, /unattend.xml, /ks.cfg
  - Determinism, content hashing, and reproducibility notes
  - Acceptance criteria:
    - Byte-stable ISO given same inputs
    - Size and performance targets
- 024_Maintenance_OS_Build_with_bootc.md
  - Containerfile, systemd units, Quadlet placement, bound images
  - Build pipeline: bootc-image-builder → ISO
  - Acceptance criteria:
    - Boots on supported hardware
    - Contains dispatcher and required tools
- 025_Dispatcher_Go_Binary.md
  - CLI behavior, mounting logic, schema validation, /run/provision output
  - Failure modes and exit codes
  - Acceptance criteria:
    - Valid/invalid recipe handling
    - Starts specified systemd target
- 026_Systemd_and_Quadlet_Orchestration.md
  - Master targets and service dependency graphs
  - Standard units: partition, image-linux, image-windows, bootloader-linux, bootloader-windows, config-drive
  - Acceptance criteria:
    - Order guarantees
    - Idempotency where applicable
- 027_Embedded_OCI_Registry.md (Addendum)
  - /v2/ handler integration, storage layout, auth
  - oras/podman interoperability tests
  - Acceptance criteria:
    - Push/pull artifacts >10GB
    - Authn/z policy tests
- 028_Redfish_Operations.md
  - Endpoints used, request/response examples
  - Virtual media mount, boot override, power operations
  - Polling/backoff strategies and vendor quirks
  - Acceptance criteria:
    - Works against mock Redfish
    - Timeout and retry behavior proven
- 029_Workflow_Linux.md
  - Partition schema mapping, filesystem creation, rootfs extraction, GRUB setup, cloud-init cidata
  - Acceptance criteria:
    - Boots to Linux with expected hostname and SSH keys
- 030_Workflow_Windows.md
  - Partitioning (EFI/MSR/NTFS), wimapply, boot files, unattend.xml placement
  - Acceptance criteria:
    - First boot completes unattended
- 031_Workflow_ESXi.md
  - Dual-ISO with official installer + ks.cfg logic
  - Acceptance criteria:
    - Fully unattended ESXi install and reboot
- 032_Error_Handling_and_Webhooks.md
  - OnSuccess/OnFailure services, payload shapes, secrets, retries
  - Mapping systemd failed unit to job.failed_step
  - Acceptance criteria:
    - Accurate failure attribution and durable delivery
- 033_Security_Model.md
  - API auth, webhook auth, registry auth, secret management
  - Image provenance considerations
  - Acceptance criteria:
    - Unauthorized requests rejected; secrets redacted in logs
- 034_CI_CD_Pipelines_and_Artifacts.md
  - Building dispatcher, images, maintenance ISO, and pushing artifacts
  - Versioning and provenance
  - Acceptance criteria:
    - Reproducible builds and artifact SBOMs (if feasible)
- 035_Test_Strategy.md
  - Unit, integration (mock Redfish, fake registry), and E2E plans
  - Hardware-in-the-loop smoke plan
  - Acceptance criteria:
    - go run build.go validate passes; coverage goals defined
- 036_Release_and_Deployment.md
  - Packaging controller binary, maintenance ISO, configuration, and storage
  - Upgrade/downgrade considerations
  - Acceptance criteria:
    - Rollout playbook validated in staging
- 037_Operations_and_Runbook.md
  - Monitoring, metrics, log triage, capacity planning
  - Common failure modes and remedies
  - Acceptance criteria:
    - SRE can resolve common incidents using the runbook
- 038_Compliance_and_Licensing.md
  - License headers, third-party dependency checks, provenance
  - Acceptance criteria:
    - Dependency license scan clean; headers present

Key Interfaces and Contracts

- Controller ↔ Redfish
  - Operations:
    - MountVirtualMedia(isoURL, bootIndex)
    - SetOneTimeBoot(CD)
    - Reboot(GracefulWithFallback)
    - UnmountVirtualMedia()
  - Timeouts/backoff:
    - Configurable; linear backoff for BMC rate limits; circuit-breaker on persistent 5xx
- Controller ↔ Maintenance OS
  - Webhook:
    - POST /api/v1/status-webhook/{server_serial}
    - Body: {"status": "success"} or {"status": "failed","failed_step": "unit.service"}
    - Auth: shared secret header or mTLS client cert
- Dispatcher ↔ Systemd/Quadlet
  - /run/provision/recipe.env for scalar inputs
  - /run/provision/layout.json, user-data, unattend.xml for larger payloads
- Controller ↔ Embedded OCI (optional)
  - Standard /v2/ registry semantics (oras/podman compatible)
  - Storage: filesystem OCI Layout under configurable root

Acceptance Criteria (system-level)

- End-to-end Linux workflow:
  - POST job with Linux recipe → machine boots maintenance OS → partitions, extracts rootfs, installs bootloader, writes cidata → success webhook → controller unmounts media and reboots → system boots to Linux
- Robust error reporting:
  - Failure in any step returns failed_step correlating to systemd unit
- Deterministic task ISO:
  - Same input → same ISO hash; ISO size within target bounds
- Security controls:
  - API, webhook, and registry endpoints enforce configured authn/z
- Test suite:
  - Unit + integration tests pass via go run build.go validate

Open Questions and Risks

- Vendor-specific Redfish quirks:
  - Catalog and gate with vendor capability profiles
- Large artifact performance:
  - Throttling and I/O isolation when embedded registry is enabled
- Air-gapped updates:
  - Artifact import/export procedures and tooling

References

- Shoal Provisioner Design Combined.md (source specification)
- AGENTS.md (workflow mandates, licensing, test execution)

Change Log

- v0.1 (2025-11-03): Initial architecture index and breakdown.
