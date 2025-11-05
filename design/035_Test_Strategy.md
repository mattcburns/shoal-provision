# 035: Test Strategy

Progress (2025-11-05)
- Implemented Phase 1 provisioner tests: store (migrations/CRUD/leasing), API (jobs/webhook), validator (022), worker (awaitWebhook/lease heartbeats), ISO placeholder determinism, and end-to-end integration (success, failure, timeout).
- Validation pipeline passes and repo coverage increased; see build output for current percentage.

Status: Proposed
Owners: Provisioning Working Group
Last updated: 2025-11-05

Summary

This document defines the end-to-end test strategy for the Layer 3 bare‑metal provisioner. It covers the test pyramid (unit → integration → end‑to‑end), mocking/faking of external systems (Redfish, OCI registry, media server), determinism and reproducibility checks, concurrency and recovery testing, security tests, coverage targets, and CI pipeline integration. It is designed so contributors can implement, extend, and reliably run tests locally and in CI while adhering to repository protocols.

Goals

- Establish clear, enforceable expectations for test scope, depth, and quality.
- Provide deterministic, reproducible tests (no flakes; fixed seeds; bounded timeouts).
- Validate functional correctness, error handling, idempotency, and recovery across components.
- Ensure security controls (authN/Z, secrets redaction, signed URLs) are continuously verified.
- Integrate with the repo’s standard workflow and gates (format, lint, test, build, coverage, license compliance).

Non‑Goals

- Full hardware qualification across all vendors (reserved for staging/hardware-in-the-loop).
- Performance/stress certification under extreme scale (lightweight load tests only in CI).
- UI/visual regression testing (not in scope for the provisioning controller).

References

- 020_Provisioner_Architecture.md
- 021_Provisioner_Controller_Service.md
- 022_Recipe_Schema_and_Validation.md
- 023_Task_ISO_Builder.md
- 024_Maintenance_OS_Build_with_bootc.md
- 025_Dispatcher_Go_Binary.md
- 026_Systemd_and_Quadlet_Orchestration.md
- 027_Embedded_OCI_Registry.md
- 028_Redfish_Operations.md
- 029_Workflow_Linux.md
- 030_Workflow_Windows.md
- 031_Workflow_ESXi.md
- 032_Error_Handling_and_Webhooks.md
- 033_Security_Model.md
- 034_CI_CD_Pipelines_and_Artifacts.md
- AGENTS.md (workflow mandates; test gates)

1) Test Pyramid

- Unit tests (largest volume)
  - Pure package-level tests for business logic, validation, small adapters.
  - Fast (<100ms typical per test), deterministic, hermetic (no network/disk beyond temp dirs).
- Integration tests (moderate volume)
  - Exercise multi-component flows in-process with fakes/mocks for external systems.
  - Include SQLite with migrations, ISO builder via temp dirs, mock Redfish, fake OCI registry, media server with signed URLs.
- End‑to‑End smoke (small volume)
  - Controller process with fakes; job submission → provisioning orchestration → webhook/cleanup → completion.
  - Optional nightly VM tests for the maintenance OS when available; not blocking PR merges.

2) Coverage Targets

- Global target: ≥ 80% line coverage.
- Critical packages:
  - Controller API, jobs/state machine, ISO builder, Redfish client adapters, signed URL validator, webhook handler: ≥ 90%.
- No test skips in critical paths; documented exceptions require rationale in the test.

3) Mocks and Fakes

- Mock Redfish server
  - Implements minimal Redfish endpoints used: ServiceRoot, Systems, Managers, VirtualMedia (Insert/Eject), Boot override, Reset.
  - Vendor profiles: iDRAC, iLO, XCC, Supermicro behavior variants (path layout, timing, 429 rate-limit, delayed API readiness).
  - Fault injection: timeouts, 5xx, 401 (session refresh), 409/429 with Retry-After, partial insert/eject failures.
