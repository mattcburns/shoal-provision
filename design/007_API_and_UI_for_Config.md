# 007: API and UI for Config Management

## Summary
Define REST API endpoints and Web UI flows for discovering settings, creating/applying profiles, and auditing changes, including visibility into exact Redfish paths and payloads.

## API Endpoints

### Settings (Read-Only)
- `GET /api/bmcs/{name}/settings` → list settings with metadata and current values.
  - Query: `resource`, `oem`, `search`, `page`, `page_size`.
- `GET /api/bmcs/{name}/settings/{descriptor_id}` → detail.

### Profiles
- `POST /api/profiles` → create profile
  - Body: `{ name, notes, source: { bmc: <name> } | entries: [...] }`
- `GET /api/profiles` / `GET /api/profiles/{id}`
- `POST /api/profiles/{id}/apply` → apply to BMC
  - Body: `{ bmc: <name>, dryRun?: true, continueOnError?: false }`
- `POST /api/profiles/diff` → diff two profiles
  - Body: `{ leftId|left, rightId|right }`

### Auditing
- `GET /api/audit` with filters
- `GET /api/audit/{id}` full record (RBAC gated)
- `POST /api/audit/export`

### Redfish Path/Request Visibility
- For any setting detail or apply preview, include:
  - `resource_path` (`@odata.id`), `http_method`, `request_url`, `request_body`, `apply_time`.
- After apply, link to associated audit record IDs.

## UI Flows

### BMC Detail → Settings
- Tabs: Overview | Settings | Profiles | Changes
- Settings list: group by resource, search/filter, badges for OEM and apply-time.
- Setting detail: current value, allowed values, path, payload example for change.

### Create Profile (Snapshot)
- Wizard: Select BMC → Select categories (BIOS/Network/Storage) → Review → Save.

### Apply Profile
- Preview page: request groups with exact Redfish paths/payloads and apply-time notes.
- Execution view: progress per group; show response status; link to audit.

### Changes (Audit)
- Timeline of operations; drill-down to raw request/response with redactions.

## Error Handling
- Map common HTTP statuses to user-friendly messages; preserve raw bodies for auditing.
- Show remediation hints (e.g., required reboot, privilege errors).

## Performance
- Paginate settings; stream audit exports; cache discovery per BMC with TTL.

## Security
- Enforce RBAC on profile apply and audit views.
- Redact secret fields in UI; require elevated role to reveal raw bodies.

## Open Questions
- Pagination defaults and maximums for settings and audit endpoints?
- API versioning strategy as Redfish/OEM coverage expands?
- How to surface apply-time requirements consistently in UI (toasts vs. banners)?
- Cross-BMC portability heuristics for profiles.

## Milestones
1. Settings read-only API + UI tab.
2. Profiles snapshot/diff with apply preview (dry-run).
3. Apply execution + auditing integration.
