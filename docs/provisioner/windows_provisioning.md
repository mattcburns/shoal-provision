# Windows Provisioning Guide

This guide describes how to provision Windows Server systems using Shoal's provisioner.

## Overview

Shoal supports automated Windows Server provisioning via the `install-windows.target` workflow. The process applies a WIM (Windows Imaging Format) image to an NTFS partition and configures UEFI boot with an unattend.xml answer file for hands-off deployment.

## Idempotency Enhancements (Phase 6 Milestone 3)

Phase 6 introduced additional safeguards to make re-runs safe and fast:

* Imaging now records a manifest digest stamp at `Windows/.provisioner_wim_digest`. If the digest for the referenced OCI artifact matches the stamp, the WIM apply step is skipped.
* Bootloader setup skips creation of the UEFI boot entry when one with the configured label already exists (checked via `efibootmgr`).
* `Unattend.xml` placement now hashes existing content; identical content results in a no-op.

These changes reduce unnecessary writes and shorten subsequent provisioning attempts (e.g., recovery after dispatcher or controller restart).

### Observing Skips

Dry-run or journal output will include messages such as:

```
image-windows-plan: WIM digest unchanged (<digest>), skipping apply
bootloader-windows-plan: boot entry 'Windows' already exists; skipping creation
bootloader-windows-plan: unattend.xml unchanged (sha256: <hash>), skipping write
```

### Forcing Reapply

To force a reimage despite a matching digest, push an updated artifact (new digest) or remove the stamp file inside the Windows partition before rerunning the workflow.

## Prerequisites

- **Maintenance OS**: Shoal maintenance OS with `wimlib-utils` and `ntfs-3g` packages
- **WIM Image**: Windows Server installation media converted to WIM format and stored in an OCI-compatible registry
- **Unattend.xml**: Windows answer file for automated setup (see examples below)

## Partition Layout Requirements

Windows requires a specific GPT partition layout:

1. **EFI System Partition (ESP)**
   - Size: 300-512MB recommended
   - Type GUID: `ef00` (EFI System)
   - Format: `vfat` (FAT32)
   - Purpose: UEFI boot files

2. **Microsoft Reserved Partition (MSR)**
   - Size: 16MB (128MB for >128GB disks, but 16MB is typical)
   - Type GUID: `0c01` (Microsoft Reserved)
   - Format: `raw` or `none` (no filesystem)
   - Purpose: Reserved for Windows disk management operations

3. **Windows Primary Partition**
   - Size: Remaining space (use `100%`)
   - Type GUID: `0700` (Microsoft Basic Data)
   - Format: `ntfs`
   - Purpose: Windows installation

## Recipe Structure

A minimal Windows provisioning recipe:

```json
{
  "schema_version": "1.0",
  "task_target": "install-windows.target",
  "target_disk": "/dev/sda",
  "oci_url": "controller.internal:8080/os-images/windows-server:2022",
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
    "content": "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<unattend xmlns=\"urn:schemas-microsoft-com:unattend\">...</unattend>"
  }
}
```

## Key Fields

### `wim_index` (optional, default: 1)

WIM files can contain multiple Windows editions (e.g., Standard, Datacenter, Core). Use `wim_index` to select which edition to install:

```json
{
  "wim_index": 2
}
```

To determine available indexes in a WIM:
```bash
wiminfo image.wim
```

### `unattend_xml` (required)

Windows answer file for automated installation. Can be provided inline or referenced:

**Inline:**
```json
{
  "unattend_xml": {
    "content": "<?xml version=\"1.0\"?>..."
  }
}
```

**From URL (future):**
```json
{
  "unattend_xml": {
    "url": "https://config.example.com/unattend.xml"
  }
}
```

## Workflow Steps

1. **Partition Creation** (`partition.service`)
   - Creates GPT table with EFI/MSR/NTFS layout
   - MSR partition left unformatted as required by Windows

2. **WIM Imaging** (`image-windows.service`)
   - Mounts NTFS partition via `ntfs-3g`
   - Pulls WIM from OCI registry via `oras`
   - Applies selected WIM index via `wimapply`
   - Unmounts partition

3. **Bootloader Configuration** (`bootloader-windows.service`)
   - Mounts ESP and Windows partitions
   - Copies Windows boot files to ESP (`/EFI/Microsoft/Boot/`)
   - Creates fallback boot entry (`/EFI/Boot/bootx64.efi`)
   - Configures firmware boot entry via `efibootmgr`
   - Places `unattend.xml` at `Windows/Panther/Unattend.xml`
   - Unmounts partitions

4. **Success/Failure Webhooks**
   - Reports completion status to controller

## Security Considerations

### Unattend.xml Handling

