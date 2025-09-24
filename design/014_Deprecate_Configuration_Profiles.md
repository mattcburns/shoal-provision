# 014: Deprecate Configuration Profiles

**Date**: 2025-09-23
**Status**: Proposed

## 1. Summary

This design proposes the complete removal of the Configuration Profile feature from Shoal. The concept of creating, versioning, applying, and managing configuration snapshots will be deprecated in favor of a simpler, read-only view of discovered settings.

This is the first step in a larger refactoring to simplify the settings management feature.

## 2. Motivation

The Configuration Profile feature, while powerful, has proven to be overly complex for the primary use case of viewing device settings. The overhead of managing profiles, versions, and diffs is not justified by its utility. A simplified, direct view of the current Redfish settings is more aligned with user needs.

## 3. Proposed Changes

### 3.1. Database Schema Changes

The following tables will be **removed** from the database schema (`internal/database/database.go`):

-   `profiles`
-   `profile_versions`
-   `setting_snapshots`

The `setting_descriptors` table will be simplified. The `profile_id` column will be removed, as descriptors will now be associated directly with a BMC.

### 3.2. Model Changes

The following models in `pkg/models/models.go` will be **removed**:

-   `ConfigurationProfile`
-   `ProfileVersion`
-   `SettingSnapshot`

The `SettingDescriptor` model will be updated to remove any fields related to profiles.

### 3.3. API Endpoint Removal

The following API endpoints and their corresponding handlers in `internal/api/api.go` and `internal/web/web.go` will be **removed**:

-   `GET /api/profiles`
-   `POST /api/profiles`
-   `GET /api/profiles/{id}`
-   `PUT /api/profiles/{id}`
-   `DELETE /api/profiles/{id}`
-   `GET /api/profiles/{id}/versions`
-   `POST /api/profiles/{id}/snapshot`
-   `GET /api/profiles/{id}/versions/{version}`
-   `DELETE /api/profiles/{id}/versions/{version}`
-   `GET /api/profiles/{id}/versions/{v1}/diff/{v2}`
-   `POST /api/profiles/import`
-   `GET /api/profiles/{id}/export`

### 3.4. Backend Logic Removal

-   All functions in `internal/database/database.go` related to creating, reading, updating, or deleting profiles and their versions will be removed.
-   The discovery logic in `internal/bmc/service.go` will be simplified to no longer create `SettingSnapshot` records or associate `SettingDescriptor` records with a profile.
-   Web handlers in `internal/web/web.go` for rendering profile pages (`handleProfiles`, `handleProfileDetails`, etc.) will be removed.

## 4. Impact

-   This change is **not backwards compatible**. Existing Shoal databases will be incompatible and will need to be re-initialized.
-   The user-facing UI for managing configuration profiles will be removed entirely in a subsequent step (covered in Design 015).
-   The backend will be significantly simplified, reducing complexity and maintenance overhead.
-   The boot order configuration feature is unaffected by this design document but will be re-integrated into the new UI as described in Design 015.
