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

## Provisioner Phase 3 (Linux workflow)

Active work for the provisioner’s Phase 3 happens on the branch `feature/provisioner-phase3`.

- Designs: see `design/020_Provisioner_Architecture.md`, `021_Provisioner_Controller_Service.md`, `025_Dispatcher_Go_Binary.md`, `026_Systemd_and_Quadlet_Orchestration.md`, and `029_Workflow_Linux.md`.
- Planning: `design/039_Provisioner_Phase_3_Plan.md` tracks scope, milestones, and acceptance criteria.
- Tests: end-to-end coverage for the dispatcher plus planner wrappers lives in `internal/provisioner/integration/linux_workflow_integration_test.go` (happy path + failure attribution).
- Maintenance OS image: build the bootc maintenance ISO with `./scripts/build_maintenance_os.sh` (assets under `images/maintenance/`). See details below.

Before sending a PR:

```bash
go run build.go validate
```

Ensure new source files carry the AGPLv3 header (see `AGENTS.md`).

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
