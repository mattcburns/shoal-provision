# 021: Provisioner Controller Service

Status: In Progress (Phase 1 implemented)
Owners: Provisioning Working Group
Last updated: 2025-11-05

Summary

This document specifies the Provisioner Controller Service that orchestrates Layer 3 bare-metal provisioning using Redfish, dual virtual media (maintenance.iso + task.iso), and an optional embedded OCI registry. It defines the HTTP API, job state machine, storage schema, workers/concurrency model, error handling, and operational concerns so AI agents can implement it incrementally and verifiably.

Goals

- Provide a stable, minimal HTTP API for users and the maintenance OS.
- Implement a resilient job orchestration state machine and persistence.
- Perform Redfish operations to mount/unmount virtual media and trigger reboots.
- Generate and serve small, deterministic task ISOs per job.
- Optionally co-host an embedded OCI registry on the same HTTP server.
- Be robust (idempotent, recoverable) and observable (structured logs/metrics).

Non-Goals

- A full registry UI, vulnerability scanning, or SBOM services.
- General Redfish aggregation outside of provisioning flows.
- Hardware inventory or discovery beyond serial→BMC mapping needed for jobs.

Related

- 020_Provisioner_Architecture.md (top-level breakdown and index)
- 022_Recipe_Schema_and_Validation.md
- 023_Task_ISO_Builder.md
- 027_Embedded_OCI_Registry.md (optional addendum)
- 028_Redfish_Operations.md
- 032_Error_Handling_and_Webhooks.md
- 035_Test_Strategy.md

High-Level Responsibilities

- User API: accept a recipe, validate it, create a Job, and return a job_id.
- Worker: pick queued jobs, build task.iso, perform Redfish orchestration, mark job provisioning, await webhook, finalize cleanup, and mark complete.
- Webhook API: accept success/failure from maintenance OS and update job status.
- Optional: Serve /v2/ registry endpoints backed by local storage.
- Serve task ISO artifacts at short-lived URLs for virtual media mounting.

Security

- User API: Basic auth or JWT bearer.
- Webhook API: Shared secret header (e.g., X-Webhook-Secret) or mTLS client cert.
- Task ISO: Signed short-lived URL or controller-authenticated Redfish mount URL.
- Registry (optional): Basic auth with per-repo ACLs.
- Never log secrets; redact sensitive fields (credentials, secrets in recipes).

Configuration (examples)

- CONTROLLER_HTTP_ADDR=:8080
- DB_PATH=/var/lib/shoal/provisioner.db
- STORAGE_ROOT=/var/lib/shoal
- TASK_ISO_DIR=/var/lib/shoal/task-isos
- MAINTENANCE_ISO_URL=https://controller.example/isos/bootc-maintenance.iso
- ENABLE_REGISTRY=true|false
- REGISTRY_STORAGE=/var/lib/shoal/oci
- AUTH_MODE=basic|jwt|none
- WEBHOOK_SECRET=env:PROVISIONER_WEBHOOK_SECRET
- WORKER_CONCURRENCY=4
- REDFISH_TIMEOUT=30s
- REDFISH_RETRIES=5
- JOB_LEASE_TTL=10m
- JOB_STUCK_TIMEOUT=4h

API Specification

All responses are JSON unless otherwise noted.

1) POST /api/v1/jobs

- Purpose: Create a provisioning job and enqueue it.
- Auth: Required (basic/jwt).
- Request body:
  {
    "server_serial": "XF-12345ABC",
    "recipe": { ... } // Valid per recipe.schema.json
  }
- Behavior:
  - Validate presence of server_serial and recipe.
  - Validate recipe against the schema.
  - Resolve server_serial → BMC endpoint/credentials (from DB).
  - Insert Job with status="queued".
  - Return 202 Accepted with job metadata.
- Response (202):
  {
    "job_id": "uuid",
    "status": "queued",
    "server_serial": "XF-12345ABC",
    "created_at": "RFC3339"
  }
- Errors:
  - 400 Invalid recipe: include validation errors.
  - 404 Unknown server_serial.
  - 401/403 Unauthorized/Forbidden.

2) GET /api/v1/jobs/{job_id}

