# Development Guide

This guide covers the project's architecture and development practices.

## Architecture

### Core Components

- **API Layer** (`internal/api`): Redfish-compliant REST API handlers.
- **Authentication** (`internal/auth`): Redfish session management and basic auth.
- **BMC Service** (`internal/bmc`): BMC communication and proxy functionality.
- **Database Layer** (`internal/database`): SQLite database operations.
- **Web Interface** (`internal/web`): Server-side rendered management UI.
- **Logging** (`internal/logging`): Structured logging configuration.

### Directory Structure

```
├── internal/            # Private application code
│   ├── api/            # Redfish API handlers
│   ├── auth/           # Authentication system
│   ├── bmc/            # BMC management service
│   ├── database/       # Database operations
│   ├── logging/        # Logging configuration
│   └── web/            # Web interface
├── pkg/                # Public packages
│   ├── models/         # Core data structures
│   └── redfish/        # Redfish type definitions
├── static/             # Static web assets (CSS, JS)
└── templates/          # HTML templates (embedded in code)
```

## Adding New Features

1.  **API Endpoints**: Add new handlers in `internal/api/`.
2.  **Database Operations**: Extend `internal/database/database.go`.
3.  **Web Pages**: Add templates and handlers in `internal/web/`.
4.  **Models**: Define new data structures in `pkg/models/`.

## Testing

Shoal includes a comprehensive test suite with unit tests and integration tests. Always use the Go-based build tool as the single source of truth.

```bash
# Run all tests (recommended)
go run build.go test

# Run tests with coverage reporting
go run build.go coverage

# Manual Go testing (if needed)
go test -v ./...
go test -race ./...
```

## Quality Assurance

Every build goes through automated quality gates. Run the full validation pipeline before committing changes.

```bash
# Full validation (all quality gates)
go run build.go validate

# Individual quality checks
go run build.go fmt     # Code formatting
go run build.go lint    # Static analysis
go run build.go test    # Test execution
```

## Provisioner (Linux, Windows, ESXi)

Active provisioner work is tracked in the `plans/` and `design/` docs. Current milestone: Phase 6 (hardening & ESXi handoff).

- Designs: `design/020_Provisioner_Architecture.md`, `021_Provisioner_Controller_Service.md`, `025_Dispatcher_Go_Binary.md`, `026_Systemd_and_Quadlet_Orchestration.md`, `028_Redfish_Operations.md`, `029_Workflow_Linux.md`, `031_Workflow_ESXi.md`.
- Plans: `plans/004_Phase_6_Provisioner_Plan.md`.
- ESXi handoff details: `docs/provisioner/esxi_handoff.md`.
- Try-it overview: `docs/provisioner/try_it_overview.md`.
- Try-it guides: `docs/provisioner/try_it_linux.md`, `docs/provisioner/try_it_esxi.md`.
- Windows preview guide: `docs/provisioner/try_it_windows.md`.
 - Fixture samples: `docs/provisioner/fixtures/` (`user-data.yaml`, `ks.cfg`, `unattend.xml`).
- Tests: Linux workflow integration tests at `internal/provisioner/integration/linux_workflow_integration_test.go`. ESXi logic is covered by unit tests in `internal/provisioner/jobs`.
- Maintenance OS image: build the bootc maintenance ISO with `./scripts/build_maintenance_os.sh` (assets under `images/maintenance/`). See details below.

Before sending a PR:

```bash
go run build.go validate
```

Ensure new source files carry the AGPLv3 header (see `AGENTS.md`).

### Provisioner controller configuration

Key environment variables and flags (flags override env):

- `MAINTENANCE_ISO_URL` / `--maintenance-iso-url`: maintenance OS ISO (Linux/Windows flows)
- `ESXI_INSTALLER_URL` / `--esxi-installer-url`: vendor ESXi installer ISO (ESXi handoff)
- `TASK_ISO_DIR` / `--task-iso-dir`: where task ISOs are written (served at `/media/tasks/{job}/task.iso`)
- `REDFISH_MODE` / `--redfish-mode`: `http` or `noop` (stub for development)
- `REDFISH_TIMEOUT` / `--redfish-timeout`: per-request timeout
- `REDFISH_RETRIES` / `--redfish-retries`: retry budget for Redfish operations

ESXi recipes must include:

```json
{
	"task_target": "install-esxi.target",
	"ks_cfg": "vmaccepteula\ninstall --firstdisk --overwritevmfs\nreboot\n"
}
```

The controller embeds `ks_cfg` at `/ks.cfg` in `task.iso`, mounts the vendor installer ISO as CD1, and performs a dual‑ISO handoff via Redfish.

### Building the Maintenance OS ISO

Shoal provides a helper script to build the maintenance OS container image and produce a bootable ISO using bootc-image-builder.

Usage:

```bash
# Basic (writes artifacts to build/maintenance-os)
./scripts/build_maintenance_os.sh

# Options
./scripts/build_maintenance_os.sh \
	--output "$(pwd)/build/maintenance-os" \
	--arch x86_64 \
	--rootfs ext4
```

Notes:

- The script builds the container image from `images/maintenance/Containerfile` and then invokes `bootc-image-builder` to produce an ISO.
- `bootc-image-builder` requires rootful Podman. The script auto-detects rootless environments, exports the local image, imports it into rootful storage, and runs the builder under `sudo`.
- Output artifacts (including `install.iso`) are written to the directory specified by `--output` (default: `build/maintenance-os`).
- Flags:
	- `--output`: Output directory for artifacts (absolute or relative path accepted).
	- `--arch`: Target architecture (defaults to host if omitted).
	- `--rootfs`: Root filesystem type for the image (default: `ext4`).

If you encounter permissions or SELinux issues, ensure `podman` and `sudo` are available and your user is configured appropriately to run Podman. The script mounts `/var/lib/containers/storage` for the rootful builder to resolve and lock the local image correctly.
