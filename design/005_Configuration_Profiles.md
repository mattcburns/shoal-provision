# 005: Configuration Profiles

## Summary
Capture, store, compare, and apply collections of BMC Redfish settings as versioned profiles. Profiles are portable JSON documents that reference discovered settings (004) and are future-enriched by registry metadata (008). Enable snapshotting current state and reapplying to the same or similar hardware.

## Goals
- Snapshot current settings into a portable profile.
- Diff two profiles and show setting deltas.
- Validate applicability before apply (hardware/model/firmware guards).
- Support apply-time semantics (immediate vs. on-reset) and batched changes.
- CRUD for profiles, versions, entries, and assignments to targets.
- Import/export JSON for portability.

## Non-Goals
- Cross-vendor semantic translation beyond attribute-level mapping.
- Attribute Registry enrichment beyond what is already available from discovery (see 008).

## Data Model
- `Profile`:
  - `id`, `name`, `description`, `created_at`, `updated_at`, `created_by`
  - `hardware_selector` (make/model/family/sku; optional regex)
  - `firmware_ranges` (min/max or allowed set)
- `ProfileVersion`:
  - `id`, `profile_id`, `version`, `created_at`, `notes`
  - `entries`: array of `ProfileEntry`
- `ProfileEntry`:
  - `resource_path` (`@odata.id`)
  - `attribute`
  - `desired_value` (JSON value)
  - `apply_time_preference` (optional: `Immediate`, `OnReset`, etc.)
  - `oem_vendor` (optional)
- `Assignment`:
  - `id`, `profile_id`, `version`, `target`: `bmc:<name>` or `group:<group_name>`

Database tables (SQLite):
- `profiles (id TEXT PK, name TEXT UNIQUE, description TEXT, created_at, updated_at, created_by TEXT, hardware_selector TEXT, firmware_ranges_json TEXT)`
- `profile_versions (id TEXT PK, profile_id TEXT, version INTEGER, created_at, notes TEXT)`
- `profile_entries (id TEXT PK, profile_version_id TEXT, resource_path TEXT, attribute TEXT, desired_value_json TEXT, apply_time_preference TEXT, oem_vendor TEXT)`
- `profile_assignments (id TEXT PK, profile_id TEXT, version INTEGER, target_type TEXT, target_value TEXT)`

## Snapshot Flow
1. Use 004 API to fetch descriptors and current values for a BMC in the selected resource scope.
2. Filter to writable attributes by default; allow include read-only for documentation.
3. Emit `Profile` with `ProfileEntry[]` keyed by `(resource_path, attribute)`; values copied from `current_value`.

## Diffing
- Compare two profiles entry-by-entry using `(resource_path, attribute)` tuple.
- Produce a `ProfileDiff` with: added, removed, changed entries.

## Apply Flow
1. Validate BMC against `hardware_selector` and `firmware_ranges`.
2. Group entries by resource/action target and apply-time.
3. Generate Redfish PATCH/Action requests with minimal payload.
4. Respect `SupportedApplyTimes`; if `OnReset`, schedule a reminder.
5. Collect per-request results and aggregate status.

Note: If write/apply is split into its own design (006), this section becomes “apply planning” returning a plan without executing changes.

## Safety & Rollback
- Pre-apply: capture an automatic baseline snapshot profile.
- If failures occur, surface partial apply details and provide revert-to-baseline capability.

## Persistence
- Store profiles, versions, entries, and assignments; track provenance.

## API
- `GET /api/profiles` → list profiles with latest version summary
- `POST /api/profiles` → create profile `{name, description, scope}` or from snapshot
- `GET /api/profiles/{id}` → detail with versions
- `POST /api/profiles/{id}/versions` → create new version with entries
- `GET /api/profiles/{id}/versions/{version}` → get specific version
- `POST /api/profiles/{id}/assignments` → assign profile/version to targets
- `GET /api/profiles/{id}/preview?bmc={name}` → compute diff vs current BMC settings
- `POST /api/profiles/{id}/export` → export profile+version as JSON
- `POST /api/profiles/import` → import a profile JSON (create or update)

## UX Considerations
- Snapshot wizard from a BMC detail page.
- Diff viewer with filters (changed only, OEM).
- Apply preview: grouped requests, apply-time notes, estimated reboot needs.

## Testing Strategy
- DB tests for CRUD of profiles, versions, entries, and assignments.
- API tests for create/list/detail/version/assign/import/export.
- Preview tests using httptest BMC with known current values to verify diffs.
- Unit tests for snapshot serialization/deserialization.
- Apply simulator against Mock BMC; assert minimal PATCH payloads (if included).

## Open Questions
- Minimum metadata for `hardware_selector` to ensure safe applicability (exact match vs. regex)?
- How to handle settings that require reboot vs. immediate apply in batch operations?
  A: Warn for reboot settings, attempt to batch them together.
- Should profiles support variables/templating for environment-specific values (e.g., NTP/DNS)?
- Cross-firmware compatibility policy: warn, block, or attempt apply with guardrails?

## Milestones
1. Persistence + CRUD APIs (profiles, versions, entries, assignments) — database layer [done]; web/API endpoints [done]; API tests (CRUD, versions, assignments) [done].
2. Preview/diff endpoint using discovered settings (004) — [pending].
3. Import/export JSON — [pending].
4. Apply planning (optional here; may move to 006 apply design).
