# Shoal - Redfish Aggregator

Shoal is a Go-based Redfish aggregator service that discovers and manages multiple Baseboard Management Controllers (BMCs) through a single, unified, Redfish-compliant API.

## Features

- **Redfish-Compliant API**: Fully compliant DMTF Redfish v1.6.0 RESTful API with service root and collections
- **BMC Aggregation**: Manages multiple BMCs through a single, unified interface
- **Request Proxying**: Transparently proxies Redfish requests to downstream BMCs with credential injection
- **Power Management**: Execute power control actions (On, ForceOff, Reset, etc.) across multiple BMCs
- **User Management**: Multi-user support with role-based access control (Admin, Operator, Viewer)
- **Web Management Interface**: Complete web UI for BMC management, power control, user management, and monitoring
- **Detailed BMC Status**: Comprehensive drill-down view showing system information, network interfaces, storage devices, and System Event Log (SEL) entries
- **Detailed BMC Status**: Comprehensive drill-down view showing system information, network interfaces, storage devices, and System Event Log (SEL) entries
- **Settings Tab (read-only)**: Per‑BMC Settings tab with search, OEM filter, and pagination over discovered configurable settings
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
python3 build.py build

# Build for all supported platforms (Linux, Windows, macOS, arm64/x86_64)
python3 build.py build-all

# Build for a specific platform (e.g., linux/amd64, windows/amd64, darwin/arm64)
python3 build.py build --platform linux/amd64
python3 build.py build --platform windows/amd64
python3 build.py build --platform darwin/arm64

# Or run the full validation pipeline
python3 build.py validate
```
└── templates/          # HTML templates (embedded in code)
```

## Getting Started

### Prerequisites

- Go 1.23 or later
- Network access to BMCs you want to manage

### Building

```bash
# Clone the repository
git clone <repository-url>
cd shoal

# Build using the automated build system (recommended)
python3 build.py build

# Or run the full validation pipeline
python3 build.py validate
```

**Build Requirements:**
- Go 1.21 or later
- Python 3.7 or later (for build automation)
- Network access to download Go modules

The build system uses Python for cross-platform automation without external dependencies.

### Running

```bash
# Build first (if not already built)
python3 build.py build

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
- **Detailed BMC Status**: Click "Details" button to view comprehensive information about any BMC including:
  - **System Information**: Serial number, SKU, power state, model, and manufacturer
  - **Network Interfaces**: NIC details with MAC addresses and IP addresses
  - **Storage Devices**: Drive information with capacity, model, serial numbers, and health status
  - **System Event Log (SEL)**: Recent log entries with severity levels and timestamps
- **Connection Testing**: Quick connectivity tests for any BMC with one-click "Test" button in the management table
- **Power Control**: Execute power actions (On, ForceOff, ForceRestart) directly from the web UI
- **Real-time Feedback**: Success/error messaging for all operations
- **Pre-validation Testing**: Test BMC connectivity before adding/saving using the "Test Connection" button in forms

**BMC Management Features:**
- **Detailed Status View**: Click "Details" button to drill down into comprehensive BMC information retrieved live from the Redfish API
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

**Core Endpoints:**
- `GET /redfish/v1/` - Service root
- `GET /redfish/v1/Managers` - List of aggregated managers from all BMCs
- `GET /redfish/v1/Systems` - List of aggregated systems from all BMCs
- `GET /redfish/v1/Managers/{bmc-name}` - Proxy to specific BMC manager
- `GET /redfish/v1/Systems/{bmc-name}` - Proxy to specific system

**Web Interface Endpoints:**
- `GET /bmcs/details?name={bmc-name}` - Detailed BMC status page
- `GET /api/bmcs/details?name={bmc-name}` - JSON endpoint for detailed BMC information
- `GET /api/bmcs/{bmc-name}/settings[?resource={path}]` - JSON endpoint for discovered configurable settings (read-only)
- `GET /api/bmcs/{bmc-name}/settings/{descriptor_id}` - JSON endpoint for a single setting descriptor with current value

### Settings Discovery

- `GET /api/bmcs/{bmc-name}/settings`
  - Returns `{ bmc_name, resource, descriptors: [...], page, page_size, total }`. Results are persisted for subsequent detail lookups.
  - Detail: `GET /api/bmcs/{bmc-name}/settings/{descriptor_id}` returns a single descriptor. If not present, the server will perform discovery on-demand and retry.
  - Auth: same as other web API endpoints (session or basic auth)
  - Query params:
    - `resource`: Optional. Filter to a specific Redfish resource path (substring match), e.g. `/redfish/v1/Systems/<id>/Bios` or `Managers/<id>/NetworkProtocol`.
    - `search`: Optional. Free‑text filter across attribute, display_name, description, resource_path, and OEM vendor.
    - `oem`: Optional. Filter OEM vs non‑OEM settings. Accepted values: `true|false|1|0|yes|no`.
    - `page`: Optional. 1‑based page number for pagination.
    - `page_size`: Optional. Page size for pagination. When omitted, returns all filtered results.
  - Legacy compatibility: the query-form endpoint `GET /api/bmcs/settings?name={bmc-name}` supports the same query parameters.
  - Returns: a JSON object with discovered setting descriptors for the target BMC. Current scope focuses on common Redfish settings surfaces that expose `@Redfish.Settings` such as `Systems/<id>/Bios` and `Managers/<id>/NetworkProtocol`.

Example:
```bash
curl -s -u admin:admin \
  "http://localhost:8080/api/bmcs/bmc1/settings" | jq .
