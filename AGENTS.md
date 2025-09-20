# AGENTS.md

This file provides guidance to AI agents, such as GitHub Copilot, when working with code in this repository.

## About this project

Shoal is a Redfish aggregator service with a layered architecture:
-   **Presentation Layer**: Web interface (`internal/web`) and REST API (`internal/api`).
-   **Business Logic Layer**: BMC management (`internal/bmc`) and authentication (`internal/auth`).
-   **Data Access Layer**: Database operations (`internal/database`).
-   **Infrastructure Layer**: Logging (`internal/logging`), Models (`pkg/models`).

It is designed for single-binary deployment with an embedded SQLite database.

### License and Dependencies

This project is licensed under the GNU Affero General Public License v3.0 (AGPLv3).

**CRITICAL: All new dependencies added to this project MUST be compatible with AGPLv3.**

-   **Compatible Licenses**: MIT, Apache 2.0, BSD, ISC.
-   **Incompatible Licenses**: Licenses more restrictive than AGPLv3.

Before adding a dependency, you must verify its license is compatible with AGPLv3.

## Tools

This section lists common commands for building, testing, and running the application. All commands use the Python-based automation system (`build.py`).

- `python build.py validate`: Run the full validation pipeline (recommended for development).
- `python build.py build`: Build the binary only.
- `python build.py test`: Run tests only.
- `python build.py coverage`: Run tests with coverage reporting.
- `python build.py fmt`: Format Go code.
- `python build.py lint`: Run linting and static analysis.
- `python build.py clean`: Clean build artifacts.
- `python build.py deps`: Download and verify dependencies.

To run the application:

- `./build/shoal`: Run the built binary with default settings.
- `./build/shoal -port 8080 -db shoal.db -log-level debug`: Run with custom settings.

## Tasks

This section outlines development tasks.

### High Priority

1.  **Documentation Maintenance**
    - **ALWAYS update README.md** with any changes to features, build process, or usage.
    - **ALWAYS update DEPLOYMENT.md** with any changes to build process, deployment, or production configuration.
    - Keep API documentation current with endpoint changes.
    - Update troubleshooting guides based on user feedback.
    - Maintain build instruction accuracy across platforms.

2.  **Security Enhancements**
    - Add HTTPS support for the aggregator service.
    - Improve input sanitization and validation.

3.  **API Completeness**
    - Implement missing Redfish endpoints (e.g., `SessionService`, `AggregationService`).
    - Add support for Redfish `EventService` (webhooks).
    - Implement proper Redfish schema validation.

### Medium Priority

1.  **Web Interface Enhancements**
    - Implement a system monitoring dashboard.
    - Add support for bulk operations for multiple BMCs.

2.  **Operational Features**
    - Add support for a configuration file.
    - Implement health check endpoints.
    - Add Metrics/Prometheus integration.
    - Implement background BMC health monitoring.

3.  **BMC Discovery**
    - Implement network scanning for BMC discovery.
    - Add mDNS/DNS-SD service discovery.
    - Add SSDP discovery support.

## Constraints

### Git Workflow

**CRITICAL: All development work MUST be done in feature branches. Direct commits to `master` are NOT allowed.**

1.  **Create a feature branch** before making any changes:
    ```bash
    git checkout -b feature/<description>
    ```
2.  **Use descriptive branch names**:
    - **Good**: `feature/add-redis-caching`, `fix/auth-token-expiry`
    - **Bad**: `feature1`, `fix`, `updates`
3.  **Run full validation** before considering work complete:
    ```bash
    python build.py validate
    ```

### Development Principles

-   Focus on simplicity, understandability, and readability.
-   Prefer the standard library over external dependencies.
-   Ensure deployment is as simple as possible (e.g., a single binary).
-   Write tests for all code.
-   **Always work in feature branches.**

### Documentation Standards

-   **README.md**: For human developers. Focus on onboarding, usage, and troubleshooting.
-   **AGENTS.md**: For AI agents. Provide explicit, unambiguous instructions and process requirements.
