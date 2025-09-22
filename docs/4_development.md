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

Shoal includes a comprehensive test suite with unit tests, integration tests, and benchmarks.

```bash
# Run all tests (recommended)
python3 build.py test

# Run tests with coverage reporting
python3 build.py coverage

# Manual Go testing (if needed)
go test -v ./...
go test -race ./...
```

## Quality Assurance

Every build goes through automated quality gates. Run the full validation pipeline before committing changes.

```bash
# Full validation (all quality gates)
python3 build.py validate

# Individual quality checks
python3 build.py fmt     # Code formatting
python3 build.py lint    # Static analysis
python3 build.py test    # Test execution
```
