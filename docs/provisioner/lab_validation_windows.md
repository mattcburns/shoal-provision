# Lab Validation: Windows Workflow (Phase 6 Milestone 3)

This document provides instructions for validating the Windows provisioning workflow in a lab environment.

## Prerequisites

### Hardware/VM Requirements
- UEFI-capable virtual machine or physical server
- At least 40GB disk space for Windows Server installation
- 4GB RAM minimum (8GB recommended)
- BMC/IPMI with Redfish support and virtual media capabilities

### Software Requirements
- Windows Server 2022 WIM image (install.wim)
- Valid unattend.xml file for automated setup
- Access to Shoal controller service
- OCI registry access (embedded or external)

### Network Access
- VM/Server can reach Shoal controller API
- Controller can reach BMC Redfish endpoint
- (Optional) Webhook endpoint to capture provisioning events

## Test Setup

### 1. Prepare Windows Artifacts

Upload the Windows Server WIM to the OCI registry:

```bash
# Example using oras CLI
oras push controller.internal:8080/os-images/windows-wim:2022 \
  --artifact-type application/vnd.shoal.windows.wim \
  install.wim:application/octet-stream
```

Verify the artifact is available:

```bash
oras manifest fetch controller.internal:8080/os-images/windows-wim:2022
```

### 2. Create Recipe

Create a Windows provisioning recipe JSON:

```json
{
  "schema_version": "1.0",
  "task_target": "install-windows.target",
  "target_disk": "/dev/sda",
  "oci_url": "controller.internal:8080/os-images/windows-wim:2022",
  "wim_index": 1,
  "partition_layout": [
    {
      "size": "512M",
      "type_guid": "ef00",
      "format": "vfat",
      "label": "EFI"
    },
    {
      "size": "16M",
      "type_guid": "0c01",
      "format": "raw",
      "label": "MSR"
    },
    {
      "size": "100%",
      "type_guid": "0700",
      "format": "ntfs",
      "label": "Windows"
    }
  ],
  "unattend_xml": {
    "content": "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<unattend xmlns=\"urn:schemas-microsoft-com:unattend\">\n  <settings pass=\"specialize\">\n    <component name=\"Microsoft-Windows-Shell-Setup\" processorArchitecture=\"amd64\" publicKeyToken=\"31bf3856ad364e35\" language=\"neutral\" versionScope=\"nonSxS\">\n      <ComputerName>SHOAL-WIN-01</ComputerName>\n    </component>\n  </settings>\n  <settings pass=\"oobeSystem\">\n    <component name=\"Microsoft-Windows-Shell-Setup\" processorArchitecture=\"amd64\" publicKeyToken=\"31bf3856ad364e35\" language=\"neutral\" versionScope=\"nonSxS\">\n      <OOBE>\n        <HideEULAPage>true</HideEULAPage>\n        <ProtectYourPC>3</ProtectYourPC>\n      </OOBE>\n      <UserAccounts>\n        <AdministratorPassword>\n          <Value>YourPasswordHere</Value>\n          <PlainText>true</PlainText>\n        </AdministratorPassword>\n      </UserAccounts>\n    </component>\n  </settings>\n</unattend>"
  },
  "webhook_url": "https://webhook.site/your-unique-id",
  "webhook_secret": "test-secret-123"
}
```

**Important**: Replace `YourPasswordHere` with a secure administrator password.

### 3. Register BMC

Register the target BMC with Shoal:

```bash
curl -X POST http://localhost:8080/api/v1/bmcs \
  -H "Content-Type: application/json" \
  -d '{
    "name": "test-windows-bmc",
    "address": "https://192.168.1.100",
    "username": "admin",
    "password": "admin123",
    "verify_ssl": false
  }'
```

### 4. Create Provisioning Job

Submit the Windows provisioning job:

```bash
curl -X POST http://localhost:8080/api/v1/provisioning/jobs \
  -H "Content-Type: application/json" \
  -d @windows-recipe.json
```

Note the returned job ID for status tracking.

## Validation Steps

### Phase 1: Job Initialization

1. **Verify job created**:
   ```bash
   curl http://localhost:8080/api/v1/provisioning/jobs/{job_id}
   ```
   Expected: `status: "pending"`

2. **Monitor controller logs**:
   ```bash
   journalctl -u shoal -f
   ```
   Expected: Task ISO generation, virtual media insert, boot override

3. **Check BMC virtual media**:
   - Verify maintenance.iso and task.iso are mounted
   - Verify one-time boot is set to CD/DVD

