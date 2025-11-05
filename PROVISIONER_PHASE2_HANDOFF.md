# Provisioner Phase 2 — Handoff Summary

Owner: Provisioning WG
Branch: feature/provisioner-phase2-redfish-http-client
Last updated: 2025-11-05

This document summarizes the current status of Provisioner Phase 2 work, what’s implemented, how to run and test it, and a prioritized plan for the next agent to continue seamlessly.

---

## 1) High-Level Status

- Phase 1 (baseline) is implemented and verified:
  - Controller service with jobs API and webhook.
  - SQLite store with schema/migrations and job event log.
  - Deterministic task ISO builder (file-based stub, sufficient for E2E).
  - Redfish no-op client for orchestration without real BMCs.
  - End-to-end tests for success, failure, timeout paths.
  - API auth middleware (none/basic/jwt), webhook shared secret.

- Phase 2 (in-progress):
  - New Redfish HTTP client (real implementation) with discovery and core ops.
  - Vendor-aware adaptations for iDRAC, iLO, Supermicro.
  - Resilience features: session auth (X-Auth-Token), retries/backoff, TLS policy toggle.
  - ESXi no-webhook flow: worker scaffolding for BMC readiness (Ping) and placeholder for power-state polling.
  - Controller flag/env wiring to switch between noop and real clients (default remains noop for Phase 1 compatibility).

All repository tests pass:
- Run: `go run build.go validate`
- Coverage currently reported by the build pipeline.

---

## 2) Repo Map (Relevant Files/Dirs)

- Controller entrypoint
  - cmd/provisioner-controller/main.go (flags/env, mux, worker wiring, media serving)
- Provisioner components
  - internal/provisioner/api/ (jobs API, webhook, validation, auth)
  - internal/provisioner/store/store.go (SQLite + migrations + leasing)
  - internal/provisioner/iso/ (deterministic task ISO builder)
  - internal/provisioner/jobs/worker.go (orchestrates ISO build + Redfish ops + waits)
  - internal/provisioner/redfish/
    - client.go (interfaces, Noop client, factory, redactions)
    - http_client.go (real Redfish client: discovery, insert/eject, boot override, reset, session, retries, TLS)
    - http_client_test.go (unit tests for the real client)
- Shared models
  - pkg/provisioner/models.go (Job, Server, JobEvent, enums)
- Design docs
  - design/028_Redfish_Operations.md (updated vendor guidance, ops, and testing strategy)

---

## 3) What’s Implemented (Phase 2)

- Real Redfish HTTP client (internal/provisioner/redfish/http_client.go):
  - Discovery
    - GET /redfish/v1/ → Systems → pick first
    - System.Links.ManagedBy → Manager OR fallback to ServiceRoot.Managers
    - Manager.VirtualMedia OR fallback to System.VirtualMedia
    - Enumerates VirtualMedia instances; selects two CD/DVD-capable entries deterministically
  - Virtual media
    - InsertMedia payload includes Inserted:true, TransferProtocolType:"URI", WriteProtected:true
    - Idempotent mount: skip if identical Image already mounted and Inserted
    - EjectMedia for cleanup
  - One-time boot
    - BootSourceOverrideEnabled: Once, BootSourceOverrideTarget: Cd
    - Adds BootSourceOverrideMode:"UEFI" for iDRAC & iLO; omitted for Supermicro
  - Reset
    - GracefulRestart, fallback to ForceRestart, then PowerCycle
  - Session auth
    - On 401, POST SessionService/Sessions, read X-Auth-Token, retry original request once
  - Retries/backoff
    - Exponential backoff with jitter for 5xx/429 and transport errors (bounded attempts)
  - TLS policy
    - Optional InsecureSkipVerify for lab BMCs (configurable)
  - Liveness
    - Client.Ping() → GET /redfish/v1/

- Worker scaffolding for ESXi
  - awaitBMCReady(ctx, client, deadline): ping loop until BMC/API responsiveness after reset
  - pollPowerStatePlaceholder(...): placeholder to be replaced with real power-state heuristics