- Fake OCI registry
  - Minimal /v2/ subset: blobs upload lifecycle, manifests PUT/GET, HEAD, Range for blob GET.
  - Auth modes: none/basic.
  - Validates digests; stores content-addressed blobs in temp dir; simulates large blobs via sparse files.
- Media server
  - Serves task.iso with signed URL validation and Range support.
  - Validates signature (HMAC) and expiry with configurable skew.
- Time harness
  - Abstraction for advanceable clock to test lease expiry, backoff, and timeouts deterministically (injectable clock or interface).
- SQLite harness
  - Test DB creation, migrations, and rollback in temp file; PR tests must not use shared global DB.

4) Unit Tests (by area)

- Recipe schema and validation (022)
  - Valid/invalid examples; conditional requirements per task_target; size limits enforcement.
  - Error details format: path/code/message.
- ISO builder (023)
  - Determinism: given fixed inputs and SOURCE_DATE_EPOCH, verify identical SHA‑256 across runs.
  - Layout checks: expected files present, read‑only perms, bounded size.
  - Signed URL generation/verification: happy path, expiry, tamper, skew handling.
- Controller API (021)
  - POST /jobs: success and failure (invalid recipe, unknown serial, conflicts, idempotency key behavior).
  - GET /jobs/{id}: returns status and events; 404 on unknown.
  - Webhook: success/failure; auth required; duplicate payload idempotency.
- Jobs/state machine (021, 032)
  - Transitions: queued → provisioning → {succeeded|failed} → complete.
  - Lease/worker concurrency: single ownership, heartbeat, lease steal on expiry.
  - Recovery: restart mid‑provisioning → reconciliation path.
- Redfish client (028)
  - Discovery logic branch coverage (Managers vs Systems VirtualMedia).
  - Insert/Eject, boot override, reset; retries/backoff classes; vendor profile switches.
  - Idempotency checks when media already inserted or override already set.
- Security (033)
  - Auth middlewares (API/registry); password hashing; rate-limit stubs.
  - Webhook secret enforcement; absence → 401/403.
  - Media signed URL validator; IP-binding (if enabled) behaviors; redaction in logs.

5) Integration Tests (in-process system tests)

- Controller + SQLite + ISO builder + mock Redfish + media server
  - Happy path (Linux): POST job → build task.iso → Redfish mounts → webhook success → cleanup → complete.
  - Failure (Linux): induce bootloader failure → webhook failed with precise unit → cleanup.
  - Windows path: WIM unavailable → image-windows failure; verify failed_step and cleanup.
  - ESXi path: dual ISO mount; poll‑based completion; timeout scenario triggers redfish.poll failure.
- Registry integration (027)
  - oras push/pull small artifacts; blob PUT digest check; Range GET; basic auth.
  - podman push/pull of a tiny test image (if available in CI) or a mocked layer manifest.
- Determinism and HTTP behaviors
  - ETag correctness on task.iso; Range serving segments; content-length headers.
- Security integration
  - API auth required; unauthorized returns 401; webhook requires secret; logs redact secrets.
- Concurrency and backpressure
  - Multiple jobs across distinct servers; ensure per‑server serialization.
  - Worker concurrency limit; ensure correct throttling when I/O heavy phases occur.

6) End‑to‑End (E2E) Smoke

- Controller process with in‑process mocks:
  - Simulate full Linux workflow including signed ISO and final cleanup.
  - Idempotency: duplicate webhooks do not double transition; retry cleanup without harm.
- Optional VM smoke (nightly/non‑blocking):
  - Maintenance OS boot with tiny task.iso (no real imaging) to verify dispatcher mounts task.iso and triggers a trivial target that posts a webhook.

7) Concurrency, Idempotency, and Recovery

- Lease behavior
  - Single worker acquires queued job; second worker cannot double pick.
  - Heartbeat/lease extension; lease steal after expiry; ensure no double orchestration.
- Redfish idempotency
  - Insert when already inserted; Eject when already ejected; boot override repeated; reset debounce.