### Phase 2: Maintenance OS Boot

1. **Server boots into maintenance OS**:
   - Watch console for systemd boot messages
   - Look for "Shoal Provisioner Dispatcher" startup

2. **Check dispatcher execution**:
   Expected journal entries:
   ```
   provisioner-dispatcher.service: Starting Shoal Provisioner Dispatcher
   provisioner-dispatcher: validated recipe against schema
   provisioner-dispatcher: starting systemctl start install-windows.target
   ```

3. **Verify environment files created**:
   - `/run/provision/recipe.env` should contain `TASK_TARGET=install-windows.target`
   - `/run/provision/layout.json` should have EFI/MSR/NTFS layout
   - `/run/provision/unattend.xml` should exist

### Phase 3: Partition Step

1. **Monitor partition.service**:
   ```bash
   journalctl -u partition.service -f
   ```

2. **Expected behaviors**:
   - Creates GPT partition table on target disk
   - Creates 3 partitions: EFI (ef00), MSR (0c01), Windows (0700)
   - Formats EFI as vfat, Windows as NTFS
   - MSR remains raw (no filesystem)

3. **Validation**:
   ```bash
   # From maintenance OS console
   sgdisk -p /dev/sda
   lsblk -f /dev/sda
   ```
   Expected output:
   - `/dev/sda1`: vfat, label "EFI", ~512M
   - `/dev/sda2`: (no fs), label "MSR", 16M
   - `/dev/sda3`: ntfs, label "Windows", remaining space

### Phase 4: Image Windows (WIM Apply)

1. **Monitor image-windows.service**:
   ```bash
   journalctl -u image-windows.service -f
   ```

2. **Expected behaviors**:
   - Mounts NTFS partition to `/mnt/new-windows`
   - Fetches OCI manifest for WIM artifact
   - Computes digest and checks stamp file
   - **First run**: Streams WIM via `oras pull | wimapply`, writes digest stamp
   - **Subsequent run**: Skips if digest matches

3. **Key log messages**:
   ```
   image-windows-plan: applying WIM (index=1, digest=sha256:abc...)
   ```
   Or (on re-run):
   ```
   image-windows-plan: WIM digest unchanged (sha256:abc...), skipping apply
   ```

4. **Validation**:
   ```bash
   # Verify Windows directory structure
   ls -la /mnt/new-windows/Windows
   # Should see Boot/, System32/, etc.

   # Verify stamp file
   cat /mnt/new-windows/.provisioner_wim_digest
   # Should show only the raw sha256 hex digest (e.g., "abcdef1234...")
   ```

### Phase 5: Bootloader Windows

1. **Monitor bootloader-windows.service**:
   ```bash
   journalctl -u bootloader-windows.service -f
   ```

2. **Expected behaviors**:
   - Mounts ESP and Windows partitions
   - Copies Windows boot files from `Windows/Boot/EFI` to ESP
   - Creates fallback `bootx64.efi` in `/EFI/Boot/`
   - Creates/ensures UEFI boot entry via `efibootmgr`
   - Writes `unattend.xml` to `Windows/Panther/Unattend.xml` (with hash check)

3. **Key log messages**:
   ```
   bootloader-windows-plan: boot entry 'Windows' already exists; skipping creation
   bootloader-windows-plan: unattend.xml written (sha256: 1a2b3c...)
   ```
   Or (on re-run):
   ```
   bootloader-windows-plan: unattend.xml unchanged (sha256: 1a2b3c...), skipping write
   ```

4. **Validation**:
   ```bash
   # Check ESP contents
   ls -la /mnt/efi/EFI/Microsoft/Boot/
   ls -la /mnt/efi/EFI/Boot/bootx64.efi

   # Check boot entry
   efibootmgr | grep -i windows

   # Verify unattend.xml placement
   ls -la /mnt/new-windows/Windows/Panther/Unattend.xml
   # Should be mode 0600
   ```

### Phase 6: Workflow Completion

1. **Verify install-windows.target success**:
   ```bash
   systemctl status install-windows.target
   ```
   Expected: `Active: active`

2. **Check provision-success.service**:
   ```bash
   journalctl -u provision-success.service
   ```
   Expected: Webhook POST with `status: "completed"`

3. **Verify webhook delivery** (if configured):
   - Check webhook.site or your endpoint
   - Payload should include `job_id`, `status: "completed"`, `serial_number`

### Phase 7: First Boot into Windows