- Controller configuration (cmd/provisioner-controller/main.go)
  - REDFISH_MODE=http|noop (default: noop)
  - REDFISH_INSECURE_TLS=true|false
  - REDFISH_TIMEOUT, REDFISH_RETRIES (retries currently governed internally; can be exposed fully later)
  - Continues to serve /media/tasks/{job_id}/task.iso

- Tests (internal/provisioner/redfish/http_client_test.go)
  - Full mount → boot override → reset → unmount flow
  - Discovery fallback when VirtualMedia is under System
  - Idempotency (insert skip when same Image)
  - Session retry on 401
  - Retry/backoff on transient 5xx
  - Vendor mode behavior: UEFI for iDRAC/iLO; none for Supermicro

---

## 4) How to Run

- Build & validate (required workflow per AGENTS.md):
  - `go run build.go validate`

- Run controller binary:
  - Defaults simulate Phase 1 with noop Redfish client.
  - Example (real client, lab BMCs):
    - `REDFISH_MODE=http REDFISH_INSECURE_TLS=true ./build/shoal -addr :8080`
    - Optional:
      - `REDFISH_TIMEOUT=30s` (tuned per environment)
  - Media serving path for task ISO:
    - `GET /media/tasks/{job_id}/task.iso`

- Auth:
  - Jobs API auth: none|basic|jwt via flags/env in controller
  - Webhook auth: shared secret via X-Webhook-Secret header

---

## 5) Current Behavior by Vendor (Heuristics)

- Dell iDRAC:
  - InsertMedia payload expects/accepts TransferProtocolType: "URI" and Inserted:true
  - Boot override sets Cd plus UEFI mode for reliability
- HPE iLO:
  - Same InsertMedia shape as iDRAC; Inserted:true commonly required
  - Boot override sets Cd plus UEFI mode
- Supermicro:
  - InsertMedia shape is generally tolerant; mode not added by default
- All vendors:
  - Idempotent mounting (skip if same image present)
  - Reset fallback chain (Graceful → ForceRestart → PowerCycle)
  - Post-reset, expect brief BMC/API unavailability

Note: Vendor detection is pragmatic: case-insensitive checks against provided Server.Vendor, matching common strings for "dell", "idrac", "hpe"/"hp", "ilo", "supermicro".

---

## 6) Phase 2 Roadmap (Prioritized Next Steps)

1) ESXi workflow (no webhook)
- Implement power-state/API-up polling per design/028:
  - Poll ServiceRoot readiness, then Systems endpoints
  - Observe power state transitions (On → cycling → On) with stabilization window
  - Cleanup: eject media and perform final reset
- Add contract tests to simulate API downtime and transitions

2) Controller reconciliation on restart
- On startup, scan jobs in provisioning state
- Re-discover and resume: extend leases, continue wait/cleanup
- Tests for restart mid-provision and proper recovery

3) Vendor profile expansion
- Add per-vendor retry tuning, rate-limit handling (429 with Retry-After)
- Consider InsertMedia credentials fields if a target vendor requires them (keep default off)

4) Session auth hardening
- Token refresh if expired
- Optional logout on cleanup (if beneficial and safe)

5) Observability & metrics
- Structured job events for each Redfish op (op, status, elapsed, attempts)
- Export basic metrics counters/histograms where appropriate
- Redact all secrets (passwords, tokens, Authorization headers)

6) Dispatcher and Maintenance OS (025–026)
- Quadlet/systemd targets per workflows (Linux, Windows, ESXi)
- Dispatcher (Go) validates task ISO, reads recipe, starts target, posts webhook status
- Integration tests spanning controller → dispatcher path

7) Security & artifact delivery
- Optional signed URLs for task.iso
- Tighten media serving access (time-limited tokens or BMC-scoped access)

---

## 7) Acceptance Criteria (Phase 2 Target)

