# 026: Systemd and Quadlet Orchestration

Progress (2025-11-05)
- Phase 1 (controller) is complete; Systemd/Quadlet orchestration will land in Phase 2 alongside the maintenance OS and dispatcher.
- This document remains in progress; concrete unit graphs and Quadlet definitions will be finalized with the dispatcher tasks.

Status: Proposed
Owners: Provisioning Working Group
Last updated: 2025-11-03

Summary

This document defines how provisioning workflows are orchestrated inside the bootc-based maintenance OS using systemd and Quadlet. It specifies the unit graph, naming and layout conventions, standard units for Linux and Windows workflows, a robust error/success reporting scheme, idempotency guarantees, and security practices. It also provides example unit definitions for immediate implementation and testing.

Related

- 020_Provisioner_Architecture.md (index and overview)
- 021_Provisioner_Controller_Service.md (API, state machine, storage)
- 022_Recipe_Schema_and_Validation.md (recipe contract and limits)
- 023_Task_ISO_Builder.md (deterministic task.iso generation)
- 024_Maintenance_OS_Build_with_bootc.md (image composition)
- 025_Dispatcher_Go_Binary.md (on-host entrypoint)
- 027_Embedded_OCI_Registry.md (optional /v2/ handler)
- 028_Redfish_Operations.md (BMC flows)
- 032_Error_Handling_and_Webhooks.md (reliability and payloads)

Goals

- Provide a clear, reproducible service graph for each workflow (Linux, Windows, maintenance).
- Minimize custom logic by delegating tool execution to containers via Quadlet.
- Ensure deterministic behavior, robust error attribution, and clean success/failure reporting.
- Keep the host minimal and secure; privilege is granted only to the containers that need it.

Non-Goals

- Implementing vendor-specific tool logic (lives in container images).
- Implementing the controller or Redfish client (covered elsewhere).
- Covering VMware ESXi installer details (that flow boots the vendor ISO directly).

1. Orchestration Model

