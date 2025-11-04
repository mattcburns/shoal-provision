# 019: Codebase Simplification and Refactor Plan (No Behavioral Changes)

Author: Matthew Burns  
Status: Proposed  
Date: 2025-11-04

## Summary

This document proposes a set of mechanical simplifications and internal refactors to reduce complexity, improve readability, and make testing easier without changing externally observable behavior or the public API surface. The focus is on:

- Splitting monolithic handlers into focused modules
- Centralizing cross-cutting concerns (headers, errors, JSON/ETag writing)
- Removing duplicated logic and tightening helper boundaries
- Clarifying routing and proxy boundaries
- Consolidating repeated DB encryption/decryption checks

All changes are intended to be behavior-preserving. Validation will continue to use the existing pipeline.

## Goals

- Reduce cognitive load by organizing API endpoints into cohesive files with clear responsibilities.
- Eliminate copy-paste patterns around request creation, ETag handling, and error responses.
- Improve testability with thinner, reusable helpers and smaller units.
- Keep external behavior stable: same endpoints, payloads, headers, and status codes.
- Leave room for incremental improvements (e.g., future generalized ETag support) without committing to them now.

## Non-goals

- No breaking API changes.
- No new dependencies.
- No functional changes to authorization, persistence, proxying, or payloads.
- No performance work beyond small incidental gains from code reuse.

## Current Pain Points

1. Monolithic API file
   - `internal/api/api.go` contains many endpoint families (Service Root, SessionService, AccountService, AggregationService, proxy, metadata/registries, stubs). This makes navigation and focused testing harder.

2. Cross-cutting concerns scattered
   - `Content-Type`, `OData-Version`, `ETag` handling, `Allow` headers, and error responses are mostly consistent but not centralized everywhere (e.g., `auth.RequireAuth` writes a bespoke JSON error).

3. Duplicated HTTP request logic
   - In `internal/bmc/service.go`, several places manually create requests that could share the same helpers as `fetchRedfishResource` (e.g., `GetFirstManagerID`, `GetFirstSystemID`, `TestConnection`).

4. Cache helpers are similar but not unified
   - `idCache`, `regCache`, and `aiCache` follow similar patterns (keying, TTL checks, bypass via context) but replicate TTL/config and access patterns.

5. DB encryption/decryption checks duplicated
   - Repeated checks for `encryptor != nil` and `IsEncrypted` across BMC and ConnectionMethods read paths.

6. Hardcoded Base message IDs
   - `validMessageIDs` is hardcoded in `internal/api/api.go`. That’s fine for now; future work could derive from embedded registries. For this pass, keep behavior but move constants to a focused place.

## Proposed Simplifications

The changes below are mechanical and should not alter behavior.

### A) Split API into focused modules

Refactor `internal/api/api.go` into cohesive files under `internal/api/`:

- `service_root.go`: `handleServiceRoot`
- `session_service.go`: routing and handlers for SessionService and Sessions
- `account_service.go`: routing and handlers for AccountService, Accounts, Roles
- `aggregation_service.go`: routing and handlers for AggregationService and ConnectionMethods
- `metadata.go`: `$metadata`, `Registries`, `SchemaStore`
- `proxy.go`: `isBMCProxyRequest`, `handleBMCProxy`
- `errors.go`: error helpers, severity mapping, registry message ID constants
- `respond.go`: JSON/ETag helpers, `writeAllow`, common header application
- `router.go`: `New` and top-level `/redfish/` routing that delegates to modules

Benefits:
- Clear boundaries and easier to find handlers and helpers.
- Smaller files facilitate targeted tests and reviews.

Notes:
- Keep signatures and exported types unchanged.
- Module-level files only reorganize code; no route changes.

### B) Centralize response writing and headers

Introduce a small response helper surface (pure refactor; same behavior):

- `writeJSONResponse(w, status, data)` and `writeJSONResponseWithETag(w, status, data, etag)` already exist. Move them into a dedicated `respond.go` and ensure they are used consistently across all handlers, including inside `auth.RequireAuth` (by calling a public error helper instead of writing ad-hoc JSON).
- Ensure `OData-Version: 4.0` and `Content-Type: application/json` are uniformly set via these helpers.
- Keep `writeAllow(w, methods...)` in the same module and use it consistently for all OPTIONS endpoints.