- Real Redfish client:
  - Discover System/Manager/VirtualMedia robustly across iDRAC/iLO/Supermicro
  - Mount maintenance.iso + task.iso reliably or idempotently skip when already mounted
  - Set one-time boot to CD (with UEFI mode on vendors that require it)
  - Reset host with fallback strategy on unsupported types
  - Eject media and reset on cleanup

- ESXi flow (no webhook):
  - Detect reboot progress; proceed to cleanup after stable API availability
  - Clear, attributed job events for each phase, including timeouts

- Resilience:
  - Retries/backoff for transient failures
  - Session-based auth with refresh on 401
  - Controller resumes provisioning jobs after a restart

- Tests:
  - Redfish contract tests covering discovery variants, ops, idempotency, retry, session
  - ESXi simulation tests for no-webhook path and cleanup
  - Integration tests remain green for Phase 1 scenarios

---

## 8) Risks and Considerations

- Vendor variability:
  - VirtualMedia location (Managers vs Systems)
  - InsertMedia quirks across firmware versions
  - ResetType acceptance and timing
- TLS policies:
  - InsecureSkipVerify only for non-production; expose clearly via config
- Long-running orchestration:
  - Robust timeouts and budgets for each phase are key
- Dispatcher/OS integration:
  - Requires careful handoff (task ISO content/paths, systemd/Quadlet targets, and webhook reliability)

---

## 9) Quick Reference (Flags/Env)

- Redfish client selection:
  - Env: `REDFISH_MODE=http|noop` (default: noop)
- TLS policy:
  - Env: `REDFISH_INSECURE_TLS=true|false` (default: false)
- Timeouts:
  - Env: `REDFISH_TIMEOUT=30s`
- Jobs API auth (see internal/provisioner/api/auth.go):
  - `AUTH_MODE=none|basic|jwt`
  - Basic creds: `BASIC_USER`, `BASIC_PASS`
  - JWT: `JWT_SECRET`, `JWT_AUDIENCE`, `JWT_ISSUER`
- Webhook:
  - `WEBHOOK_SECRET=<shared-secret>`

---

## 10) How Another Agent Should Proceed

1) Implement ESXi power-state polling and stabilization in worker:
   - Replace `pollPowerStatePlaceholder()` with real logic per design/028.
   - Add unit tests that simulate API downtime and recovery windows.

2) Add reconciliation on restart:
   - New controller startup routine to rehydrate provisioning jobs and resume orchestration.
   - Unit/integration tests for mid-flight restarts.

3) Expand vendor profiles:
   - Add capability flags and more granular retry/backoff per vendor.
   - Confirm Boot override behavior with/without explicit UEFI.

4) Improve session lifecycle:
   - Token expiry detection and refresh.
   - Optional session teardown in cleanup (only if beneficial).

5) Observability:
   - Emit job events with op names (discover, mount.maintenance, mount.task, boot-override, reset, cleanup.*)
   - Integrate simple metrics counters (if repository policy allows; otherwise focus on events/logs).

6) Keep tests green:
   - Always run `go run build.go validate` before concluding work.
   - Maintain license headers and update docs where behavior changes (README, DEPLOYMENT, design docs).

---

## 11) Contact Points and Docs

- Start here: design/020_Provisioner_Architecture.md (index)
- Controller service: design/021_Provisioner_Controller_Service.md
- Recipe schema: design/022_Recipe_Schema_and_Validation.md
- Task ISO builder: design/023_Task_ISO_Builder.md
- Maintenance OS/dispatcher: design/024, design/025, design/026
- Redfish ops and vendor notes: design/028_Redfish_Operations.md (updated)

This handoff equips the next agent to continue Phase 2 by finalizing the ESXi workflow, improving resilience and observability, and integrating the dispatcher/Maintenance OS path per the design series. The core real Redfish client and controller wiring are in place and validated with unit tests; Phase 1 E2E flows remain fully functional via the noop client by default.