# ESXi Handoff Workflow (Dual-ISO)

This document describes the ESXi provisioning handoff implemented by the provisioner controller in Phase 6 (Milestone 4).

- CD1: VMware ESXi vendor installer ISO
- CD2: task.iso containing a Kickstart file at /ks.cfg
- Control plane: Redfish VirtualMedia + one-time boot + reset
- Detection: BMC/API polling (no in-guest webhook)

## Configure controller

Set the ESXi vendor installer URL and where to write task ISOs:

- Environment variables (preferred for containers):
  - `ESXI_INSTALLER_URL` — HTTPS URL to vendor ISO (CD1)
  - `TASK_ISO_DIR` — Directory for generated task ISOs (default: ./var/shoal/task-isos)
  - `MAINTENANCE_ISO_URL` — Still used for Linux/Windows jobs; ignored for ESXi

- Flags (override env):
  - `--esxi-installer-url` (CD1)
  - `--task-iso-dir` (CD2 build location)

The controller exposes the task ISO at `/media/tasks/{job_id}/task.iso`. Workers derive the base URL from the controller’s bind address or from `MAINTENANCE_ISO_URL` host (to keep scheme/host consistent). See also: `docs/provisioner/try_it_esxi.md` for a quickstart.

## Recipe contract

Submit a job recipe with these fields (subset shown):

- `task_target`: must be `install-esxi.target` to route via ESXi handoff
- `ks_cfg`: string — the exact Kickstart contents; will be embedded at `/ks.cfg` in task.iso

Example (abbreviated):

```json
{
  "task_target": "install-esxi.target",
  "server_serial": "ABCDEF123456",
  "ks_cfg": "vmaccepteula\ninstall --firstdisk --overwritevmfs\nreboot\n"
}
```

Validation:
- `ks_cfg` is required for ESXi jobs and must be <= 64 KiB. Missing/oversized values cause immediate job failure (`failed_step=validate-recipe`).

## Orchestration sequence

1) Build task.iso
- Embed `ks_cfg` as `/ks.cfg` (plus optional recipe files for auditing)

2) Redfish actions
- Mount CD1: `ESXI_INSTALLER_URL`
- Mount CD2: task ISO URL `/media/tasks/{job_id}/task.iso`
- Set one-time boot to `Cd`
- Reset host (graceful with safe fallbacks)

3) Detect installer completion
- Poll Redfish ServiceRoot/System; tolerate unavailability and power transitions
- Upon stable `PowerState=On` for a configured window (default 90s), mark job succeeded

4) Cleanup
- Eject both virtual media drives
- Reset host to boot into the installed ESXi

## Observability

The controller emits job events and Prometheus metrics:
- Events include operation names and durations: `mount.maintenance`, `mount.task`, `boot.override`, `reset`, `esxi.await_bmc`, `esxi.poll_power`, and cleanup steps
- Metrics: `shoal_provisioner_redfish_request_duration_seconds`, `shoal_provisioner_redfish_requests_total`, `shoal_provisioner_provisioning_phase_duration_seconds`

## Security notes

- Serve the vendor ISO and task ISO over HTTPS where possible; avoid logging secrets
- `ks_cfg` contents are not logged; only size and hashes are recorded in events

## Troubleshooting

- Job fails with `validate-recipe`: ensure `task_target=install-esxi.target` and provide non-empty `ks_cfg`
- Hangs on `esxi.await_bmc` or `esxi.poll_power`: verify BMC reachability, ISO URLs, and vendor firmware compatibility with virtual media
- Final reboot not taking effect: cleanup retries are best effort; you can manually eject media and reset from the BMC UI
