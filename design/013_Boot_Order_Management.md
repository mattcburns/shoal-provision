# 013: Boot Order Management

## Summary

This document outlines the design for implementing boot order management within Shoal. The goal is to allow users to view the current boot order, see available boot options, and set a new boot order for a given system via its Redfish interface. This functionality will be integrated into the existing Settings Discovery and Configuration Profiles features.

## Redfish Background

The Redfish `ComputerSystem` resource provides the necessary properties for managing boot configuration. The key properties are:

-   **`Boot.BootOrder`**: An ordered array of strings representing the persistent boot sequence. The values in this array must be members of the `BootOrder@Redfish.AllowableValues` list.
-   **`BootOrder@Redfish.AllowableValues`**: A top-level property on the `ComputerSystem` resource that lists all valid boot source targets (e.g., `Pxe`, `Hdd`, `Cd`, `Usb`).
-   **`Boot.BootSourceOverrideTarget`**: The boot source to use for the *next* boot only.
-   **`Boot.BootSourceOverrideEnabled`**: Enables the one-time override (`Once`, `Continuous`).

For persistent boot order changes, a `PATCH` request is sent to the `ComputerSystem` resource with a new `BootOrder` array.

**Example `ComputerSystem` JSON:**

```json
{
    "@odata.id": "/redfish/v1/Systems/Sys1",
    "Id": "Sys1",
    "Name": "My Server",
    "BootOrder@Redfish.AllowableValues": [
        "Pxe",
        "Hdd",
        "Cd",
        "Usb",
        "BiosSetup"
    ],
    "Boot": {
        "BootOrder": [
            "Hdd",
            "Pxe"
        ],
        "BootSourceOverrideEnabled": "Disabled",
        "BootSourceOverrideTarget": "None"
    }
}
```

## Implementation Plan

This feature will be built upon the existing settings discovery and configuration profile framework.

### 1. Discovery (`internal/bmc/service.go`)

The `DiscoverSettings` function will be enhanced to inspect the `ComputerSystem` resource for boot order information.

1.  **Probe `ComputerSystem`:** After fetching the `ComputerSystem` resource (which is already done to get the `systemID`), the logic will check for the presence of the `Boot` object and the `BootOrder@Redfish.AllowableValues` array.
2.  **Create `SettingDescriptor`:** If found, a new `models.SettingDescriptor` will be created to represent the boot order setting.
    -   **`Attribute`**: `Boot.BootOrder`
    -   **`DisplayName`**: "Boot Order"
    -   **`Description`**: "Sets the persistent boot device order."
    -   **`Type`**: `StringArray` (a new type to be handled by the UI)
    -   **`EnumValues`**: Populated from the `BootOrder@Redfish.AllowableValues` array.
    -   **`CurrentValue`**: The current `Boot.BootOrder` array, marshaled as a JSON string.
    -   **`ActionTarget`**: The `@odata.id` of the `ComputerSystem` resource itself, as this is the target for the `PATCH` request.

### 2. API (`internal/web/web.go`)

No new API endpoints are required. The existing settings and profiles API will support this feature.

-   **`GET /api/bmcs/{name}/settings`**: Will automatically return the new "Boot Order" `SettingDescriptor` along with other discovered settings.
-   **Configuration Profiles**: Users can snapshot the "Boot Order" setting into a profile. Applying a profile with this setting will trigger a `PATCH` request to the `ComputerSystem` resource with the specified `Boot.BootOrder` array.

### 3. UI (`internal/web/web.go` templates)

The UI will be updated to provide a user-friendly interface for managing the boot order.

1.  **Display Setting**: The BMC "Settings" page will list the "Boot Order" setting.
2.  **Custom UI Control**: The UI will detect the `StringArray` type for the `Boot.BootOrder` attribute and render a specialized control instead of a simple text field. This control will be an interactive, re-orderable list (e.g., using drag-and-drop).
    -   The list will be populated with the available boot options from `EnumValues`.
    -   The current order will be reflected.
    -   Users can drag and drop items to change the order.
3.  **Profile Integration**: When adding this setting to a Configuration Profile, the UI will store the user-ordered array as the desired value.

## Milestones

1.  **Backend**: Update `DiscoverSettings` to identify and model the `Boot.BootOrder` property.
2.  **Frontend**: Implement a new UI control for `StringArray` types to allow re-ordering of the boot devices.
3.  **Integration**: Ensure the setting can be successfully snapshotted into and applied from a Configuration Profile.
4.  **Testing**: Add a unit test with a mock BMC that exposes the `BootOrder` properties and verify that the setting is discovered correctly. Update UI tests to validate the new control.
