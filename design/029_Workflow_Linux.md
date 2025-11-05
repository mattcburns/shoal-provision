# 029: Workflow — Traditional Linux Install

Status: Proposed
Owners: Provisioning Working Group
Last updated: 2025-11-05

Summary

This document defines the end-to-end Linux provisioning workflow performed by the maintenance OS using systemd and Quadlet. It details the partitioning model, filesystem creation, rootfs imaging, bootloader installation for UEFI systems, and optional cloud-init Config Drive creation. It specifies inputs, outputs, idempotency guarantees, error handling, and acceptance criteria so the implementation can be validated and operated reliably.

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

- Provision a traditional, mutable Linux system to a target disk using an OCI-hosted rootfs tarball.
- Support flexible GPT partition layouts via a declarative schema.
- Install a UEFI GRUB bootloader and generate a correct fstab.
- Optionally create a cloud-init Config Drive (CIDATA) partition with user-data.
- Ensure idempotency (safe re-runs), robust failure attribution, and clear observability.

Non-Goals

- BIOS/Legacy boot support (UEFI-first; BIOS may be added later).
- Distro-specific post-install customization beyond rootfs extraction and bootloader install (cloud-init handles customization).
- Secure Boot enablement (requires signed shim; out of scope here).

2. Inputs and Outputs

2.1 Inputs (from Dispatcher)

- /run/provision/recipe.env (ENV):
  - TASK_TARGET=install-linux.target
  - TARGET_DISK=/dev/sda or /dev/nvme0n1
  - OCI_URL=controller:8080/os-images/ubuntu-rootfs:22.04
  - SERIAL_NUMBER=XF-12345ABC
  - Optional: WEBHOOK_URL, WEBHOOK_SECRET
- /run/provision/layout.json (partition schema; see §3)
- /run/provision/user-data (optional; cloud-init NoCloud)
- From registry (if not pre-bound):
  - OCI artifact at $OCI_URL (rootfs tarball)

2.2 Outputs

- Target disk partitioned and formatted per layout.
- New root filesystem extracted at /mnt/new-root (temporary mount path).
- EFI System Partition (ESP) mounted at /mnt/efi during bootloader step.
- Installed bootloader (GRUB UEFI) and generated /mnt/new-root/etc/fstab.
- Optional CIDATA partition populated with user-data and meta-data.
- Success/failure webhook emitted by provision-success.service / provision-failed@.service.

3. Partitioning Model

3.1 Schema-to-Device Mapping

- The partition layout is an ordered list; each entry yields a new GPT partition on TARGET_DISK.
- Supported fields (see 022):
  - size: "512M", "1G", "100%"
  - type_guid: sgdisk alias (ef00, 8300, 8200, 0700, 0c01, fd00, …) or full GUID
  - format: vfat | ext4 | xfs | btrfs | ntfs | swap | raw
  - label: optional filesystem label (<= 32 chars)
- Device naming:
  - SATA/SAS/virtio: /dev/sdX1, /dev/vdX1
  - NVMe: /dev/nvme0n1p1
  - The partition tool must compute the correct suffix.

3.2 Typical Layout Examples

- Minimal UEFI Linux:
  - p1: 512M, ef00, vfat, label "EFI"
  - p2: 100%, 8300, ext4, label "root"
- With swap:
  - p1: 512M, ef00, vfat
  - p2: 32G, 8200, swap
  - p3: 100%, 8300, ext4
- With CIDATA:
  - Add a small vfat partition (e.g., 16M) labeled "cidata" as the last partition.

3.3 Idempotency

- The partition container should:
  - Read current layout (sgdisk -p, blkid).
  - If partitions already match desired GUID types and sizes (within tolerance), skip destructive ops.
  - Otherwise:
    - wipefs -a TARGET_DISK (if permitted) or sgdisk --zap-all, then recreate GPT and partitions.
  - Create filesystems only if missing or mismatched.
  - Labels are enforced (relabel if necessary).

3.4 Example Partition Wrapper Behavior (Pseudo)

```/dev/null/partition-wrapper.sh#L1-40
#!/usr/bin/env bash
set -euo pipefail
disk="${TARGET_DISK:?missing}"
layout="/run/provision/layout.json"

# 1. Validate input and detect current table
# 2. If mismatch, sgdisk --zap-all "$disk" && sgdisk create partitions
# 3. For each partition with "format", mkfs accordingly (mkfs.vfat, mkfs.ext4, mkswap)
# 4. Set filesystem labels if provided (fatlabel, e2label, swaplabel)
# 5. Print resulting table and blkid info
```

4. Imaging (RootFS Extraction)

4.1 RootFS Artifact Expectations

