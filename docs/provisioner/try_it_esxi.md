# Try it: ESXi Handoff (Dual-ISO)

This quickstart shows how the ESXi handoff workflow will look end-to-end using the controller’s current scaffolding. The Jobs API is not implemented yet, so we simulate job creation in the DB and focus on the operator steps and payload shape.

> Status: Phase 6 in progress. Redfish client defaults to `noop` for fast local runs.

## Prerequisites

- Built binary at `build/shoal` and running on `:8080` (or adjust URLs)
- ESXi vendor installer URL available to the BMC (HTTPS recommended)
- One server record with a known serial (e.g., `ABC12345`) in the controller DB
- Environment (examples):
  - `ESXI_INSTALLER_URL=https://controller.internal/static/VMware-VMvisor-Installer-8.0U2.iso`
  - `MAINTENANCE_ISO_URL=https://controller.internal/media/isos/bootc-maintenance.iso`
  - `TASK_ISO_DIR=$(pwd)/var/shoal/task-isos`
  - `REDFISH_MODE=noop` (stubbed client for local testing)

## Start the controller

```bash
# One-time
go run build.go validate

# Run controller (binary already built by validate)
./build/shoal --log-level debug --redfish-mode noop
```

The controller will serve task ISOs at `/media/tasks/{job_id}/task.iso`.

## Compose a job recipe (ESXi)

Save to `recipe-esxi.json`:

```json
{
  "task_target": "install-esxi.target",
  "server_serial": "ABC12345",
  "ks_cfg": "vmaccepteula\ninstall --firstdisk --overwritevmfs\nnetwork --bootproto=dhcp\nreboot\n"
}
```

Notes:
- `task_target` routes to the ESXi handoff workflow.
- `ks_cfg` is embedded at `/ks.cfg` in `task.iso`.

## Submit the job (placeholder)

The POST `/api/v1/jobs` endpoint isn’t implemented yet. Once available, it will look like this:

```bash
curl -X POST http://localhost:8080/api/v1/jobs \
  -H 'Content-Type: application/json' \
  -d @recipe-esxi.json
```

Expected (future) response:

```json
{
  "job_id": "<uuid>",
  "status": "queued"
}
```

## Observe progress (today)

Since the Jobs API isn’t wired yet, run with `REDFISH_MODE=noop` and watch logs. You should see events emitted for:

- `mount.maintenance` (for ESXi this is the vendor installer ISO)
- `mount.task` (task.iso with /ks.cfg)
- `boot.override`
- `reset`
- `esxi.await_bmc`
- `esxi.poll_power`
- Cleanup steps

You can also verify the placeholder `task.iso` and sibling files were produced:

```bash
ls -l var/shoal/task-isos/<job_id>/
# Expect: task.iso, recipe.json, ks.cfg, optionally recipe.schema.json
```

## What will change when the API lands

- You’ll create servers and jobs via REST calls.
- Job events and status transitions will be visible under `/api/v1/jobs/{id}`.
- The ESXi handoff will work the same: CD1=vendor ISO, CD2=task.iso, then a reboot and cleanup.

## Troubleshooting

- Missing `ks_cfg` → job fails at `validate-recipe`.
- BMC reachability or power-state polling issues will surface as `esxi.await_bmc` or `esxi.poll_power` errors.
- Ensure ESXi ISO URLs are reachable from the BMC network and served over HTTPS where possible.
