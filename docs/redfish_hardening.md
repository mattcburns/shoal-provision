# Redfish Operations Hardening (Phase 6, Milestone 1)

This document summarizes the reliability, idempotency, and observability work added in Phase 6 for Redfish operations (virtual media, boot override, power control).

## What changed

- Standardized retry/backoff with jitter for Redfish HTTP calls (5xx/429/network timeouts retryable).
- Idempotent virtual media insert/eject and one-time boot override logic (checks current state and avoids redundant writes).
- Vendor quirks registry to normalize differences in boot targets, action names, and slot selection heuristics.
- Metrics: per-operation request counts, durations, and retry counters via Prometheus.
- Reconciliation on restart to reassert expected media/boot state when desired (opt-in API).

## Retry & backoff

- Exponential backoff with base 300ms (bounded) and light jitter; 4 attempts by default for critical ops.
- Retries on: HTTP 5xx, 429 Too Many Requests, and network timeouts/temporary errors.
- Metrics recorded for each attempt: `shoal_provisioner_redfish_requests_total`, `shoal_provisioner_redfish_request_duration_seconds`, `shoal_provisioner_redfish_retries_total`.

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