- OCI reference (oras/podman compatible) pointing to a tarball of a Linux root filesystem:
  - Tar layout: top-level directories relative to / (e.g., etc/, var/, usr/, …)
  - Recommended media type: application/vnd.my-org.rootfs.tar.gz
  - Must contain an /etc/os-release with a recognizable distro for tooling expectations.

4.2 Extraction Flow

- image-linux.container wrapper should:
  - Create /mnt/new-root (if missing).
  - Pull OCI_URL with oras; stream to tar:
    - oras pull $OCI_URL --output - | tar -xpf - -C /mnt/new-root
  - Ensure ownership/permissions preserved (-p).
  - Optional: write a stamp file with artifact digest for idempotency.

```/dev/null/image-linux-wrapper.sh#L1-40
#!/usr/bin/env bash
set -euo pipefail
: "${OCI_URL:?missing}"
root="/mnt/new-root"
mkdir -p "$root"

# Pull and extract
oras pull "${OCI_URL}" --output - | tar -xpf - -C "${root}"

# Record digest if available (oras can print digest; capture and write to stamp)
# echo "${DIGEST}" > "${root}/.provisioner_artifact_digest"
```

4.3 Mount Preparation for Bootloader Step

- After extraction, the bootloader step will require:
  - /mnt/new-root mounted
  - EFI partition mounted at /mnt/efi
  - Bind mounts during chroot (proc, sys, dev, run)

5. Bootloader Installation (UEFI GRUB)

5.1 Assumptions

- UEFI firmware with EFI System Partition (type ef00, vfat).
- GRUB2 packages available in the bootloader container image.

5.2 Steps

- bootloader-linux.container wrapper should:
  - Identify partitions and their PARTUUID/UUID via blkid.
  - Mount the ESP at /mnt/efi (e.g., /dev/sdX1 → /mnt/efi).
  - Bind mount /dev, /proc, /sys, /run into /mnt/new-root.
  - chroot into /mnt/new-root and:
    - Ensure grub2-efi packages installed (if distro requires; otherwise the container may provide tooling and copy assets accordingly).
    - grub2-install --target=x86_64-efi --efi-directory=/efi --bootloader-id="Linux" --removable (or without --removable where Shim/Secure Boot is configured)
    - grub2-mkconfig -o /boot/grub2/grub.cfg (path may vary by distro)
  - Create /etc/fstab with correct UUIDs (see §5.3).
  - Unmount bind mounts and ESP.

5.3 fstab Generation

- Compute UUIDs for:
  - Root filesystem (e.g., UUID=<uuid> / ext4 defaults 0 1)
  - EFI (UUID=<uuid> /boot/efi vfat umask=0077 0 2)
  - Swap if present (UUID=<uuid> none swap sw 0 0)
- Write to /mnt/new-root/etc/fstab; overwrite if existing but mismatched.

```/dev/null/fstab.example#L1-10
UUID=aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee /         ext4 defaults        0 1
UUID=ffffffff-1111-2222-3333-444444444444 /boot/efi vfat umask=0077     0 2
UUID=99999999-8888-7777-6666-555555555555 none      swap sw              0 0
```

5.4 Idempotency

- Re-running should:
  - Recreate fstab only if contents differ.
  - Reinstall GRUB safely; if already installed, commands should be no-ops or overwrite consistently.
  - Cleanly handle bind mount re-entry and unmounting.

6. Config Drive (cloud-init NoCloud)

6.1 Purpose

- Provide initial configuration (hostname, SSH keys, users, network) to the Linux OS via cloud-init using a local “NoCloud” data source.

6.2 Partition Choice

- Create a small vfat partition labeled "cidata" (per recipe layout) and mount at /mnt/cidata during creation.

6.3 Files and Structure

- Required files:
  - user-data (from /run/provision/user-data; YAML or script)
  - meta-data (generated): at minimum instance-id and local-hostname
- Recommended meta-data content:

```/dev/null/meta-data#L1-5
instance-id: ${SERIAL_NUMBER}
local-hostname: linux-host
```

- The config-drive container should:
  - Mount the CIDATA partition, create cloud-init directory structure at the root (NoCloud accepts files at root for disks).
  - Copy user-data and meta-data; sync and unmount.

6.4 Idempotency

- If the partition exists and files match (hash), no-op.
- If label or filesystem type are wrong, reformat (if policy allows).

6.5 Security

- Do not log user-data contents. Log sizes/hashes only.

7. Unit Graph, Ordering, and Timeouts

- Master target: install-linux.target
  - Requires (in order): partition.service → image-linux.service → bootloader-linux.service → config-drive.service
  - OnSuccess: provision-success.service
  - OnFailure: provision-failed@%n.service
