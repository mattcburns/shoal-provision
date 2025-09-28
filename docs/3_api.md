# API Guide

Shoal provides a Redfish-compliant API for programmatic management.

## Authentication

The API supports two authentication methods:

1.  **HTTP Basic Auth**: Include credentials in the `Authorization` header.
    ```bash
    curl -u admin:admin http://localhost:8080/redfish/v1/
    ```

2.  **Redfish Sessions**: Create a session to get a token, then use the `X-Auth-Token` header.
    ```bash
    # Create session
    curl -X POST http://localhost:8080/redfish/v1/SessionService/Sessions \
      -H "Content-Type: application/json" \
      -d '{"UserName": "admin", "Password": "admin"}'

    # Use session token
    curl -H "X-Auth-Token: <token>" http://localhost:8080/redfish/v1/
    ```

## Core Redfish Endpoints

- `GET /redfish/v1/`: Service root.
- `GET /redfish/v1/EventService`: Minimal EventService stub (ServiceEnabled=false).
- `GET /redfish/v1/TaskService`: Minimal TaskService stub.
- `GET /redfish/v1/TaskService/Tasks`: Empty Tasks collection.
- `GET /redfish/v1/Managers`: List of aggregated managers from all BMCs.
- `GET /redfish/v1/Systems`: List of aggregated systems from all BMCs.
- `GET /redfish/v1/Managers/{bmc-name}`: Proxy to a specific BMC manager.
- `GET /redfish/v1/Systems/{bmc-name}`: Proxy to a specific system.
- `GET /redfish/v1/SessionService`: Session service root.

### Protocol Compliance Endpoints (Phase 1)

- `GET /redfish/v1/$metadata` (no auth): OData CSDL describing the service. Returns `Content-Type: application/xml` and `OData-Version: 4.0` with strong `ETag` support.
- `GET /redfish/v1/Registries` (auth required): Message registries collection (includes Base).
  - `GET /redfish/v1/Registries/Base` (auth required): Base registry file (en locale).
  - `GET /redfish/v1/Registries/Base/Base.json` (auth required): Explicit locale path.
- `GET /redfish/v1/SchemaStore` (auth required): JSON Schema store root enumerating embedded schemas.
  - `GET /redfish/v1/SchemaStore/{SchemaName}.vX_Y_Z.json` (auth required): Individual schema file.

### Caching and ETags

Shoal includes HTTP ETag support for static Redfish assets to improve client-side caching:

- `GET /redfish/v1/$metadata`
- `GET /redfish/v1/Registries/{name}[/{file}]` (e.g., Base)
- `GET /redfish/v1/SchemaStore/{path}.json`

Responses include an `ETag` header. Clients may send `If-None-Match` with the previously received ETag to receive `304 Not Modified` when content has not changed. ETags are strong validators derived from the content hash.

Examples:

```bash
# $metadata (no auth)
curl -i http://localhost:8080/redfish/v1/$metadata

# Registries (requires session token)
curl -s -X POST http://localhost:8080/redfish/v1/SessionService/Sessions \
  -H 'Content-Type: application/json' \
  -d '{"UserName":"admin","Password":"admin"}' | jq -r '. | .@odata.id' >/dev/null
TOKEN=$(curl -s -X POST http://localhost:8080/redfish/v1/SessionService/Sessions \
  -H 'Content-Type: application/json' \
  -d '{"UserName":"admin","Password":"admin"}' -D - 2>/dev/null | awk '/X-Auth-Token:/ {print $2}' | tr -d '\r')
curl -i -H "X-Auth-Token: $TOKEN" http://localhost:8080/redfish/v1/Registries/Base

# Conditional GET using ETag
ETAG=$(curl -sI -H "X-Auth-Token: $TOKEN" http://localhost:8080/redfish/v1/Registries/Base | awk -F': ' '/^ETag:/ {print $2}' | tr -d '\r')
curl -i -H "X-Auth-Token: $TOKEN" -H "If-None-Match: $ETAG" http://localhost:8080/redfish/v1/Registries/Base

# Schema file
curl -i -H "X-Auth-Token: $TOKEN" http://localhost:8080/redfish/v1/SchemaStore/ServiceRoot.v1_5_0.json
```

