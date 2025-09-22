# Audit Logging

Shoal records an audit trail for proxied Redfish operations and other significant actions.

## Audit Log UI

- **Main Audit View**: Navigate to `/audit` (the link is visible to administrators).
  - Filter by BMC, user, action, method, path substring, HTTP status, and date range.
  - Results are displayed in a table and can be exported as JSON.
- **Per-BMC View**: On the BMC details page (`/bmcs/details?name=...`), a "Changes" tab shows audits scoped to that specific BMC.
  - Non-admins see metadata only; admins see full request/response bodies.

## Audit Log API

- `GET /api/audit`: List recent audit entries.
  - **Filters**: `bmc`, `user`, `action`, `method`, `path`, `status_min`, `status_max`, `since`, `until`, `limit`.
- `GET /api/audit/{id}`: Retrieve a full audit record by its ID.

**Notes:**
- Sensitive fields in JSON payloads (like `Password`) are redacted before being stored.
- Very large request/response bodies are truncated to save space.
- All audit endpoints and the `/audit` UI require an `admin` role.
