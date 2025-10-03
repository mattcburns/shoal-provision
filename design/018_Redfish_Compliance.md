# 018: Completing Redfish Service Compliance for Shoal

Author: Matthew Burns
Status: Proposed
Date: 2025-09-26

## Summary

Shoal already exposes a useful Redfish-like surface: Service Root, SessionService (including Sessions), Managers and Systems collections, and AggregationService with ConnectionMethods. This document defines the remaining work to reach a solid baseline of Redfish compliance so common Redfish clients, SDKs, and the Redfish Service Validator interoperate cleanly.

The scope focuses on protocol-level requirements (OData metadata and headers), required/expected core resources (AccountService, Registries/SchemaStore), message registry-backed error reporting, and a few correctness fixes to existing payloads. It does not aim to implement the full Redfish universe (e.g., UpdateService, EventService, TaskService) in this milestone.

## Goals

- Provide an OData $metadata endpoint and host required Redfish schemas the service claims to implement.
- Serve Redfish Message Registries and SchemaStore so clients can resolve schema/registry references.
- Ensure headers and payload annotations follow Redfish requirements and conventions.
- Add AccountService with Accounts and Roles mapped to Shoal’s RBAC.
- Return registry-backed ExtendedInfo errors for better client diagnostics.
- Keep AggregationService and proxy behavior intact and standards-aligned.
- Add conformance-oriented tests and validation guidance.

## Non-goals

- Implement EventService, TaskService, UpdateService in this milestone (we will add stubs only if necessary for conformance gating).
- Implement full PATCH/ETag concurrency across all resources (we’ll define the contract and enable it where feasible later).
- Establish server-managed telemetry or subscriptions.

## Current State (as of 2025-09-26)

Implemented endpoints and behavior:
- Service Root: `/redfish/v1/`
- Version pass-through root: `/redfish/v1`
- SessionService: `/redfish/v1/SessionService` with `Sessions` collection and session POST under `/Sessions`
- AggregationService: `/redfish/v1/AggregationService` with `ConnectionMethods` collection and create/delete
- Managers and Systems collections at `/redfish/v1/Managers` and `/redfish/v1/Systems` with member URIs that proxy to managed BMCs (Managers/<bmc>, Systems/<bmc>)
- Redfish-style error object with `code` and `message`

Not yet present (high level):
- `$metadata` endpoint and schema hosting
- Message registries (Base, ResourceEvent, etc.) and Registries collection
- SchemaStore (JSON schema hosting) or links to where schemas are resolved
- `OData-Version: 4.0` header on Redfish responses
- `ServiceRoot` links for Registries/SchemaStore and stable UUID generation
- AccountService (`/redfish/v1/AccountService`, `/Accounts`, `/Roles`)
- `@Message.ExtendedInfo` in error bodies
- Optional protocol niceties: HEAD/OPTIONS/Allow, ETag/If-Match where PATCH is supported

## Compliance Baseline and References

We target compatibility with commonly used Redfish clients and the DMTF Redfish Service Validator for:
- ServiceRoot + navigation properties
- SessionService + Sessions (token issuance via POST)
- AccountService (accounts and roles)
- AggregationService and Collections
- Message registries availability and `$metadata` resolution
- Standard headers and OData annotations

Note: Redfish relies on OData v4 framing. In practice, serving compliant `$metadata` and the message registries is a frequent blocker for tools that expect them to exist.

## Requirements and Design

### 1) OData Metadata and Schema Hosting

Contract:
- GET `/redfish/v1/$metadata` returns an XML CSDL document describing schemas used by the service (ServiceRoot, SessionService/Session, Manager, ComputerSystem, AggregationService/ConnectionMethod, AccountService/Account/Role, and the Base/Message definitions).
- Optional: provide SchemaStore endpoints to host JSON schema files for the same resources.

Implementation approach:
- Embed a curated set of DMTF-published CSDL files that cover the resources Shoal exposes. Serve them as `$metadata` via a static, concatenated CSDL or as a minimal CSDL referencing per-schema files.
- Add a small handler (e.g., `internal/api/metadata.go`) that:
  - Serves `$metadata` with content-type `application/xml`.
  - Optionally serves a `SchemaStore` namespace under `/redfish/v1/SchemaStore/en/*.json` (files embedded via `internal/assets`).
- Link `@odata.context` values in payloads to the corresponding entity sets in `$metadata`.

Notes:
- Keep the schema set minimal and aligned to what we expose (avoid claiming support for resources we don’t implement).
- Choose a Redfish version line (e.g., 1.18.x) consistent with the schema set we embed. The ServiceRoot `RedfishVersion` should match.

### 2) Registries: Message Registries and Collection

Contract:
- Expose `/redfish/v1/Registries` (MessageRegistryFileCollection) and individual registry files for at least:
  - `Base` message registry
  - Any additional registries referenced by our error payloads
