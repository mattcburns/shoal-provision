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
- Tests: a placeholder E2E test exists at `internal/provisioner/integration/linux_workflow_integration_test.go` and will be enabled as implementation lands.

Before sending a PR:

```bash
go run build.go validate
```

Ensure new source files carry the AGPLv3 header (see `AGENTS.md`).
