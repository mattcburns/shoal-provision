# 009: Expanded Resource Coverage for Network and Storage

## Summary
This milestone expands on the settings discovery and enrichment framework established in `004` and `008`. The goal is to extend coverage to include network (`EthernetInterfaces`) and storage (`Storage` controllers, volumes, and drives) resources within a system. This will allow Shoal to provide a more comprehensive view of a server's configurable settings.

## Goals
- Discover and persist settings from `EthernetInterfaces` resources.
- Discover and persist settings from `Storage` collections, including associated `StorageControllers`, `Volumes`, and `Drives`.
- Apply the existing attribute registry and `ActionInfo` enrichment logic from `008` to these newly discovered settings.
- Ensure the existing UI and API surfaces can display and filter these new settings without requiring new endpoints.

## Non-Goals
- Implementation of complex storage operations (e.g., creating RAID volumes, managing virtual disks). This design focuses on discovering and displaying existing settings, not executing complex actions.
- Deep configuration of network switch hardware. The focus is on the server's own network interfaces.

## Inputs & Surfaces
The discovery process will be extended to probe the following Redfish paths:

- **Network Interfaces:**
  - `Systems/<id>/EthernetInterfaces` (Collection)
  - `Systems/<id>/EthernetInterfaces/<id>` (Individual NIC)
- **Storage Subsystems:**
  - `Systems/<id>/Storage` (Collection)
  - `Systems/<id>/Storage/<id>` (Individual Storage Subsystem)
  - `Systems/<id>/Storage/<id>/StorageControllers/<id>`
  - `Systems/<id>/Storage/<id>/Volumes` (Collection)
  - `Systems/<id>/Storage/<id>/Volumes/<id>`
  - `Systems/<id>/Storage/<id>/Drives` (Collection)
  - `Systems/<id>/Storage/<id>/Drives/<id>`

## Data Mapping to Internal Model
The existing `pkg/models.SettingDescriptor` model is sufficient to store settings from these new resources. No schema changes are required.

Examples of settings we expect to discover include:

- **Network (`EthernetInterfaces`):**
  - `DHCPv4.Enabled`, `DHCPv6.OperatingMode`
  - `StaticIPv4Addresses`, `StaticIPv6Addresses`
  - `VLAN.Enabled`, `VLAN.Id`
  - `LinkStatus`, `SpeedMbps` (as read-only attributes)
- **Storage (`Storage`, `StorageControllers`, `Drives`):**
  - `RAID.Enable`, `Controller.Mode`
  - `Drive.WriteCache`, `Drive.EncryptionStatus`
  - `Security.EraseOnDelete`
  - `Identifiers.DurableName` (as a read-only attribute)

## Discovery Algorithm
The discovery logic in `internal/bmc/service.go` will be updated to traverse these new collections.

1.  **Extend Resource Probing:** The main discovery function will be modified to iterate through the `EthernetInterfaces` and `Storage` collections found on a `System` resource.
2.  **Recursive Discovery:** For each member of these collections, the system will recursively call the resource discovery function, which already handles `@Redfish.Settings`, attributes, and actions.
3.  **Apply Enrichment:** The existing enrichment logic (`enrichDescriptors`) will be applied to the settings found in these new resources. No changes to the enrichment functions themselves are anticipated, as they are designed to be generic.
4.  **Persistence:** The newly discovered and enriched `SettingDescriptor` objects will be upserted into the database using the existing `UpsertSettingDescriptors` method.

## API Impact
- **No New Endpoints:** The new settings will be automatically included in the responses from the existing settings endpoints:
  - `GET /api/bmcs/{name}/settings`
  - `GET /api/bmcs/{name}/settings/{descriptor_id}`
- **Filtering:** The `?resource=...` query parameter can be used to filter for the new resource types. For example:
  - `?resource=EthernetInterfaces`
  - `?resource=Storage`

## UI Impact
- The "Settings" tab on the BMC details page will automatically display settings from network and storage devices.
- The resource path filter on the UI may need to be enhanced to better handle the deeper and more complex paths of these new resources. A tree-view or grouped filter could be considered to improve usability.

## Testing Strategy
- **Mock Server Updates:** The `httptest` server used in integration tests will be updated with handlers for the new Redfish paths (`EthernetInterfaces`, `Storage`, etc.).
- **Mock Payloads:** These handlers will serve realistic JSON payloads, including attributes, `@Redfish.Settings` annotations, and `Actions` with associated `ActionInfo` links.
- **New Tests:** New test cases will be added to `internal/bmc/service_test.go` to:
  - Verify that settings from `EthernetInterfaces` are discovered and correctly enriched.
  - Verify that settings from `Storage` controllers and drives are discovered and enriched.
  - Confirm that filtering by the new resource paths works as expected.

## Milestones
1.  **Network Interface Discovery:** Implement the logic to traverse `EthernetInterfaces` collections and discover their settings.
2.  **Storage Discovery:** Implement the logic to traverse `Storage` collections and their sub-resources (`StorageControllers`, `Drives`, `Volumes`).
3.  **UI Enhancements:** Improve the filtering or grouping on the settings UI to better accommodate the new resource types.
4.  **Validation:** Add comprehensive tests and run the full validation pipeline to ensure correctness and prevent regressions.