```

Response shape (example):
```json
{
  "bmc_name": "bmc1",
  "resource": "",
  "descriptors": [
    {
      "id": "e3b0c44298fc1c149afbf4c8996fb924...",
      "bmc_name": "bmc1",
      "resource_path": "/redfish/v1/Systems/System.Embedded.1/Bios",
      "attribute": "ProcTurboMode",
      "type": "boolean",
      "read_only": false,
      "oem": false,
      "current_value": true
    }
  ]
}
```

Notes:
- The legacy query form `GET /api/bmcs/settings?name={bmc-name}[&resource=...&search=...&oem=...&page=...&page_size=...]` is still supported for backward compatibility.

Current limitations (to be expanded in future milestones):
- AttributeRegistry, ActionInfo, and full constraints (enums, ranges) are not yet fully resolved
- Apply/preview flows are tracked under Configuration Profiles and will be linked to auditing

### Configuration Profiles

Shoal can capture, version, compare, and export/import Redfish settings as Configuration Profiles. These endpoints are JSON-only and require authentication (session cookie or basic auth).

Key concepts:
- A profile has versioned entries. Each entry targets a `resource_path` (an `@odata.id`) and an `attribute` (dot-notated for nested keys like `HTTPS.Port` or `Attributes.LogicalProc`).
- Snapshot records the current settings from a BMC into a new profile version.
- Preview compares a profile version to a live BMC.
- Diff compares two profile versions.
- Export/Import makes profiles portable as JSON.

Endpoints:
- `GET /api/profiles` — List profiles
- `POST /api/profiles` — Create a profile `{name, description}`
- `GET /api/profiles/{id}` — Profile detail
- `GET /api/profiles/{id}/versions` — List versions
- `GET /api/profiles/{id}/versions/{version}` — Get a specific version
- `POST /api/profiles/{id}/versions` — Create new version with entries
- `GET /api/profiles/{id}/preview?bmc={name}[&version=N]` — Compare desired vs. current BMC values
- `POST /api/profiles/{id}/apply` — Apply profile version to a BMC (dry‑run or execute)
- `POST /api/profiles/{id}/export` — Export `{profile, versions:[...]}` (defaults to latest version when body is `{}`)
- `POST /api/profiles/import` — Import `{profile, versions}` (creates or updates)
- `POST /api/profiles/snapshot?bmc={name}` — Create a new version from live settings
- `POST /api/profiles/diff` — Compare two versions `{left:{profile_id,version}, right:{profile_id,version}}`

Examples:

Snapshot current settings into a new profile:
```bash
curl -s -u admin:admin \
  -X POST "http://localhost:8080/api/profiles/snapshot?bmc=bmc1" \
  -H "Content-Type: application/json" \
  -d '{"name":"baseline-lab","description":"Baseline captured from bmc1"}' | jq .
```

Preview differences against a BMC:
```bash
curl -s -u admin:admin \
  "http://localhost:8080/api/profiles/<profile-id>/preview?bmc=bmc1" | jq .
