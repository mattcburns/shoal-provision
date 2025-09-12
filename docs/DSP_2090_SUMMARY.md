Redfish Aggregation Rules for Compute Expansion Modules
This document outlines the rules and constraints for an AI agent responsible for performing Redfish aggregation of a compute expansion module's data model into a primary system's data model. The goal is to present a single, unified Redfish interface to the end user.

1. Core Principle: Implicit Aggregation
The primary function is to perform "implicit aggregation" as defined in the Redfish Specification. The system's Baseboard Management Controller (BMC) will discover and integrate the Redfish data model from the compute expansion module's BMC, making the module's resources appear as part of the main system.

2. Common Aggregation Patterns
These general rules apply to all resource aggregation operations:

Full Resource Aggregation: When a resource (singleton or collection member) is aggregated from the module, all of its subordinate resources, actions, and OEM extensions must also be aggregated.

ID Collision Avoidance: The Id property of aggregated resources must be adjusted if it conflicts with an existing Id in the system BMC's data model.

ID Naming Convention: To resolve collisions, a recommended practice is to prepend a unique identifier to the incoming resource's Id. This identifier should be descriptive, such as the Id of the top-level Chassis resource representing the compute expansion module (e.g., UBB-GPU1 instead of GPU1).

3. Resource-Specific Aggregation Rules
3.1. Chassis (/redfish/v1/Chassis)
The ChassisCollection represents the physical containers of the system.

Action: Aggregate all Chassis resources from the compute expansion module's ChassisCollection into the system BMC's ChassisCollection.

Linking:

The system BMC must create a Contains link in one of its own Chassis resources that points to the top-most Chassis resource of the expansion module.

The system BMC must create a ContainedBy link in the top-most Chassis of the expansion module that points back to the containing Chassis in the system.

3.2. Systems (/redfish/v1/Systems)
The ComputerSystemCollection represents the functional view of the system's compute resources. The module's ComputerSystem is treated as a container of additional resources for the main system, not as a separate system.

Action: Merge, do not append. The resources from the module's ComputerSystem instance must be merged into the system BMC's primary ComputerSystem instance.

Result: The ComputerSystemCollection in the system BMC should not increase in size.

Subordinate Collections: Collections subordinate to the ComputerSystem resource (e.g., ProcessorCollection, MemoryCollection) must be extended with the members from the corresponding collections in the module's ComputerSystem.

3.3. Managers (/redfish/v1/Managers)
The ManagerCollection represents the management controllers (BMCs) in the system.

Action: Aggregate all Manager resources from the expansion module's ManagerCollection into the system BMC's ManagerCollection.

Constraint: Pay close attention to Id property collisions. It is highly likely the module's BMC will have an Id of BMC. This must be renamed to something unique (e.g., UBB-BMC).

3.4. Fabrics (/redfish/v1/Fabrics)
The FabricCollection represents data planes like PCIe or cache coherency networks. Aggregation logic depends on the fabric type.

Case 1: Shared Fabric (e.g., PCIe)

Context: The module contains elements of a fabric that extends into the main system. The module BMC only has a partial view.

Action: Merge. The resources within the module's Fabric (e.g., members of the SwitchCollection) must be merged into the corresponding Fabric resource on the system BMC.

Result: The FabricCollection in the system BMC should not increase in size for shared fabrics.

Case 2: Self-Contained Fabric (e.g., internal GPU-to-GPU link)

Context: The fabric exists entirely within the expansion module.

Action: Aggregate. The entire Fabric resource from the module must be added as a new member to the system BMC's FabricCollection.

3.5. Telemetry Service (/redfish/v1/TelemetryService)
The aggregation strategy for the TelemetryService depends on the capabilities of the system BMC.

Scenario 1: System BMC has no Telemetry Service

Action: Aggregate the entire TelemetryService resource from the module.

Scenario 2: System BMC has a basic Telemetry Service

Action: Aggregate the members of the following collections from the module into the system's corresponding collections:

MetricDefinitionCollection

MetricReportDefinitionCollection

MetricReportCollection

Scenario 3: System BMC has an advanced, user-configurable Telemetry Service

Action: Only aggregate the MetricDefinition resources from the module to expand the list of available metrics.

Constraint: The system BMC retains control over MetricReportDefinition and MetricReport creation. Do not aggregate these.

4. Services to Exclude from Aggregation
The following services are used for internal communication between the system BMC and the module BMC. They must not be aggregated or exposed to the end user.

EventService (/redfish/v1/EventService)

SessionService (/redfish/v1/SessionService)

AccountService (/redfish/v1/AccountService)
