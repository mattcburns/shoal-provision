# 002: Full Implementation of Redfish SessionService

**Author**: AI Agent
**Status**: Proposed
**Date**: 2025-09-20

## 1. Overview

This document outlines the design for the full implementation of the Redfish `SessionService` within the Shoal aggregator.

The current implementation is partial, only supporting session creation via `POST` to `/redfish/v1/SessionService/Sessions`. This is insufficient for full Redfish compliance and client-side management of sessions.

This design proposes adding the following capabilities:
- A `SessionService` root resource.
- A collection of all active sessions.
- The ability to view and delete individual sessions.

## 2. Proposed Changes

### 2.1. Redfish Type Definitions

The `pkg/redfish/types.go` file shall be updated to include a struct for the `SessionService` resource.

```go
// pkg/redfish/types.go

// SessionService represents the Redfish SessionService.
type SessionService struct {
	ODataContext    string     `json:"@odata.context"`
	ODataID         string     `json:"@odata.id"`
	ODataType       string     `json:"@odata.type"`
	ID              string     `json:"Id"`
	Name            string     `json:"Name"`
	Description     string     `json:"Description"`
	ServiceEnabled  bool       `json:"ServiceEnabled"`
	SessionTimeout  int        `json:"SessionTimeout"`
	Sessions        ODataIDRef `json:"Sessions"`
}
```

### 2.2. Database Layer

The database layer needs to be enhanced to support querying and deleting sessions by their ID, and listing all active sessions.

**File**: `internal/database/sessions.go`

**New Functions**:
1. **`GetSession(ctx context.Context, id string) (*models.Session, error)`**: Retrieve a single session by its unique ID.
2. **`GetSessions(ctx context.Context) ([]models.Session, error)`**: Retrieve all active sessions from the database.
3. **`DeleteSessionByID(ctx context.Context, id string) error`**: Delete a session by its unique ID.

### 2.3. API Layer

The API layer will be updated to handle the new `SessionService` endpoints.

**File**: `internal/api/api.go`

#### 2.3.1. Routing

The main router in `handleRedfish` shall be updated to delegate requests for `/redfish/v1/SessionService` to a new dedicated handler.

```go
// internal/api/api.go -> func handleRedfish(...)

	// ...
	// Handle authentication endpoints (no auth required for POST)
	if strings.HasPrefix(path, "/v1/SessionService") {
		h.handleSessionService(w, r, path)
		return
	}

	// All other endpoints require authentication
	user, err := h.auth.AuthenticateRequest(r)
	// ...
```

#### 2.3.2. New Handlers

1.  **`handleSessionService(w, r, path)`**: This new top-level handler will route requests based on the path and method.
    *   `POST /v1/SessionService/Sessions`: Route to the existing `handleLogin`.
    *   All other `SessionService` endpoints require authentication.
    *   `GET /v1/SessionService`: Route to `handleGetSessionServiceRoot`.
    *   `GET /v1/SessionService/Sessions`: Route to `handleGetSessionsCollection`.
    *   `GET /v1/SessionService/Sessions/{id}`: Route to `handleGetSession`.
    *   `DELETE /v1/SessionService/Sessions/{id}`: Route to `handleDeleteSession`.

2.  **`handleGetSessionServiceRoot(w, r)`**:
    *   Handles `GET /redfish/v1/SessionService`.
    *   Constructs and returns a `redfish.SessionService` object.
    *   The `SessionTimeout` can be hardcoded to `1800` seconds (30 minutes) for now.
    *   `ServiceEnabled` should be `true`.

3.  **`handleGetSessionsCollection(w, r)`**:
    *   Handles `GET /redfish/v1/SessionService/Sessions`.
    *   Calls the new `db.GetSessions()` method.
    *   Formats the results into a standard Redfish collection response.

4.  **`handleGetSession(w, r, id)`**:
    *   Handles `GET /redfish/v1/SessionService/Sessions/{id}`.
    *   Calls the new `db.GetSession(id)` method.
    *   Returns the session resource or a 404 error if not found.

5.  **`handleDeleteSession(w, r, id)`**:
    *   Handles `DELETE /redfish/v1/SessionService/Sessions/{id}`.
    *   Calls the new `db.DeleteSessionByID(id)` method.
    *   Returns `204 No Content` on success or a 404 error if not found.

The existing `handleSessions` function can be removed.

## 3. Implementation Steps for an AI Agent

1.  **Update Redfish Types**: Add the `SessionService` struct to `pkg/redfish/types.go` as defined in section 2.1.
2.  **Update Database Layer**:
    *   Navigate to `internal/database/sessions.go`.
    *   Implement the three new functions: `GetSession`, `GetSessions`, and `DeleteSessionByID`. Use the existing functions in the file as a template for structure and error handling.
3.  **Update API Layer**:
    *   Navigate to `internal/api/api.go`.
    *   Modify the `handleRedfish` function to delegate `/v1/SessionService` requests to a new `handleSessionService` function as shown in section 2.3.1.
    *   Implement the new handler functions: `handleSessionService`, `handleGetSessionServiceRoot`, `handleGetSessionsCollection`, `handleGetSession`, and `handleDeleteSession`.
    *   Ensure that all new endpoints (except `POST` to create a session) are protected by the `h.auth.RequireAuth` middleware. A good pattern would be to check for the POST login first, and then apply authentication for all other cases inside `handleSessionService`.
    *   Remove the old placeholder `handleSessions` function.
4.  **Add Integration Tests**:
    *   Navigate to `test/integration/server_test.go`.
    *   Add a new test function, `TestRedfishSessionService`, to validate the new functionality.
    *   This test should:
        1.  Create a session by `POST`ing to `/redfish/v1/SessionService/Sessions`.
        2.  Perform a `GET` on `/redfish/v1/SessionService` and validate the response.
        3.  Perform a `GET` on `/redfish/v1/SessionService/Sessions` and verify the new session is in the collection.
        4.  Extract the session ID and perform a `GET` on `/redfish/v1/SessionService/Sessions/{id}`.
        5.  Perform a `DELETE` on `/redfish/v1/SessionService/Sessions/{id}` and expect a `204 No Content` response.
        6.  Verify the session is no longer in the collection by performing another `GET` on the collection endpoint.