```

Apply a profile to a BMC (dry-run and execute):

```bash
# Dry-run (preview planned PATCH requests and summary)
curl -s -u admin:admin \
  -X POST "http://localhost:8080/api/profiles/<profile-id>/apply" \
  -H "Content-Type: application/json" \
  -d '{
        "bmc": "bmc1",
        "dryRun": true,
        "continueOnError": false
      }' | jq .

# Execute (issue PATCH requests via proxy with auditing)
curl -s -u admin:admin \
  -X POST "http://localhost:8080/api/profiles/<profile-id>/apply" \
  -H "Content-Type: application/json" \
  -d '{
        "bmc": "bmc1",
        "dryRun": false,
        "continueOnError": true
      }' | jq .
```

Apply request body:

```json
{
  "bmc": "bmc-name",
  "dryRun": true,
  "continueOnError": false,
  "version": 0
}
```

- `bmc`: Target BMC name (required)
- `dryRun`: When true, returns planned requests only; no changes are made
- `continueOnError`: When false, stops on first failed request; when true, continues
- `version`: Optional version number; defaults to latest when omitted or 0

Dry-run response (shape):

```json
{
  "profile_id": "...",
  "version": 1,
  "bmc": "bmc1",
  "dry_run": true,
  "requests": [
    {
      "resource_path": "/redfish/v1/Managers/Manager.Embedded.1/NetworkProtocol",
      "http_method": "PATCH",
      "request_url": "https://bmc.example/redfish/v1/Managers/Manager.Embedded.1/NetworkProtocol",
      "request_body": {"HTTPS": {"Port": 8443}},
      "apply_time_preference": "",
      "entries": [ { /* profile entries merged into this request */ } ]
    }
  ],
  "same": [ /* entries that already match */ ],
  "unmatched": [ /* entries without a current value match */ ],
  "summary": {"total_entries": 10, "request_count": 3, "same": 6, "unmatched": 1}
}
```

Execute response (adds per-request results):

```json
{
  "dry_run": false,
  "requests": [ /* same as above */ ],
  "results": [
    {"target_path": "/redfish/v1/Managers/.../NetworkProtocol", "status_code": 200, "ok": true, "body": "..."}
  ],
  "summary": {"request_count": 3, "success": 2, "failed": 1}
}
```

Execution details:
- BIOS settings are sent to the `/Settings` subresource with an `Attributes` root, aligning with Redfish BIOS semantics.
- Requests are grouped by target path and merged to minimize round-trips.
- Execution uses the same proxy path as normal Redfish requests, so credentials and audit logging are consistently applied. The initiating user is attributed in audit records under the `apply_profile` action.
- Large response bodies are truncated in the API response for readability; full details remain in audit records (subject to redaction/truncation policies).

RBAC:
- Operator or Administrator role required to call the apply endpoint.
- Non-admins can execute applies; audit body visibility remains restricted per RBAC in audit views.

Diff two profile versions:
```bash
curl -s -u admin:admin \
  -X POST "http://localhost:8080/api/profiles/diff" \
  -H "Content-Type: application/json" \
  -d '{
        "left":  {"profile_id": "<id>", "version": 1},
        "right": {"profile_id": "<id>", "version": 2}
      }' | jq .
```

Export a profile (latest version by default):
```bash
curl -s -u admin:admin \
  -X POST "http://localhost:8080/api/profiles/<profile-id>/export" \
  -H "Content-Type: application/json" \
  -d '{}' | jq .
```

Import a profile JSON (creates a new profile unless IDs match existing objects):
```bash
curl -s -u admin:admin \
  -X POST "http://localhost:8080/api/profiles/import" \
  -H "Content-Type: application/json" \
  -d @exported-profile.json | jq .
```

**SessionService API:**
- `POST /redfish/v1/SessionService/Sessions` - Create authentication session (unauthenticated)
- `GET /redfish/v1/SessionService` - SessionService root (requires auth)
- `GET /redfish/v1/SessionService/Sessions` - List active sessions (requires auth)
- `GET /redfish/v1/SessionService/Sessions/{id}` - Get a specific session (requires auth)
- `DELETE /redfish/v1/SessionService/Sessions/{id}` - Delete a session (logout) (requires auth)

Example usage:
```bash
# Create a session (returns X-Auth-Token header)
curl -s -X POST http://localhost:8080/redfish/v1/SessionService/Sessions \
  -H "Content-Type: application/json" \
  -d '{"UserName":"admin","Password":"admin"}' -D -

