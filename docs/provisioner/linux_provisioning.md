# Linux Provisioning Workflow

This document describes the end-to-end Linux provisioning workflow implemented in Shoal, following the design specifications in `design/029_Workflow_Linux.md`.

## Overview

The Linux provisioning workflow installs a traditional, mutable Linux system to a target disk using:

- **GPT partitioning** with flexible declarative layouts
- **OCI-hosted rootfs artifacts** for the operating system image
- **UEFI GRUB bootloader** with automatic configuration
- **Cloud-init NoCloud Config Drive** for initial system configuration

All operations are orchestrated through systemd and Quadlet, ensuring robust error handling, clear observability, and safe idempotency.

## Workflow Architecture

### Master Target: `install-linux.target`

The workflow is driven by a systemd target that coordinates the following ordered steps:

1. **prepare-mounts** - Create mount points (`/mnt/new-root`, `/mnt/efi`, `/mnt/cidata`)
2. **partition** - Create GPT partition table and filesystems
3. **image-linux** - Extract rootfs from OCI artifact
4. **bootloader-linux** - Install GRUB and generate `/etc/fstab`
5. **config-drive** - Create cloud-init CIDATA partition (optional)
6. **provision-success** - Send success webhook to controller

On failure at any step, `provision-failed@<step>.service` sends a webhook with precise attribution.

### Systemd Unit Dependencies

```
install-linux.target
├── prepare-mounts.service (creates mount points)
├── partition.service → partition-tool.container
├── image-linux.service → image-linux-tool.container  (After partition)
├── bootloader-linux.service → bootloader-linux-tool.container (After image-linux)
├── config-drive.service → config-drive-tool.container (After bootloader-linux)
├── OnSuccess → provision-success.service
└── OnFailure → provision-failed@%n.service
```

## Input Files

All inputs are written by the dispatcher to `/run/provision/`:

### recipe.env

Environment variables consumed by all workflow steps:

```bash
TASK_TARGET=install-linux.target
TARGET_DISK=/dev/nvme0n1
OCI_URL=controller.internal:8080/os-images/ubuntu-rootfs:22.04
SERIAL_NUMBER=XF-12345ABC
WEBHOOK_URL=http://controller.internal:8080
WEBHOOK_SECRET=<redacted>
PARTITION_APPLY=1
IMAGE_APPLY=1
BOOTLOADER_APPLY=1
```

### layout.json

Partition layout specification (see [Recipe Schema](../../design/022_Recipe_Schema_and_Validation.md)):

```json
[
  {
    "size": "512M",
    "type_guid": "ef00",
    "format": "vfat",
    "label": "ESP"
  },
  {
    "size": "100%",
    "type_guid": "8300",
    "format": "ext4",
    "label": "rootfs"
  },
  {
    "size": "16M",
    "type_guid": "8300",
    "format": "vfat",
    "label": "cidata"
  }
]
```

### user-data (optional)

Cloud-init user-data for system configuration:

```yaml
#cloud-config
hostname: server01
ssh_pwauth: false
users:
  - name: admin
    groups: sudo
    shell: /bin/bash
    sudo: ALL=(ALL) NOPASSWD:ALL
    ssh_authorized_keys:
      - ssh-rsa AAAA...
```

## Workflow Steps

### 1. Partition

**Service:** `partition.service` → `partition-tool.container`

**Wrapper:** `/opt/shoal/bin/partition-wrapper.sh`

**Operations:**
- Reads `/run/provision/layout.json`
- Generates partition table using `sgdisk`
- Creates filesystems (`mkfs.vfat`, `mkfs.ext4`, `mkswap`, etc.)
- Sets partition labels

**Idempotency:** Compares current layout with desired state; skips operations if matching.

**Example Commands:**
```bash
sgdisk --zap-all /dev/nvme0n1
sgdisk --new=1:0:+512M --typecode=1:ef00 /dev/nvme0n1
mkfs.vfat -F 32 -n ESP /dev/nvme0n1p1
sgdisk --new=2:0:0 --typecode=2:8300 /dev/nvme0n1
mkfs.ext4 -L rootfs /dev/nvme0n1p2
```

### 2. Image Linux RootFS

**Service:** `image-linux.service` → `image-linux-tool.container`

**Wrapper:** `/opt/shoal/bin/image-linux-wrapper.sh`

**Operations:**
- Creates `/mnt/new-root` mount point
- Pulls OCI artifact using `oras` or `podman`
- Streams tarball extraction to root partition
- Preserves ownership and permissions

