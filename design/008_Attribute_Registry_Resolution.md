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
   - Optional: `?refresh=true` query parameter on the list/detail read paths to bypass caches and re-run discovery/enrichment on-demand (see Refresh Semantics). This is additive and backwards-compatible.

## Caching & Performance
- Per-BMC TTL cache for registry payloads and action metadata. Default TTL: 15 minutes (configurable in code constant; can be promoted to flag later if needed).
- Store enriched fields in DB; reads serve from DB without re-resolving unless:
   - Cache expired, or
   - Caller provides `?refresh=true`, or
   - Descriptor is missing enrichment fields (bootstrap/first run).
- Limit concurrent registry fetches per BMC to avoid stampedes (simple mutex/once around resolution is sufficient for now).
- All Redfish fetches go through the existing proxy path to benefit from authentication + auditing.

### Refresh Semantics
- `refresh=true` forces a fresh resolve of registries and actions for the targeted BMC/resource, bypassing in-memory caches. DB will be upserted with the latest enrichment.
- RBAC: viewers can read cached data; forcing `refresh=true` requires operator or admin privileges.
- Use sparingly; large registries can be multi-hundred KB.

## Edge Cases
- Missing/invalid registry references: return basic descriptors as in 004.
- Partial OEM coverage: enrich what is known; mark OEM vendor when detected.
- Large registries: avoid fetching repeatedly; paginate only at UI layer if needed.
- ActionInfo present but not attribute-specific: do not attempt to map AllowableValues unless a parameter can be unambiguously associated with a single attribute.
- BIOS attributes that only accept changes via Action POST: set `action_target` and leave `enum_values` unchanged unless ActionInfo provides explicit allowable values.
- If both Registry enum candidates and ActionInfo allowable values exist and differ, prefer ActionInfo for the action-specific context and keep Registry values as secondary reference.

## Testing Strategy
- Unit tests for registry parsing (DMTF BIOS-like payload with Attributes array).
- Tests for `SupportedApplyTimes` extraction from `@Redfish.Settings`.
- Tests for Actions + ActionInfo (allowable values extraction).
- Integration-style tests using httptest servers that expose:
  - BIOS with `AttributeRegistry` and `@Redfish.Settings`
  - Manager NetworkProtocol with an Action plus `ActionInfo`
  - An OEM registry sample (Dell or Supermicro) stub
   - Note for tests: when targeting the local httptest server, use explicit `http://` in BMC URLs to avoid the default-https normalization in `buildBMCURL()`.

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

### ActionInfo Parameter Matching
- Map `ActionInfo.Parameters[*]` to attributes when there is a clear key match. Prefer these hints in order:
   1) A parameter name that exactly equals the attribute name.
   2) A JSON pointer/path or property name that clearly references the attribute within the Attributes bag.
   3) Vendor documentation hints (only for targeted OEM cases we explicitly support in tests).
- If no unambiguous mapping exists, do not attach `AllowableValues` to the descriptor.

### OEM Handling Nuance
- Detect OEM registries by vendor fields within the registry payload (e.g., `OwningEntity`, `RegistryPrefix`, or vendor namespaces). Set `oem=true` and `oem_vendor` accordingly.
- Do not guess semantics or normalize OEM attributes; surface vendor-provided metadata as-is.
- Prefer DMTF fields when present; augment with OEM fields only when DMTF-equivalent is absent.

### Security & Auditing
- All registry and ActionInfo fetches are proxied and audited per existing mechanisms (request/response truncated/redacted as configured).
- Do not persist secrets; only persist metadata and non-sensitive enumerations/ranges.
- Respect existing role gates on settings endpoints; require operator/admin for `refresh=true` as noted above.

### Failure Behavior
- Network/parse errors during enrichment must not break settings reads; fall back to previously persisted descriptors or the basic 004 descriptors.
- Log at debug level with concise context (BMC, resource, error category) without dumping full payloads.