- The Dispatcher writes /run/provision/* and starts the master target specified by recipe.task_target.
- Each master target Requires= a set of Type=oneshot services that perform steps (partition, image, bootloader, etc.). Order is controlled with After=.
- Services use Quadlet (.container) units to execute tooling inside privileged containers.
- OnSuccess/OnFailure hooks at the master target level notify the controller via webhook. Additionally, each critical step service declares its own OnFailure hook with a templated instance to capture the exact failing unit name.

2. Filesystem Layout and Conventions

- Host units
  - /etc/systemd/system/*.target
  - /etc/systemd/system/*.service
- Quadlet units (containers)
  - /etc/containers/systemd/*.container
- Runtime data produced by Dispatcher
  - /run/provision/recipe.env (key=value)
  - /run/provision/layout.json
  - /run/provision/user-data (optional)
  - /run/provision/unattend.xml (optional)
- Mount points used by services
  - /mnt/new-root (Linux rootfs)
  - /mnt/new-windows (Windows OS volume)
  - /mnt/efi (EFI System Partition)
  - /mnt/cidata (cloud-init CIDATA, if required)

Naming

- Master targets:
  - install-linux.target
  - install-windows.target
  - supermicro-update.target
- Step services (examples):
  - partition.service
  - image-linux.service
  - bootloader-linux.service
  - config-drive.service
  - image-windows.service
  - bootloader-windows.service
- Webhook services:
  - provision-success.service
  - provision-failed@.service (templated instance to capture the failing unit)

3. Master Target Definitions

3.1 install-linux.target

- Requires: partition.service, image-linux.service, bootloader-linux.service, config-drive.service
- Order: partition → image-linux → bootloader-linux → config-drive
- Hooks: OnSuccess=provision-success.service, OnFailure=provision-failed@%n.service (fallback)

Example

```
[Unit]
Description=Master Target: Install Traditional Linux
Requires=partition.service image-linux.service bootloader-linux.service config-drive.service
After=partition.service image-linux.service bootloader-linux.service config-drive.service
OnSuccess=provision-success.service
OnFailure=provision-failed@%n.service
RefuseManualStart=no
RefuseManualStop=no
```

3.2 install-windows.target

- Requires: partition.service, image-windows.service, bootloader-windows.service
- Order: partition → image-windows → bootloader-windows
- Hooks: OnSuccess=provision-success.service, OnFailure=provision-failed@%n.service (fallback)

Example

```
[Unit]
Description=Master Target: Install Windows
Requires=partition.service image-windows.service bootloader-windows.service
After=partition.service image-windows.service bootloader-windows.service
OnSuccess=provision-success.service
OnFailure=provision-failed@%n.service
```

3.3 supermicro-update.target

- Requires: supermicro-firmware.service
- Hook: OnSuccess=provision-success.service, OnFailure=provision-failed@%n.service

Example

```
[Unit]
Description=Master Target: Supermicro Firmware Update
Requires=supermicro-firmware.service
After=supermicro-firmware.service
OnSuccess=provision-success.service
OnFailure=provision-failed@%n.service
```

Note on ESXi:
- The ESXi workflow mounts the vendor installer ISO as CD1 and the Kickstart task.iso as CD2; the maintenance OS does not run. No units are required here.

4. Step Service Patterns (host-side .service wrapping Quadlet)

4.1 Common directives

- Type=oneshot with RemainAfterExit=yes for clear dependency semantics.
- TimeoutSec set per-step with generous but bounded values.
- Restart=no (fail fast; let the target’s failure path handle retries via re-run policies if needed).
- Environment:
  - EnvironmentFile=/run/provision/recipe.env
- Failure capture:
  - OnFailure=provision-failed@%n.service (sends webhook with %i=%n, i.e., failing unit instance)

Example template

```
[Unit]
Description=Step: <name>
Wants=network-online.target
After=network-online.target
OnFailure=provision-failed@%n.service

[Service]
Type=oneshot
EnvironmentFile=/run/provision/recipe.env
ExecStart=/usr/bin/systemctl start <quadlet-unit>.service
RemainAfterExit=yes
TimeoutSec=45min

[Install]
WantedBy=multi-user.target
```

Rationale:
- We call systemctl start for the corresponding Quadlet unit so the host service remains a thin wrapper (better logs and step control). Alternatively, the host service can directly ExecStart the container via /usr/bin/podman, but Quadlet abstracts this.

5. Quadlet Units (.container)

5.1 Common directives

- Type=oneshot; Remove=true for ephemeral containers.
- Image= points to tool container (pre-bound or pulled from controller’s /v2/).
- EnvironmentFile=/run/provision/recipe.env (inputs from Dispatcher).
- Privileges:
  - AddDevice=/dev:/dev:rwm
  - AddCapability=ALL (tighten per tool if feasible)
  - Umask=0022
- Bind mounts:
  - /run/provision:/run/provision:ro
  - Host directories for mount points (/mnt/new-root, /mnt/efi, etc.) as read-write
- TimeoutStopSec reasonable (10s default) since Type=oneshot.

5.2 partition.container

```
[Unit]
Description=Quadlet Tool: Partition Disk with sgdisk

[Container]
Image=controller.internal:8080/tools/sgdisk:1.0
Type=oneshot
Remove=true
EnvironmentFile=/run/provision/recipe.env
AddDevice=/dev:/dev:rwm
AddCapability=ALL
Volume=/run/provision:/run/provision:ro
Volume=/mnt:/mnt:rshared
Exec=/usr/local/bin/partition-wrapper.sh
```

Notes:
- partition-wrapper.sh reads /run/provision/recipe.env (TARGET_DISK) and /run/provision/layout.json, then runs sgdisk/mkfs accordingly.
- Idempotency: wrapper should no-op if the partition table already conforms (compare sgdisk -p output), or be safe to re-apply.

5.3 image-linux.container

```
[Unit]
Description=Quadlet Tool: Linux Imager (oras + tar)

[Container]
Image=controller.internal:8080/tools/linux-imager:1.0
Type=oneshot
Remove=true
EnvironmentFile=/run/provision/recipe.env
AddDevice=/dev:/dev:rwm
AddCapability=ALL
Volume=/run/provision:/run/provision:ro
Volume=/mnt/new-root:/mnt/new-root:rshared,rw
Exec=/usr/local/bin/image-linux-wrapper.sh
```

Notes:
- image-linux-wrapper.sh:
  - Creates /mnt/new-root if missing.
  - Pulls ${OCI_URL} via oras and streams/extracts to /mnt/new-root (e.g., tar -xpf).
  - Ensures ownership and SELinux contexts if needed.
- Idempotency: if /mnt/new-root has a stamp file with the same artifact digest, no-op.

5.4 bootloader-linux.container

```
[Unit]
Description=Quadlet Tool: Linux Bootloader (GRUB/EFI)

[Container]
Image=controller.internal:8080/tools/linux-bootloader:1.0
Type=oneshot
Remove=true
EnvironmentFile=/run/provision/recipe.env
AddDevice=/dev:/dev:rwm
AddCapability=ALL
Volume=/mnt/new-root:/mnt/new-root:rshared,rw
Volume=/mnt/efi:/mnt/efi:rshared,rw
Exec=/usr/local/bin/bootloader-linux-wrapper.sh
```

Notes:
- bootloader-linux-wrapper.sh mounts necessary pseudo-filesystems into /mnt/new-root (bind mounts), installs GRUB to TARGET_DISK, and ensures EFI entries exist (efibootmgr).
- Idempotency: re-running should not break existing bootloader; ensure chroot mount/unmount is robust.

5.5 config-drive.container

```
[Unit]
Description=Quadlet Tool: Cloud-Init Config Drive (CIDATA)

[Container]
Image=controller.internal:8080/tools/config-drive:1.0
Type=oneshot
Remove=true
EnvironmentFile=/run/provision/recipe.env
AddDevice=/dev:/dev:rwm
AddCapability=ALL
Volume=/run/provision:/run/provision:ro
Volume=/mnt/cidata:/mnt/cidata:rshared,rw
Exec=/usr/local/bin/config-drive-wrapper.sh
```

Notes:
- Creates a small VFAT partition labeled CIDATA if required by the partition layout; writes /run/provision/user-data into appropriate structure.
- Idempotency: if CIDATA already exists and matches contents (hash), no-op.

5.6 image-windows.container

```
[Unit]
Description=Quadlet Tool: Windows Imager (wimapply)

[Container]
Image=controller.internal:8080/tools/wimapply:1.0
Type=oneshot
Remove=true
EnvironmentFile=/run/provision/recipe.env
AddDevice=/dev:/dev:rwm
AddCapability=ALL
Volume=/run/provision:/run/provision:ro
Volume=/mnt/new-windows:/mnt/new-windows:rshared,rw
Exec=/usr/local/bin/image-windows-wrapper.sh
```

Notes:
- Pulls ${OCI_URL} (WIM) via oras and pipes to wimapply targeting /mnt/new-windows.
- Copies unattend.xml into Windows/Panther directory within /mnt/new-windows after extraction.

5.7 bootloader-windows.container

```
[Unit]
Description=Quadlet Tool: Windows Boot (EFI files)

[Container]
Image=controller.internal:8080/tools/windows-bootloader:1.0
Type=oneshot
Remove=true
EnvironmentFile=/run/provision/recipe.env
AddDevice=/dev:/dev:rwm
AddCapability=ALL
Volume=/mnt/new-windows:/mnt/new-windows:rshared,rw
Volume=/mnt/efi:/mnt/efi:rshared,rw
Exec=/usr/local/bin/bootloader-windows-wrapper.sh
```

Notes:
- Copies Windows/Boot/EFI files to EFI System Partition; sets up BCD as needed (bcdboot).
- Idempotency: safe to re-run.

5.8 supermicro-firmware.container

```
[Unit]
Description=Quadlet Tool: Supermicro Firmware Updater

[Container]
Image=controller.internal:8080/tools/supermicro-flasher:1.2
Type=oneshot
Remove=true
EnvironmentFile=/run/provision/recipe.env
AddDevice=/dev:/dev:rwm
AddCapability=ALL
Exec=/usr/local/bin/firmware-update-wrapper.sh
```

Notes:
- Wrapper downloads ${FIRMWARE_URL}, verifies checksum/signature if configured, executes vendor tool.

6. Standard Step Services (host .service → quadlet .service)

6.1 partition.service

```
[Unit]
Description=Step: Partition Target Disk
After=network-online.target
Wants=network-online.target
OnFailure=provision-failed@%n.service

[Service]
Type=oneshot
EnvironmentFile=/run/provision/recipe.env
ExecStart=/usr/bin/systemctl start partition.service.container
RemainAfterExit=yes
TimeoutSec=45min
```

6.2 image-linux.service

```
[Unit]
Description=Step: Image Linux RootFS
Requires=partition.service
After=partition.service network-online.target
Wants=network-online.target
OnFailure=provision-failed@%n.service

[Service]
Type=oneshot
EnvironmentFile=/run/provision/recipe.env
ExecStart=/usr/bin/systemctl start image-linux.service.container
RemainAfterExit=yes
TimeoutSec=90min
```

6.3 bootloader-linux.service

```
[Unit]
Description=Step: Install Linux Bootloader
Requires=image-linux.service
After=image-linux.service
OnFailure=provision-failed@%n.service

[Service]
Type=oneshot
EnvironmentFile=/run/provision/recipe.env
ExecStart=/usr/bin/systemctl start bootloader-linux.service.container
RemainAfterExit=yes
TimeoutSec=20min
```

6.4 config-drive.service

```
[Unit]
Description=Step: Create Cloud-Init Config Drive
After=bootloader-linux.service
OnFailure=provision-failed@%n.service

[Service]
Type=oneshot
EnvironmentFile=/run/provision/recipe.env
ExecStart=/usr/bin/systemctl start config-drive.service.container
RemainAfterExit=yes
TimeoutSec=10min
```

6.5 image-windows.service

```
[Unit]
Description=Step: Image Windows WIM
Requires=partition.service
After=partition.service network-online.target
Wants=network-online.target
OnFailure=provision-failed@%n.service

[Service]
Type=oneshot
EnvironmentFile=/run/provision/recipe.env
ExecStart=/usr/bin/systemctl start image-windows.service.container
RemainAfterExit=yes
TimeoutSec=120min
```

6.6 bootloader-windows.service

```
[Unit]
Description=Step: Configure Windows Boot (EFI)
Requires=image-windows.service
After=image-windows.service
OnFailure=provision-failed@%n.service

[Service]
Type=oneshot
EnvironmentFile=/run/provision/recipe.env
ExecStart=/usr/bin/systemctl start bootloader-windows.service.container
RemainAfterExit=yes
TimeoutSec=20min
```

6.7 supermicro-firmware.service

```
[Unit]
Description=Step: Supermicro Firmware Update
After=network-online.target
Wants=network-online.target
OnFailure=provision-failed@%n.service

[Service]
Type=oneshot
EnvironmentFile=/run/provision/recipe.env
ExecStart=/usr/bin/systemctl start supermicro-firmware.service.container
RemainAfterExit=yes
TimeoutSec=60min
```

Note:
- The “.service.container” naming follows Quadlet’s systemd unit derivation: a file named foo.container becomes foo.service at runtime. If the system uses the same name, adjust ExecStart to the generated service name accordingly.

7. Success/Failure Webhook Units

7.1 provision-success.service

```
[Unit]
Description=Report Provisioning Success
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
EnvironmentFile=/run/provision/recipe.env
# SERIAL_NUMBER must be set by the dispatcher in recipe.env
# Optional: WEBHOOK_URL and WEBHOOK_SECRET may be baked into the image or injected
ExecStart=/usr/bin/bash -c '\
  curl -fsSL -X POST \
    -H "Content-Type: application/json" \
    -H "X-Webhook-Secret: ${WEBHOOK_SECRET}" \
    -d "{\"status\":\"success\"}" \
    "${WEBHOOK_URL}/api/v1/status-webhook/${SERIAL_NUMBER}" \
'

[Install]
WantedBy=multi-user.target
```

7.2 provision-failed@.service (templated)

- The instance name (%i) will be the failing unit if referenced as OnFailure=provision-failed@%n.service.
- Include the failing unit name in payload for accurate attribution.

```
[Unit]
Description=Report Provisioning Failure (%i)
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
EnvironmentFile=/run/provision/recipe.env
ExecStart=/usr/bin/bash -c '\
  FAIL_UNIT="%i"; \
  curl -fsSL -X POST \
    -H "Content-Type: application/json" \
    -H "X-Webhook-Secret: ${WEBHOOK_SECRET}" \
    -d "{\"status\":\"failed\",\"failed_step\":\"${FAIL_UNIT}\"}" \
    "${WEBHOOK_URL}/api/v1/status-webhook/${SERIAL_NUMBER}" \
'

[Install]
WantedBy=multi-user.target
```

Notes:
- Prefer templated OnFailure for each step service. Keep a fallback OnFailure at the master target level for unexpected failures.
- WEBHOOK_URL/SECRET can be:
  - Compiled into the maintenance OS (air-gapped).
  - Injected via /run/provision/recipe.env (dynamic environments).
- Use curl -f to fail on non-2xx responses; systemd logs will capture retries if desired via Restart=on-failure (optional).

8. Idempotency and Robustness

- All step containers must be safe to re-run:
  - Use stamp files or verify current state (e.g., partition layout match, bootloader presence, identical artifact digest).
  - Avoid destructive operations unless required (e.g., conditionally wipefs based on recipe policy).
- Units should validate preconditions and abort early with clear error messages.
- Use ConditionPathExists=/ ConditionPathExists=! to gate steps when appropriate.
- Provide meaningful TimeoutSec values; provisioning steps (WIM/rootfs extraction) may take a long time on slow links/disks.

9. Security

- Host OS has no inbound services; only outbound calls to controller/registry.
- Containers:
  - Grant only necessary privileges. While examples use AddCapability=ALL for simplicity, reduce to specific caps (SYS_ADMIN, SYS_RAWIO, FOWNER, etc.) as you gain confidence.
  - Bind only required host paths; prefer read-only for /run/provision.
  - Use signed images when available; configure CA trust for the controller registry.
- Secrets:
  - Do not log WEBHOOK_SECRET or sensitive recipe fields.
  - If secrets are in unattend.xml/user-data, never print their contents; log sizes/hashes only.

10. Resource Management

- Systemd directives:
  - MemoryMax=, CPUQuota= can be set per step if needed.
  - IOSchedulingClass/IOSchedulingPriority can be tuned for imaging steps.
- For large artifacts (WIM, rootfs), ensure sufficient tmp space (consider tmpfs for intermediates if RAM allows).

11. Observability

- Standardize log prefixes in wrapper scripts (e.g., [partition], [imager], [bootloader]).
- Use journalctl -u <unit> for step diagnostics; summarize major milestones (start, key sub-steps, completion).
- Emit artifact digests, partition tables, and boot entries in logs (non-sensitive).

12. Testing

- Offline VM tests:
  - Mount maintenance.iso (CD1) and a crafted task.iso (CD2) with recipe.json and schema; verify master target succeeds and success webhook is called (can target a local mock server).
- Negative tests:
  - Partitioning failure: intentionally invalid layout → bootloader step never runs; failure webhook includes partition.service.
  - Missing unattend.xml on Windows path → image-windows.service fails early.
- systemd verification:
  - systemd-analyze verify /etc/systemd/system/*.service /etc/containers/systemd/*.container
- Quadlet:
  - podman systemd unit generation is automatic; verify that <name>.container appears as <name>.service when systemd is reloaded.

13. Acceptance Criteria

- Dispatcher starts target reliably; target drives step services in the intended order.
- On any step failure, provision-failed@<step>.service is invoked and posts the correct payload with the failing unit name.
- On success, provision-success.service posts success and the controller proceeds to cleanup.
- Steps are idempotent and safe to retry; logs provide actionable insights.
- go run build.go validate (with associated tests) passes when units and wrappers are integrated into the repo’s maintenance OS build.

Appendix A: Example Environment File (/run/provision/recipe.env)

```
TASK_TARGET=install-linux.target
TARGET_DISK=/dev/nvme0n1
OCI_URL=controller.internal:8080/os-images/ubuntu-rootfs:22.04
FIRMWARE_URL=
SERIAL_NUMBER=XF-12345ABC
WEBHOOK_URL=http://controller.internal:8080
WEBHOOK_SECRET=redacted
```

Appendix B: Directory Creation Helper (host)

Ensure expected mount points exist before steps run. You can add a preparatory oneshot unit WantedBy the master target:

```
[Unit]
Description=Prepare mount points for provisioning

[Service]
Type=oneshot
ExecStart=/usr/bin/mkdir -p /mnt/new-root /mnt/new-windows /mnt/efi /mnt/cidata

[Install]
WantedBy=install-linux.target install-windows.target supermicro-update.target
```

Appendix C: Notes on Quadlet Unit Names

- A file /etc/containers/systemd/partition.container generates a systemd unit named partition.service.
- When starting via ExecStart=/usr/bin/systemctl start partition.service.container, adjust the name if your distro’s Quadlet uses a different mapping. In many environments, invoking “systemctl start partition.service” is sufficient because Quadlet translates the .container to .service automatically after daemon-reload.

Change Log

- v0.1 (2025-11-03): Initial orchestration design with standard units, Quadlet patterns, and webhook handling.