### OPTIONS and Allow

Shoal advertises supported HTTP methods via OPTIONS with the `Allow` header on key resources. Examples:

- `OPTIONS /redfish/v1/` → `Allow: GET`
- `OPTIONS /redfish/v1/AccountService/Accounts` → `Allow: GET, POST`
- `OPTIONS /redfish/v1/AccountService/Accounts/{id}` → `Allow: GET, PATCH, DELETE`
- `OPTIONS /redfish/v1/SessionService/Sessions` → `Allow: GET, POST` (accessible without auth)

All Redfish JSON responses include `OData-Version: 4.0`.

### Error Responses and Message Registries

Shoal returns Redfish-compliant error envelopes that include `@Message.ExtendedInfo`. The `MessageId` values map to entries in the Base Message Registry, allowing clients to correlate errors with standardized messages.

- Example `MessageId` values: `Base.1.0.Unauthorized`, `Base.1.0.MethodNotAllowed`, `Base.1.0.ResourceNotFound`, `Base.1.0.InsufficientPrivilege`, `Base.1.0.GeneralError`.
- The Base registry is available at `/redfish/v1/Registries/Base` (and `/redfish/v1/Registries/Base/Base.json`).
- 401 responses also include `WWW-Authenticate: Basic realm="Redfish"`.

Sample error payload:

```json
{
  "error": {
    "code": "Base.1.0.Unauthorized",
    "message": "Authentication required",
    "@Message.ExtendedInfo": [
      {
        "@odata.type": "#Message.v1_1_0.Message",
        "MessageId": "Base.1.0.Unauthorized",
        "Message": "Authentication required",
        "Severity": "Critical",
        "Resolution": "Provide valid credentials and resubmit the request."
      }
    ]
  }
}
```

## Settings Discovery

- `GET /api/bmcs/{bmc-name}/settings`: Returns discovered configurable settings for a BMC.
  - **Query Parameters:**
    - `resource`: Filter to a specific Redfish resource path (e.g., `EthernetInterfaces`, `/Storage`).
    - `search`: Free-text filter across attribute, display name, description, etc.
    - `oem`: Filter by OEM vs. non-OEM (`true` or `false`).
    - `page` / `page_size`: For pagination.
    - `refresh`: `true` to bypass caches and force re-discovery (requires Operator or Admin).
  - **Scope**: Includes settings from `Bios`, `ManagerNetworkProtocol`, `EthernetInterfaces`, and `Storage` resources.
  - **Enrichment**: Descriptors are enriched with metadata from Redfish Attribute Registries and `ActionInfo`.

**Example:**
```bash
curl -s -u admin:admin \
  "http://localhost:8080/api/bmcs/bmc1/settings?resource=EthernetInterfaces" | jq .
```


## DMTF Standard AggregationService

Shoal implements the DMTF Redfish AggregationService standard for programmatic BMC management.

- `GET /redfish/v1/AggregationService/ConnectionMethods`: List connection methods (BMCs).
- `POST /redfish/v1/AggregationService/ConnectionMethods`: Add a new BMC connection.
- `DELETE /redfish/v1/AggregationService/ConnectionMethods/{id}`: Remove a BMC connection.

**Example: Add a BMC**
```bash
curl -X POST http://localhost:8080/redfish/v1/AggregationService/ConnectionMethods \
  -H "Content-Type: application/json" \
  -H "X-Auth-Token: <token>" \
  -d '{
    "Name": "Production Server BMC",
    "ConnectionMethodType": "Redfish",
    "ConnectionMethodVariant.Address": "192.168.1.100",
    "ConnectionMethodVariant.Authentication": {
      "Username": "admin",
      "Password": "password"
    }
  }'
```
