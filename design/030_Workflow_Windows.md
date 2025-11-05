# 030: Workflow — Windows Server Install

Status: Proposed
Owners: Provisioning Working Group
Last updated: 2025-11-05

Summary

This document defines the end-to-end Windows provisioning workflow performed by the maintenance OS using systemd and Quadlet. It covers GPT partitioning (EFI/MSR/NTFS), WIM imaging (wimlib), UEFI boot configuration, and unattend.xml placement. It specifies inputs, outputs, idempotency, error handling, and acceptance criteria so the implementation can be validated and operated reliably.

Related

- 020_Provisioner_Architecture.md (index and overview)
- 021_Provisioner_Controller_Service.md (API, state machine, storage)
- 022_Recipe_Schema_and_Validation.md (recipe contract and limits)
- 023_Task_ISO_Builder.md (deterministic task.iso generation)
- 024_Maintenance_OS_Build_with_bootc.md (maintenance OS)
- 025_Dispatcher_Go_Binary.md (on-host entrypoint)
- 026_Systemd_and_Quadlet_Orchestration.md (targets and unit graph)
- 027_Embedded_OCI_Registry.md (optional /v2/ handler for artifacts)
- 028_Redfish_Operations.md (BMC flow)
- 032_Error_Handling_and_Webhooks.md (error taxonomy and webhooks)

1. Overview and Goals

- Provision a Windows Server installation by applying a WIM image to a target NTFS partition and setting up a UEFI boot path.
- Support the canonical GPT layout: EFI System Partition (FAT32), Microsoft Reserved (MSR), and primary Windows NTFS partition.
- Place an unattend.xml into the Windows image to drive unattended first boot.
- Ensure idempotency (safe re-runs), robust failure attribution, and clear observability.

Non-Goals

- BIOS/Legacy boot (UEFI-first).
- Domain join, drivers, or advanced post-install customizations beyond what unattend.xml provides.
- Running Windows-native tools on the host; all steps run from the maintenance OS via containerized tools.

2. Inputs and Outputs

2.1 Inputs (from Dispatcher)

- /run/provision/recipe.env:
  - TASK_TARGET=install-windows.target
  - TARGET_DISK=/dev/sda or /dev/nvme0n1
  - OCI_URL=controller:8080/os-images/windows-wim:2022
  - SERIAL_NUMBER=XF-12345ABC
  - Optional: WEBHOOK_URL, WEBHOOK_SECRET
- /run/provision/layout.json (partition schema; see §3)
- /run/provision/unattend.xml (Windows answer file; required by schema in 022 for Windows)
- From registry (if not pre-bound):
  - OCI artifact at $OCI_URL (Windows install WIM; specific index chosen by wrapper, default index=1)

2.2 Outputs

- Target disk partitioned: EFI (vfat), MSR (no fs), primary NTFS for Windows.
- Windows image applied to the NTFS partition (mounted at /mnt/new-windows).
- UEFI boot files copied to the EFI partition (mounted at /mnt/efi) and a firmware boot entry created.
- unattend.xml placed at /mnt/new-windows/Windows/Panther/Unattend.xml.
- Success/failure webhook emitted by provision-success.service / provision-failed@.service.

3. Partitioning Model

3.1 Schema-to-Device Mapping

- The partition layout is an ordered list; typical Windows layout:
  - p1: EFI System Partition — size: 300–512M, type_guid: ef00, format: vfat, label: "EFI"
  - p2: Microsoft Reserved Partition (MSR) — size: 16M–128M, type_guid: 0c01 (MSR), format: raw, label optional
  - p3: Windows — size: remaining (e.g., 100%), type_guid: 0700 (Basic data), format: ntfs, label: "Windows"
- Device naming:
  - SATA/SAS/virtio: /dev/sdX1, /dev/sdX2, /dev/sdX3
  - NVMe: /dev/nvme0n1p1, /dev/nvme0n1p2, /dev/nvme0n1p3
- The partition tool must compute the correct suffix and alignment.

3.2 Implementation Notes

- EFI: format FAT32 (mkfs.vfat); label optional but recommended.
- MSR: no filesystem; create the partition only (no mkfs).
- Windows: format NTFS (mkfs.ntfs -f); label "Windows" (optional).
- Create mount points:
  - /mnt/efi for the EFI partition
  - /mnt/new-windows for the NTFS partition

