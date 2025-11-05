# 027: Embedded OCI Registry (Addendum)

Status: Proposed
Owners: Provisioning Working Group
Last updated: 2025-11-03

Summary

This document defines the embedded OCI Distribution API implementation that runs inside the Provisioner Controller process. The goal is to co-host the provisioning API (/api/v1/…) and the OCI registry (/v2/…) on the same HTTP server to achieve a single-binary, air‑gap friendly deployment.

The embedded registry enables:
- Pushing/pulling tool container images (for Quadlet steps).
- Pushing/pulling large OS artifacts (e.g., Linux rootfs tarballs, Windows WIMs) via oras with custom media types.
- Local artifact storage using an OCI layout on disk with content-addressed blobs and manifest/refs.

Related

- 020_Provisioner_Architecture.md (system overview)
- 021_Provisioner_Controller_Service.md (APIs, orchestration, persistence)
- 022_Recipe_Schema_and_Validation.md (recipe contract)
- 023_Task_ISO_Builder.md (task.iso generation)
- 024_Maintenance_OS_Build_with_bootc.md (maintenance OS)
- 026_Systemd_and_Quadlet_Orchestration.md (in-OS workflows)
- 032_Error_Handling_and_Webhooks.md (reliability, payloads)

Goals

- Serve a standards-compliant subset of the OCI Distribution API on /v2/.
- Support oras and podman clients for pushing and pulling artifacts and container images.
- Store artifacts on local disk using OCI Image Layout, with integrity and deduplication by digest.
- Provide basic authentication and optional TLS termination.
- Offer acceptable performance and concurrency for small clusters provisioning in parallel.
- Keep implementation minimal; prioritize reliability and debuggability.

Non-Goals

- Complete feature parity with enterprise registries (catalog browsing, GC UI, CVE scanning).
- Multi-tenant RBAC beyond a simple push/pull/auth policy.
- Proxy/mirror capabilities or cross-replication.

1. Architecture

1.1 HTTP Routing