- Error payloads include `@Message.ExtendedInfo` entries that reference `MessageId`s from these registries.

Implementation approach:
- Embed official DMTF Base registry (JSON) and serve under a stable path such as `/redfish/v1/Registries/Base/Base.json` (and/or the common `en` locale pattern). Add a Registries collection that lists available registries with `@odata.id` links.
- Update error responses to include ExtendedInfo (see Section 5).

### 3) Protocol Headers and Annotations

Contract (applies to all Redfish JSON responses):
- Set `OData-Version: 4.0` header.
- Keep `Content-Type: application/json`.
- Preserve existing annotations: `@odata.id`, `@odata.type`, `@odata.context` (ensure contexts match `$metadata`).
- Return `Location` on 201 Created where applicable (already done for Sessions and ConnectionMethods).
- Optionally support `HEAD` and `OPTIONS` with `Allow` header, as some clients probe capabilities.

Implementation approach:
- Update the shared JSON write helper to set `OData-Version: 4.0` and ensure correct content-type consistently.
- Add simple OPTIONS handlers where easy (collections and new resources), returning `Allow` with the appropriate verbs.

### 4) ServiceRoot Corrections and Links

Contract:
- Provide a stable Service UUID (persisted in DB or config) and correct `@odata.type`/`RedfishVersion` alignment.
- Include navigation properties commonly expected by clients:
  - `SessionService`, `Managers`, `Systems` (already present)
  - `AggregationService` (present)
  - `AccountService` (to be added)
  - `Registries` and `JsonSchemas`/`SchemaStore` (to be added)

Implementation approach:
- Store a generated UUID in the database at first run; use it in ServiceRoot.
- Add `Registries` and `JsonSchemas` (or `Schemas`/`SchemaStore`) links per the schema set we host.

### 5) Error Responses with @Message.ExtendedInfo

Contract:
- Error payloads should include `error` with `code` and `message` (already present) and also an `@Message.ExtendedInfo` array with entries containing:
  - `@odata.type` (e.g., `#Message.v1_1_0.Message`)
  - `MessageId` (e.g., `Base.1.0.GeneralError`)
  - `Message`
  - `Severity`
  - `Resolution`
  - Optional `MessageArgs`

Implementation approach:
- Introduce a small helper to map our internal error situations to a Base registry `MessageId` and fill ExtendedInfo.
- Ensure the referenced `MessageId`s exist in the registries we serve.

### 6) AccountService, Accounts, Roles

Contract:
- Implement Redfish AccountService at `/redfish/v1/AccountService` with:
  - `ServiceEnabled` and links to `Accounts` and `Roles` collections
  - Session-based auth is already supported; AccountService must allow administrators to manage users via Redfish.
- `Accounts` collection (ManagerAccount resources): list, GET individual; support POST (create), PATCH (enable/disable, role, password), DELETE.
- `Roles` collection (Role resources): map Shoal RBAC roles to Redfish roles (`Administrator`, `Operator`, `ReadOnly`). Custom roles can be added later as OEM.

Mapping to Shoal:
- Use the existing users table and RBAC in `pkg/auth/rbac.go`. Define a mapping table Role->Privileges aligned to Redfish expectations.
- Enforce that only Administrator can manage accounts/roles.

Implementation approach:
- Add a new handler module (e.g., `internal/api/accountservice.go`). Reuse existing DB methods for users and roles. Avoid exposing password hashes; accept `Password` on create/update and hash securely.
- Ensure responses carry the proper `@odata.*` annotations and link back to AccountService.

### 7) Optional: Minimal Stubs for EventService and TaskService

Contract:
- If the validator requires these to exist, provide read-only stubs:
  - `/redfish/v1/EventService` with `ServiceEnabled: false`
  - `/redfish/v1/TaskService` with an empty Tasks collection

Implementation approach:
- Serve static JSON with correct `@odata.*` annotations and 200 responses. Return 501/405 for operations we don’t support yet.

### 8) Collections and Proxy Semantics Review

Contract:
- Keep Managers and Systems collections as aggregations of managed BMCs. Member `@odata.id`s must resolve via Shoal and return the upstream Manager/System payloads with proper proxying and auth.
- Ensure collection `@odata.context` values match `$metadata` entity sets and include `Members@odata.count` (already present).

Implementation approach:
- Validate that member URIs work for both root and sub-resources of Managers/Systems via proxy logic. Adjust if needed to maintain stable URIs.

### 9) ETag and Conditional Requests (Forward-looking)

Contract:
- Define where Shoal supports PATCH/DELETE with `If-Match`. For this milestone, focus on accounts (PATCH password/role) and connection methods (DELETE already supported; future PATCH could support enable/disable).