3.3 Idempotency

- If GPT layout already matches desired types and number/order (within tolerance) and filesystems exist with expected types, skip destructive changes.
- If differences are detected:
  - Use sgdisk to recreate table and partitions; reformat filesystems as needed (never format MSR).
- Ensure labels and UUIDs reflect current state and log any changes.

4. Imaging (WIM Apply)

4.1 WIM Artifact Expectations

- OCI reference (oras-compatible) pointing to an install WIM for the Windows edition intended.
- Media type can be a custom artifact type (e.g., application/vnd.my-org.install.wim).
- The wrapper defaults to applying index 1; optionally support index selection via recipe metadata in future.

4.2 Apply Flow

- Ensure NTFS partition mounted at /mnt/new-windows (via ntfs-3g).
- Pull the WIM via oras and pipe to wimapply (wimlib) targeting /mnt/new-windows:
  - oras pull "${OCI_URL}" --output - | wimapply - /mnt/new-windows --index=1
- Preserve timestamps and attributes as far as wimlib supports.
- Record a stamp file under /mnt/new-windows (e.g., .provisioner_wim_digest) with the artifact digest for idempotency.

4.3 Idempotency

- If a stamp file matches the artifact digest, skip reapplying the image (configurable).
- If forced or digest differs, reapply the image after confirming adequate free space and mount state.

5. UEFI Boot Configuration

5.1 Strategy

- Copy Windows UEFI boot files from the applied image into the EFI partition and create a firmware boot entry pointing to Microsoft Boot Manager.
- This avoids needing Windows-native tools (e.g., bcdboot) during imaging.

5.2 Steps

- Mount EFI partition at /mnt/efi (ensure FAT32).
- Create directory structure on ESP:
  - /mnt/efi/EFI/Microsoft/Boot
  - /mnt/efi/EFI/Boot
- Copy files from the applied image:
  - From /mnt/new-windows/Windows/Boot/EFI to /mnt/efi/EFI/Microsoft/Boot (preserve directory structure and file attributes).
  - Copy or symlink bootmgfw.efi to the default fallback path:
    - /mnt/efi/EFI/Boot/bootx64.efi (for x86_64 UEFI)
- Create or update firmware boot entry via efibootmgr:
  - efibootmgr -c -d "$TARGET_DISK" -p <efi_partnum> -L "Windows Boot Manager" -l "\EFI\Microsoft\Boot\bootmgfw.efi"
- Verify boot order and ensure the new entry is prioritized for next boot if required.

5.3 Notes and Caveats

- BCD store: The copied files include a default BCD which Windows will finalize during first boot; this approach is widely functional but can vary by WIM content.
- Secure Boot: Out of scope; if enabled, ensure boot chain uses signed bootmgfw.efi compatible with platform policy.
- Architecture:
  - Assumes x86_64 (AMD64). For ARM64 or other architectures, adjust EFI paths accordingly.

6. Unattend.xml Placement

6.1 Path and Naming

- Place unattend.xml at: /mnt/new-windows/Windows/Panther/Unattend.xml (create directories if missing).
- Ensure the file is encoded in UTF-8 or UTF-16LE as required by Windows setup; wrapper can preserve content as-is.

6.2 Content and Security

- The unattend file may contain secrets (product keys, local admin password hashes). Do not log the content. Log only size and a non-reversible hash (e.g., SHA-256).

6.3 Idempotency

- If an identical Unattend.xml already exists (same hash), skip overwrite.
- If different, overwrite with the provided unattend.xml.

7. Unit Graph, Ordering, and Timeouts

- Master target: install-windows.target
  - Requires (in order): partition.service → image-windows.service → bootloader-windows.service
  - OnSuccess: provision-success.service
  - OnFailure: provision-failed@%n.service
- Suggested timeouts:
  - partition.service: 45m
  - image-windows.service: 120m (WIMs may be large)
  - bootloader-windows.service: 20m

8. Error Handling and Attribution

- Step failures map to:
  - workflow.partition
  - workflow.image-windows
  - workflow.bootloader-windows
- The provision-failed@<unit>.service webhook includes failed_step for precision.
- The controller transitions job to failed and performs cleanup (see 032).

9. Idempotency and Recovery

