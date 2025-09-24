# 006: Change Tracking and Auditing

## Summary
Track all setting changes initiated via Shoal, including exact Redfish requests/responses, timestamps, users, and resulting BMC state.

## Goals
- Maintain an append-only audit log of configuration operations.
- Preserve exact `@odata.id` paths, HTTP methods, and JSON payloads.
- Link changes to originating user, BMC, and optional profile.
- Provide search and export (JSONL/CSV) for audits.

## Events Tracked
- Discovery snapshots (read-only) – optional.
- Profile apply start/finish; per-request results.
- Ad-hoc single-setting edits.

## Audit Record Schema
- `id`, `timestamp`, `user_id`, `bmc_id`
- `operation` (discovery|apply-profile|set-attribute)
- `resource_path`, `attribute` (if applicable)
- `http_method`, `request_url`, `request_headers`, `request_body`
- `response_status`, `response_headers`, `response_body` (truncated with size limits)
- `profile_id` (nullable)
- `result` (success|partial|failed)
- `latency_ms`

## Storage & Retention
- SQLite table partitioned by time index; optional max retention and size caps.
- Large body fields stored compressed; redact secrets (passwords, tokens) proactively.

## API
- `GET /api/audit?bmc={name}&op=&from=&to=&q=`
- `GET /api/audit/{id}` → full record.
- `POST /api/audit/export` → streaming download of filtered set.

## UI
- Per-BMC “Changes” tab with timeline.
- Drill-down to a request showing exact path and payload diffs (pre/post).
- Filters by operation, status, user, timeframe.

## Security & Privacy
- Redact known secret fields in both requests and responses.
- RBAC: only admins can view bodies; operators may see metadata only.

## Test Plan
- Ensure secret redaction logic through unit tests.
- Integration tests verifying records are written for simulated applies.

## Open Questions
- Default retention and export limits; should these be configurable per deployment?
- Scope of redaction: which fields beyond passwords (e.g., API keys, LDAP binds)?
- RBAC granularity: who can view raw bodies vs. metadata-only?
- Do we also track read-only discovery events by default, or opt-in?