Benefits:
- One implementation for headers and body writing, consistent behavior, easier tweaks later.

### C) Unify Redfish error responses

- Move `writeErrorResponse`, `severityForStatus`, `resolutionForMessageID`, and the `validMessageIDs` map into `errors.go`.
- Make `writeErrorResponse` the single entry point from all places, including `auth.RequireAuth` (middleware) and non-JSON fallbacks. The middleware can import and invoke this function rather than writing a bespoke JSON literal.

Benefits:
- Prevent drift of error shapes and headers.
- Single place to adjust registry message handling later (if we derive IDs from embedded registries).

### D) Reduce duplication in BMC service

- Reuse `fetchRedfishResource` more consistently within:
  - `GetFirstManagerID` and `GetFirstSystemID`: fetch the collection via `fetchRedfishResource` and parse, rather than re-creating request logic.
  - `TestConnection`: align to a small shared helper that does an authenticated GET to a path (root `/redfish/v1/`) and checks status. This can call a generalized “get JSON” helper and ignore body on success.
- Create internal tiny helpers:
  - `newAuthedGET(ctx, bmc, path)` producing a request with auth and common headers.
  - `decodeJSON(resp, &out)` to standardize error handling and defer/close patterns.

Benefits:
- Less repeated HTTP request scaffolding and fewer error-handling branches.
- More uniform logging and header behavior.

Constraints:
- Preserve all existing logging keys and messages where visible in tests.

### E) Consolidate small cache utilities

- Introduce internal helpers for TTL checks and keyed cache retrieval in `internal/bmc/service.go`:
  - `isFresh(ts time.Time, ttl time.Duration) bool`
  - Optionally group `regCache` and `aiCache` access via small methods (`getRegistryFromCache`, `setRegistryCache`, etc.), keeping same TTL (`registryCacheTTL`) and refresh semantics (`ctxkeys.Refresh`) to avoid drift.

Benefits:
- Clearer intent, less repeated locking patterns in each call site.
- No behavior changes; purely internal factoring.

### F) DRY database encryption/decryption checks

- Add private helpers on `DB` that encapsulate repeated patterns:
  - `decryptIfNeeded(s string) string` that tries to decrypt when `encryptor != nil` and `IsEncrypted(s)`, logging on failure and returning the original if decryption fails.
  - `encryptIfConfigured(plaintext string) (string, error)` that returns the input unchanged when encryptor is nil.
- Use these in `GetBMCs`, `GetBMC`, `GetBMCByName`, `GetConnectionMethods`, `GetConnectionMethod`, `CreateBMC`, `UpdateBMC`, `CreateConnectionMethod`.
- Maintain current warnings and info logs (e.g., “Password encryption enabled/disabled”) emitted from initialization as-is.

Benefits:
- Reduce repeated branches and make intent explicit.
- No change to stored formats or runtime behavior.

### G) Router clarity and auth boundaries

- In `router.go`, make the `/redfish/v1/...` path registration explicit by grouping:
  - Unauthenticated endpoints: `Service Root`, `SessionService` POST for session creation, `$metadata`, and static schema/registry files.
  - Authenticated endpoints via middleware: AggregationService, AccountService, Managers/Systems aggregator lists, SchemaStore/Registries GET when required to be protected (current behavior is already behind middleware for registries/schemas; keep it).
- Keep `http.ServeMux` registrations explicit; avoid “catch-all” then filtering inside handlers when a route can be registered directly. For routes that need a `user *models.User`, pass via context (already established with `ctxkeys.User`).

Benefits:
- Faster mental resolution of which endpoints are public vs. protected.
- Less branching in `handleRedfish`.

### H) Keep ETag logic but allow reuse

- Preserve `computeETag`, `weakETag`, `formatTimeForETag`, `ifNoneMatchMatches` behavior.
- Move them into the response helper module or a small `etag.go` co-located with `respond.go` and import where needed.
- Maintain the resource-specific ETag builders (`accountETag`, `accountsCollectionETag`, `connectionMethodETag`, `connectionMethodsCollectionETag`) as-is. Optionally centralize their common timestamp fallback logic into a tiny helper, but do not alter the resulting strings.

