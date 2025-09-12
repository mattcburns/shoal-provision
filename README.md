# Shoal - Redfish Aggregator

Shoal is a Go-based Redfish aggregator service that discovers and manages multiple Baseboard Management Controllers (BMCs) through a single, unified, Redfish-compliant API.

## Features

- **Redfish-Compliant API**: Fully compliant DMTF Redfish v1.6.0 RESTful API with service root and collections
- **BMC Aggregation**: Manages multiple BMCs through a single, unified interface
- **Request Proxying**: Transparently proxies Redfish requests to downstream BMCs with credential injection
- **Power Management**: Execute power control actions (On, ForceOff, Reset, etc.) across multiple BMCs
- **User Management**: Multi-user support with role-based access control (Admin, Operator, Viewer)
- **Web Management Interface**: Complete web UI for BMC management, power control, user management, and monitoring
- **Dual Authentication**: Supports both HTTP Basic Auth and Redfish session tokens
- **BMC Health Testing**: Automatic connection testing when adding/updating BMCs
- **Password Security**: bcrypt password hashing for users, AES-256-GCM encryption for BMC passwords
- **SQLite Database**: Lightweight, embedded database with automatic migrations
- **Structured Logging**: JSON-formatted logs using Go's slog library with configurable levels
- **Single Binary Deployment**: Zero external dependencies - just run the binary
- **Comprehensive Testing**: Unit, integration, and benchmark tests with coverage reporting
- **Automated Build System**: Python-based cross-platform build automation with quality gates
- **Quality Assurance**: Automated code formatting, linting, security scanning, and validation

## Architecture

### Core Components

- **API Layer** (`internal/api`): Redfish-compliant REST API handlers
- **Authentication** (`internal/auth`): Redfish session management and basic auth
- **BMC Service** (`internal/bmc`): BMC communication and proxy functionality
- **Database Layer** (`internal/database`): SQLite database operations
- **Web Interface** (`internal/web`): Server-side rendered management UI
- **Logging** (`internal/logging`): Structured logging configuration

### Directory Structure

├── internal/            # Private application code
│   ├── api/            # Redfish API handlers
│   ├── auth/           # Authentication system
│   ├── bmc/            # BMC management service
│   ├── database/       # Database operations
│   ├── logging/        # Logging configuration
│   └── web/            # Web interface
├── pkg/                # Public packages

```bash
# Clone the repository
│   └── redfish/        # Redfish type definitions
├── static/             # Static web assets

# Build for the current platform (recommended)
python build.py build

# Build for all supported platforms (Linux, Windows, macOS, arm64/x86_64)
python build.py build-all

# Build for a specific platform (e.g., linux/amd64, windows/amd64, darwin/arm64)
python build.py build --platform linux/amd64
python build.py build --platform windows/amd64
python build.py build --platform darwin/arm64

# Or run the full validation pipeline
python build.py validate
```
└── templates/          # HTML templates (embedded in code)
```

## Getting Started

### Prerequisites

- Go 1.21 or later
- Network access to BMCs you want to manage

### Building

```bash
# Clone the repository
git clone <repository-url>
cd shoal

# Build using the automated build system (recommended)
python build.py build

# Or run the full validation pipeline
python build.py validate
```

**Build Requirements:**
- Go 1.21 or later
- Python 3.12 or later (for build automation)
- Network access to download Go modules

The build system uses Python for cross-platform automation without external dependencies.

### Running

```bash
# Build first (if not already built)
python build.py build

# Run with default settings (port 8080, shoal.db, info logging)
./build/shoal

# Run with custom settings
./build/shoal -port 8080 -db shoal.db -log-level debug
```

### Command Line Options

- `-port`: HTTP server port (default: 8080)
- `-db`: SQLite database file path (default: shoal.db)
- `-log-level`: Log level - debug, info, warn, error (default: info)
- `-encryption-key`: Encryption key for BMC passwords (optional, uses SHOAL_ENCRYPTION_KEY env var if not set)

## Build Automation

Shoal uses a Python-based build automation system (`build.py`) for cross-platform development without external dependencies.

### Available Commands

```bash
# Full validation pipeline (recommended for development)
python build.py validate

