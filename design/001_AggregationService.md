# AggregationService API Design

**Author:** Gemini
**Date:** 2025-09-20
**Status:** Proposed

## Abstract

This document outlines the design for implementing the DMTF Redfish `AggregationService` within the Shoal project. This service will provide a standard, interoperable way to add, remove, and manage connections to external Baseboard Management Controllers (BMCs), aggregating their resources into a single management interface.

## Background

The Shoal project needs a robust and standardized mechanism to manage a dynamic collection of BMCs. Instead of creating a proprietary or OEM-specific API, we will implement the `AggregationService` as defined by the Redfish specification. This ensures compliance with industry standards and allows any Redfish-compliant client to manage the aggregation of resources.

The `AggregationService` acts as a factory and registry for connections to other Redfish services. By creating a `ConnectionMethod` resource, a client instructs the service to connect to an external BMC. The service then populates its own `Managers` and `ComputerSystems` collections with read-only representations of the resources from the managed BMC.

## API Design

The API will be structured according to the Redfish specification.

### Service Root

The service root at `/redfish/v1/` will be updated to include a navigation property to the `AggregationService`.

```json
{
    "@odata.type": "#ServiceRoot.v1_15_0.ServiceRoot",
    "Id": "RootService",
    "Name": "Root Service",
    "RedfishVersion": "1.18.0",
    ...
    "AggregationService": {
        "@odata.id": "/redfish/v1/AggregationService"
    },
    ...
}
```

### AggregationService Endpoint

-   **Path:** `/redfish/v1/AggregationService`
-   **Description:** Provides details about the `AggregationService` and links to its collections, primarily the `ConnectionMethods`.

### ConnectionMethods Collection

-   **Path:** `/redfish/v1/AggregationService/ConnectionMethods`
-   **Description:** This collection contains the `ConnectionMethod` resources, each representing a connection to an external BMC.

## Core Operations

### Adding a BMC

To add a new BMC to be managed, a client will `POST` a new `ConnectionMethod` resource to the `ConnectionMethods` collection.

-   **Action:** `POST`
-   **Path:** `/redfish/v1/AggregationService/ConnectionMethods`
-   **Request Body:**

    ```json
    {
        "@odata.type": "#ConnectionMethod.v1_0_0.ConnectionMethod",
        "Name": "Managed BMC 1",
        "ConnectionMethodType": "Redfish",
        "Address": "192.168.1.100",
        "Authentication": {
            "Username": "admin",
            "Password": "password"
        }
    }
    ```

Upon receiving this request, the service will:
1.  Validate the request body.
2.  Attempt to connect to the specified `Address` using the provided credentials.
3.  If successful, create a new `ConnectionMethod` resource and return a `201 Created` with a `Location` header pointing to the new resource.
4.  Begin the process of aggregating the remote BMC's `Managers` and `ComputerSystems` into the local collections.

### Removing a BMC

To remove a managed BMC, a client will `DELETE` the corresponding `ConnectionMethod` resource.

-   **Action:** `DELETE`
-   **Path:** `/redfish/v1/AggregationService/ConnectionMethods/{id}`

Upon receiving this request, the service will:
1.  Remove the `ConnectionMethod` resource.
2.  Disconnect from the external BMC.
3.  Remove the aggregated resources from the local `Managers` and `ComputerSystems` collections.

## Implementation Details

-   **Resource Aggregation and Persistence:** The existing passthrough model for `Managers` and `ComputerSystems` will be replaced with a persistent, aggregated collection. When a `ConnectionMethod` is created, the service will fetch the `Manager` and `ComputerSystem` resources from the remote BMC and store them locally as read-only copies. These aggregated resources will be clearly linked back to their source `ConnectionMethod`. All `GET` requests to `/redfish/v1/Managers` and `/redfish/v1/ComputerSystems` will be served from this local, aggregated cache.

-   **Data Synchronization:** For the initial implementation, data from managed BMCs will be fetched once when the `ConnectionMethod` is created. The aggregated resources will represent a snapshot in time. A refresh mechanism (such as polling or subscribing to events) to keep the data current is a future consideration.

-   **Action Passthrough:** While `GET` requests for aggregated resources are served from the local cache, any actions (`POST` requests, such as `Reset` on a `ComputerSystem`) will be passed through to the original BMC for execution. The service will act as a proxy for these operations.

-   **Persistence:** `ConnectionMethod` resources, including encrypted credentials, must be stored persistently.

## Internal Architecture

To ensure consistency and minimize code duplication, a core, shared module for BMC management will be developed.

-   **Shared BMC Management Logic:** A central module (e.g., a "BMC Manager" or "BMC Service") will encapsulate all the business logic for managing BMCs. This includes:
    -   Adding and removing BMCs.
    -   Storing and managing `ConnectionMethod` details and credentials.
    -   Fetching data from BMCs to populate the aggregated collections.
    -   Proxying actions (e.g., `Reset`) to the correct BMC.

-   **Thin API Layers:** Both the existing UI's backend and the new Redfish `AggregationService` endpoint will be implemented as thin API layers. They will not contain any business logic themselves but will instead translate incoming requests (from the UI or a Redfish client) into calls to the shared BMC Management module.

This layered architecture ensures that there is a single source of truth for all BMC management operations, regardless of which interface initiates them.

## Future Considerations

-   **Support for other ConnectionMethodTypes:** The initial implementation will focus on `Redfish`, but the design should be extensible to support other types like `SNMP` or `IPMI` in the future.
-   **Certificate Management:** For secure Redfish connections, handling of HTTPS certificates for managed BMCs will be necessary.
