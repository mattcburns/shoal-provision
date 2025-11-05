# 037: Operations and Runbook

Status: Proposed
Owners: SRE / Provisioning Working Group
Last updated: 2025-11-05

Summary

This document is the day-2 operations and SRE runbook for the Layer 3 bare‑metal provisioner. It defines monitoring and alerting, dashboards, incident response playbooks, capacity and scaling guidance, maintenance procedures (backups, GC, rotations), and concrete step-by-step remedies for the most common failure modes. It complements the deployment, security, and CI/CD designs in 033–036.

Related

- 020_Provisioner_Architecture.md (system overview)
- 021_Provisioner_Controller_Service.md (APIs, state machine, storage)
- 023_Task_ISO_Builder.md (deterministic ISO generation)
- 027_Embedded_OCI_Registry.md (/v2/ registry)
- 028_Redfish_Operations.md (BMC flows)
- 032_Error_Handling_and_Webhooks.md (error taxonomy, webhooks)
- 033_Security_Model.md (auth, TLS, secrets, signed URLs)
- 034_CI_CD_Pipelines_and_Artifacts.md (pipelines, artifacts)
- 036_Release_and_Deployment.md (packaging, rollout, rollback)

1) Service Overview and Responsibilities

- Controller
  - User API for jobs (POST, GET)
  - Internal webhook API (terminal success/failure)
  - Redfish orchestration (insert/eject media, boot override, reset)
  - Task ISO build and HTTP serving (Range support, signed URLs)
  - Optional: embedded /v2/ OCI registry
  - Persistence: SQLite DB, task ISO storage, registry storage (OCI layout)
- Maintenance OS
  - Dispatcher (mounts task.iso, validates recipe, starts systemd target)
  - Quadlet-driven containers (partitioning, imaging, bootloader, config drive)
  - Emits success/failure webhook (no inbound services)

2) Monitoring and Observability

2.1 Metrics (must have)

- Jobs
  - job_total{status} (counter per terminal status and transitions)
  - job_duration_seconds{outcome,task_target} (histogram)
  - job_transitions_total{from,to} (counter)
- Redfish
  - redfish_request_duration_seconds{op,vendor} (histogram)
  - redfish_requests_total{op,code,vendor} (counter)
  - redfish_retries_total{op,vendor} (counter)
- Webhooks
  - webhook_received_total{status} (counter)
  - webhook_processing_duration_seconds (histogram)
  - webhook_auth_failures_total (counter)
- ISO / Media
  - iso_build_duration_seconds (histogram)
  - iso_size_bytes (gauge or histogram)
  - media_requests_total{code} (counter)
- Registry (/v2/)
  - registry_upload_bytes_total, registry_download_bytes_total (counters)
  - registry_inflight_uploads (gauge)
  - registry_blob_count, registry_storage_bytes (gauges)
  - registry_request_duration_seconds{route,code} (histogram)
- System/Host
  - process_cpu_seconds_total, process_resident_memory_bytes
  - disk_free_bytes{path=/var/lib/shoal/tasks}, disk_free_bytes{path=/var/lib/shoal/oci}
  - open_fds, goroutines (if exported)
- HTTP
  - http_requests_total{route,code}
  - http_request_duration_seconds{route,code}

2.2 Logs (structured)

- Include fields: time, level, job_id, server_serial, step (taxonomy or unit), worker_id, op, code, duration_ms, message
- Redact: secrets, tokens, unattend.xml/user-data content (log sizes/hashes only)
- Key phases to log:
  - Validation (recipe, schema id)
  - ISO build (path, size, sha256)
  - Redfish: discovery, insert, boot-override, reset, polling milestones
  - Webhook: status, failed_step (if any), dedupe result
  - Cleanup: eject both drives, final reset
  - Errors: taxonomy key and whether retried

2.3 Tracing (optional)

- Span per job; child spans for ISO build, Redfish phases, webhook handling
- Correlate span ids with logs

2.4 Dashboards

