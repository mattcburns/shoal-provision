# Try it: Windows Workflow (Planning Phase)

This quickstart outlines the intended Windows provisioning flow. The actual workflow units (image-windows, bootloader-windows) and unattend processing are planned; use this as a preview of the recipe contract and controller orchestration.

> Status: Windows workflow is scheduled (Phase 4). Redfish orchestration pieces (mount virtual media, boot override, reset, cleanup) are already scaffolded; image/application steps will follow Linux pattern.

## Prerequisites

- Controller running (`./build/shoal --redfish-mode noop` for local simulation)
- Maintenance OS ISO (still used to perform dispatcher tasks for Windows in early phases) accessible at `MAINTENANCE_ISO_URL`
- Server record with serial (e.g., `WIN001`) in DB

## Environment

```bash
export MAINTENANCE_ISO_URL="https://controller.internal/media/isos/bootc-maintenance.iso"
export TASK_ISO_DIR="$(pwd)/var/shoal/task-isos"
export REDFISH_MODE=noop
./build/shoal --log-level debug --redfish-mode "$REDFISH_MODE"
```

## Windows recipe (planned shape)

Save as `recipe-windows.json`:

```json
{
  "task_target": "install-windows.target",
  "server_serial": "WIN001",
  "target_disk": "\\\\.\\PhysicalDrive0",
  "image": {
    "type": "wim",
    "url": "https://controller.internal/images/minimal.wim"
  },
  "unattend_xml": "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<unattend xmlns=\"urn:schemas-microsoft-com:unattend\">\n  <settings pass=\"oobeSystem\">\n    <component name=\"Microsoft-Windows-Shell-Setup\" processorArchitecture=\"amd64\" publicKeyToken=\"31bf3856ad364e35\" language=\"neutral\" versionScope=\"nonSxS\" xmlns:wcm=\"http://schemas.microsoft.com/WMIConfig/2002/State\" xmlns:xsi=\"http://www.w3.org/2001/XMLSchema-instance\">\n      <TimeZone>UTC</TimeZone>\n      <RegisterOwner>Shoal</RegisterOwner>\n      <AutoLogon>\n        <Enabled>true</Enabled>\n        <Username>Administrator</Username>\n      </AutoLogon>\n    </component>\n  </settings>\n</unattend>"
}
```

Notes:
- Escaping backslashes: `\\\\.\\PhysicalDrive0` is the JSON string for `\\.\PhysicalDrive0`.
- `unattend_xml` will be written into task ISO as `unattend.xml` (Phase 1 placeholder builder already supports this asset field).

## Future API call

```bash
curl -X POST http://localhost:8080/api/v1/jobs \
  -H 'Content-Type: application/json' \
  -d @recipe-windows.json
```

Expected (future) response:

```json
{ "job_id": "<uuid>", "status": "queued" }
```

## Orchestration Summary

1. Build task.iso with `unattend.xml` and recipe.
2. Mount maintenance.iso (CD1) and task.iso (CD2).
3. Boot once from CD; maintenance OS applies WIM image, configures EFI/BCD.
4. Dispatcher triggers reboot; webhook signals success/failure.
5. Cleanup: eject media, final reboot to Windows.

## Observability

Events to expect (placeholder naming):
- `mount.maintenance`
- `mount.task`
- `boot.override`
- `reset`
- `await-webhook`
- cleanup events

Artifacts under `TASK_ISO_DIR/<job_id>/`:
- `task.iso`
- `recipe.json`
- `unattend.xml`

## Troubleshooting (planned)

- Invalid WIM URL → early failure at mount or dispatcher image step.
- Missing or malformed unattend XML → dispatcher service logs parse errors; job fails with step key referencing bootloader or apply phase.
- Webhook timeout (no dispatcher completion) → controller fails job at `webhook-timeout`.

## Next Steps

Implementation will add:
- WIM apply unit (using `dism`/image engine in maintenance OS container)
- Bootloader configuration unit (EFI/BCD handling)
- Validation for unattend XML size and basic schema heuristics

Track progress in `plans/004_Phase_6_Provisioner_Plan.md`.
