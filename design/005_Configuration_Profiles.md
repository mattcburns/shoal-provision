# 005: Configuration Profiles

## Summary
Capture, store, compare, and apply collections of BMC Redfish settings as versioned profiles. Enable snapshotting current state and reapplying to the same or similar hardware.

## Goals
- Snapshot current settings into a portable profile.
- Diff two profiles and show setting deltas.
- Validate applicability before apply (hardware/model/firmware guards).
- Support apply-time semantics (immediate vs. on-reset) and batched changes.

## Non-Goals
- Cross-vendor semantic translation beyond attribute-level mapping.

## Profile Model
- `Profile`:
  - `id`, `name`, `created_at`, `created_by`
  - `hardware_selector` (make/model/family/sku; optional regex)
  - `firmware_ranges` (min/max or allowed set)
  - `notes`
- `ProfileEntry`:
  - `resource_path` (`@odata.id`)
  - `attribute`
  - `desired_value`
  - `apply_time` (when supported)
  - `oem_vendor` (optional)

## Snapshot Flow
1. Run Discovery for target BMC to fetch descriptors and current values.
2. Filter to writable attributes by default; allow include read-only for documentation.
3. Emit `Profile` with `ProfileEntry[]` using descriptor keys.

## Diffing
- Compare two profiles entry-by-entry using `(resource_path, attribute)` tuple.
- Produce a `ProfileDiff` with: added, removed, changed entries.

## Apply Flow
1. Validate BMC against `hardware_selector` and `firmware_ranges`.
2. Group entries by resource/action target and apply-time.
3. Generate Redfish PATCH/Action requests with minimal payload.
4. Respect `SupportedApplyTimes`; if `OnReset`, schedule a reminder.
5. Collect per-request results and aggregate status.

## Safety & Rollback
- Pre-apply: capture an automatic baseline snapshot profile.
- If failures occur, surface partial apply details and provide revert-to-baseline capability.

## Persistence
- Store profiles and their entries in DB; track provenance (BMC, time, user).

## API
- `POST /api/profiles` create from BMC snapshot or JSON upload.
- `GET /api/profiles` list; `GET /api/profiles/{id}` detail.
- `POST /api/profiles/{id}/apply?bmc={name}` apply to a BMC.
- `POST /api/profiles/diff` accept two IDs (or payloads) and return diff.

## UX Considerations
- Snapshot wizard from a BMC detail page.
- Diff viewer with filters (changed only, OEM).
- Apply preview: grouped requests, apply-time notes, estimated reboot needs.

## Test Plan
- Unit tests for snapshot serialization/deserialization.
- Apply simulator against Mock BMC; assert minimal PATCH payloads.
- Diff correctness with edge cases (missing attrs, type changes).

## Open Questions
- Minimum metadata for `hardware_selector` to ensure safe applicability (exact match vs. regex)?
- How to handle settings that require reboot vs. immediate apply in batch operations?
  A: Warn for reboot settings, attempt to batch them together.
- Should profiles support variables/templating for environment-specific values (e.g., NTP/DNS)?
- Cross-firmware compatibility policy: warn, block, or attempt apply with guardrails?
