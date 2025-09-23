# 015: Refactor Settings UI to Read-Only View

**Date**: 2025-09-23
**Status**: Proposed

## 1. Summary

This design outlines the UI/UX changes to replace the deprecated Configuration Profile feature with a simplified, read-only settings view. The goal is to provide a direct view of a BMC's discovered settings without the overhead of profile management. The existing boot order management feature will be retained and integrated into this new view.

## 2. Motivation

With the backend for Configuration Profiles removed (as per Design 014), the user interface must be updated to reflect this simplification. Users need a straightforward way to view the current state of discovered Redfish settings for a given BMC.

## 3. Proposed Changes

### 3.1. UI Component Removal

The following UI pages and components will be **removed**:

-   **Main Navigation**: The "Profiles" link in the top navigation bar will be removed.
-   **Profile Pages**: The following pages and their corresponding templates will be deleted:
    -   Profile List (`/profiles`)
    -   Profile Detail (`/profiles/{id}`)
    -   Profile Version/Diff views
-   **Buttons**: All buttons related to profiles ("Snapshot", "Import/Export", "Apply", "Delete Profile/Version") will be removed from the UI.

### 3.2. New Settings View

A new "Settings" view will be created within the BMC details page.

-   **Route**: A new route, `/bmc/{id}/settings`, will be created to render this view.
-   **Handler**: A new web handler, `handleBMCSettings`, will be implemented in `internal/web/web.go`. This handler will fetch all `SettingDescriptor` records for the given BMC and render them.
-   **Template**: A new template file will be created for this view.
-   **Content**:
    -   The page will display a simple table or list of all discovered settings for the BMC.
    -   For each setting, it will display the **Attribute** (e.g., `Bios.Enabled`) and its **Current Value**. The display will be simplified to remove columns like "Type" and "ApplyTime".
    -   This view will be **read-only**.

### 3.3. Integration of Boot Order Management

The boot order management feature will be preserved and moved into the new "Settings" view.

-   The existing boot order UI widget (the drag-and-drop list) will be included on the `/bmc/{id}/settings` page.
-   The backend API endpoint used to save the boot order (`POST /api/bmc/{id}/settings`) will be retained and will continue to function as-is.
-   This will be the only interactive, writeable component on the new Settings page.

## 4. Implementation Plan

1.  Remove the "Profiles" link from the main layout template.
2.  Delete the HTML templates associated with the old profiles feature.
3.  Remove the web handlers for the deleted profile pages from `internal/web/web.go`.
4.  Create a new handler and route for the `/bmc/{id}/settings` page.
5.  Create the new template for the settings view, displaying a read-only list of settings.
6.  Integrate the existing boot order management component into the new settings template.
7.  Update the BMC details page to link the "Settings" tab to the new route.