**Idempotency:** Optionally checks artifact digest stamp to avoid redundant extraction.

**Example Commands:**
```bash
mkdir -p /mnt/new-root
mount /dev/nvme0n1p2 /mnt/new-root
oras pull localhost:8080/os-images/ubuntu:22.04 --output - | tar -xpf - -C /mnt/new-root
echo "<digest>" > /mnt/new-root/.provisioner_artifact_digest
```

### 3. Bootloader Installation

**Service:** `bootloader-linux.service` → `bootloader-linux-tool.container`

**Wrapper:** `/opt/shoal/bin/bootloader-linux-wrapper.sh`

**Operations:**
- Mounts ESP at `/mnt/efi`
- Discovers partition UUIDs using `blkid`
- Generates `/etc/fstab` with correct UUIDs
- Installs GRUB to ESP with chroot
- Creates GRUB configuration

**Idempotency:** Safe to re-run; overwrites GRUB configuration consistently.

**Example Commands:**
```bash
mount /dev/nvme0n1p1 /mnt/efi
mount --bind /dev /mnt/new-root/dev
mount --bind /proc /mnt/new-root/proc
mount --bind /sys /mnt/new-root/sys
chroot /mnt/new-root grub-install --target=x86_64-efi --efi-directory=/boot/efi
chroot /mnt/new-root grub-mkconfig -o /boot/grub/grub.cfg
# Generate /etc/fstab
cat <<EOF > /mnt/new-root/etc/fstab
UUID=<root-uuid>  /          ext4  defaults  0 1
UUID=<esp-uuid>   /boot/efi  vfat  umask=0077 0 2
EOF
```

### 4. Config Drive (Cloud-init)

**Service:** `config-drive.service` → `config-drive-tool.container`

**Wrapper:** `/opt/shoal/bin/config-drive-wrapper.sh`

**Operations:**
- Mounts CIDATA partition at `/mnt/cidata`
- Copies `user-data` from `/run/provision/`
- Generates `meta-data` with instance-id and hostname
- Ensures cloud-init NoCloud format compatibility

**Idempotency:** Compares file hashes; skips write if unchanged.

**Example Commands:**
```bash
mount /dev/nvme0n1p3 /mnt/cidata
cp /run/provision/user-data /mnt/cidata/user-data
cat <<EOF > /mnt/cidata/meta-data
instance-id: XF-12345ABC
local-hostname: server01
EOF
umount /mnt/cidata
```

## Error Handling and Webhooks

### Success Path

When all steps complete successfully:

1. `install-linux.target` reaches `OnSuccess` state
2. `provision-success.service` executes
3. Webhook payload sent to controller:

```json
{
  "status": "success",
  "serial_number": "XF-12345ABC"
}
```

### Failure Path

When any step fails:

1. Failed unit triggers `provision-failed@<unit>.service`
2. Templated service captures exact failing unit name (`%i`)
3. Webhook payload sent to controller:

```json
{
  "status": "failed",
  "failed_step": "partition.service",
  "serial_number": "XF-12345ABC"
}
```

Controller can map `failed_step` to specific error category:
- `partition.service` → `workflow.partition`
- `image-linux.service` → `workflow.image-linux`
- `bootloader-linux.service` → `workflow.bootloader-linux`
- `config-drive.service` → `workflow.config-drive`

## Observability

### Logging

All wrapper scripts log to journald with structured prefixes:

```bash
journalctl -u partition.service
journalctl -u image-linux.service
journalctl -u bootloader-linux.service
journalctl -u config-drive.service
```

### Standard Log Events

- **Start/Finish markers** for each step
- **Device and partition identifiers**
- **Artifact URLs and digests** (sizes only, no sensitive data)
- **UUID assignments** for fstab generation
- **Idempotent skip notifications** when state matches

### Metrics (Future)

- Step duration
- Bytes extracted
- Retry counts
- Idempotent skip rate

## Testing

### Unit Tests

Individual planner tests validate command generation:

```bash
go test ./internal/provisioner/maintenance/partition
go test ./internal/provisioner/maintenance/image
go test ./internal/provisioner/maintenance/bootloader
go test ./internal/provisioner/maintenance/configdrive
```

### Integration Tests

End-to-end workflow validation:

```bash
go test ./test/integration -run TestLinuxWorkflow
```

Tests verify:
- Partition plan generates correct `sgdisk` commands
- Image plan uses `oras` and `tar` correctly
- Bootloader plan creates fstab and installs GRUB
- Config drive creates proper NoCloud structure

