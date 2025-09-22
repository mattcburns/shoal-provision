# Milestone 010: Configuration Profiles UI (Read-Only)

## 1. Overview

This milestone introduces the foundational, read-only user interface for managing Configuration Profiles. The goal is to provide administrators and operators with the ability to view existing profiles, inspect their versions, and see the specific settings captured in each version.

This work is the first step towards a full CRUD interface and intentionally defers create, update, and delete operations to a subsequent milestone.

## 2. Rationale

- **Visibility**: Users need to see what profiles exist before they can manage them.
- **Incremental Development**: Building the read-only views first establishes the UI structure and data flow without the complexity of handling user input and state changes.
- **Agent Task Scoping**: This milestone represents a clear, self-contained unit of work suitable for a single PR.

## 3. Scope

### 3.1. Key Features

- **Profile List Page**: A new page at `/profiles` that displays a table of all existing configuration profiles.
  - Columns: Profile Name, Description, Number of Versions, Created/Updated Timestamps.
  - Each profile name will be a link to its detail page.
- **Profile Detail Page**: A new page at `/profiles/{id}` that shows:
  - The profile's name and description.
  - A table listing all versions of the profile, with columns for Version Number, Number of Entries, and Creation Date.
  - Each version number will be a link to the version detail page.
- **Version Detail Page**: A new page at `/profiles/{id}/versions/{version}` that displays:
  - A table of all setting entries (key-value pairs) for that specific version.
  - Columns: Resource Path, Attribute, and Stored Value.
- **Navigation**: Add a "Profiles" link to the main navigation bar in the web UI, visible to authenticated users.

### 3.2. Out of Scope

- Creating, editing, or deleting profiles.
- Creating new profile versions (including snapshots).
- Applying profiles to a BMC.
- Comparing (diffing) profile versions.

## 4. Technical Design

### 4.1. Web Layer (`internal/web`)

- **New Handlers**:
  - `handler.go`:
    - `profilesPage(w, r)`: Handles `GET /profiles`. Fetches all profiles from the database and renders the `profiles.html` template.
    - `profileDetailPage(w, r)`: Handles `GET /profiles/{id}`. Fetches profile details and its versions, then renders `profile_detail.html`.
    - `profileVersionDetailPage(w, r)`: Handles `GET /profiles/{id}/versions/{version}`. Fetches the specific profile version and its entries, then renders `profile_version_detail.html`.
- **New Router Entries**:
  - `web.go`: Register the new routes in the main router, ensuring they are protected by the authentication middleware.
- **New Templates**:
  - `templates/profiles.html`: Main list view. Will contain a table iterating over `[]models.ConfigProfile`.
  - `templates/profile_detail.html`: Detail view. Will show profile metadata and a table iterating over `[]models.ConfigProfileVersion`.
  - `templates/profile_version_detail.html`: Version detail view. Will show a table iterating over `[]models.ConfigProfileEntry`.
- **Navigation Update**:
  - `templates/base.html` (or equivalent layout template): Add a link to `/profiles` in the navigation menu.

### 4.2. Database Layer (`internal/database`)

The existing API already implies that functions to fetch profiles and their sub-components exist. This milestone will primarily involve exposing them to the web handlers. If any of the following functions do not exist, they must be created.

- `database.go`:
  - `GetConfigProfiles() ([]*models.ConfigProfile, error)`: Fetch all profiles.
  - `GetConfigProfile(id string) (*models.ConfigProfile, error)`: Fetch a single profile by its ID.
  - `GetConfigProfileVersions(profileID string) ([]*models.ConfigProfileVersion, error)`: Fetch all versions for a given profile.
  - `GetConfigProfileVersion(profileID string, version int) (*models.ConfigProfileVersion, error)`: Fetch a specific version, including its entries.

## 5. Acceptance Criteria

- An AI agent can implement these changes in a single pull request.
- All new pages render correctly without errors.
- The "Profiles" link appears in the main navigation for logged-in users.
- The profile list at `/profiles` accurately reflects the profiles stored in the database.
- Clicking a profile name navigates to `/profiles/{id}`.
- The profile detail page correctly displays the versions for that profile.
- Clicking a version number navigates to `/profiles/{id}/versions/{version}`.
- The version detail page correctly displays the settings captured in that version.
- All existing tests, including the `build.py validate` pipeline, must pass.
- New tests should be added to cover the new handlers.
