# Design Doc: 003_UI_Detailed_BMC_Status

**Objective**: Create a detailed view in the web UI that allows users to drill down into a specific BMC from the BMC list and see detailed information.

## Feature Details

This feature will introduce a new page that displays comprehensive information about a selected BMC. The data will be fetched on-demand using a passthrough to the BMC's Redfish API. This approach avoids storing a large amount of detailed data in the Shoal database, ensuring that the information is always up-to-date.

### Data to be Displayed

The detailed view will include the following information:

*   **System Information**:
    *   Serial Number
    *   SKU
    *   Power State (On, Off, etc.)
*   **Network Interfaces**:
    *   List of NICs
    *   For each NIC:
        *   MAC Address
        *   IP Address (if available)
*   **Storage**:
    *   List of storage devices
    *   For each device:
        *   Model
        *   Serial Number
        *   Capacity
        *   Status (e.g., Healthy, Degraded)
*   **System Event Log (SEL)**:
    *   A view of the SEL, allowing users to see recent events and diagnose issues. This could be a paginated or searchable view.

### Data Retrieval

As mentioned, the data for this view will be retrieved by making passthrough calls to the BMC's Redfish API. This keeps the data fresh and reduces the storage load on the Shoal database. When a user navigates to the detailed view for a BMC, Shoal will make the necessary Redfish calls to the BMC in real-time.

### UI/UX

The UI will consist of a new page that is accessed by clicking on a BMC in the main BMC list. The information will be organized into logical sections for clarity.

*   A "Back to BMC List" link will be provided for easy navigation.
*   The SEL log may be displayed in a collapsible section or a separate tab to avoid cluttering the main view.