- Purpose: Fetch job status and details.
- Auth: Required.
- Response (200):
  {
    "job_id": "uuid",
    "server_serial": "XF-12345ABC",
    "status": "queued|provisioning|succeeded|failed|complete",
    "failed_step": "string|null",
    "created_at": "RFC3339",
    "last_update": "RFC3339",
    "events": [
      {
        "time": "RFC3339",
        "level": "info|warn|error",
        "message": "string",
        "step": "string|null"
      }
    ]
  }
- Errors:
  - 404 Not found.

3) POST /api/v1/status-webhook/{server_serial}

- Purpose: Receive final status from maintenance OS.
- Auth: Required (shared secret header or mTLS).
- Request body:
  - Success:
    { "status": "success" }
  - Failure:
    { "status": "failed", "failed_step": "bootloader-linux.service" }
- Behavior:
  - Find the active job for server_serial with status="provisioning".
  - Transition to "succeeded" or "failed" accordingly.
  - Trigger reconciliation/cleanup (unmount media, reboot).
  - Return 200 OK.
- Errors:
  - 404 No active job for this serial.
  - 400 Invalid payload.
  - 401/403 Unauthorized/Forbidden.

4) (Optional) Embedded Registry: /v2/...

- Purpose: Co-host OCI Distribution API for oras/podman clients.
- Auth: Optional (basic).
- Backing store: filesystem OCI layout under REGISTRY_STORAGE.
- See 027_Embedded_OCI_Registry.md.

Task ISO Serving

- GET /media/tasks/{job_id}/task.iso
  - Purpose: Serve the per-job ISO to BMCs as virtual media.
  - Auth: Either:
    - Signed short-lived URL (query param signature + expiry), or
    - Basic auth credentials configured on BMC mount call, or
    - IP allowlist (BMC mgmt networks).
  - Content-Type: application/x-iso9660-image
  - Range requests allowed for compatibility.

Job State Machine

States

- queued: persisted after initial creation; not yet picked by a worker.
- provisioning: actively orchestrating Redfish operations through webhook handling; mounted media and reboot in progress.
- succeeded: maintenance OS reported success (via webhook).
- failed: maintenance OS reported failure or controller timeout/irrecoverable error.
- complete: post-webhook cleanup finished (unmount media, reboot to final OS).

Transitions

- queued → provisioning
  - Worker successfully acquires lease and starts orchestration.
- provisioning → succeeded
  - Webhook reports "success".
- provisioning → failed
  - Webhook reports "failed" or orchestration timeout/hard error.
- succeeded|failed → complete
  - Cleanup finished (unmount; reboot).
- Any → failed (controller)
  - Terminal error (e.g., unrecoverable Redfish error after retries). Worker logs reason and transitions.

Idempotency and Recovery

- Workers must be able to resume jobs after controller restart:
  - provisioning jobs are reconciled on startup:
    - Verify virtual media mounts; unmount stale; re-issue cleanup if needed.
  - queued jobs are safe to pick again; the lease mechanism prevents duplicate work.
- Redfish operations are guarded by read-then-write:
  - If media already mounted as desired, skip mounting.
  - Setting one-time boot to CD is idempotent.
  - Reboot attempts include grace period and retry with force if safe.

Persistence Model

Database: SQLite (single-node, embedded), accessed via internal/store.

Tables (DDL sketch)

- servers
  - serial TEXT PRIMARY KEY
  - bmc_address TEXT NOT NULL
  - bmc_username TEXT NOT NULL
  - bmc_password TEXT NOT NULL (consider external secret store ref)
  - vendor TEXT NULL
  - last_seen TIMESTAMP NULL

- jobs
  - id TEXT PRIMARY KEY (uuid)
  - server_serial TEXT NOT NULL REFERENCES servers(serial) ON DELETE RESTRICT
  - status TEXT NOT NULL CHECK (status IN ('queued','provisioning','succeeded','failed','complete'))
  - failed_step TEXT NULL
  - recipe_json TEXT NOT NULL
  - created_at TIMESTAMP NOT NULL
  - updated_at TIMESTAMP NOT NULL
  - picked_at TIMESTAMP NULL
  - worker_id TEXT NULL
  - lease_expires_at TIMESTAMP NULL
  - task_iso_path TEXT NULL
  - maintenance_iso_url TEXT NOT NULL

- job_events
  - id INTEGER PRIMARY KEY AUTOINCREMENT
  - job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE
  - time TIMESTAMP NOT NULL
  - level TEXT NOT NULL CHECK (level IN ('info','warn','error'))
  - message TEXT NOT NULL
  - step TEXT NULL

