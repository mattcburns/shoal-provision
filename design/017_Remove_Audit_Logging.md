# 017: Remove Audit Logging Feature

- **State**: Implemented
- **Author**: Matthew Burns
- **Date**: 2025-09-24

## Summary

This design removes the Audit Logging feature from Shoal across all layers: data storage, business logic, API, web UI, and tests. It also removes the per‑BMC “Changes (Audit)” UI from the BMC Details page.

## Motivation

Audit logging added non‑essential complexity relative to Shoal’s core goals (aggregation and BMC management). Removing it:

- **Reduces complexity**: fewer tables, handlers, code paths, and tests.
- **Improves maintainability**: less branching logic and feature‑specific plumbing.
- **Focuses effort**: accelerates work on core Redfish aggregation and configuration.

## Scope

In scope for removal:
- Database schema, queries, and helpers for audit records.
- API endpoints for listing and reading audit records.
- Web routes, handlers, and UI for audit, including the “Changes (Audit)” tab.
- Model types and constants specific to audit logging.
- Unit/integration tests that asserted audit behavior.

Out of scope:
- Any replacement observability or traceability mechanism.
- Historical migration of audit data to an external system.

## Implementation Overview

Changes were applied surgically to keep unrelated behavior stable.

### Data Layer (`internal/database`)
- Removed audit table creation from migrations. New databases will not create an audit table.
- Removed audit CRUD/query helpers and filtering logic.
- Existing databases may retain an `audits` table; it is now unused. A drop is optional and can be performed manually if desired (see Migration below).

### Business Logic (`internal/bmc`)
- Removed audit recording hooks and any calls that attempted to persist audit entries during proxy or service operations.

### Web/API Layer (`internal/web`)
- Removed the `/audit` page and related handlers.
- Removed audit API routes (e.g., `/api/audit`, `/api/audit/{id}`); these now return 404 as they are no longer registered.
- Removed the “Changes (Audit)” tab and any related placeholders from the BMC Details page.
- Updated navigation to remove any Audit links.

### Models (`pkg/models`)
- Deleted the `AuditRecord` type and any `AuditAction*` constants.

### Ancillary Refactor
- Removed `pkg/contextkeys` and replaced usages with simple string keys:
    - User context key: `"user"`
    - Refresh flag key: `"refresh"`
    This reduced a tiny package dependency and simplified context usage.

### Tests
- Removed audit‑specific tests across packages (database, bmc, web).
- Updated remaining tests to no longer reference audit UI/API.

## API & UI Changes

- Removed endpoints:
    - `GET /api/audit`
    - `GET /api/audit/{id}`
    - (Any JSON/JSONL audit export endpoints)
- Removed UI:
    - `/audit` page and nav link
    - Per‑BMC “Changes (Audit)” tab and related elements

These are breaking removals for external consumers who depended on the audit endpoints. A major or minor version bump with release notes is recommended.

## Database Migration

- New installs: `Migrate` no longer creates any audit table.
- Existing installs: the previous audit table (if present) is unused. Optional cleanup:
    - SQLite example:
        - `DROP TABLE IF EXISTS audits;` (table name from prior schema)
    - Back up the database before performing manual maintenance.

No automatic destructive migration is executed by Shoal for this removal.

## Security & Compliance Considerations

- Removing audit logs reduces traceability of user actions. If auditability is required in your environment, consider forwarding reverse proxy logs, database change logs, or integrating an external SIEM.

## Rollback Plan

- Revert the merge commit for this design.
- Restore `AuditRecord` model, database schema, web routes/handlers, and tests as per pre‑017 state.
- Reintroduce `pkg/contextkeys` or continue with string keys if preferred.

## Documentation Updates

- Remove or archive `docs/6_auditing.md` and any references in README or site navigation.
- Update user/admin documentation to reflect the absence of the Audit/Changes features.

## Acceptance Criteria

- Build succeeds across all modules.
- Navigating to `/audit` returns 404 (no route registered) and no Audit link appears in the UI.
- BMC Details page contains no “Changes (Audit)” tab.
- Models and database code contain no audit types or queries.
- Tests compile and run without referring to audit features.

## Release Notes (Draft)

- Removed: Audit Logging feature, including `/audit` UI and `/api/audit` endpoints.
- Removed: `AuditRecord` model and audit database storage.
- UI: “Changes (Audit)” tab removed from BMC Details.
- Developer note: `pkg/contextkeys` removed; use context keys `"user"` and `"refresh"` directly.