Benefits:
- Single location for ETag helpers, simpler to audit.

### I) Small consistency fixes with no behavior change

- Inline constants: promote small literal strings used across files (e.g., “Method not allowed”, “Resource not found”) into `errors.go` to reduce typos and drift. Keep the exact texts.
- Normalize `w.Header().Set("OData-Version", "4.0")` placement via `respond.go`.
- Replace ad-hoc status-method checks with common guard helpers where it shortens code without changing flow.

## Risks and Mitigations

- Risk: Hidden behavior change due to routing changes.
  - Mitigation: Keep exact route registrations and handler signatures; only split files and delegate. Validate with existing tests.
- Risk: Header differences via consolidation.
  - Mitigation: Ensure existing helpers are the source of truth, and reuse them. Add targeted tests where coverage is thin.
- Risk: Logging changes could affect tests.
  - Mitigation: Preserve log messages that are asserted in tests. Do not change log text or keys.

## Migration Plan

1. Create a feature branch per AGENTS protocol.
2. Extract response helpers (`respond.go`, `etag.go`) and error helpers (`errors.go`); switch call sites in `internal/api` and `internal/auth`.
3. Split `internal/api/api.go` into modules, moving code verbatim; wire up `router.go` with the same `ServeMux` registrations currently in `New`.
4. In `internal/bmc/service.go`, introduce micro-helpers and reduce duplication where safe (use `fetchRedfishResource` for collection fetches).
5. Add DB helper methods for encrypt/decrypt; replace direct checks in DAO functions.
6. Run full validation: `go run build.go validate`. All tests are expected to pass, as changes are intended to be behavior-preserving.
7. If any failures, add minimal tests to ensure headers/errors stay identical and adjust code to preserve behavior.

## Validation

- Unit/Integration tests must pass unchanged.
- Manual spot checks:
  - Verify `OData-Version: 4.0` on JSON responses.
  - Verify `$metadata` and registry/schema endpoints return same content and ETags.
  - Exercise SessionService (POST/GET/DELETE) and confirm unchanged payloads and headers.
  - Confirm AccountService CRUD and role gating behaves exactly the same.
  - Confirm AggregationService flow and ConnectionMethods CRUD behave the same.
  - Confirm proxying paths for Managers/Systems remain unchanged and functional.

## Work Items

- [ ] Extract `respond.go`, `etag.go` and adjust imports
- [ ] Extract `errors.go` and update all call sites (including `auth.RequireAuth`)
- [ ] Split API modules: `service_root.go`, `session_service.go`, `account_service.go`, `aggregation_service.go`, `metadata.go`, `proxy.go`, `router.go`
- [ ] `bmc/service.go`: add micro-helpers; reduce duplication in ID discovery and connection tests
- [ ] `database`: add `encryptIfConfigured` and `decryptIfNeeded`; refactor DAO methods to use them
- [ ] Confirm OPTIONS handling unchanged across endpoints
- [ ] Run validation pipeline and update/augment tests only if needed to capture preserved behavior
- [ ] PR summary detailing that the refactor is behavior-preserving and mapping files before/after

## Appendix A: File map (proposed)

- `internal/api/router.go` — constructs mux, registers routes, holds `New`
- `internal/api/respond.go` — JSON/headers/ETag helpers, `writeAllow`
- `internal/api/etag.go` — `computeETag`, `weakETag`, etc.
- `internal/api/errors.go` — error response helpers, severity/resolution, `validMessageIDs`
- `internal/api/service_root.go`
- `internal/api/session_service.go`
- `internal/api/account_service.go`
- `internal/api/aggregation_service.go`
- `internal/api/metadata.go`
- `internal/api/proxy.go`

No exported API or handler semantics change; this is an internal organization and DRY pass.

## Appendix B: Future (deferred) opportunities

Not part of this simplification milestone, but enabled by it:

- Derive `validMessageIDs` from embedded registry JSON instead of hardcoding.
- Generic ETag builders from canonical JSON serialization for collection resources.
- Request/response logging middleware with unified correlation IDs for proxied calls.
- Configurable TTLs for caches via settings.