- Webhook idempotency
  - Accept duplicates; ensure no extra state transitions; verify dedupe cache/window.
- Controller restart tests
  - Restart between phases (after mount, before webhook, after webhook before cleanup); reconciliation logic continues safely.

8) Performance and Scalability (lightweight)

- Large blob simulation
  - Use sparse file to emulate multi‑GB artifact; measure upload (PATCH/PUT), digest verification, and Range reads (sanity only; not a benchmark).
- Timeouts and backoff
  - Validate exponential backoff and cap; request timeouts enforced; hung connections do not starve workers.
- Memory usage boundaries
  - Verify streaming paths (oras → tar/wimapply) are not loading whole artifacts into memory (sanity checks via small monitors if feasible).

9) Security Tests

- AuthN/Z
  - API endpoints require auth; role/permission enforcement if introduced.
  - Registry requires auth for push; optional for pull per configuration.
- Transport and secrets
  - Media signed URLs: expiration and tamper detection; signature not logged.
  - Webhook secret required; wrong secret → 401/403; success with correct secret.
- Logs redaction
  - Ensure no secret values appear in logs (redaction test via log capture).
- Fuzzing (optional)
  - Fuzz recipe validator and webhook handler inputs for robustness (if supported by toolchain).

10) Test Data and Fixtures

- Recipes
  - Valid Linux and Windows recipes; ESXi recipe with ks_cfg; malformed variants for negative tests.
- Layouts
  - Minimal GPT examples; missing fields; invalid sizes/type_guid patterns.
- Unattend/user-data
  - Sanitized minimal content; avoid secrets; include large payload boundary tests.
- Vendor profiles
  - JSON/configs for mock Redfish to emulate vendor quirks and timing.

11) Flake and Stability Policy

- No sleeps for timing; use controllable clock and synchronization points.
- All external calls mocked/faked; no real network to the Internet.
- Upper bounds on test durations; per‑test timeouts; global suite timeout enforced.
- Randomized tests must seed deterministically and record seed on failure.

12) CI Integration and Gates

- PR validation (must pass):
  - Format, lint, unit tests, integration tests subset, coverage threshold, dead code scan, license checks, basic CVE scan.
- Main branch:
  - Same as PR plus extended integration suite (including determinism and registry tests).
- Release tags:
  - Full suite; reproducible checks; SBOM/sign verification tests; smoke E2E.
- Command of record:
  - `go run build.go validate`
- Reports:
  - Coverage report generated (HTML and summary); junit-compatible output if supported.

13) Local Developer Workflow

- Fast cycle:
  - Run package unit tests; run focused integration tests via build tags.
- Reproducibility checks:
  - Export `SOURCE_DATE_EPOCH` for ISO determinism tests.
- Debugging:
  - Enable verbose logs for failing tests; capture logs to artifacts for diagnosis.

14) Acceptance Criteria

- Tests exist for all critical flows and error paths across the controller, ISO builder, Redfish client, and security boundaries.
- Coverage meets targets (global and per critical package).
- Determinism tests for task.iso pass and are stable across runs/environments (when tool versions are fixed).
- Concurrency/lease/idempotency behaviors are validated; no double‑processing occurs.
- Security tests enforce auth, signed URL validation, webhook secrets, and log redaction.
- CI pipelines enforce gates; PRs cannot merge with failing tests or insufficient coverage.
- `go run build.go validate` passes locally and in CI.

15) Open Questions

- Should nightly jobs include VM‑based maintenance OS boot tests by default (resource cost vs. signal value)?
- Do we add a heavier load/perf job weekly (longer time budgets, larger sparse blobs, concurrent job storms)?
- Should we integrate fuzzing targets into CI (budgeted, with crash artifact capture and issue filing)?

Change Log

- v0.1 (2025-11-05): Initial test strategy spanning unit, integration, end‑to‑end, mocks/fakes, determinism, coverage, and CI gates.