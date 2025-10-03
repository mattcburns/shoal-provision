# Getting Started

This guide covers how to build and run Shoal from source.

## Prerequisites

- Go 1.23 or later
- Network access to BMCs you want to manage

## Building

Shoal uses a Go-based build automation system (`build.go`) for development and builds.

```bash
# Clone the repository
git clone <repository-url>
cd shoal

# Build using the automated build system (recommended)
# This creates an optimized binary in the `build/` directory.
go run build.go build

# Or run the full validation pipeline (formats, lints, tests, and builds)
go run build.go validate
```

## Running

After building, you can run the application from the `build/` directory.

```bash
# Run with default settings (port 8080, shoal.db, info logging)
./build/shoal

# Run with custom settings
./build/shoal -port 8080 -db shoal.db -log-level debug
```

### Command Line Options

- `-port`: HTTP server port (default: 8080)
- `-db`: SQLite database file path (default: shoal.db)
- `-log-level`: Log level - debug, info, warn, error (default: info)
- `-encryption-key`: Encryption key for BMC passwords (optional, uses `SHOAL_ENCRYPTION_KEY` env var if not set)

## Build Automation

The `build.go` script handles all development and build tasks.

### Available Commands

```bash
# Full validation pipeline (recommended for development)
go run build.go validate

# Individual commands
go run build.go build      # Build binary only
go run build.go test       # Run tests only
go run build.go coverage   # Run tests with coverage reporting
go run build.go fmt        # Format Go code
go run build.go lint       # Run linting/static analysis
go run build.go deps       # Download and verify dependencies
go run build.go clean      # Clean build artifacts
go run build.go build-all  # Build for all supported platforms
```

### Cross-Platform Builds

To build for a specific platform:

```bash
# Build for a specific platform (must be run before the command)
go run build.go -platform linux/amd64 build
go run build.go -platform windows/amd64 build
go run build.go -platform darwin/arm64 build

# Or build for all platforms at once
go run build.go build-all
```

### Development Workflow

The `validate` command runs the complete development pipeline:

1.  **Prerequisites Check** - Verifies Go installation and module setup
2.  **Dependencies** - Downloads and verifies Go module dependencies
3.  **Format** - Formats Go code using `go fmt`
4.  **Lint** - Runs static analysis (golangci-lint or go vet)
5.  **Tests** - Executes all tests with coverage reporting
6.  **Security** - Runs security scan (gosec if available)
7.  **Build** - Creates optimized binary in `build/` directory
8.  **Build Info** - Generates build metadata JSON file

### Build Artifacts

After building, you'll find:
- `build/shoal` - Optimized binary for the current platform (Linux/macOS) or `build/shoal.exe` (Windows)
- `build/shoal-{os}-{arch}` - Platform-specific binaries (when using build-all)
- `build/build-info.json` - Build metadata with Git info and timestamps
- `coverage.out` - Test coverage data (if coverage command was used)
- `coverage.html` - HTML coverage report (if coverage command was used)