1. **Controller cleanup**:
   - Virtual media should be ejected
   - Boot override should be cleared
   - System should power cycle or reset

2. **Windows first boot**:
   - System boots from local disk (EFI → bootmgfw.efi)
   - Windows Setup runs using `unattend.xml`
   - Computer name should match recipe (e.g., `SHOAL-WIN-01`)
   - Administrator password set per unattend

3. **Login and verify**:
   - Log in as Administrator
   - Check computer name: `hostname`
   - Verify OS version: `winver`

## Idempotency Testing

To validate idempotent behavior, re-run the workflow without changing artifacts:

1. **Resubmit the same job** (or restart provisioner mid-flow).

2. **Expected skip messages**:
   ```
   image-windows-plan: WIM digest unchanged (...), skipping apply
   bootloader-windows-plan: boot entry 'Windows' already exists; skipping creation
   bootloader-windows-plan: unattend.xml unchanged (...), skipping write
   ```

3. **Timing comparison**:
   - Initial run: ~15-45 minutes (depending on WIM size)
   - Idempotent re-run: ~2-5 minutes (skips imaging)

## Error Scenarios

### Unreachable OCI URL

1. **Simulate**: Use invalid `oci_url` in recipe.
2. **Expected**: `image-windows.service` fails with clear error.
3. **Webhook**: `status: "failed"`, `failed_step: "workflow.image-windows"`

### Missing EFI Partition

1. **Simulate**: Provide layout without EFI partition (type_guid `ef00`).
2. **Expected**: `bootloader-windows.service` fails; cannot mount ESP.
3. **Logs**: Error message indicates ESP device not found.

### Unattend.xml Validation

1. **Simulate**: Provide malformed XML in `unattend_xml.content`.
2. **Expected**: Recipe validation rejects at job submission (before provisioning starts).
3. **Error**: `400 Bad Request` with validation details.

## Troubleshooting

### Service Fails with "mount: unknown filesystem type 'ntfs-3g'"

**Cause**: Maintenance OS image missing `ntfs-3g` package.

**Fix**: Rebuild maintenance OS with `ntfs-3g` included (see `design/024_Maintenance_OS_Build_with_bootc.md`).

### WIM Apply Times Out

**Cause**: Large WIM (e.g., >10GB) exceeds 90-minute timeout.

**Fix**: Increase `TimeoutSec` in `image-windows.service` or use a smaller/optimized WIM.

### Boot Entry Not Created

**Cause**: `efibootmgr` cannot determine ESP partition number.

**Fix**: Ensure ESP is properly labeled and partition table is GPT. Check `lsblk -no PARTN <esp_device>`.

### Unattend.xml Not Applied

**Symptom**: Windows boots but prompts for computer name/setup.

**Cause**: Unattend.xml not placed at correct path or malformed.

**Fix**:
- Verify file at `C:\Windows\Panther\Unattend.xml` on booted Windows.
- Check XML validity with `xmllint` or Windows Setup log (`C:\Windows\Panther\setupact.log`).

## Success Criteria

✅ Partition layout matches recipe (GPT, EFI/MSR/NTFS)  
✅ WIM applied to Windows partition  
✅ Digest stamp file exists at root of Windows partition (e.g., `C:\\.provisioner_wim_digest` or `/mnt/new-windows/.provisioner_wim_digest`)  
✅ Boot files copied to ESP (`/EFI/Microsoft/Boot/bootmgfw.efi`, `/EFI/Boot/bootx64.efi`)
✅ UEFI boot entry created (`efibootmgr` lists "Windows" entry)
✅ `Unattend.xml` present at `Windows/Panther/Unattend.xml` with mode `0600`
✅ Webhook delivered with `status: "completed"`
✅ System boots into Windows and completes unattended setup
✅ Re-run skips already-applied steps (idempotency confirmed)

## Cleanup

After validation:

```bash
# Delete provisioning job
curl -X DELETE http://localhost:8080/api/v1/provisioning/jobs/{job_id}

# Remove BMC (optional)
curl -X DELETE http://localhost:8080/api/v1/bmcs/test-windows-bmc

# Clean up OCI artifact (if needed)
oras manifest delete controller.internal:8080/os-images/windows-wim:2022
```

## References

- Design: `design/030_Workflow_Windows.md`
- Plan: `plans/004_Phase_6_Provisioner_Plan.md` (Milestone 3)
- User docs: `docs/provisioner/windows_provisioning.md`