- settings (optional generic key-value)
  - key TEXT PRIMARY KEY
  - value TEXT NOT NULL

Indexes

- CREATE INDEX idx_jobs_status ON jobs(status);
- CREATE INDEX idx_jobs_server ON jobs(server_serial);
- CREATE INDEX idx_job_events_job_time ON job_events(job_id, time);

Migrations

- Maintain a monotonic schema version in settings.
- Ship SQL migration scripts and a migrator that runs on startup.
- Tests verify forward-only migrations.

Workers and Concurrency

Leasing

- Workers poll for jobs WHERE status='queued' LIMIT N.
- Acquire lease via atomic UPDATE:
  UPDATE jobs
     SET status='provisioning',
         worker_id=:wid,
         picked_at=now(),
         lease_expires_at=now()+:LEASE_TTL,
         updated_at=now()
   WHERE id=:id AND status='queued';
- If rowcount=1, the worker owns the job.
- The worker extends lease (heartbeat) periodically:
  UPDATE jobs SET lease_expires_at=now()+:LEASE_TTL WHERE id=:id AND worker_id=:wid;
- If a worker crashes, another worker can steal after lease_expires_at < now() with:
  UPDATE jobs
     SET worker_id=:wid, picked_at=now(), lease_expires_at=now()+:LEASE_TTL
   WHERE id=:id AND status='provisioning' AND lease_expires_at<now();

Concurrency Controls

- WORKER_CONCURRENCY controls parallel jobs.
- Per-server limit: disallow >1 active job on the same server_serial.
- Backpressure: throttle picking when registry I/O or ISO builder queue is high.

Worker Orchestration Flow (per job)

1) Validate preconditions
   - servers mapping exists.
   - recipe already validated at POST time; may re-validate for safety.
2) Build task.iso
   - Create temp staging dir; write recipe.json, recipe.schema.json, any extra assets (user-data, unattend.xml).
   - Produce deterministic ISO file (023_Task_ISO_Builder.md).
   - Store under TASK_ISO_DIR with job_id; set jobs.task_iso_path.
   - Generate signed URL if using signed media URLs.
3) Redfish operations (see 028_Redfish_Operations.md)
   - Ensure existing virtual media is unmounted or in a known state (idempotent).
   - Mount maintenance.iso (CD1) (MAINTENANCE_ISO_URL).
   - Mount task.iso (CD2) (controller-served URL).
   - Set one-time boot to CD; reboot.
   - Update job_events throughout with step metadata.
4) Await webhook
   - Controller continues running; webhook handler updates job.status to succeeded/failed.
   - Worker monitors status change (poll DB) with timeout.
   - On timeout, mark failed (reason: webhook timeout) and proceed to cleanup.
5) Cleanup (finalize to complete)
   - Unmount both virtual media.
   - Reboot to final OS (graceful; fallback to force if needed).
   - Set status=complete.

Error Handling and Retries

- Redfish calls: retry with exponential backoff; classify transient (5xx, timeouts) vs terminal (4xx invalid).
- ISO builder: capture stderr/stdout; report size/hash; terminal on build tool errors.
- Webhook: allow duplicate success/failure messages (idempotent). If already transitioned, return 200 OK.
- Cleanup: best-effort; if unmount fails, log and still mark complete with warnings.

Task ISO Lifecycle

- Creation: during provisioning.
- Serving: via /media/tasks/{job_id}/task.iso.
- Expiry: delete after job.status transitions to complete and RETENTION window passes.
- Disk pressure: periodic GC task removes ISO files for complete jobs older than N days.

Security: Secrets and Access

- BMC credentials stored in servers table can be:
  - Plain (encrypted at rest via filesystem measures), or
  - References to external secret providers (out of scope here).
- Webhook secret: required header X-Webhook-Secret must match configured secret. Log only presence, never its value.
- Signed media URL: embed HMAC signature and expiry; validate per-request.

Observability

- Structured logs: include job_id, server_serial, step, worker_id.
- Metrics:
  - job_total{status}
  - job_duration_seconds{outcome}
  - redfish_request_duration_seconds{op}
  - iso_build_duration_seconds, iso_size_bytes
  - webhook_latency_seconds (provisioning→webhook)
- Tracing (optional): span per job with subspans for ISO build, Redfish ops.