- Primary HTTP mux in the controller owns the namespace.
  - /api/v1/** → Provisioning API (jobs, webhook, media)
  - /media/tasks/** → Task ISO static server (023)
  - /v2/** → Embedded OCI Distribution API
- Authentication middleware attaches to both API and registry paths (configurable modes).

1.2 Storage Backend

- On-disk OCI Image Layout under REGISTRY_STORAGE (default: /var/lib/shoal/oci).
  - blobs/sha256/<digest> for content-addressed blob storage
  - index.json and refs for top-level references
  - oci-layout for layout versioning
- Directory structure is append-only for reliability; deletions are mediated by a GC process (see 4.4).
- Concurrency guard: advisory file locks on index.json and ref files to avoid race conditions on concurrent tag updates.

1.3 Supported Media

- Container images (manifests, config, layers) usable by podman.
- Generic artifacts (oras) with custom artifact-type headers, e.g.:
  - application/vnd.my-org.rootfs.tar.gz
  - application/vnd.my-org.install.wim
  - application/vnd.oci.image.config.v1+json (as needed by images)
- The registry stores media types as provided; policy enforcement (allow/deny) is configuration-driven.

2. API Semantics (subset of OCI Distribution Spec)

2.1 Endpoints

- GET /v2/
  - Ping. Returns 200 if healthy (and authenticated if auth enabled).

- Blobs
  - GET /v2/<name>/blobs/<digest>
    - Streams blob content by digest (sha256:…).
    - Supports Range requests for large blobs (optional but recommended).
  - HEAD /v2/<name>/blobs/<digest>
    - Verifies existence and returns size and media type (if known).
  - POST /v2/<name>/blobs/uploads/
    - Initiates an upload; returns upload location with uuid.
  - PATCH /v2/<name>/blobs/uploads/<uuid>
    - Chunked upload continuation (Content-Range optional).
  - PUT /v2/<name>/blobs/uploads/<uuid>?digest=<sha256>
    - Completes the upload, verifies digest, moves temp to final location atomically.
  - Monolithic upload shortcut:
    - POST /v2/<name>/blobs/uploads/?digest=<sha256> with full body as some clients do.
    - If supported by client library, accept in one request.

- Manifests
  - GET /v2/<name>/manifests/<reference>
    - reference is a tag or digest.
    - Returns manifest JSON with mediaType and config/descriptors.
  - HEAD /v2/<name>/manifests/<reference>
  - PUT /v2/<name>/manifests/<reference>
    - Writes/updates tag to a manifest digest; mediaType must be supported.

- Deletions (optional)
  - DELETE /v2/<name>/manifests/<reference>
    - Soft-deletes tag (removes tag pointer), blob GC is deferred.

Notes:
- The Catalog API (/v2/_catalog) is optional; omit or guard via config.

2.2 Integrity and Validation

- Blob writes:
  - Verified against provided digest; mismatch returns 400.
  - Writes occur to a temp file; finalization via atomic rename after digest check.
- Manifest writes:
  - Validate JSON structure minimally. For container images, ensure required fields exist (schema2).
  - Reference descriptors must refer to present blobs or be pushable after (some clients PUT manifest first; allow within a short window if configured).

2.3 Tags and Immutability

- Default policy: tag updates allowed (mutable tags).
- Optional: enforce tag immutability (first-writer wins) per repository prefix or global switch.

3. Client Compatibility

3.1 oras

- Pushing a rootfs tarball:
  - oras push controller:8080/os-images/ubuntu-rootfs:22.04 \
      --artifact-type application/vnd.my-org.rootfs.tar.gz \
      ./ubuntu-22.04-rootfs.tar.gz
- Pushing a Windows WIM:
  - oras push controller:8080/os-images/windows-wim:2022 \
      --artifact-type application/vnd.my-org.install.wim \
      ./win2022.wim
- Pulling artifacts inside maintenance OS Quadlet containers:
  - oras pull controller:8080/os-images/ubuntu-rootfs:22.04
  - stdout piping to tar/wimapply supported by wrapper scripts.

3.2 podman

- Pushing tool containers:
  - podman push tools/sgdisk:1.0 controller:8080/tools/sgdisk:1.0
- Pulling tool containers:
  - podman pull controller:8080/tools/sgdisk:1.0
- Ensure registries.conf trusts controller CA or marks controller as insecure if TLS is disabled (development only).

4. Storage, Retention, and GC

4.1 Disk Layout

- REGISTRY_STORAGE (default /var/lib/shoal/oci)
  - blobs/sha256/<digest>
  - index.json (top-level refs)
  - oci-layout (version file)
  - repositories/<name>/refs/<tag or digest> → pointer files mapping to manifest digests (implementation-specific)
- Large files: ensure filesystem and mount options tuned for large file writes (WIM/rootfs).

4.2 Deduplication

- Blobs keyed by digest are stored once.
- Multiple manifests referencing the same blob share content.
- Optional: use hardlinks across repositories to avoid duplication if layout forks per repo; otherwise store globally under blobs/.

4.3 Retention Policies

- Configurable retention by repository prefix:
  - tools/*: keep last N tags per repo, delete older tags (soft-delete only).
  - os-images/*: keep tagged artifacts indefinitely by default.
- Tag deletion only removes refs. Blobs are GC’d when unreferenced.

4.4 Garbage Collection (GC)

- Background GC scans:
  - Build reachability graph from manifests/tags → blobs.
  - Anything not reachable for longer than GRACE_PERIOD is deleted.
- Safety:
  - Use temporary quarantine for deletes; only finalize after next scan confirms no ref reappeared.
- Scheduling:
  - Periodic (e.g., hourly) and on-demand (admin endpoint or CLI).

5. Authentication, Authorization, and TLS

5.1 Auth Modes

- none (dev only): unauthenticated access to /v2/.
- basic: single set of credentials or credentials table (username/password hash) validated on each /v2/ request.
- bearer (future): JWT with limited scopes (pull/push); not required initially.

5.2 Authorization

- Simple policy:
  - Pull: allowed for authenticated users by default.
  - Push: allowed for authenticated users; optionally restricted by repository prefix allowlist.
- Optional: read-only mode for maintenance OS networks.

5.3 TLS

- Recommended: terminate TLS at the controller (single binary) or a front proxy.
- Maintenance OS must trust the controller’s CA to pull artifacts.
- Development mode may run HTTP; do not use in production.

5.4 Secrets and Logging

- Do not log Authorization headers or passwords.
- Log only username and repo in audit events.

6. Performance and Concurrency

6.1 Limits

- Max concurrent uploads (REGISTRY_MAX_CONCURRENCY), default 8.
- Max upload size per request; enforce both per-chunk and total upload limits.
- Read/Write timeouts at HTTP layer; sane defaults with overrides.

6.2 Large Blob Handling

- Support chunked PATCH uploads for >10 GiB artifacts (WIM).
- Disk write path:
  - Stream to disk, fsync at commit boundary (PUT finalize) to ensure durability.
- Range support:
  - Enable Range on GET /blobs responses for partial reads.

6.3 Caching

- ETag: "sha256:<digest>" on blobs.
- Last-Modified from file mtime (or RECORD timestamp).
- Clients often ignore; harmless and may help proxies.

7. Observability and Auditing

7.1 Metrics

- http_requests_total{route,code}
- registry_upload_bytes_total{repo}
- registry_download_bytes_total{repo}
- registry_inflight_uploads
- registry_blob_count, registry_storage_bytes
- registry_upload_duration_seconds

7.2 Logs

- Structured logs for:
  - Upload start/finish (user, repo, digest, size, duration)
  - Manifest PUT (repo, tag, digest)
  - Errors (validation, digest mismatch, auth failures)
- Redact secrets; include correlation ID if present.

7.3 Audit Events

- Optional table or file log:
  - time, user, action (push/pull/delete), repo, reference, digest, size

8. Configuration

Environment variables (examples):

- ENABLE_REGISTRY=true|false
- REGISTRY_STORAGE=/var/lib/shoal/oci
- REGISTRY_MAX_CONCURRENCY=8
- REGISTRY_ALLOW_ANON=false
- REGISTRY_AUTH_MODE=basic|none
- REGISTRY_BASIC_USERS_FILE=/etc/shoal/registry-users.htpasswd (or DB-backed)
- REGISTRY_MAX_BLOB_SIZE=21474836480  # 20 GiB
- REGISTRY_UPLOAD_TIMEOUT=1h
- REGISTRY_READ_TIMEOUT=30s
- REGISTRY_RETENTION_TOOLS_KEEP=5
- REGISTRY_GC_INTERVAL=1h
- REGISTRY_IMMUTABLE_TAGS=false
- TLS_CERT_FILE=/etc/shoal/tls/cert.pem
- TLS_KEY_FILE=/etc/shoal/tls/key.pem

9. Failure Modes and Handling

- Digest mismatch on upload finalize → 400; temp file removed; client must retry.
- Out of space → 507 Insufficient Storage; log critical; reject further uploads.
- Concurrent PUT manifest on same tag:
  - If immutable tags: 409 Conflict after first writer.
  - If mutable: last write wins; audit both events.
- Corrupted blobs:
  - On read, if digest check fails (optional background check), quarantine file, return 500; operator must restore or re-push.

10. Testing Strategy

Unit tests

- Upload lifecycle:
  - POST → PATCH → PUT with matching digest; verify blob exists with correct size and sha256.
  - Monolithic upload (POST with digest only) if client supports; verify.
- Manifest PUT:
  - Write a valid image manifest that references existing blobs; GET returns same JSON and headers.
- Auth:
  - Require basic auth; unauthenticated requests receive 401 with WWW-Authenticate.
  - Unauthorized repository prefixes rejected correctly.

Integration tests

- oras push/pull of:
  - rootfs tarball (custom media type)
  - Windows WIM (>1 GiB simulated with sparse file)
- podman push/pull:
  - tools/sgdisk:1.0 image round-trip
- Concurrency:
  - Parallel uploads to distinct repos; ensure no deadlocks or cross-corruption.
- GC:
  - Tag delete → blob remains referenced by another tag; no deletion.
  - Remove all refs → blob deleted after GC run with grace period.

Performance tests

- Sustained upload of a 10–20 GiB blob:
  - Validate throughput, CPU usage, memory stability, and final digest check.

Security tests

- Brute-force attempts:
  - Rate-limit or detect repeated auth failures; ensure no credential leaks.
- Path traversal:
  - Ensure repo names are sanitized; no traversal outside REGISTRY_STORAGE.

11. Acceptance Criteria

Functionality

- The controller serves /v2/ and responds with 200 on GET /v2/ (auth permitting).
- oras:
  - Can push and pull artifacts with custom media types to/from controller /v2/.
  - Can transfer artifacts ≥ 10 GiB using chunked uploads with digest verification.
- podman:
  - Can push/pull tool container images; Quadlet steps can pull from the embedded registry during provisioning.

Integrity and correctness

- All uploaded blobs are stored content-addressably and verified against digest.
- Manifest PUT creates/upserts tags correctly (respecting immutability config).
- GET/HEAD of blobs and manifests return correct headers (Content-Length, Docker-Content-Digest or OCI equivalent, mediaType).

Security

- With auth enabled, unauthenticated requests are rejected; authenticated users can push/pull per policy.
- TLS can be enabled; maintenance OS can pull with controller CA installed.
- Secrets are never logged; audit logs include only non-sensitive metadata.

Performance and robustness

- Concurrent uploads (≥ WORKER_CONCURRENCY) do not corrupt blobs; leases and locks prevent races.
- Upload/Download timeouts are enforced; hung connections do not exhaust resources.
- GC removes unreferenced blobs after the grace period without affecting referenced content.

Operations

- Storage directory is recoverable: registry reconstructs state from OCI layout on restart.
- Metrics are exported for requests, bytes in/out, durations, and storage usage.
- Configuration via environment or flags; changes documented and validated at startup.

12. Open Questions

- Do we need per-repo quotas and soft/hard limits? (Future enhancement)
- Should we expose a minimal admin API for GC trigger and retention policy updates?
- Do we require catalog listing for developer convenience? (Guard behind auth if implemented)

Change Log

- v0.1 (2025-11-03): Initial embedded registry design, scope, API coverage, storage model, and acceptance criteria.