- All steps are safe to re-run:
  - Partition: compare GPT layout; only rebuild if mismatched. Never format MSR.
  - Imaging: use digest stamp; skip if already applied; otherwise reapply cleanly.
  - Bootloader: copy files idempotently; ensure efibootmgr entry exists (update or create).
  - Unattend: overwrite only if changed.
- On controller or maintenance OS restart mid-workflow, systemd dependencies ensure resumption without corruption.

10. Observability

- Wrapper logs:
  - Start/finish, selected device/partitions, filesystem UUIDs, WIM digest/index, efibootmgr results.
  - Never log unattend contents; only log size and hash.
- Metrics (future):
  - Imaging duration, bytes written, step durations, idempotent skips.

11. Performance Considerations

- WIMs can be multi-GB:
  - Use streaming pipeline (oras → wimapply) to avoid large temporary files.
  - Ensure sufficient I/O throughput and raise timeouts accordingly.
- NTFS mount:
  - Use ntfs-3g with appropriate mount options for reliability.
- CPU:
  - wimlib is CPU-bound for decompression; consider container image with optimized libraries.

12. Security Considerations

- Containers run privileged; minimize capabilities as we gain experience.
- Keep mount sets minimal; /run/provision should be read-only when possible.
- Secrets in unattend.xml must never be logged. Consider encrypting at rest in controller storage; the maintenance OS only receives the plaintext in task.iso when necessary.

13. Testing Strategy

13.1 Unit (in-container wrappers)

- Partition planner tests: schema → sgdisk argument mapping for EFI/MSR/NTFS.
- Imaging: mock oras stream; verify wimapply invocation and extraction to target mount.
- Bootloader: validate copy plan, efibootmgr invocation arguments, and fallback file placement.
- Unattend: validate path creation and content hashing logic.

13.2 Integration (VM)

- Attach maintenance.iso (CD1) + task.iso (CD2); run install-windows.target.
- Verify:
  - GPT partitions: ef00, 0c01, 0700 with expected sizes.
  - Filesystems: vfat on EFI; NTFS on Windows; MSR raw.
  - WIM applied: presence of Windows directory tree on /mnt/new-windows.
  - EFI: files under /EFI/Microsoft/Boot and fallback /EFI/Boot/bootx64.efi.
  - efibootmgr lists "Windows Boot Manager" entry pointing to correct path.
  - Unattend placed at Windows/Panther/Unattend.xml.
- Re-run workflow (idempotency) → fast completion; no destructive changes.

13.3 Negative Cases

- Missing or invalid layout.json → partition.service fails with clear error.
- OCI_URL unreachable → image-windows.service fails and reports.
- No EFI partition present → bootloader-windows.service fails with actionable message.

14. Acceptance Criteria

- End-to-end provisioning:
  - Given a valid recipe with EFI/MSR/NTFS layout and a Windows WIM artifact, the machine boots to Windows and unattended setup runs using Unattend.xml.
- Correctness:
  - Firmware boot entry exists and points to \EFI\Microsoft\Boot\bootmgfw.efi; fallback bootx64.efi present.
  - Unattend.xml is placed at Windows/Panther/Unattend.xml.
- Idempotency:
  - Re-running install-windows.target completes without reformatting/reapplying if state matches (or performs only necessary adjustments).
- Reliability:
  - Failure in any step triggers provision-failed webhook with the precise unit name.
- Observability:
  - Logs show clear start/end and device/artifact identifiers; unattend content is never logged.

15. Example Recipe (Windows)

- task_target: install-windows.target
- target_disk: /dev/nvme0n1
- oci_url: controller.internal:8080/os-images/windows-wim:2022
- unattend_xml: "<unattend>...</unattend>"
- partition_layout:
  - { size: "300M", type_guid: "ef00", format: "vfat", label: "EFI" }
  - { size: "16M", type_guid: "0c01", format: "raw",  label: "MSR" }
  - { size: "100%", type_guid: "0700", format: "ntfs", label: "Windows" }

16. Open Questions

- WIM index selection: Add optional field (e.g., wim_index) to the recipe; default to 1 if omitted.
- BCD robustness: In rare cases, bcdboot may be preferred; consider a Windows PE–based bootloader container (future work) vs. current copy + efibootmgr approach.
- Secure Boot: Define a separate design if required (shim, signed bootmgfw.efi, policy).

Change Log

- v0.1 (2025-11-05): Initial Windows workflow covering EFI/MSR/NTFS, WIM apply, UEFI boot configuration, and unattend placement.