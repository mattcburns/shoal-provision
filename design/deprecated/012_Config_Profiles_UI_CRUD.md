# Milestone 012: Configuration Profiles UI (Create/Update/Delete)

## 1. Overview

Build on 010/011 by adding full CRUD for Configuration Profiles in the web UI. Users can create profiles, edit metadata, create new versions manually, and delete profiles or versions (with safe guards). Out of scope: bulk operations.

## 2. Scope

### 2.1 Pages & Actions
- Profiles List (`/profiles`): Add "New Profile" button. Supports delete with confirm.
- Profile Detail (`/profiles/{id}`): Edit profile metadata (name/description), create new version (manual entry or import JSON), delete profile.
- Version Detail (`/profiles/{id}/versions/{version}`): Delete version. Import entries to create a new version.

### 2.2 API Integration
- Use existing JSON APIs where possible: `POST /api/profiles` (create), `PATCH /api/profiles/{id}` (update), `DELETE /api/profiles/{id}` (delete), `POST /api/profiles/{id}/versions` (new version), `DELETE /api/profiles/{id}/versions/{v}` (delete version), `POST /api/profiles/import` (import), `POST /api/profiles/{id}/export` (export).

## 3. UX

- Inline forms with SSR templates consistent with current style.
- Confirmations for destructive actions. Display concise success/error messages.

## 4. Acceptance Criteria

- All actions available via UI with appropriate auth.
- New tests for UI handlers/forms; `go run build.go validate` passes.
- No changes to existing APIs; UI-only in this milestone.