# List sessions using the token
curl -s -H "X-Auth-Token: ${TOKEN}" http://localhost:8080/redfish/v1/SessionService/Sessions | jq .

# Get a specific session
curl -s -H "X-Auth-Token: ${TOKEN}" http://localhost:8080/redfish/v1/SessionService/Sessions/{id} | jq .

# Delete a session (logout)
curl -s -H "X-Auth-Token: ${TOKEN}" -X DELETE http://localhost:8080/redfish/v1/SessionService/Sessions/{id} -D -
```

**AggregationService API (DMTF Standard):**
- `GET /redfish/v1/AggregationService` - AggregationService resource
- `GET /redfish/v1/AggregationService/ConnectionMethods` - List connection methods
- `POST /redfish/v1/AggregationService/ConnectionMethods` - Add new BMC connection
- `GET /redfish/v1/AggregationService/ConnectionMethods/{id}` - Get specific connection method
- `DELETE /redfish/v1/AggregationService/ConnectionMethods/{id}` - Remove BMC connection

#### DMTF Standard AggregationService

Shoal implements the DMTF Redfish AggregationService standard, which provides a native Redfish-compliant way to manage aggregated BMC connections. This is the recommended approach for programmatic BMC management.

**Adding a BMC via AggregationService:**
```bash
curl -X POST http://localhost:8080/redfish/v1/AggregationService/ConnectionMethods \
  -H "Content-Type: application/json" \
  -H "X-Auth-Token: <token>" \
  -d '{
    "Name": "Production Server BMC",
    "ConnectionMethodType": "Redfish",
    "ConnectionMethodVariant.Address": "192.168.1.100",
    "ConnectionMethodVariant.Authentication": {
      "Username": "admin",
      "Password": "password"
    }
  }'
```

**Testing the AggregationService:**
```bash
# Use the included Python test script
python3 scripts/test_aggregation_service.py --help

# Test without adding a BMC (useful if no real BMC available)
python3 scripts/test_aggregation_service.py --skip-add

# Test with custom BMC address
python3 scripts/test_aggregation_service.py --bmc-address 192.168.1.200
```

**Benefits of AggregationService:**
- Standards-compliant DMTF Redfish implementation
- Automatic aggregation of managers and systems collections
- Transparent integration with existing Redfish clients
- Read-only cached data for fast response times
- Action passthrough to original BMCs for power operations

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
python3 build.py test

# Run tests with coverage reporting
python3 build.py coverage

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

## Audit Logging

Shoal records an audit trail for proxied Redfish operations and other actions.

- UI: Navigate to `/audit` (link visible to admins). Filter by BMC, user, action, method, path substring, HTTP status range, and date range. Results render a table and provide a JSON export link.
- Per-BMC view: On the BMC details page (`/bmcs/details?name=...`), a "Changes" tab shows audits scoped to that BMC with the same filters and an export link. Non-admins see metadata only; admins see full request/response bodies.

- API: `GET /api/audit` supports filters and a limit parameter:
  - `bmc`: exact BMC name
  - `user`: exact username
  - `action`: e.g., `proxy`, `power`, `apply_profile`
  - `method`: HTTP method (e.g., `GET`, `POST`)
  - `path`: substring match on request path
  - `status_min`, `status_max`: HTTP code bounds
  - `since`, `until`: ISO dates `YYYY-MM-DD` (`until` inclusive of that day)
  - `limit`: number of rows (default 100, max 500)

Endpoints:
- `GET /api/audit?...` — list recent audit entries matching filters (request/response bodies truncated in list views)
- `GET /api/audit/{id}` — full audit record by ID

Notes:
- Sensitive fields in JSON payloads are redacted before storage; very large bodies are truncated.
- All audit endpoints and the `/audit` UI require `admin` role.

```bash
# Full validation (all quality gates)
python3 build.py validate

# Individual quality checks
python3 build.py fmt     # Code formatting
python3 build.py lint    # Static analysis
python3 build.py test    # Test execution
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
