# 039: Provisioner — Phase 3 Plan (Full Linux Workflow)

Status: In progress (nearing completion)
Owners: Provisioning Working Group
Last updated: 2025-11-07

Summary

Phase 3 implements the end-to-end Linux provisioning workflow inside the maintenance OS using systemd and Quadlet, driven by recipes provided by the controller. This phase delivers partitioning, rootfs imaging, bootloader installation, and optional cloud-init Config Drive creation, with robust idempotency and failure attribution. It builds directly on the Phase 1 Controller and Phase 2 Dispatcher/Maintenance OS.

References

- 020_Provisioner_Architecture.md (breakdown and roadmap)
- 021_Provisioner_Controller_Service.md (API/state machine)
- 025_Dispatcher_Go_Binary.md (on-host entrypoint)
- 026_Systemd_and_Quadlet_Orchestration.md (targets/units)
- 029_Workflow_Linux.md (step specs and acceptance)
- 032_Error_Handling_and_Webhooks.md (webhook behavior)
- 035_Test_Strategy.md (tests and coverage goals)

Scope (Phase 3)

- Linux workflow units and Quadlet containers:
  - partition.service + partition.container (sgdisk/mkfs wrappers)
  - image-linux.service + image-linux.container (oras → tar extraction)
  - bootloader-linux.service + bootloader-linux.container (UEFI GRUB)
  - config-drive.service + config-drive.container (NoCloud CIDATA; optional)
- Dispatcher integration: assumes Phase 2 writes /run/provision/* and starts install-linux.target
- Controller integration: no API surface change; ensure webhook consumption and cleanup paths already in place
- Deliver an E2E path using small test artifacts (rootfs tarball via oras)

Out of scope (for Phase 3)

- Windows and ESXi workflows (030, 031)
- Embedded registry (/v2/) implementation (027)
- Secure Boot enablement (future enhancement)

Milestones and Deliverables

1) Unit Graph and Stubs
- Commit host .service units and Quadlet .container files (with placeholder images)
- Wrapper scripts for each step with dry-run logic and logging
- systemd-analyze verification tests
- Status: stub units, Quadlet definitions, and scripts added (2025-11-06)

2) Partitioning
- Implement partition-wrapper with schema → sgdisk/mkfs mapping and idempotency checks
- Tests: layout translation, idempotent re-run, error cases (invalid schema)
- Status: Go planner (`internal/provisioner/maintenance/partition`) and CLI (`cmd/partition-plan`) emit deterministic sgdisk/mkfs commands; wrapper invokes planner in dry-run by default (2025-11-06)

3) Imaging
- Implement image-linux-wrapper: oras pull → tar extraction to /mnt/new-root
- Tests: digest capture/stamp, extraction flags (-xpf), negative case (unreachable OCI)
  - Status: image planner CLI emits oras→tar commands and wrapper now supports dry-run/apply execution (2025-11-06)

4) Bootloader
- Implement bootloader-linux-wrapper: mount ESP, chroot, grub install, fstab generation
- Tests: fstab content, UUID detection, idempotency on re-run
- Status: bootloader planner and wrapper emit apply/dry-run plans; full integration testing pending (2025-11-06)

5) Config Drive (optional)
- Implement config-drive-wrapper: create VFAT CIDATA partition (if present in layout), write user-data and meta-data
- Tests: content hashing, idempotent overwrite
- Status: config-drive planner and wrapper handle optional CIDATA partition with dry-run/apply wiring (2025-11-06)

6) Dispatcher Normalization
- Implement provisioner-dispatcher binary: validate recipe, write /run/provision assets, start declared target
- Tests: unit coverage for env persistence, schema failure mapping, systemctl invocation toggles
- Status: dispatcher CLI normalizes recipe inputs and exercises exit codes via unit tests (2025-11-06)

7) Webhooks and Attribution
- Ensure OnSuccess/OnFailure wiring posts correct payloads with failing unit name
- Integration: controller transitions and cleanup as per 021/032
- Status: webhook services persist delivery_id, include optional metadata, and retry via systemd to report failing units accurately (2025-11-06)

8) E2E Validation
- VM-based or containerized integration test harness mounting maintenance.iso + task.iso
- Happy path to success; 1-2 negative cases (e.g., invalid layout)
  - Status: repo integration harness exercises dispatcher → planners path, covering happy flow plus simulated image-linux failure for webhook attribution (2025-11-07). Maintenance ISO is produced via bootc-image-builder; installed system includes default serial console kargs for headless debugging. Installer serial output is deferred (not required for Phase 3).

Acceptance Criteria (summarized)

- End-to-end Linux job provisions and boots with expected fstab and optional CIDATA (029 §14)
- Failures attribute precise unit names via provision-failed@.service (026 §7.2)
- Re-runs are safe and mostly no-ops when state matches (idempotency)
- go run build.go validate passes with added tests and wiring (035)

Additional Notes (Maintenance OS)

- The maintenance ISO is built from `images/maintenance/Containerfile` using `scripts/build_maintenance_os.sh`.
- bootc-image-builder requires rootful Podman; the build script handles exporting from rootless storage and running the builder with sudo.
- Kernel args for serial (console=ttyS0,115200 console=tty0) are injected via `/usr/lib/bootc/kargs.d/50-serial.conf` and apply to the installed system’s boot entries.
- Capturing installer-stage serial logs is out-of-scope for Phase 3 and can be revisited later if needed.

Remaining Tasks to Close Phase 3

- Optional: capture live webhook payloads during integration to document exact shapes per 032.
- Optional: CI job to build the maintenance ISO artifact (gated/opt-in).
- Final doc polish and PR to merge `feature/provisioner-phase3` into `master`.

Test Strategy (Phase 3)

- Unit tests per wrapper where feasible (logic extracted into small helpers)
- Integration tests with mock artifacts and loopback devices (or skipped when not available in CI)
- Keep tests deterministic and offline; prefer local fixtures over network

Operational Notes

- Image references may point to controller-hosted artifacts once embedded registry (Phase 5) is ready; for Phase 3 use pre-bound/test images
- Ensure logs redact sensitive data (user-data); log sizes/digests only

Risks & Mitigations

- Disk/UEFI variance: validate against a small set of VM/device types; provide clear error messages
- Large artifacts: start with small rootfs images to keep CI fast; scale later
- Tooling availability inside containers: pin container images with required utilities

Start Checklist

- Branch: feature/provisioner-phase3
- Baseline: go run build.go validate → PASS
- Designs: 020/021/025/026/029 reviewed
- Initial tests: added (skipped where necessary to keep CI green)

Change Log

- v0.1 (2025-11-06): Initial Phase 3 plan document.
- v0.2 (2025-11-07): E2E tests expanded; maintenance ISO build scripted; serial kargs for installed system; installer serial deferred.