# Individual commands
python build.py build      # Build binary only
python build.py test       # Run tests only
python build.py coverage   # Run tests with coverage reporting
python build.py fmt        # Format Go code
python build.py lint       # Run linting/static analysis
python build.py deps       # Download and verify dependencies
python build.py clean      # Clean build artifacts
```

### Development Workflow

The `validate` command runs the complete development pipeline:

1. **Prerequisites Check** - Verifies Go installation and module setup
2. **Dependencies** - Downloads and verifies Go module dependencies
3. **Format** - Formats Go code using `go fmt`
4. **Lint** - Runs static analysis (golangci-lint or go vet)
5. **Tests** - Executes all tests with coverage reporting
6. **Security** - Runs security scan (gosec if available)
7. **Build** - Creates optimized binary in `build/` directory
8. **Build Info** - Generates build metadata JSON file

### Build Artifacts


After building, you'll find:
- `build/shoal` - Optimized binary for the current platform (Linux/macOS) or `build/shoal.exe` (Windows)
- `build/shoal-<os>-<arch>[.exe]` - Cross-compiled binaries for each platform (e.g., `shoal-linux-amd64`, `shoal-windows-amd64.exe`, `shoal-darwin-arm64`)
- `build/build-info.json` - Build metadata with Git info and timestamps
- `coverage.out` - Test coverage data (if coverage command was used)
- `coverage.html` - HTML coverage report (if coverage command was used)

**Note:**
- The `build-all` command produces binaries for all supported platforms and architectures.
- The `--platform` option allows building for a specific OS/architecture pair.

## Usage

### Web Interface

Access the web interface at `http://localhost:8080`

- **Dashboard**: Overview of managed BMCs with status and last seen timestamps
- **BMC Management**: Complete CRUD operations - add, edit, delete, and enable/disable BMCs
- **Connection Testing**: Quick connectivity tests for any BMC with one-click "Test" button in the management table
- **Power Control**: Execute power actions (On, ForceOff, ForceRestart) directly from the web UI
- **Real-time Feedback**: Success/error messaging for all operations
- **Pre-validation Testing**: Test BMC connectivity before adding/saving using the "Test Connection" button in forms

**BMC Management Features:**
- **Quick Testing**: Each BMC in the management table has a "Test" button for instant connectivity verification
- **Add BMCs**: Enter IP address (e.g., `192.168.1.100`) or hostname, with "Test Connection" button to verify before saving
- **Live Status**: Test results appear inline with success/failure indicators
- **Automatic Protocol Handling**: Shoal automatically handles HTTPS protocol and Redfish API path construction

