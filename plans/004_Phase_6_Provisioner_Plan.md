# Phase 6: Hardening, Workflows, and Operations

Status: Planned  
Owners: Provisioning Working Group  
Last updated: 2025-11-09

Summary

Phase 6 focuses on hardening the provisioner to production quality and completing end-to-end workflows with strong operational guarantees. This includes robust Redfish operations, finalized Linux/Windows/ESXi workflows, consistent error handling and webhooks, security tightening, CI/CD pipelines for all artifacts (dispatcher, maintenance OS, tool containers), an expanded test strategy, release/deployment practices, operations runbooks, and license/compliance checks. This phase turns the provisioner from feature-complete prototypes into an operable, supportable system.

References

- design/020_Provisioner_Architecture.md — Top-level architecture and roadmap
- design/021_Provisioner_Controller_Service.md — Controller API, state machine, persistence
- design/022_Recipe_Schema_and_Validation.md — Authoritative recipe schema
- design/023_Task_ISO_Builder.md — Deterministic task ISO builder
- design/024_Maintenance_OS_Build_with_bootc.md — Bootc-based maintenance OS
- design/025_Dispatcher_Go_Binary.md — On-host dispatcher
- design/026_Systemd_and_Quadlet_Orchestration.md — Targets and service graph
- design/028_Redfish_Operations.md — Redfish orchestration details
- design/029_Workflow_Linux.md — Linux workflow specifics
- design/030_Workflow_Windows.md — Windows workflow specifics
- design/031_Workflow_ESXi.md — ESXi handoff workflow
- design/032_Error_Handling_and_Webhooks.md — Reliability and payloads
- design/033_Security_Model.md — AuthN/Z, secrets, hardening
- design/034_CI_CD_Pipelines_and_Artifacts.md — Pipelines and artifact management
- design/035_Test_Strategy.md — Unit, integration, E2E, performance
- design/036_Release_and_Deployment.md — Packaging and rollout
- design/037_Operations_and_Runbook.md — Day-2 operations and SRE guidance
- design/038_Compliance_and_Licensing.md — License policy and checks

Scope (Phase 6)

In Scope

- Redfish operation robustness (timeouts, retries, idempotency, vendor variance) per 028.
- Completion and hardening of Linux, Windows, and ESXi workflows per 029–031 with systemd/Quadlet (026) and dispatcher (025).
- Unified error handling and webhook reliability/semantics per 032.
- Security model enforcement across controller API, webhooks, and embedded registry per 033.
- End-to-end CI/CD for dispatcher, maintenance OS, tool images, and recipes per 034.
- Expanded tests (unit, integration, E2E, performance, security) per 035.
- Release and deployment practices (artifacts, config, versioning) per 036.
- Operations playbooks, observability, alerts, backup, and recovery per 037.
- Compliance and licensing policy and automation per 038.

Out of Scope

- New workflows beyond Linux/Windows/ESXi in this phase.
- Inventory/discovery services and non-provisioning Shoal features.
- Registry enhancements beyond Phase 5 (e.g., catalog UI, replication).

Milestones and Deliverables

1) Redfish Operations Hardening (028)
- Tasks:
  - Implement robust retry/timeout/backoff policies for: Insert/Eject virtual media, one-time boot override, power cycle.
  - Idempotent reconciliation on restart: verify/repair mounts, boot settings, and power state.
  - Vendor variance handling: tolerant parsing, known quirks registry, and defensive checks.
  - Structured logging with correlation IDs; metrics for call latencies, retries, failures.
- Tests: Mock Redfish coverage for happy paths and failures; injection of transient errors; restart recovery scenarios.
- Acceptance:
  - All Redfish calls are safely retryable and idempotent.
  - Controller restarts reconcile correctly without orphaned mounts.
  - Metrics and logs allow diagnosis of failed steps.

2) Linux Workflow Completion (029) with Quadlet (026) and Dispatcher (025)
- Tasks:
  - Finalize partition, image-linux, bootloader-linux, config-drive units.
  - Dispatcher integration: recipe.env, layout.json, auxiliary files consumed by units.
  - Mount point conventions, permissions, and idempotency guarantees.
- Tests: E2E on VM with small rootfs artifact; failure-mode tests for each step.
- Acceptance:
  - Fresh disk install boots into OS; repeat run is safe (idempotent where feasible).
  - Clear failure attribution to specific units.

3) Windows Workflow Completion (030)
- Tasks:
  - Finalize image-windows and bootloader-windows units; integrate unattend.xml.
  - Handle WIM apply and EFI configuration with clear error surface.
- Tests: E2E on VM with small WIM; negative tests for unattend and partition mismatches.
- Acceptance:
  - Windows install completes and first boot succeeds; logs attribute failures precisely.