Implementation approach:
- Start by emitting weak ETags for mutable resources we own (Accounts, ConnectionMethods). Honor `If-Match` optionally after the initial milestone.

## Testing and Validation

Unit and integration tests to add:
- `$metadata` returns 200, content-type xml, and contains types/sets used by the service.
- `Registries` collection lists Base; `Base` registry fetch returns 200 with valid JSON.
- Error responses include `@Message.ExtendedInfo` with resolvable `MessageId`s.
- ServiceRoot carries stable UUID, correct `RedfishVersion`, and links to Registries/SchemaStore and AccountService.
- Headers include `OData-Version: 4.0` for JSON payloads.
- AccountService CRUD flows:
  - Admin can create, patch (password/role/enable), delete accounts
  - Role enforcement: non-admin receives 403 with ExtendedInfo
  - Roles collection lists supported roles and privileges
- Collections return `Members@odata.count` and valid member links.

Validator guidance (manual, optional):
- Run the DMTF Redfish Service Validator against Shoal’s URL. Record any failures and either address them or document as deferred.

## Work Items

Phase 1 – Protocol and Surface:
1. Add `$metadata` handler and embed required CSDLs; wire to `/redfish/v1/$metadata`.
2. Add Registries collection and serve Base registry JSON; add link(s) from ServiceRoot.
3. Add SchemaStore (JSON schema) for core types we expose; link from ServiceRoot as `JsonSchemas` (or `Schemas`).
4. Ensure `OData-Version: 4.0` header on all Redfish responses.
5. Update ServiceRoot: stable UUID from DB, correct `@odata.type`/`RedfishVersion`, add `Registries`, `JsonSchemas` links.
6. Update error helper to emit `@Message.ExtendedInfo` using Base registry IDs.

Phase 2 – AccountService:
7. Implement `/redfish/v1/AccountService` (root, Accounts, Roles) mapped to Shoal users/RBAC.
8. Add tests for all AccountService flows and RBAC.

Phase 3 – Stubs and Nice-to-haves:
9. Minimal stubs for EventService and TaskService if required by validator; otherwise defer.
10. Add OPTIONS/Allow on top-level collections.
11. Start emitting ETags on mutable resources owned by Shoal (Accounts, ConnectionMethods) – optional in this milestone.

## Data Shapes (contract sketch)

- Error with ExtendedInfo:
  {
    "error": {
      "code": "Base.1.0.GeneralError",
      "message": "A general error has occurred. See ExtendedInfo for more information.",
      "@Message.ExtendedInfo": [
        {
          "@odata.type": "#Message.v1_1_0.Message",
          "MessageId": "Base.1.0.ResourceNotFound",
          "Message": "The resource was not found.",
          "Severity": "Critical",
          "Resolution": "Provide a valid resource identifier and resubmit the request.",
          "MessageArgs": ["/redfish/v1/Managers/abc"]
        }
      ]
    }
  }

- Account (ManagerAccount): minimal example
  {
    "@odata.type": "#ManagerAccount.v1_9_0.ManagerAccount",
    "@odata.id": "/redfish/v1/AccountService/Accounts/<id>",
    "Id": "<id>",
    "Name": "User Account",
    "UserName": "operator",
    "RoleId": "Operator",
    "Enabled": true
  }

- AccountService root
  {
    "@odata.type": "#AccountService.v1_16_0.AccountService",
    "@odata.id": "/redfish/v1/AccountService",
    "Id": "AccountService",
    "Name": "Account Service",
    "ServiceEnabled": true,
    "Accounts": {"@odata.id": "/redfish/v1/AccountService/Accounts"},
    "Roles": {"@odata.id": "/redfish/v1/AccountService/Roles"}
  }

## Security and Privacy

- Never return or log secrets. Accept `Password` only on create/update and hash using existing password functions.
- Enforce role checks on AccountService operations. Only Administrators can manage accounts/roles.
- Maintain existing authentication headers and do not mix session tokens across proxied BMCs.

## Dependencies and Licensing

- Prefer embedding official DMTF schemas/registries as static assets; no new runtime dependencies.
- Ensure embedded files’ licenses are compatible with AGPLv3 distribution. Store attribution in the repository under `docs/` or a NOTICES file as needed.

## Migration and Backwards Compatibility

- New endpoints are additive. Existing UI and API behavior remains unchanged.
- ServiceRoot’s `UUID`, `RedfishVersion`, and added links are backwards compatible.
- Error payloads gain ExtendedInfo but preserve existing fields.

## Milestones and Acceptance

Done when:
- `$metadata` returns valid CSDL; ServiceRoot and collections reference it correctly.
- Registries are served and `@Message.ExtendedInfo` references resolve.
- `OData-Version` header present in JSON responses.
- AccountService implemented with passing tests.
- Full repo validation passes: `go run build.go validate`.
