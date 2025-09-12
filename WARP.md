
## License and Dependencies

This project is licensed under the GNU Affero General Public License v3.0 (AGPLv3).

**CRITICAL: All new dependencies added to this project MUST be compatible with AGPLv3.**

- **Compatible Licenses**: MIT, Apache 2.0, BSD (2-clause and 3-clause), ISC, and other permissive licenses.
- **Incompatible Licenses**: Any license that is more restrictive than AGPLv3 or has conflicting terms. This includes many commercial licenses and some copyleft licenses that are not GPL-compatible.

### Dependency Vetting Process

Before adding any new dependency, you MUST:

1.  **Identify the license**: Check the `LICENSE` file, `go.mod` file, and project documentation.
2.  **Verify AGPLv3 compatibility**: Use a license compatibility chart or consult with a legal expert if unsure.
3.  **Update the dependency list**: Add the new dependency and its license to a `DEPENDENCIES.md` file (to be created).

Failure to comply with this requirement may result in the new dependency being removed.

# WARP.md

This file provides guidance to WARP (warp.dev) when working with code in this repository.

> ⚠️ **IMPORTANT: Git Branch Requirement**
> **All changes MUST be made in a feature branch. Never commit directly to main.**
> Before starting any work, create a new branch: `git checkout -b feature/<description>`
> See the [Git Workflow Requirements](#git-workflow-requirements) section for details.

## Repository Status

This is a new repository that has not yet been fully initialized with source code. The project structure and development workflow are still being established.

## Getting Started

Since this is a new project, begin by:

1. Examining the README.md to understand the project's intended purpose
2. Checking git history for any initial commits that establish the project structure
3. Looking for configuration files that indicate the project type (package.json, Cargo.toml, go.mod, etc.)

## Development Commands

*This section will be populated once the project's build system and tools are established.*

## Architecture Overview

*This section will be populated once the codebase structure is established.*

## Development Commands

**Note: All build and test commands use the Python-based automation system (`build.py`) for simplicity and cross-platform compatibility.**

### Primary Build Commands
```bash
# Full validation pipeline (recommended for development)
python build.py validate

# Build binary only
python build.py build

# Run tests only
python build.py test

# Run tests with coverage reporting
python build.py coverage

# Format code
python build.py fmt

# Run linting/static analysis
python build.py lint

# Clean build artifacts
python build.py clean

# Download and verify dependencies
python build.py deps
```

### Running the Application
```bash
# Run the built binary with default settings
./build/shoal

# Run with custom settings
./build/shoal -port 8080 -db shoal.db -log-level debug
```

### Development Workflow
The `python build.py validate` command runs the complete pipeline:
1. **Prerequisites Check** - Verifies Go installation and module setup
2. **Dependencies** - Downloads and verifies Go module dependencies
3. **Format** - Formats Go code using `go fmt`
4. **Lint** - Runs static analysis (golangci-lint or go vet)
5. **Tests** - Executes all tests with coverage reporting
6. **Security** - Runs security scan (gosec if available)
7. **Build** - Creates optimized binary in `build/` directory
8. **Build Info** - Generates build metadata JSON file

### Manual Go Commands (for reference)
```bash
# These are handled by build.py, but can be run manually:
go mod download
go build -ldflags="-s -w" -o shoal ./cmd/shoal
go test -v ./...
go test -coverprofile=coverage.out ./...
go fmt ./...
go vet ./...
go mod tidy
```

## Architecture Overview

### High-Level Architecture

Shoal implements a Redfish aggregator service with the following key architectural patterns:

**Layered Architecture**:
- **Presentation Layer**: Web interface (`internal/web`) and REST API (`internal/api`)
- **Business Logic Layer**: BMC management (`internal/bmc`), Authentication (`internal/auth`)
- **Data Access Layer**: Database operations (`internal/database`)
- **Infrastructure Layer**: Logging (`internal/logging`), Models (`pkg/models`)

**Request Flow**:
1. HTTP requests hit either the web interface or Redfish API endpoints
2. Authentication middleware validates credentials or session tokens
3. Business logic processes the request (BMC proxy, power control, etc.)
4. Database layer handles persistence of BMC info and sessions
5. Responses are formatted appropriately (HTML for web, JSON for API)

**Key Design Decisions**:
- **Single Binary Deployment**: All dependencies are embedded or use Go standard library
- **SQLite Database**: Lightweight, file-based storage requiring no external database server
- **Embedded Templates**: HTML templates are defined in Go code for single binary deployment
- **Proxy Pattern**: API requests to BMCs are proxied through with credential injection
- **Standard HTTP Patterns**: Uses Go's standard HTTP multiplexer and middleware pattern

### Core Components

**Database Layer** (`internal/database`):
- SQLite-based with automatic migrations
- Handles BMCs, users, and session storage
- Uses prepared statements and transactions for safety
- Implements connection pooling through Go's sql.DB

**Authentication System** (`internal/auth`):
- Supports both HTTP Basic Auth and Redfish session tokens
- Session tokens expire after 24 hours
- Uses crypto/rand for secure token generation
- Implements Redfish-compliant error responses

**BMC Management** (`internal/bmc`):
- HTTP client configured for BMC communication (insecure TLS, 30s timeout)
- Request proxying with automatic credential injection
- Power control action support
- Connection testing functionality

**API Layer** (`internal/api`):
- Redfish v1.6.0 compliant service root and collections
- Regex-based routing for BMC proxy requests
- Session management endpoints
- Structured error responses following Redfish standard

**Web Interface** (`internal/web`):
- Server-side rendered HTML using Go's html/template
- Embedded CSS for styling (no external dependencies)
- CRUD operations for BMC management
- Power control interface

### Security Architecture

- **Credential Storage**: BMC passwords encrypted with AES-256-GCM when encryption key is provided
- **Session Management**: Cryptographically secure session tokens with expiration
- **BMC Communication**: HTTPS with certificate verification disabled (common for BMCs)
- **Input Validation**: Basic form validation and SQL injection protection via prepared statements
- **Encryption**: Optional AES-256-GCM encryption for BMC passwords with PBKDF2 key derivation

## Git Workflow Requirements

**CRITICAL: All development work MUST be done in feature branches. Direct commits to main/master are NOT allowed.**

### Branch Creation Requirements

Before making ANY changes to the codebase, you MUST:

1. **Create a new feature branch** from the current main branch:
   ```bash
   # Ensure you're on the latest main branch
   git checkout main
   git pull origin main

   # Create and checkout a new feature branch
   git checkout -b feature/<description>
   # OR for bug fixes:
   git checkout -b fix/<description>
   # OR for documentation:
   git checkout -b docs/<description>
   ```

2. **Use descriptive branch names** that clearly indicate the work being done:
   - ✅ Good: `feature/add-redis-caching`, `fix/auth-token-expiry`, `docs/api-endpoints`
   - ❌ Bad: `feature1`, `fix`, `updates`, `changes`

3. **Make all changes in the feature branch**:
   ```bash
   # Verify you're on the feature branch
   git branch --show-current

   # Make your changes and commit regularly
   git add .
   git commit -m "Descriptive commit message"
   ```

4. **Push the branch to remote** when ready:
   ```bash
   git push -u origin <branch-name>
   ```

5. **Run full validation** before considering the work complete:
   ```bash
   python build.py validate
   ```

### Branch Naming Conventions

- `feature/<description>` - New features or enhancements
- `fix/<description>` - Bug fixes
- `hotfix/<description>` - Urgent production fixes
- `docs/<description>` - Documentation updates
- `refactor/<description>` - Code refactoring without functional changes
- `test/<description>` - Test additions or improvements
- `chore/<description>` - Build process, dependencies, or tooling updates

### Commit Message Guidelines

Every commit should have a clear, descriptive message:
- First line: Brief summary (50 chars or less)
- Optional body: Detailed explanation (wrap at 72 chars)
- Reference issues when applicable: "Fixes #123"

Example:
```
Add BMC connection retry logic

Implements exponential backoff for failed BMC connections
with a maximum of 3 retry attempts. This improves reliability
when dealing with temporarily unresponsive BMCs.

Fixes #42
```

### Before Merging

Before any branch can be merged:
1. All tests must pass: `python build.py test`
2. Code must be formatted: `python build.py fmt`
3. Full validation must succeed: `python build.py validate`
4. README.md must be updated if applicable
5. DEPLOYMENT.md must be updated if build process or deployment changed
6. Branch must be up-to-date with main

## Development Principles

- Focus on simplicity, understandability and readability when writing and designing
  code. Think of the target developer that will be working with this code as a
  junior developer.
- Prefer to use the standard library and write our own libraries instead of bringing
  in external dependencies. Minimize external dependencies as much as possible.
- Deployment of code should be as simple as possible. Minimize or eliminate any
  external dependencies so we can deploy as a simple single binary.
- Write tests for all code. All of these tests should pass before completing tasks.
- **Always work in feature branches** - Never commit directly to main/master.

## Documentation Standards

### Documentation Maintenance Requirements

**CRITICAL: Different audiences require different documentation approaches.**

#### README.md (Human Developers & Users)
- **Purpose**: Onboarding, usage guides, troubleshooting for human developers
- **Style**: Clear, approachable, tutorial-focused with explanations
- **Content**: Getting started guides, feature overviews, usage examples, troubleshooting
- **Updates Required**: When user-facing features, build processes, or usage patterns change

#### WARP.md (AI Agents & Automation)
- **Purpose**: Explicit instructions for AI agents, Copilot, and automation workflows
- **Style**: Unambiguous, directive, process-focused with precise requirements
- **Content**: Workflow requirements, validation steps, automation instructions, agent behavior rules
- **Updates Required**: When development processes, validation requirements, or agent workflows change

### Update Requirements by Change Type

**New Features:**
- README.md: Add user-focused feature descriptions, usage examples, configuration options
- WARP.md: Add validation requirements, testing procedures, agent workflow updates

**Build/Deployment Changes:**
- README.md: Update build instructions for human developers
- WARP.md: Update automation commands, validation pipeline, agent build procedures

**API Changes:**
- README.md: Update endpoint documentation, authentication examples for users
- WARP.md: Update validation requirements, testing procedures, agent interaction patterns

**Development Process Changes:**
- README.md: Update contribution guidelines for human developers
- WARP.md: Update workflow requirements, validation steps, agent behavior rules

### Writing Standards

**For README.md (Humans):**
- Write for developers new to the project
- Use clear, tutorial-style explanations
- Include context and reasoning behind decisions
- Provide troubleshooting guidance
- Use encouraging, helpful tone

**For WARP.md (AI Agents):**
- Write explicit, unambiguous instructions
- Use directive language ("MUST", "NEVER", "ALWAYS")
- Focus on process requirements and validation steps
- Minimize ambiguity with precise specifications
- Use imperative, command-focused tone

## Completed Work Summary

### Initial Project Setup (Completed)
- ✅ Go module initialization with minimal dependencies (only modernc.org/sqlite)
- ✅ Directory structure following Go project conventions
- ✅ Main application entry point with graceful shutdown
- ✅ Structured logging using Go's slog with JSON output

### Database Layer (Completed)
- ✅ SQLite database with automatic migrations
- ✅ BMC, User, and Session models
- ✅ CRUD operations for all entities
- ✅ Prepared statements and proper error handling

### Authentication System (Completed)
- ✅ HTTP Basic Authentication support
- ✅ Redfish session token authentication
- ✅ Session creation/deletion endpoints
- ✅ Authentication middleware
- ✅ Redfish-compliant error responses

### Redfish API (Completed)
- ✅ Service root endpoint (/redfish/v1/)
- ✅ Managers and Systems collections
- ✅ BMC proxy request handling
- ✅ Session management endpoints
- ✅ Redfish v1.6.0 compliant responses

### BMC Management (Completed)
- ✅ BMC proxy service with credential injection
- ✅ Power control actions (On, ForceOff, etc.) with dynamic system ID discovery
- ✅ Connection testing
- ✅ HTTPS client with insecure TLS for BMCs
- ✅ Flexible URL construction supporting standard BMCs and mock servers with path prefixes
- ✅ Automatic protocol handling (defaults to HTTPS when not specified)
- ✅ Comprehensive test coverage for URL construction and power control

### Web Interface (Completed)
- ✅ Dashboard with BMC status overview
- ✅ BMC management (full CRUD: add, edit, delete, power control)
- ✅ Server-side rendered HTML templates
- ✅ Embedded CSS styling
- ✅ Form-based BMC configuration with validation
- ✅ Connection testing functionality
- ✅ Comprehensive test coverage for all web handlers

### Build & Test Automation (Completed)
- ✅ Python-based build automation system (`build.py`)
- ✅ Cross-platform build support (Linux, Windows, macOS on x86_64 and ARM64)
- ✅ Platform-specific binary naming and output
- ✅ `build-all` command for building all supported platforms
- ✅ `--platform` flag for building specific target platforms
- ✅ Comprehensive test suite (unit and integration tests)
- ✅ Automated build validation pipeline
- ✅ Code formatting and linting integration
- ✅ Coverage reporting with HTML output
- ✅ Security scanning integration (gosec)
- ✅ Build artifact management
- ✅ Git integration for build metadata
- ✅ Production deployment documentation (DEPLOYMENT.md)

### Security Enhancements (Completed)
- ✅ AES-256-GCM encryption for BMC passwords
- ✅ PBKDF2 key derivation with 100,000 iterations
- ✅ Environment variable and command-line flag support for encryption key
- ✅ Transparent encryption/decryption in database operations
- ✅ Backward compatibility with existing plaintext passwords
- ✅ Comprehensive test coverage for encryption functionality

### User Management System (Completed)
- ✅ Multi-user support with role-based access control (Admin, Operator, Viewer)
- ✅ bcrypt password hashing for user accounts
- ✅ User CRUD operations (create, read, update, delete)
- ✅ Web-based login/logout with session cookies
- ✅ User profile and password change functionality
- ✅ Admin-only user management interface
- ✅ Role-based middleware for protecting routes
- ✅ Default admin account creation on first run
- ✅ Comprehensive test coverage for authentication flows

### Release & Deployment Automation (Completed)
- ✅ Automated GitHub Actions release workflow triggered by version tags
- ✅ Multi-platform binary builds (Linux, Windows, macOS for x86_64 and ARM64)
- ✅ Full validation pipeline before release (tests, linting, security checks)
- ✅ Automated release notes generation with installation instructions
- ✅ SHA256 checksum generation for security verification
- ✅ Smart pre-release detection based on tag naming conventions

## Next Steps for Development

### High Priority
1. **Documentation Maintenance**
   - **ALWAYS update README.md** with any changes to features, build process, or usage
   - **ALWAYS update DEPLOYMENT.md** with any changes to build process, deployment, or production configuration
   - Keep API documentation current with endpoint changes
   - Update troubleshooting guides based on user feedback
   - Maintain build instruction accuracy across platforms

2. **Security Enhancements**
   - ~~Encrypt BMC passwords in database~~ ✅ Completed
   - ~~Implement proper user management system~~ ✅ Completed
   - Add HTTPS support for the aggregator service
   - Input sanitization and validation improvements

3. **API Completeness**
   - Implement missing Redfish endpoints (SessionService, AggregationService)
   - Add support for Redfish EventService (webhooks)
   - Implement proper Redfish schema validation

### Medium Priority
4. **Web Interface Enhancements**
   - ~~BMC edit functionality~~ ✅ Completed
   - ~~User management interface~~ ✅ Completed
   - System monitoring dashboard
   - Bulk operations for multiple BMCs

5. **Operational Features**
   - Configuration file support
   - Health check endpoints
   - Metrics/Prometheus integration
   - Background BMC health monitoring

6. **BMC Discovery**
   - Network scanning for BMC discovery
   - mDNS/DNS-SD service discovery
   - SSDP discovery support

### Low Priority
7. **Advanced Features**
   - BMC firmware update orchestration
   - Certificate management for BMCs
   - LDAP/SSO integration
   - Multi-tenant support

8. **Performance & Scalability**
   - Connection pooling for BMC connections
   - Request caching
   - Database optimization and indexing
   - Horizontal scaling considerations

### Development Guidelines for Next Steps

- **Always write tests first** - Follow TDD principles
- **Maintain single binary deployment** - Avoid external runtime dependencies
- **Keep it simple** - Don't over-engineer solutions
- **Follow Redfish specification** - Ensure API compliance
- **Security by design** - Consider security implications of new features
- **Backward compatibility** - Don't break existing API contracts