4) ESXi Workflow Handoff (031)
- Tasks:
  - Handoff flow to vendor ISO + task.iso Kickstart; document controller orchestration sequence.
  - Minimal controller/dispatcher coordination as required.
- Tests: E2E handoff in VM with mocked BMC media behavior.
- Acceptance:
  - ESXi installer boots and consumes Kickstart; controller transitions and cleanup succeed.

5) Error Handling and Webhooks (032)
- Tasks:
  - Unified webhook payloads; shared secret or mTLS; idempotent delivery.
  - Map failing systemd unit to Job.failed_step and event log.
- Tests: Duplicate delivery, ordering anomalies, and network outages.
- Acceptance:
  - Webhooks are authenticated; duplicate-safe; failures consistently attributed.

6) Security Model (033)
- Tasks:
  - Auth modes for API and webhooks (basic/JWT; secret/mTLS) and registry parity.
  - Redaction of sensitive fields; no secrets in logs; secrets sourced from env/FS.
- Tests: Negative auth tests, redaction unit tests, policy checks.
- Acceptance:
  - All endpoints enforce configured auth; no secrets appear in logs; policies documented.

7) CI/CD Pipelines and Artifacts (034)
- Tasks:
  - Build pipelines for dispatcher (Go), maintenance OS (bootc), and tool containers.
  - Publish artifacts to embedded registry (from Phase 5) and/or local storage.
  - Reproducibility notes and provenance metadata.
- Tests: Pipeline smoke tests; artifact pull/pin tests; reproducibility checks.
- Acceptance:
  - One-click CI pipeline produces versioned artifacts; consumers can pull/pin them reliably.

8) Test Strategy Expansion (035)
- Tasks:
  - Unit, integration (mock Redfish, loopback ISOs), E2E (VM), performance (parallel jobs), security tests.
- Acceptance:
  - go run build.go validate passes; target coverage threshold met; E2E green for Linux and Windows.

9) Release and Deployment (036)
- Tasks:
  - Binary packaging, config templates, environment variables/flags audit, sample systemd units.
  - Versioning policy; upgrade notes; migration for DB and storage.
- Acceptance:
  - Release bundle with documented install/upgrade; rollback guidance.

10) Operations and Runbook (037)
- Tasks:
  - Day-2 ops tasks: backups, restores, GC operations, certificate rotation, scaling knobs.
  - SLOs/alerts: job latency, failure rate, Redfish errors, storage use.
- Acceptance:
  - Runbook covers common procedures; dashboards and alerts are defined.

11) Compliance and Licensing (038)
- Tasks:
  - License policy (AGPL-compatible only) and automated checks in CI.
  - Notice file generation and third-party attribution.
- Acceptance:
  - CI fails on incompatible licenses; notices generated; docs updated.

Acceptance Criteria (Summarized)

- Redfish operations are idempotent, observable, and resilient under failure/restart.
- Linux, Windows, and ESXi workflows complete E2E with clear failure attribution.
- Webhooks are authenticated and idempotent; Job.failed_step is accurate.
- Security controls enforced; sensitive data never logged; policies documented.
- CI/CD produces dispatcher, maintenance OS, and tool images reproducibly; artifacts are retrievable from embedded registry or configured stores.
- Tests expanded across unit/integration/E2E/performance; coverage threshold achieved; go run build.go validate passes.
- Release artifacts, deployment docs, and configuration templates provided.
- Operations runbook, dashboards, and alerts in place; backup/restore documented.
- License compliance automated in CI; notices generated.

Risks & Mitigations

- Vendor Redfish variance → Implement tolerant parsing, retries, and a quirks registry; expand mock coverage.
- Windows/ESXi edge cases → Documented constraints; explicit failure messages; targeted negative tests.
- Concurrency and performance under load → Configurable worker limits and timeouts; performance tests; observability.
- Secret handling → Centralized configuration, redaction, and audit checks.

Dependencies

- Phases 3–5 components complete or available: controller, dispatcher skeleton, systemd/Quadlet scaffolding, embedded registry.
- No new external dependencies without AGPL-compatible licenses; verify via CI policy (038).

Start Checklist

- [ ] Branch: docs/provisioner-phase-6-plan
- [ ] Baseline: go run build.go validate → PASS on master
- [ ] Designs reviewed: 020–026, 028–038
- [ ] Test environment prepared: VM/BMC lab, small artifacts, webhook secret
- [ ] Observability targets identified (metrics, logs, dashboards)

Success Metrics

- E2E success for Linux and Windows workflows; ESXi handoff validated.
- Redfish error rate below target; retries/backoffs visible and effective.
- CI green with coverage ≥ target; build artifacts reproducible.
- Operational runbook covers ≥90% common tasks; on-call can resolve incidents using docs/alerts.

Change Log

- v0.1 (2025-11-09): Initial Phase 6 plan for hardening, workflows completion, and operations.