- Overview
  - Jobs by status (stacked area); job durations (P50/P90/P99); active jobs
  - HTTP 5xx rate; error taxonomy top-N
- Redfish
  - Request rate/durations by operation and vendor; retry counts; failure codes
  - Polling time to API readiness post-reset
- Webhooks
  - Success vs failure rates; auth failures; dedupe rate
- Media/ISO
  - iso build time and sizes; media requests by code; signed URL validation failures
- Registry
  - Storage used/free; uploads/downloads; in-flight uploads; large blob latency
- Storage & Host
  - Disk free for tasks and oci roots; CPU/mem; file descriptors
- SLOs
  - Job success rate; P95 job completion time per workflow

3) Alerting (suggested rules & thresholds)

- Controller availability
  - http_5xx_rate > 1% for 5m on any critical route → page
  - process not scraping metrics or liveness checks failing → page
- Job orchestration
  - jobs_provisioning_stuck > 0 where now - updated_at > 2h (Linux/Windows) or > 90m (ESXi) → page
  - webhook_auth_failures_total rate > 0 for 5m → page (likely secret mismatch)
  - job_failed_rate{task_target} > 10% for 30m → investigate
- Redfish
  - redfish_request_error_rate{op} > 5% for 10m → warn (vendor outage/network)
  - redfish_poll_timeout_count > 0 in 30m → warn
- Media/ISO
  - media_signed_url_validation_failures > 0 for 5m → warn (time skew/secret rotate)
  - iso_build_failures > 0 for 5m → investigate (disk, toolchain)
- Registry
  - registry_free_space < 20% → warn; < 10% → page
  - registry_upload_error_rate > 2% for 10m → warn
- Storage
  - task_iso_dir_free < 5 GiB → warn; < 2 GiB → page
- Security
  - login_auth_fail_rate > 5/min sustained 10m → warn; potential brute-force

4) SLOs and Error Budgets (targets)

- Job success rate
  - SLO: ≥ 98% of jobs succeed over 30 days (excludes user-induced invalid recipes)
- Job latency
  - Linux: P95 ≤ 90 min (depends on artifact size/hardware)
  - Windows: P95 ≤ 150 min
  - ESXi: P95 ≤ 90 min install + 20 min cleanup
- Availability (API)
  - SLO: ≥ 99.9% 30-day rolling
- Webhook processing
  - P95 ≤ 1s from receipt to state update

5) Incident Management

5.1 Severity levels

- SEV1 (Critical): Controller down (API 5xx), provisioning blocked, or registry offline with active jobs; data loss suspected
- SEV2 (High): Elevated job failures (>20%), persistent Redfish operation failures across vendors, storage exhaustion imminent
- SEV3 (Medium): Single vendor degradation, occasional webhook/auth issues, recoverable registry hiccups
- SEV4 (Low): Cosmetic issues, single-job anomalies, documentation gaps

5.2 On-call and escalation

- Primary SRE on-call handles triage within 15 minutes (SEV1/2)
- Escalate to:
  - Provisioning WG lead for workflow/tooling issues (Quadlet containers)
  - Platform/network for BMC reachability/TLS
  - Security for auth anomalies or suspected compromise
- Communication: incident channel with summary, timeline, remediation, follow-up actions

6) Common Runbooks (triage → diagnose → fix → verify)

Note: Commands use service and paths referenced in 036. Adjust to your environment.

6.1 Controller down / API 5xx surge

- Triage
  - Check service status and recent logs
  - Verify disk free at /var/lib/shoal and registry paths; check memory/FDs
- Diagnosis
  - Inspect last changes (deploy, config, certs)
  - Look for DB locked/busy patterns; long registry uploads; panics in logs
- Fix
  - If crashed/hung: restart service
  - If DB lock: allow in-flight ops to settle; ensure single instance is picking jobs
  - If disk full: run GC (registry unreferenced blobs), increase disk, prune old ISOs
  - Revert recent bad config/deploy if correlated
- Verify
  - Health endpoints OK; requests succeed; job processing resumes
  - Create a synthetic job against a mock Redfish to confirm end-to-end