- Suggested timeouts (adjust per environment):
  - partition.service: 45m
  - image-linux.service: 90m
  - bootloader-linux.service: 20m
  - config-drive.service: 10m

8. Error Handling and Attribution

- Step failures map to:
  - workflow.partition
  - workflow.image-linux
  - workflow.bootloader-linux
  - workflow.config-drive
- On failure, provision-failed@<unit>.service sends a webhook payload including failed_step.
- Controller transitions job to failed and performs cleanup (see 032).

9. Idempotency and Recovery

- All steps must tolerate re-run:
  - Partition: skip if matching; otherwise rebuild safely.
  - Imaging: artifact digest stamp prevents redundant extraction (optional).
  - Bootloader: reinstall is safe; ensure chroot mounts handled robustly.
  - Config-drive: overwrite only if contents changed.
- On controller or maintenance OS restart during workflow:
  - systemd dependencies ensure steps re-run or continue to completion without corruption.

10. Observability

- Wrapper scripts log:
  - Start/finish markers, key actions, detected devices, UUIDs, artifact digest.
  - Do not log sensitive user-data.
- Metrics (future):
  - Step duration, bytes extracted, retries, idempotent skips.
- Artifacts:
  - Optionally write /mnt/new-root/.provisioner_info with artifact tag/digest and timestamp.

11. Performance Considerations

- Use pipeline streaming (oras → tar) to avoid large intermediate files.
- Consider enabling xz/gzip parallel extraction in the container image (pigz) if CPU allows.
- For slow disks:
  - Increase timeouts; prefer sequential ops; avoid unnecessary fsyncs.
- For large images:
  - Ensure sufficient space on target and avoid tmp usage.

12. Security Considerations

- Containers run privileged; minimize capabilities as experience grows.
- Mount only necessary host paths; prefer read-only for /run/provision.
- Redact secrets in logs; do not echo user-data.
- Secure Boot not covered; requires signed boot chain.

13. Testing Strategy

13.1 Unit (in-container wrappers)

- Partition planner tests: schema → sgdisk argument generation.
- Imaging: mock oras output; verify tar extraction flags.
- Bootloader: dry-run mode to validate fstab content and mount orchestration.

13.2 Integration (VM)

- Attach maintenance.iso (CD1) + task.iso (CD2); run install-linux.target.
- Verify:
  - Correct partitions via sgdisk -p
  - Filesystems exist and labels set
  - Rootfs extracted
  - fstab correctness; GRUB present in ESP
  - CIDATA content matches provided user-data
- Re-run workflow (idempotency) → no destructive changes; fast completion.

13.3 Negative Cases

- Missing or invalid layout.json → partition.service fails with clear message.
- OCI_URL unreachable → image-linux.service fails and reports.
- No EFI partition → bootloader-linux.service fails with actionable error.

14. Acceptance Criteria

- End-to-end provisioning:
  - Given a valid recipe with minimal UEFI layout and rootfs artifact, system boots to Linux, hostname and SSH keys are applied via cloud-init (if provided).
- Correctness:
  - /etc/fstab references correct UUIDs; ESP is mounted at /boot/efi at first boot.
- Idempotency:
  - Re-running install-linux.target completes without reformatting/reimaging if state matches (or performs only necessary adjustments).
- Reliability:
  - Failure in any step triggers provision-failed webhook with the precise unit name.
- Observability:
  - Logs show clear start/end and device/artifact identifiers; no sensitive data leaked.

15. Example Recipe (Linux)

```/dev/null/recipe.json#L1-30
{
  "$schema": "./recipe.schema.json",
  "task_target": "install-linux.target",
  "target_disk": "/dev/nvme0n1",
  "oci_url": "controller.internal:8080/os-images/ubuntu-rootfs:22.04",
  "user_data": "#cloud-config\nhostname: server01\nssh_pwauth: false\nchpasswd:\n  expire: false\n  list: |\n    root:!locked\n",
  "partition_layout": [
    { "size": "512M", "type_guid": "ef00", "format": "vfat", "label": "EFI" },
    { "size": "100%", "type_guid": "8300", "format": "ext4", "label": "root" },
    { "size": "16M", "type_guid": "8300", "format": "vfat", "label": "cidata" }
  ]
}
```

16. Open Questions

- Should we support BIOS boot (grub2-bios) in the same workflow with conditional logic? (Future enhancement)
- Standardizing artifact structure across distros (e.g., ensuring dracut/initramfs hooks) vs. leaving to image producers.
- Secure Boot support via signed shim and vendor-specific requirements (separate design).

Change Log

- v0.1 (2025-11-05): Initial Linux workflow covering partitioning, imaging, bootloader, and config-drive.