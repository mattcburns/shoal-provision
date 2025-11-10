# Try it: Linux Workflow (Maintenance OS)

This quickstart shows how to exercise the Linux workflow via the maintenance OS and dispatcher. The Jobs API is not yet implemented; this guide focuses on the recipe shape and what to expect from the controller and dispatcher.

> Status: Linux workflow is implemented with systemd/Quadlet planners; webhook path is scaffolded. Redfish client can run in `noop` mode to simulate mounts and resets.

## Prerequisites

- Built binary at `build/shoal` and running on `:8080` (or adjust URLs)
- Maintenance OS ISO URL reachable by BMCs (bootc-built)
- One server record with a known serial (e.g., `LINUX001`) in the controller DB
- Environment suggestions:
  - `MAINTENANCE_ISO_URL=https://controller.internal/media/isos/bootc-maintenance.iso`
  - `TASK_ISO_DIR=$(pwd)/var/shoal/task-isos`
  - `REDFISH_MODE=noop` for local tests, `http` for real BMCs

## Start the controller

```bash
# Build & test
go run build.go validate

# Run controller (binary built by validate)
./build/shoal --log-level debug --redfish-mode noop
```

## Compose a job recipe (Linux)

Save to `recipe-linux.json`:

```json
{
  "task_target": "install-linux.target",
  "server_serial": "LINUX001",
  "target_disk": "/dev/sda",
  "image": {
    "type": "raw",
    "url": "https://controller.internal/images/minimal.raw"
  },
  "cloud_init": {
    "user_data": "#cloud-config\nhostname: shoal-linux\nssh_authorized_keys:\n  - ssh-ed25519 AAA... user@host\n"
  }
}
```

Notes:
- `install-linux.target` routes to the Linux workflow.
- The dispatcher consumes `recipe.env`, `layout.json`, and auxiliary files from `task.iso` (placeholder in Phase 1).
- `cloud_init.user_data` will be embedded as `user-data` in `task.iso` during full implementation; the current placeholder records content and hashes.

## Submit the job (placeholder)

Future API call:

```bash
curl -X POST http://localhost:8080/api/v1/jobs \
  -H 'Content-Type: application/json' \
  -d @recipe-linux.json
```

Expected (future) response:

```json
{ "job_id": "<uuid>", "status": "queued" }
```

## Observe progress today

With `REDFISH_MODE=noop`, logs show:

- `mount.maintenance` (maintenance.iso)
- `mount.task` (task.iso with recipe files)
- `boot.override`
- `reset`
- `await-webhook` (controller waits for dispatcher webhook)

The placeholder `task.iso` contents are created at `var/shoal/task-isos/<job_id>/`:

- `task.iso` (deterministic placeholder)
- `recipe.json` (your recipe)
- `user-data` (if provided in recipe)

## Webhook flow (overview)

- The maintenance OS boots, runs dispatcher units (partition → image → bootloader → config-drive).
- On success/failure, dispatcher posts to: `POST /api/v1/status-webhook/{server_serial}` with an event body and an HMAC using `WEBHOOK_SECRET`.
- Controller flips job status to `succeeded` or `failed` accordingly.

Webhook handler and auth are scaffolded; details will be finalized during API wiring.

## Troubleshooting

- Network reachability to the maintenance ISO from the BMC is required.
- If you don’t see `await-webhook`, verify the controller configuration and that the job recipe targets `install-linux.target`.
- For real BMCs (`REDFISH_MODE=http`), ensure TLS policy, credentials, and vendor quirks are configured as per `design/028_Redfish_Operations.md`.