6.2 Webhook auth failures (401/403)

- Triage
  - Alerts: webhook_auth_failures_total > 0
  - Logs show X-Webhook-Secret missing/mismatch
- Diagnosis
  - Check controller’s WEBHOOK_SECRET vs maintenance OS configured secret
  - Consider time window of a rotation (dual-accept period)
- Fix
  - If rotation underway: ensure both old/new accepted; keep window open
  - If mismatch: sync secret in maintenance OS image and controller; requeue jobs or allow next rollout
- Verify
  - New webhooks accepted; job states transition appropriately

6.3 Redfish insert/eject or boot override failures

- Triage
  - Identify op (mount.maintenance, mount.task, boot-override, reset)
  - Scope by vendor (iDRAC, iLO, XCC, Supermicro)
- Diagnosis
  - Network reachability to BMC; cert policies; rate limits (429 with Retry-After)
  - Vendor quirks: VirtualMedia under Manager vs System; two CD slots naming
- Fix
  - Increase retries/backoff temporarily
  - Clear stale media by forcing Eject; re-assert Insert and one-time boot
  - For systemic vendor issue: apply vendor profile override (028) and redeploy fix
- Verify
  - Subsequent Redfish ops succeed; progression resumes for affected jobs

6.4 Task ISO serving 403/404 (signed URL failures)

- Triage
  - Alerts: media_signed_url_validation_failures
  - Logs indicate expired/tampered sig or clock skew
- Diagnosis
  - Validate controller time sync; check SIGNED_URL_SECRET; verify path exists
- Fix
  - Regenerate task.iso if missing; reissue signed URL
  - Correct time skew (NTP); rotate signed URL secret (with downtime window to avoid breaking active jobs)
- Verify
  - BMC mounts succeed; no further signed URL errors

6.5 Registry storage full or slow

- Triage
  - Free space low; upload failures/timeouts; high disk IO wait
- Diagnosis
  - Identify large unreferenced blobs; tag sprawl; concurrent large pulls
