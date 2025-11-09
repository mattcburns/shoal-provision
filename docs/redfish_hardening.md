# Redfish Operations Hardening (Phase 6, Milestone 1)

This document summarizes the reliability, idempotency, and observability work added in Phase 6 for Redfish operations (virtual media, boot override, power control).

## What changed


## Retry & backoff


## Idempotency

- Virtual media insert checks the member resource. If `Inserted=true` and `Image==requested`, it is treated as success.
- Eject checks `Inserted`; if already false, treated as success.
- Boot override checks `BootSourceOverrideEnabled` and `BootSourceOverrideTarget` before patching; when already set, treated as success.

## Vendor quirks

The registry provides best-effort normalization for common vendor differences:

- Boot target mapping (canonical `Cd`, `Pxe`, `Hdd`, `Usb` → vendor values).
- Action name overrides for `InsertMedia`/`EjectMedia` if needed.
- Slot selection hints based on `MediaTypes`/member `Id` substrings.
- Optional post-insert delay to accommodate BMCs that need propagation time.

The registry is intentionally small and easily extensible in `internal/bmc/quirks.go`.

## Reconciliation on restart

`(*bmc.Service).ReconcileState(ctx, bmcName, expectedImage, ensureBootOnce)` offers a minimal reconciliation pass:

- If an `expectedImage` is provided and not mounted, it re-inserts it idempotently.
- When `ensureBootOnce` is true, it re-applies one-time boot override to CD if not already set.

Future work can extend this to power state expectations and desired policy persistence.

## Security & logging notes

- Credentials are never logged. Logs include operation labels and status codes; a `CorrelationID` context key is defined for future cross-service tracing.
- TLS validation is permissive for BMCs in trusted networks; this behavior is unchanged but documented by policy.

## Testing

- Unit tests cover retry behavior, power control retry, quirks mapping, and reconciliation flows using mock Redfish servers.
- Full validation remains green via `go run build.go validate`.

## Tuning

Retry/backoff parameters are currently hard-coded conservatively. If operational data suggests different profiles per vendor or operation, we can externalize them via configuration in a follow-up.
