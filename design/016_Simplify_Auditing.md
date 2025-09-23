# 016: Simplify Auditing Feature

**Date**: 2025-09-23
**Status**: Proposed

## 1. Summary

This design proposes simplifying the auditing and change tracking feature in light of the deprecation of Configuration Profiles (Design 014). By removing profile-related events, the audit log becomes more focused and easier to manage.

## 2. Motivation

The current audit log records numerous events related to the lifecycle of configuration profiles (e.g., `profile-create`, `profile-snapshot`, `profile-apply`). Since the entire feature is being removed, these audit records are now obsolete. Simplifying the audit trail will reduce database noise and make the feature more relevant to the remaining functionality.

## 3. Proposed Changes

### 3.1. Audit Event Type Reduction

The `AuditEventType` enumeration and the logic in `internal/bmc/service.go`'s `recordAudit` function will be modified to remove the following event types:

-   `AuditEventProfileCreate`
-   `AuditEventProfileUpdate`
-   `AuditEventProfileDelete`
-   `AuditEventProfileSnapshot`
-   `AuditEventProfileApply`
-   `AuditEventProfileImport`
-   `AuditEventProfileExport`
-   `AuditEventProfileVersionDelete`

### 3.2. Simplification of Audit Logic

-   The `recordAudit` function in `internal/bmc/service.go` will be simplified, as it will no longer need to handle the removed event types.
-   The `internal/database/database.go` functions for writing to the audit log will remain, but they will be called with a much smaller set of event types.

### 3.3. UI Simplification

-   The Audit Log page (`/audit`) will naturally become simpler as fewer event types will be displayed.
-   No direct UI changes are needed for the template itself, but the data presented will be less cluttered.

### 3.4. Retained Audit Events

The following core audit events will be **retained**:

-   Events related to user authentication (`user-login`, `user-logout`, `user-password-change`).
-   Events related to BMC management (`bmc-create`, `bmc-delete`).
-   Events related to settings changes, specifically for boot order (`setting-change`).
-   Events related to user management (`user-create`, `user-delete`, `user-update`).

## 4. Impact

-   This change simplifies the codebase and reduces the amount of data stored in the `audit_log` table.
-   The user-facing Audit Log will be easier to read and more focused on significant events.
-   This change depends on the completion of Design 014, as it removes auditing for a feature that will no longer exist.