- Fix
  - Run registry GC (remove unreferenced blobs)
  - Tighten retention (keep last N versions) for tools/*
  - Scale storage (bigger/faster disk); adjust concurrency limits
- Verify
  - Free space recovers; upload/download success and latency back to normal

6.6 Job stuck in provisioning (no webhook)

- Triage
  - Identify job age; check BMC power state transitions for ESXi; maintenance OS reachability for Linux/Windows not expected
- Diagnosis
  - Possibly failed unit with webhook not delivered (network/firewall)
  - Controller side wait timeout thresholds appropriate?
- Fix
  - If allowed, requeue job (idempotent containers should tolerate re-run)
  - For systematic webhook delivery issues: fix egress, rotate webhook secret, verify DNS/TLS trust
- Verify
  - Stuck count declines; new jobs complete or fail with precise failed_step

6.7 Database issues (locked/corruption suspected)

- Triage
  - Errors: SQLITE_BUSY, I/O error, integrity check failures
- Diagnosis
  - Check for multiple competing writers; ensure a single controller instance owns leases
- Fix
  - Gracefully stop service; take backup; run integrity check and VACUUM during maintenance window
  - If corruption: restore from latest good backup
- Verify
  - Service healthy; jobs can be created and processed

6.8 TLS/cert problems (controller or BMC)

- Triage
  - Registry pull/push TLS errors; webhook HTTPS failures; Redfish cert validation errors
- Diagnosis
  - Expired certs, CA trust missing in maintenance OS, self-signed BMC cert policy
- Fix
  - Renew/replace certs; reload or restart services
  - Ensure controller CA installed in maintenance OS trust store
  - Adjust BMC TLS policy per 033 (prefer HTTPS; allow insecure only with compensating controls)
- Verify
  - TLS handshakes succeed; errors stop

7) Capacity Planning and Scaling

7.1 Baselines

- CPU
  - Controller is CPU-light except during registry crypto/IO; 2–4 vCPU sufficient for small deployments
- Memory
  - 2–8 GiB typical; high concurrency or large registry throughput may require more
- Disk
  - Registry storage sized for all artifacts + headroom (10–30% free)
  - task.iso storage sized for concurrency × (≤10 MiB typical per job)
- Network
  - Ensure egress bandwidth for large artifacts (WIMs/rootfs); reverse proxy timeouts configured for long transfers

7.2 Workload drivers

- Artifact sizes (Windows WIMs are largest)
- Concurrency (WORKER_CONCURRENCY)
- Vendor BMC responsiveness (affects “provisioning” pipeline length)

7.3 Scaling levers

- Concurrency
  - Increase WORKER_CONCURRENCY; ensure per-server serialization remains enforced
- Backpressure
  - Throttle job picking when registry or disk IO saturates
- Storage
  - Move registry storage to faster disks; separate tasks/registry volumes
- Offload registry
  - Use external production-grade registry if embedded registry becomes a bottleneck

7.4 Headroom & Alerts

- Keep ≥ 20% free disk on registry; ≥ 2 GiB free for task.iso; alert before thresholds
- Watch P95 job durations; if creeping up, investigate artifact sizes and IO contention

8) Maintenance Procedures

8.1 Backups

- DB
  - Nightly backup; test restore regularly
  - Hot backup via SQLite backup API or filesystem snapshots
- Config and secrets
  - Secure store; versioned; rotate periodically
- Registry storage
  - Not typically backed up; artifacts can be republished; consider backups if required by policy

8.2 Garbage Collection

- Task ISOs
  - Delete completed jobs’ ISOs older than JOB_RETENTION_DAYS
- Registry
  - GC unreferenced blobs after tag deletions/retention policy execution

8.3 Rotations

- WEBHOOK_SECRET
  - Dual-accept period; coordinate with maintenance OS image rollout
- SIGNED_URL_SECRET
  - Rotate during low traffic; may invalidate outstanding URLs
- TLS certs
  - Renew before expiry; automated reminders

8.4 Upgrades & Rollbacks

- Follow 036; pre-upgrade backup; controlled rollout; smoke tests
- Rollback by restoring DB and previous binary; verify health

9) Operational Checklists

9.1 Daily

- Dashboards: job success, durations, errors
- Disk free for tasks and registry
- Alert review: webhook auth failures, Redfish error rates

9.2 Weekly

- Verify backups; test restore to staging
- Run registry GC; review retention
- Review recent incidents and error taxonomy top-N
- Dependency and CVE scan review

9.3 Monthly

- Secret rotation schedule review
- Capacity review: growth trends on artifacts and jobs
- SLO review and error budget status

10) Post-Incident Review

- For SEV1/SEV2 incidents within 5 business days:
  - Timeline: detection → mitigation → resolution
  - Root cause and contributing factors
  - What worked/what didn’t (alerting, docs, automation)
  - Concrete action items with owners and due dates (and PRs/issues where applicable)

11) Acceptance Criteria

- Monitoring
  - Metrics exported per sections 2.1; dashboards built; alerts configured with actionable thresholds
- Runbooks
  - Above playbooks verified by a dry-run or staging exercises; steps updated as needed
- Capacity
  - Headroom maintained; documented scale-up process; storage GC scheduled
- Maintenance
  - Backups, rotations, GC procedures tested and documented
- Security
  - No secrets in logs; signed URLs enforced; auth on API/webhook/registry verified periodically
- Resilience
  - SEV1 drills (failure injection or controlled restarts) demonstrate recovery within targets
- Documentation
  - This runbook linked from on-call rotation docs and kept current with releases

12) Open Questions

- Should we enforce mTLS on webhook in high-security environments by default (opt-out)?
- Do we add automatic vendor-profile detection for Redfish quirks with telemetry to reduce manual triage?
- Should we add per-job progress streaming from maintenance OS (trade-off with self-contained design)?

Change Log

- v0.1 (2025-11-05): Initial operations and SRE runbook covering monitoring, alerts, incidents, capacity, and maintenance procedures.