- **Never logged**: The content of `unattend.xml` is never written to logs
- **Only hash logged**: A SHA256 hash prefix is logged for verification
- **Secure placement**: Written with `0600` permissions to Windows partition
- **Ephemeral in maintenance OS**: Exists only in `/run/provision/unattend.xml` during provisioning

Example log output:
```
bootloader-windows-plan: unattend.xml written (sha256: 63b0236561481e8a)
```

### Passwords in Unattend.xml

If your `unattend.xml` contains passwords, consider:
- Using base64-encoded passwords as required by Windows
- Rotating passwords post-deployment via Group Policy
- Using domain join credentials instead of local admin passwords
- Storing `unattend.xml` templates in a secrets management system

## Preparing WIM Images

### Extract from Windows ISO

```bash
# Mount Windows Server ISO
sudo mount -o loop WindowsServer2022.iso /mnt/iso

# Copy install.wim to working directory
cp /mnt/iso/sources/install.wim ./windows-server-2022.wim

# List available editions
wiminfo windows-server-2022.wim

# Package as OCI artifact
oras push controller.internal:8080/os-images/windows-server:2022 \
  ./windows-server-2022.wim:application/vnd.shoal.windows.wim
```

### WIM Optimization

Large WIM files increase provisioning time. Optimize by:
- Using only the specific edition you need (export single index)
- Compressing with maximum compression
- Pre-applying updates to reduce post-install patching

```bash
# Export only Datacenter edition (example index 4)
wimexport install.wim 4 windows-datacenter.wim --compress=maximum
```

## Troubleshooting

### Provisioning Fails at Image Step

**Symptom**: `image-windows.service` fails with mount errors

**Solution**: Verify NTFS partition was created:
```bash
lsblk -f
# Look for partition with ntfs filesystem
```

### Boot Failure After Provisioning

**Symptom**: System doesn't boot into Windows

**Possible causes**:
1. **Missing boot files**: Check ESP contains `/EFI/Microsoft/Boot/bootmgfw.efi`
2. **Wrong WIM index**: Verify `wim_index` matches desired edition
3. **Unattend.xml errors**: Check Windows Setup logs in `C:\Windows\Panther`

### Unattend.xml Not Applied

**Symptom**: Windows prompts for manual setup despite unattend.xml

**Solution**: Verify `unattend.xml`:
- Placed at `C:\Windows\Panther\Unattend.xml`
- Has correct XML structure and namespaces
- Contains `specialize` and `oobeSystem` passes as needed

Use Windows SIM (System Image Manager) to validate syntax.

## Example Unattend.xml

Minimal example for automated setup:

```xml
<?xml version="1.0" encoding="utf-8"?>
<unattend xmlns="urn:schemas-microsoft-com:unattend">
  <settings pass="specialize">
    <component name="Microsoft-Windows-Shell-Setup" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS">
      <ComputerName>WIN-SERVER-01</ComputerName>
      <RegisteredOrganization>Example Org</RegisteredOrganization>
      <RegisteredOwner>IT Department</RegisteredOwner>
      <TimeZone>UTC</TimeZone>
    </component>
  </settings>
  <settings pass="oobeSystem">
    <component name="Microsoft-Windows-Shell-Setup" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS">
      <OOBE>
        <HideEULAPage>true</HideEULAPage>
        <ProtectYourPC>1</ProtectYourPC>
        <NetworkLocation>Work</NetworkLocation>
        <HideWirelessSetupInOOBE>true</HideWirelessSetupInOOBE>
      </OOBE>
      <UserAccounts>
        <AdministratorPassword>
          <Value>UABhAHMAcwB3AG8AcgBkAA==</Value>
          <PlainText>false</PlainText>
        </AdministratorPassword>
      </UserAccounts>
    </component>
  </settings>
</unattend>
```

**Note**: The `AdministratorPassword` value above is base64-encoded. Generate your own:
```powershell
[Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes("YourPassword"))
```

## Advanced: Domain Join

Add domain join configuration to `specialize` pass:

```xml
<component name="Microsoft-Windows-UnattendedJoin" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS">
  <Identification>
    <Credentials>
      <Domain>example.com</Domain>
      <Username>djoin-account</Username>
      <Password>base64-encoded-password</Password>
    </Credentials>
    <JoinDomain>example.com</JoinDomain>
  </Identification>
</component>
```

## License Compatibility

Shoal's Windows provisioning uses open-source tools compatible with AGPLv3:

- **wimlib-utils**: LGPLv3+ (Windows imaging library)
- **ntfs-3g**: GPL-2.0+ (NTFS filesystem driver)

These tools read and write standard Windows formats without requiring proprietary Microsoft software during provisioning.

## See Also

- [Recipe Schema Reference](../schema/recipe.md)
- [Provisioner Architecture](../../design/020_Provisioner_Architecture.md)
- [Windows Workflow Design](../../design/030_Workflow_Windows.md)
- [Example Recipes](./recipes/)
