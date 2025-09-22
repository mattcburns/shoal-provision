# Getting Started

This guide covers how to build and run Shoal from source.

## Prerequisites

- Go 1.23 or later
- Python 3.7 or later (for build automation)
- Network access to BMCs you want to manage

## Building

Shoal uses a Python-based build automation system (`build.py`) for cross-platform development.

```bash
# Clone the repository
git clone <repository-url>
cd shoal

# Build using the automated build system (recommended)
# This creates an optimized binary in the `build/` directory.
python3 build.py build

# Or run the full validation pipeline (formats, lints, tests, and builds)
python3 build.py validate
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

The `build.py` script handles all development and build tasks.

### Available Commands

```bash
# Full validation pipeline (recommended for development)
python3 build.py validate

# Individual commands
python3 build.py build      # Build binary only
python3 build.py test       # Run tests only
python3 build.py coverage   # Run tests with coverage reporting
python3 build.py fmt        # Format Go code
python3 build.py lint       # Run linting/static analysis
python3 build.py deps       # Download and verify dependencies
python3 build.py clean      # Clean build artifacts
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
- `build/build-info.json` - Build metadata with Git info and timestamps
- `coverage.out` - Test coverage data (if coverage command was used)
- `coverage.html` - HTML coverage report (if coverage command was used)