### Idempotency Tests

```bash
go test ./test/integration -run TestLinuxWorkflowIdempotency
```

Validates:
- Re-running steps produces identical or safely convergent results
- No destructive operations when state matches desired config

## Performance Considerations

### Timeouts

- **partition.service**: 45 minutes (allows slow disks)
- **image-linux.service**: 90 minutes (large rootfs artifacts)
- **bootloader-linux.service**: 20 minutes
- **config-drive.service**: 10 minutes

### Optimization

- **Streaming:** `oras → tar` pipeline avoids intermediate files
- **Parallel extraction:** Use `pigz` for gzip if available
- **Minimal fsyncs:** Avoid unnecessary sync operations

## Security

### Container Privileges

Containers run with:
- `AddDevice=/dev:/dev:rwm` (block device access)
- `AddCapability=ALL` (can be tightened per tool)
- Read-only `/run/provision` mount

### Secret Handling

- **WEBHOOK_SECRET** never logged
- **user-data** logged by size/hash only
- Redaction enforced in wrapper scripts

### Mount Security

- Task ISO mounted read-only with `nosuid,nodev,noexec`
- Bind mounts cleaned up after bootloader install

## Troubleshooting

### Partition Step Failures

**Symptom:** `partition.service` fails

**Common Causes:**
- Invalid `layout.json` format
- Disk already in use (mounted partitions)
- Insufficient disk space

**Debug:**
```bash
journalctl -u partition.service
cat /run/provision/layout.json
lsblk /dev/nvme0n1
```

### Image Step Failures

**Symptom:** `image-linux.service` fails

**Common Causes:**
- OCI URL unreachable
- Artifact digest mismatch
- Insufficient space on root partition

**Debug:**
```bash
journalctl -u image-linux.service
oras pull $OCI_URL --dry-run
df -h /mnt/new-root
```

### Bootloader Step Failures

**Symptom:** `bootloader-linux.service` fails

**Common Causes:**
- Missing EFI partition
- Incorrect device auto-discovery
- GRUB packages missing in rootfs

**Debug:**
```bash
journalctl -u bootloader-linux.service
blkid | grep -i efi
ls -la /mnt/new-root/boot/efi
```

### Config Drive Step Failures

**Symptom:** `config-drive.service` fails

**Common Causes:**
- Missing CIDATA partition
- Invalid `user-data` format
- Partition not formatted as vfat

**Debug:**
```bash
journalctl -u config-drive.service
blkid | grep -i cidata
file -s /dev/nvme0n1p3
```

## Example Recipe

Complete recipe for minimal Ubuntu installation:

```json
{
  "$schema": "./recipe.schema.json",
  "task_target": "install-linux.target",
  "target_disk": "/dev/nvme0n1",
  "oci_url": "controller.internal:8080/os-images/ubuntu-rootfs:22.04",
  "user_data": "#cloud-config\nhostname: server01\nssh_pwauth: false\n",
  "partition_layout": [
    { "size": "512M", "type_guid": "ef00", "format": "vfat", "label": "ESP" },
    { "size": "100%", "type_guid": "8300", "format": "ext4", "label": "rootfs" },
    { "size": "16M", "type_guid": "8300", "format": "vfat", "label": "cidata" }
  ]
}
```

## Acceptance Criteria

Per `design/029_Workflow_Linux.md`, the implementation is accepted when:

- ✅ End-to-end provisioning completes: system boots to Linux with cloud-init applied
- ✅ Correctness: `/etc/fstab` references correct UUIDs; ESP mounted at `/boot/efi`
- ✅ Idempotency: Re-running workflow completes without reformatting if state matches
- ✅ Reliability: Failure triggers precise webhook with unit name
- ✅ Observability: Logs show clear start/end and device identifiers; no sensitive data leaked

## References

- [design/029_Workflow_Linux.md](../../design/029_Workflow_Linux.md) - Complete workflow specification
- [design/026_Systemd_and_Quadlet_Orchestration.md](../../design/026_Systemd_and_Quadlet_Orchestration.md) - Systemd unit patterns
- [design/025_Dispatcher_Go_Binary.md](../../design/025_Dispatcher_Go_Binary.md) - Dispatcher behavior
- [design/022_Recipe_Schema_and_Validation.md](../../design/022_Recipe_Schema_and_Validation.md) - Recipe format

## Change Log

- v0.1 (2025-11-09): Initial Linux workflow documentation for Phase 6 Milestone 2
