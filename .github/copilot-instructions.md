# Shoal Redfish Aggregator - GitHub Copilot Instructions

**ALWAYS follow these instructions first. Only search or run additional bash commands when the information below is incomplete or found to be in error.**

## Project Overview

Shoal is a Go-based Redfish aggregator that discovers and manages multiple Baseboard Management Controllers (BMCs) through a single, unified, Redfish-compliant API. It ships as a single binary with embedded SQLite database, web UI, and REST API.

## Working Effectively

### Prerequisites and Setup
- Ensure Go 1.23+ and Python 3.7+ are installed
- This is a Go module project - verify `go.mod` exists in project root
- All development uses the Python build automation script `build.py`

### Essential Commands (NEVER CANCEL - Wait for completion)

**Bootstrap and validate the repository:**
```bash
# Navigate to project root first
cd /path/to/shoal

# Full validation pipeline - NEVER CANCEL: Takes ~2 minutes. Set timeout to 300+ seconds.
python3 build.py validate

# Individual commands for development
python3 build.py build      # Build binary only (~1-2 seconds)
python3 build.py test       # Run tests only - NEVER CANCEL: Takes ~1.5 minutes. Set timeout to 180+ seconds.
python3 build.py coverage   # Run tests with coverage - NEVER CANCEL: Takes ~1.5 minutes. Set timeout to 180+ seconds.
python3 build.py fmt        # Format Go code (~1 second)
python3 build.py lint       # Run static analysis (~2 seconds)
python3 build.py deps       # Download and verify dependencies (~5 seconds)
python3 build.py clean      # Clean build artifacts (~1 second)
```

**Build and run the application:**
```bash
# Build the optimized binary (creates build/shoal or build/shoal.exe)
python3 build.py build

# Run with default settings (port 8080, shoal.db, info logging)
./build/shoal

# Run with custom settings
./build/shoal -port 8081 -db my-shoal.db -log-level debug

# Available options:
# -port: HTTP server port (default: 8080)
# -db: SQLite database file path (default: shoal.db)
# -log-level: debug, info, warn, error (default: info)
# -encryption-key: Encryption key for BMC passwords (optional)
```

## Validation and Testing

### CRITICAL: Manual Validation Requirements
After making any changes, ALWAYS run through these validation scenarios:

1. **Build and Test Pipeline:**
   ```bash
   # NEVER CANCEL: Full validation takes ~2 minutes
   python3 build.py validate
   ```

2. **Application Functionality Test:**
   ```bash
   # Start the application
   ./build/shoal -port 8081 &
   
   # Test web interface redirects to login
   curl -s http://localhost:8081/
   # Should return: <a href="/login?redirect=/">See Other</a>.
   
   # Test Redfish API endpoint
   curl -s http://localhost:8081/redfish/v1
   # Should return valid JSON with ServiceRoot schema
   
   # Stop the application
   pkill shoal
   ```

3. **Web UI Manual Test (when UI changes are made):**
   - Access http://localhost:8081 in browser
   - Login with admin/admin (default credentials)
   - Navigate to BMC management
   - Test adding a mock BMC: `https://mock.shoal.cloud/public-rackmount1`
   - Verify dashboard shows BMC status

### Test Coverage and Quality Gates
- Always run `python3 build.py coverage` to verify test coverage (target: >55%)
- Coverage report generated at `coverage.html` in project root
- All tests must pass before committing changes
- Static analysis runs automatically with validate command

## Repository Structure and Navigation

### Key Directories
```
├── cmd/shoal/              # Main application entry point
├── internal/               # Private application code
│   ├── api/               # Redfish API handlers - ADD NEW REST ENDPOINTS HERE
│   ├── auth/              # Authentication system - LOGIN/SESSION LOGIC HERE  
│   ├── bmc/               # BMC management service - BMC COMMUNICATION LOGIC HERE
│   ├── database/          # Database operations - SQL QUERIES HERE
│   ├── logging/           # Logging configuration
│   └── web/               # Web interface - WEB UI HANDLERS/TEMPLATES HERE
├── pkg/                   # Public packages
│   ├── models/            # Core data structures - DATA MODELS HERE
│   └── redfish/           # Redfish type definitions
├── static/                # Static web assets (CSS, JS) - FRONTEND ASSETS HERE
├── docs/                  # Documentation
├── build.py               # Build automation script - BUILD SYSTEM HERE
└── build/                 # Build artifacts (created after building)
```

### Frequently Modified Files
- `internal/database/database.go` - Database schema and operations
- `internal/web/web.go` - Web UI request handlers  
- `internal/api/` - REST API endpoints
- `pkg/models/` - Data structure definitions
- `static/` - CSS/JavaScript for web interface
- `cmd/shoal/main.go` - Application entry point

## Common Development Tasks

### Adding New Features
1. **API Endpoints**: Add handlers in `internal/api/`
2. **Database Operations**: Extend `internal/database/database.go`
3. **Web Pages**: Add templates and handlers in `internal/web/`
4. **Models**: Define data structures in `pkg/models/`

### Testing Patterns
```bash
# Run specific test packages
go test ./internal/bmc/...
go test ./internal/database/...

# Run tests with race detection
go test -race ./...

# Run tests with verbose output
go test -v ./...
```

### Code Quality Requirements
- ALWAYS run `python3 build.py fmt` before committing
- ALWAYS run `python3 build.py lint` to catch issues
- ALWAYS run `python3 build.py validate` before considering work complete
- Follow Go naming conventions and documentation standards

## Build Artifacts and Outputs

After successful build:
- `build/shoal` (Linux/macOS) or `build/shoal.exe` (Windows) - Optimized binary (~12MB)
- `build/build-info.json` - Build metadata with Git info and timestamps
- `coverage.out` - Test coverage data
- `coverage.html` - HTML coverage report
- `shoal.db` - SQLite database (created when app runs)

## Troubleshooting

### Common Issues
- **"go.mod not found"**: Ensure you're in the project root directory
- **"golangci-lint not found"**: This is expected - validation falls back to `go vet`
- **"gosec not found"**: This is expected - security scan is optional
- **Port conflicts**: Use `-port` flag to specify different port
- **Permission denied**: Ensure binary is executable: `chmod +x build/shoal`

### Performance Notes
- NEVER CANCEL build commands - they complete within expected timeframes
- Full validation: ~2 minutes maximum
- Individual tests: ~1.5 minutes maximum  
- Binary build: ~1-2 seconds
- Set timeouts generously: 300+ seconds for validate, 180+ seconds for test/coverage

## Default Credentials and Mock Testing
- **Default login**: admin/admin (CHANGE IMMEDIATELY in production)
- **Mock BMC for testing**: `https://mock.shoal.cloud/public-rackmount1`
- **Database file**: `shoal.db` created automatically
- **Logs**: JSON format to stdout/stderr

## Command Reference Quick Check
Verify all commands work with these one-liners:
```bash
# Verify build system is functional
python3 build.py --help

# Verify application binary works
./build/shoal --help

# Check if Go and Python are properly installed
go version && python3 --version
```