See [BMC Configuration](#bmc-configuration) for detailed address format options.

### BMC Configuration

When adding BMCs to Shoal, you provide the base URL that represents the BMC's network address. Shoal automatically handles the Redfish API path construction.

#### Address Format

The BMC address should be the base URL **without** the `/redfish/v1` suffix:

**Standard BMCs (Physical Hardware):**
- IP address: `192.168.1.100` → Shoal uses `https://192.168.1.100/redfish/v1/...`
- With protocol: `https://192.168.1.100` or `http://192.168.1.100`
- Hostname: `bmc.example.com` → Shoal uses `https://bmc.example.com/redfish/v1/...`
- With port: `192.168.1.100:8443` → Shoal uses `https://192.168.1.100:8443/redfish/v1/...`

**Mock/Testing BMCs:**
- With path prefix: `https://mock.shoal.cloud/public-rackmount1`
  - Shoal preserves the path and appends: `https://mock.shoal.cloud/public-rackmount1/redfish/v1/...`

#### Important Notes

- If no protocol is specified, Shoal defaults to HTTPS
- The `/redfish/v1` path is automatically appended - don't include it in the address
- For mock servers or proxies with path prefixes, include the full base path
- Trailing slashes are automatically handled

#### Power Control

Shoal automatically discovers the correct system ID from each BMC for power control operations. This ensures compatibility with:
- Different BMC implementations (iDRAC, iLO, etc.)
- Mock BMCs with non-standard system IDs
- Multi-system chassis

Power actions are executed against the first available system on each BMC.

### User Management

#### Default Administrator

On first run, Shoal creates a default administrator account:
- **Username**: `admin`
- **Password**: `admin`

**IMPORTANT**: Change the default password immediately after first login.

#### User Roles

Shoal implements role-based access control with three user roles:

- **Administrator**: Full system access
  - Manage users (create, edit, delete)
  - Configure and manage BMCs
  - Execute power control actions
  - Access all Redfish API endpoints

- **Operator**: BMC management access
  - View and manage BMCs
  - Execute power control actions
  - Cannot manage users or system settings

- **Viewer**: Read-only access
  - View BMC status and configuration
  - Access read-only Redfish API endpoints
  - Cannot make any changes

#### User Operations

**Web Interface** (administrators only):
- Navigate to "Manage Users" from the main menu
- Add new users with username, password, and role
- Edit existing users (change role, reset password, enable/disable)
- Delete users (cannot delete the last admin user)

**User Profile**:
- All users can access their profile from the menu
- View account details (username, role, status)
- Change their own password

#### Authentication Methods

1. **Web Interface**: Session-based authentication with cookies
   - Login at `/login`
   - Sessions expire after 24 hours
   - Logout available from user menu

2. **API Access**: Two methods supported
   - **HTTP Basic Auth**: Include credentials in Authorization header
   - **Redfish Sessions**: Create session via `/redfish/v1/SessionService/Sessions`

### Security

#### Password Security

**User Passwords**:
- Hashed using bcrypt with cost factor 10
- Passwords must be less than 72 characters (bcrypt limitation)
- Original passwords are never stored or logged

**BMC Password Encryption**:

Shoal supports AES-256-GCM encryption for BMC passwords stored in the database. When an encryption key is provided, all BMC passwords are encrypted before storage and decrypted on-the-fly when needed.

**Enabling Encryption:**

```bash
# Using environment variable (recommended)
export SHOAL_ENCRYPTION_KEY="your-secret-encryption-key"
./build/shoal

# Or using command-line flag
./build/shoal --encryption-key "your-secret-encryption-key"
```

**Important Notes:**
- If no encryption key is provided, passwords are stored in plaintext (not recommended for production)
- The same encryption key must be used consistently - changing it will make existing passwords unreadable
- Store the encryption key securely - losing it means losing access to all BMC passwords
- Existing plaintext passwords are automatically encrypted when accessed with an encryption key

**Encryption Details:**
- **Algorithm**: AES-256-GCM (authenticated encryption)
- **Key Derivation**: PBKDF2 with SHA-256 (100,000 iterations)
- **Nonce**: Random 12-byte nonce for each encryption operation
- **Storage**: Base64-encoded encrypted data in database

### Redfish API

The Redfish API is available at `http://localhost:8080/redfish/`

#### Authentication

**Basic Authentication**:
```bash
curl -u admin:admin http://localhost:8080/redfish/v1/
```

**Session-Based Authentication**:
```bash
# Create session
curl -X POST http://localhost:8080/redfish/v1/SessionService/Sessions \
  -H "Content-Type: application/json" \
  -d '{"UserName": "admin", "Password": "admin"}'

# Use session token
curl -H "X-Auth-Token: <token>" http://localhost:8080/redfish/v1/
```

#### API Endpoints

- `GET /redfish/v1/` - Service root
- `GET /redfish/v1/Managers` - List of managed BMCs
- `GET /redfish/v1/Systems` - List of managed systems
- `GET /redfish/v1/Managers/{bmc-name}` - Proxy to specific BMC manager
- `GET /redfish/v1/Systems/{bmc-name}` - Proxy to specific system
- `POST /redfish/v1/SessionService/Sessions` - Create authentication session

## Development

### Database Schema

The application uses SQLite with the following main tables:

- `bmcs`: BMC information and credentials
- `users`: User accounts (future enhancement)
- `sessions`: Authentication sessions

### Adding New Features

1. **API Endpoints**: Add new handlers in `internal/api/`
2. **Database Operations**: Extend `internal/database/database.go`
3. **Web Pages**: Add templates and handlers in `internal/web/`
4. **Models**: Define new data structures in `pkg/models/`

### Testing

Shoal includes a comprehensive test suite with unit tests, integration tests, and benchmarks.

```bash
# Run all tests (recommended)
python build.py test

# Run tests with coverage reporting
python build.py coverage

# Manual Go testing (if needed)
go test -v ./...
go test -race ./...
```

**Test Coverage:**
- Database operations (CRUD, migrations, sessions)
- Authentication system (basic auth, session tokens, middleware)
- API endpoints (Redfish compliance, error handling)
- Web interface (HTML rendering, form handling)
- Integration tests (full application workflow)
- Concurrent request handling
- Performance benchmarks

### Quality Assurance

Every build goes through automated quality gates:

```bash
# Full validation (all quality gates)
python build.py validate

# Individual quality checks
python build.py fmt     # Code formatting
python build.py lint    # Static analysis
python build.py test    # Test execution
```

**Quality Gates:**
- ✅ Code formatting enforced (`go fmt`)
- ✅ Static analysis (golangci-lint or go vet)
- ✅ All tests must pass
- ✅ Security scanning (gosec if available)
- ✅ Dependency verification
- ✅ Build artifact validation
- ✅ Binary execution testing

## Configuration

### Default Credentials

The application ships with a default admin user:
- **Username**: admin
- **Password**: admin

⚠️ **Change these credentials in production!**

### BMC Configuration

#### BMC Address Format

When adding BMCs through the web interface or API, the address field accepts multiple flexible formats:

**Recommended formats:**
```
# Just IP address (most common)
192.168.1.100
10.0.0.50

# Hostname or FQDN
bmc-server1.local
dell-r640-bmc.lab.internal
```

**Advanced formats (if needed):**
```
# Explicit HTTPS (default protocol)
https://192.168.1.100
https://bmc-server1.local

# HTTP (only if BMC doesn't support HTTPS)
http://192.168.1.100
```

**How it works:**
- Shoal automatically adds `https://` if no protocol is specified
- HTTPS is the default since most modern BMCs use encrypted connections
- Only specify `http://` explicitly if your BMC doesn't support HTTPS
- Don't include API paths - Shoal handles Redfish endpoint construction automatically

**Connection Testing:**
- Use the "Test Connection" button in Add/Edit BMC forms to verify connectivity
- Tests the unauthenticated Redfish root endpoint (`/redfish/v1/`)
- Accepts HTTP 200 (OK) or 401 (Unauthorized) as successful responses
- 401 indicates the BMC is responding but requires authentication (normal)
- Helps identify network issues, wrong addresses, or non-Redfish devices before saving

#### BMC Requirements

- BMCs must support DMTF Redfish API (v1.6.0 or compatible)
- Network connectivity from Shoal server to BMC management interfaces
- Valid BMC credentials (username/password)
- HTTPS support (self-signed certificates are accepted)
- Certificate validation is disabled (common for BMC environments)

## Security Considerations

- BMC credentials are stored in SQLite database
- Use HTTPS in production deployments
- Change default admin credentials
- Consider implementing proper user management
- BMC communications use TLS but skip certificate verification

## Troubleshooting

### Common Issues

1. **BMC Connection Failed**
   - Verify BMC IP address and network connectivity
   - Check BMC credentials
   - Ensure BMC Redfish service is enabled

2. **Database Errors**
   - Check file permissions for database file
   - Verify disk space availability

3. **Authentication Issues**
   - Verify admin credentials (admin/admin by default)
   - Check session token expiration

### Build Issues

1. **Build Automation Fails**
   - Ensure Python 3.12+ is installed and available as `python` or `python3`
   - Verify Go 1.21+ is installed and in PATH
   - Run `python build.py deps` to verify dependencies
   - Check network connectivity for Go module downloads

2. **Tests Failing**
   - Run `python build.py test` to see detailed test output
   - Check if database permissions allow SQLite file creation
   - Ensure no other processes are using ports during integration tests

### Debug Logging

```bash
# Enable debug logging
./build/shoal -log-level debug

# Run build with verbose output
python build.py validate  # Shows all steps with detailed output
```

## Releases

Download pre-built binaries from [GitHub Releases](https://github.com/mattcburns/shoal/releases):

```bash
# Linux AMD64
curl -L -o shoal "https://github.com/mattcburns/shoal/releases/latest/download/shoal-linux-amd64"
chmod +x shoal && ./shoal

# macOS ARM64 (Apple Silicon)  
curl -L -o shoal "https://github.com/mattcburns/shoal/releases/latest/download/shoal-darwin-arm64"
chmod +x shoal && ./shoal
```

**Creating releases:** Push a version tag (`git tag v1.0.0 && git push origin v1.0.0`) to automatically create a GitHub release with multi-platform binaries.

## Deployment

Shoal is designed for simple deployment as a single, self-contained binary with no external dependencies.

For detailed deployment instructions, production builds, cross-platform compilation, systemd service setup, Docker deployment, and troubleshooting, see [DEPLOYMENT.md](DEPLOYMENT.md).

**Quick Deployment (from source):**
```bash
# Build for production
python build.py build

# Copy and run on target server
scp build/shoal user@server:/opt/shoal/
ssh user@server '/opt/shoal/shoal -port 8080 -db /var/lib/shoal/shoal.db'
```

## License

This project is licensed under the **GNU Affero General Public License v3.0**.

A copy of the license is available in the [LICENSE](LICENSE) file.

## Contributing

[Add contribution guidelines here]
