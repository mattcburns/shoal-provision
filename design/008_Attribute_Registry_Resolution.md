# 008: Attribute Registry Resolution & Actions Enrichment

## Summary
Enrich discovered settings using Redfish Attribute Registries and Action metadata. This adds human-friendly names/descriptions, constraints (enums, min/max, pattern, units), read-only flags, supported apply times, and action targets/allowable values. It builds on 004’s discovery and persistence without changing public APIs.

## Goals
- Resolve and parse Attribute Registries referenced by settings-bearing resources (e.g., BIOS, ManagerNetworkProtocol).
- Populate `SettingDescriptor` fields: `display_name`, `description`, `enum_values`, `min`, `max`, `pattern`, `units`, `read_only`.
- Capture `SupportedApplyTimes` when present via `@Redfish.Settings`.
- Discover Actions and (if exposed) `ActionInfo` allowable values; set `action_target` and enum candidates when applicable.
- Support OEM/vendor registries where feasible (priorities: Dell, Supermicro).

## Non-Goals
- Writing/applying settings (separate design).
- Full semantic unification of OEM attributes (we preserve OEM details, not normalize semantics across vendors).

## Inputs & Surfaces
- Resources already probed in 004 (initially):
  - `Systems/<id>/Bios` (and `/Settings`)
  - `Managers/<id>/NetworkProtocol` (and `/Settings`)
- Additional to probe (as we expand):
  - `Managers/<id>/DateTime`, `LDAP`, `Users` where settings/actions exist
  - `Systems/<id>/EthernetInterfaces/*` where writable (DHCP, VLAN, IPv4/IPv6)
  - `Systems/<id>/Storage/*` and controllers for policy toggles
- Registries collection: `/redfish/v1/Registries/*` and individual registry payloads
- ActionInfo (if linked from Actions via `@Redfish.ActionInfo` or similar)

## Data Mapping to Internal Model
Target model: `SettingDescriptor` (already in `pkg/models` and persisted):
- `display_name` ← Registry.DisplayName / AttributeName mapping
- `description` ← Registry.HelpText / LongDescription
- `type` ← Registry.Type (string, integer, boolean, array, object)
- `enum_values` ← Registry.Value or `AllowableValues` (from ActionInfo) when applicable
- `min`, `max` ← Registry.Minimum/Maximum
- `pattern` ← Registry.Pattern
- `units` ← Registry.Units
- `read_only` ← Registry.ReadOnly
- `apply_times` ← `@Redfish.Settings.SupportedApplyTimes` (array of strings)
- `action_target` ← Action target URI when setting requires an Action POST
- `oem`/`oem_vendor` ← true/vendor when derived from OEM registries or OEM sections

Note: Database schema already supports these fields (added in 004 persistence work).

## Discovery Algorithm
1. For each resource with `@Redfish.Settings`:
   - Parse `SettingsObject.@odata.id` and, if present, `SupportedApplyTimes` to pre-populate `apply_times`.
2. Resolve AttributeRegistry:
   - Many implementations provide a `AttributeRegistry` or `AttributeRegistry@Redfish.AllowableValues` field at the resource (e.g., `Bios`), or a link under `/Registries`.
   - Fetch the registry payload (DMTF or OEM schema). Typical BIOS registries expose `RegistryEntries.Attributes` array with entries having `AttributeName`, `DisplayName`, `HelpText`, `Type`, `ReadOnly`, `Minimum`, `Maximum`, `Value`, `Pattern`, `Units`.
   - Build an index keyed by `AttributeName` (or vendor-specific key).
3. Enrich descriptors:
   - For each discovered attribute/value pair from 004, look up the attribute in the registry index; populate the metadata fields above.
4. Actions/ActionInfo:
   - Inspect the resource `Actions` object. If an action is required to set a field (vendor or DMTF), capture the action path as `action_target`.
   - If an `ActionInfo` link is provided, resolve and parse `Parameters[*].AllowableValues` to augment `enum_values` for the relevant attribute.
5. OEM handling:
   - If the registry vendor or resource indicates OEM space, set `oem=true` and `oem_vendor` to the detected vendor (e.g., “Dell”, “Supermicro”).
6. Persist:
   - Upsert enriched descriptors and current values via existing DB methods.

## API Impact
- No new endpoints. Existing read-only endpoints from 004 now return enriched data:
  - `GET /api/bmcs/{name}/settings[?resource=...]`
  - `GET /api/bmcs/{name}/settings/{descriptor_id}`

## Caching & Performance
- Cache registry payloads per BMC for a TTL (e.g., 10–30 minutes) to avoid re-fetching large registries on each call.
- Store enriched fields in DB; subsequent reads can serve from DB without re-resolving unless cache expired or a refresh is requested.

## Edge Cases
- Missing/invalid registry references: return basic descriptors as in 004.
- Partial OEM coverage: enrich what is known; mark OEM vendor when detected.
- Large registries: avoid fetching repeatedly; paginate only at UI layer if needed.

## Testing Strategy
- Unit tests for registry parsing (DMTF BIOS-like payload with Attributes array).
- Tests for `SupportedApplyTimes` extraction from `@Redfish.Settings`.
- Tests for Actions + ActionInfo (allowable values extraction).
- Integration-style tests using httptest servers that expose:
  - BIOS with `AttributeRegistry` and `@Redfish.Settings`
  - Manager NetworkProtocol with an Action plus `ActionInfo`
  - An OEM registry sample (Dell or Supermicro) stub

## Milestones
1. BIOS Registry Enrichment: Resolve `AttributeRegistry` for BIOS; populate display/description/type/enums/min/max/pattern/units/read_only.
2. Apply-Times: Capture `SupportedApplyTimes` and persist into descriptors.
3. Actions/ActionInfo: Populate `action_target` and `enum_values` from allowable values where applicable.
4. Manager Network, EthernetInterfaces: Extend enrichment beyond BIOS (where registries/actions are available).
5. OEM Vendors: Add initial mapping/handling for Dell and Supermicro registries.
6. Caching: Add per-BMC registry cache with TTL.

## Implementation Notes
- Keep logic in `internal/bmc` (e.g., new helpers `resolveAttributeRegistry`, `enrichWithRegistry`, `resolveActionInfo`).
- Prefer standard library; no new dependencies unless strictly necessary and AGPLv3-compatible.
- Update existing DB upsert to include enriched fields (already supported by schema).