Validation and Recipe Schema

- Validate against recipe.schema.json (022).
- Error reporting: include path to invalid field(s) and a human-readable message.
- Backward-compatibility: version schema with $id; controller supports N-1 schemas.

Idempotency and Duplicate Requests

- POST /api/v1/jobs may include an optional idempotency_key header.
  - If provided, reuse existing queued/provisioning job for same key+serial.
  - Return 200 with existing job; do not create a new one.

Time Limits and Timeouts

- Redfish timeouts per operation: default REDFISH_TIMEOUT.
- Provisioning timeout: per job stuck timeout JOB_STUCK_TIMEOUT; worker marks failed if exceeded without webhook.
- Cleanup timeout: enforce upper bound; still mark complete with warning if some calls fail.

Compatibility and Vendor Quirks

- Maintain vendor capability profile (e.g., Dell iDRAC, HPE iLO, Lenovo XCC, Supermicro):
  - Endpoint differences for virtual media mount.
  - Time required after reboot before API reachable.
  - Boot override property names.
- Feature flags per server to adapt calls.

Testing Strategy (035)

- Unit tests:
  - API handlers (happy/edge paths).
  - Store layer (migrations, CRUD).
  - Lease logic and race conditions.
- Integration tests:
  - Mock Redfish server with programmable behavior (success, timeouts, 5xx).
  - ISO builder with golden file verification of contents and deterministic hashes.
  - Webhook end-to-end with concurrent workers.
- E2E (in CI or nightly):
  - Optional hardware-in-the-loop or emulator-based tests.
- Coverage target:
  - ≥80% for controller packages; critical paths ≥90%.

Acceptance Criteria

- Create/List Jobs:
  - POST /api/v1/jobs validates schema and returns job_id with status=queued.
  - GET returns job status/events.
- Orchestration:
  - Worker transitions queued→provisioning, builds ISO, performs Redfish mounts, reboot.
  - Webhook success transitions provisioning→succeeded, then cleanup → complete.
  - Webhook failure transitions provisioning→failed, then cleanup → complete.
- Idempotency:
  - Duplicate webhooks and repeated cleanup attempts are harmless.
- Resilience:
  - Controller restart during provisioning resumes without breaking job.
  - Lease mechanism prevents duplicate workers from processing the same job.
- Security:
  - Auth enforced on APIs; webhook requires secret; secrets redacted in logs.
- Observability:
  - Metrics exported; logs correlate by job_id and server_serial.

Open Questions

- Should we support per-job override of maintenance_iso_url (e.g., ESXi workflows)?
- How to integrate with external CMDB for server_serial mapping lifecycle?
- Optional: artifact prefetch to local cache before provisioning start to reduce time-to-first-byte at maintenance OS.

Appendix A: Example Payloads

POST /api/v1/jobs (Linux)

{
  "server_serial": "XF-12345ABC",
  "recipe": {
    "task_target": "install-linux.target",
    "target_disk": "/dev/sda",
    "oci_url": "controller.internal:8080/os-images/ubuntu-rootfs:22.04",
    "user_data": "IyEvYmluL2Jhc2gK...",
    "partition_layout": [
      { "size": "512M", "type_guid": "ef00", "format": "vfat" },
      { "size": "100%", "type_guid": "8300", "format": "ext4" }
    ]
  }
}

202 Accepted

{
  "job_id": "f7f5d2b6-1f1f-4b7c-9fcb-2a8e1b8e5b4a",
  "status": "queued",
  "server_serial": "XF-12345ABC",
  "created_at": "2025-11-03T20:30:00Z"
}

Webhook (success)

POST /api/v1/status-webhook/XF-12345ABC
Headers:
  X-Webhook-Secret: ****
Body:
{ "status": "success" }

200 OK
{ "ok": true }

Appendix B: Implementation Notes

- HTTP server: Go stdlib net/http with context-aware timeouts.
- ISO builder abstraction:
  - Prefer shelling to xorriso or mkisofs present on the host.
  - Fallback: library-based ISO creation if dependency constraints allow (see licensing policy).
- File serving:
  - Use http.ServeContent with support for Range requests on task.iso.
- DB access:
  - Use a small abstraction to wrap SQLite with context deadlines and retry on SQLITE_BUSY.
- Redfish client:
  - Keep small, purpose-built client tailored for virtual media, boot override, and power operations; minimize dependencies.