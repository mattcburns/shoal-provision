# 004: Redfish Settings Discovery

## Summary
Discover configurable settings exposed by BMC Redfish implementations, normalize their schema, and expose a queryable model for downstream features (profiles, change tracking, UI).

## Goals
- Enumerate configurable settings and current values for a given BMC.
- Capture metadata: setting path, type, allowed values, constraints, writeability, and Redfish actions needed.
- Support vendor variance: DMTF standard resources plus common OEM extensions (e.g., Dell, HPE, Lenovo, Supermicro).
- Provide stable internal representation for persistence and UI.

## Non-Goals (for this phase)
- Applying changes (covered in separate design).
- Cross-BMC abstraction of vendor-unique semantics (we will preserve OEM details but not fully unify semantics yet).

## Key Concepts
- Setting: A leaf that is writable through a Redfish PATCH/POST/Action with a stable `@odata.id` path.
- Descriptor: Metadata object describing a Setting (name, description, type, enum, min/max, pattern, readOnly, units, dependencies).
- Source: The Redfish resource that declares the setting (schema, attribute registry, BIOS/Manager/Network/Storage config endpoints, or OEM sections).

## Redfish Surfaces To Inspect
- Systems → BIOS Settings: `/redfish/v1/Systems/<id>/Bios/Settings`, AttributeRegistry reference.
- Managers → Network/LDAP/Users/DateTime: typical Settings and Actions.
- EthernetInterfaces: VLAN, DHCP, IPv4/IPv6 config where writable.
- Storage/Controllers: Write cache, RAID policy where applicable.
- Attribute Registries: `/redfish/v1/Registries/*` including `@Redfish.Settings` and OEM.
- Action targets: `Actions` members with allowable values.

## Discovery Strategy
1. Entry points: probe Systems, Managers, Chassis for `@Redfish.Settings` and known writable endpoints.
2. Follow `@Redfish.Settings` to locate `SettingsObject` and `SupportedApplyTimes`.
3. Resolve AttributeRegistry and RegistryEntries to enumerate attributes, constraints, and display strings.
4. For actions, read `AllowableValues` from ActionInfo where available.
5. Fetch current values from the corresponding resource (e.g., `Bios`, `ManagerNetworkProtocol`).
6. Normalize to internal `SettingDescriptor` + `SettingValue` models.

## Data Model (internal)
- `SettingDescriptor`:
  - `id`: stable key (hash of `bmc_id + @odata.id + attribute name`).
  - `bmc_id`
  - `resource_path` (`@odata.id`)
  - `attribute` (e.g., `ProcTurboMode`)
  - `display_name`, `description`
  - `type` (string, integer, boolean, enum, array, object)
  - `enum_values` (if enum)
  - `min`, `max`, `pattern`, `units`
  - `read_only`, `oem` (bool + vendor tag)
  - `apply_times` (from `SupportedApplyTimes`)
  - `action_target` (if set via Action)
- `SettingValue`:
  - `descriptor_id`
  - `current_value`
  - `source_timestamp`

## Persistence
- Add tables to store descriptors and latest values keyed by BMC.
- Index by `bmc_id` and `resource_path` for fast lookups.

## API (read-only)
- `GET /api/bmcs/{name}/settings` → list descriptors + current values.
- `GET /api/bmcs/{name}/settings?resource=<path>` → filter by resource.
- `GET /api/bmcs/{name}/settings/{descriptor_id}` → single descriptor + value.

## UX Considerations
- Group by resource (BIOS, Network, Storage).
- Show current value, allowed values, and when changes apply (on reset vs. immediate).
- Indicate OEM settings clearly.

## Edge Cases & Risks
- Large registries (hundreds of BIOS attributes) – paginate and cache.
- OEM variations without AttributeRegistry – fall back to introspection of schema/examples.
- Permissions/access – settings may be visible but not writable for given credentials.

## Validation Plan
- Start with Mock BMC fixtures covering: BIOS with AttributeRegistry, Manager DateTime, NIC DHCP toggle, OEM attribute.
- Add integration tests that verify enumeration counts and key descriptors.

## Milestones
1. Implement probe+collect pipeline for Systems/Managers.
2. Registry resolution and normalization to descriptors.
3. Persistence and read-only API endpoints.
4. UI list/detail views (read-only).

## Open Questions
- How stable are descriptor IDs across firmware updates (path/attribute changes)?
- Which OEMs should we target first for registry support (Dell/HPE/Lenovo/Supermicro)?
  A: Dell and Supermicro.
- Cache strategy and TTL for discovery results to balance freshness vs. cost?
  A: No cache for now.
- Should we include read-only attributes in the model for completeness?
