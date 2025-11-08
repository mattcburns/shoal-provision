# Phase 5: Embedded OCI Registry

Status: Planned  
Owners: Provisioning Working Group  
Last updated: 2025-11-08

Summary

Phase 5 implements an embedded OCI Distribution API (`/v2/*`) co-hosted within the Provisioner Controller process. This enables the controller to serve as a single-binary, air-gap friendly deployment that hosts both the provisioning API and an OCI registry for artifacts (rootfs tarballs, Windows WIMs) and tool container images. The embedded registry eliminates the need for external registries in isolated environments and simplifies artifact management.

References

- `design/020_Provisioner_Architecture.md` - Overall architecture and roadmap
- `design/027_Embedded_OCI_Registry.md` - Embedded registry specifications
- `design/021_Provisioner_Controller_Service.md` - Controller API and state machine
- `design/022_Recipe_Schema_and_Validation.md` - Recipe schema
- `design/032_Error_Handling_and_Webhooks.md` - Error handling patterns

Scope (Phase 5)

### In Scope

**Core Registry Functionality:**
- OCI Distribution API (`/v2/*`) co-hosted with provisioning API (`/api/v1/*`)
- Blob storage: `POST/PATCH/PUT` uploads, `GET/HEAD` downloads with digest verification
- Manifest storage: `PUT/GET/HEAD/DELETE` for tags and digests
- OCI Image Layout storage backend with content-addressed blobs
- Support for `oras` (artifact push/pull) and `podman` (container images)

**Storage and Data Management:**
- Filesystem-backed OCI Layout under configurable storage root
- Content-addressed blob deduplication (single copy per digest)
- Concurrent upload/download handling with advisory locks
- Atomic writes with temporary files and digest verification

**Authentication and Security:**
- Basic authentication for `/v2/*` endpoints
- Optional TLS termination within controller
- Credential validation via htpasswd or database-backed store
- No credentials or secrets logged

**Garbage Collection:**
- Background GC process to remove unreferenced blobs
- Configurable grace period before deletion
- Reachability analysis from manifests/tags to blobs
- Safe quarantine-before-delete pattern

**Integration with Provisioner:**
- Recipe `oci_url` field references artifacts in embedded registry
- Maintenance OS Quadlet containers pull from `controller:port/repo:tag`
- Task ISO generation remains independent (no registry dependency)

**Testing:**
- Unit tests for upload/download/manifest workflows
- Integration tests with `oras` and `podman` clients
- Large blob handling (>10GB WIM/rootfs artifacts)
- Concurrent upload/download tests

### Out of Scope

- Full catalog API (`/v2/_catalog`) - optional future enhancement
- Advanced RBAC or multi-tenant authorization beyond basic push/pull
- Proxy/mirror capabilities or cross-registry replication
- CVE scanning or vulnerability analysis
- Web UI for browsing registry contents
- Docker Registry v1 API (only OCI Distribution v2)

Architecture Overview

### HTTP Routing

The controller's HTTP mux serves multiple namespaces:

```
┌─────────────────────────────────────┐
│   Provisioner Controller (HTTP)     │
├─────────────────────────────────────┤
│  /api/v1/*      → Provisioning API  │
│  /media/tasks/* → Task ISO serving  │
│  /v2/*          → OCI Registry API  │
└─────────────────────────────────────┘
```

### Storage Layout

OCI Image Layout on disk (default: `/var/lib/shoal/oci`):

```
/var/lib/shoal/oci/
├── blobs/
│   └── sha256/
│       ├── abc123... (blob content)
│       └── def456... (blob content)
├── index.json (top-level refs)
├── oci-layout (version marker)
└── repositories/
    └── <repo-name>/
        └── refs/
            └── <tag> → manifest digest
```

### Workflow Integration

1. **CI/Build Pipeline**: Pushes artifacts to controller
   ```bash
   oras push controller:8080/os-images/ubuntu-rootfs:22.04 \
     --artifact-type application/vnd.my-org.rootfs.tar.gz \
     ./ubuntu-22.04-rootfs.tar.gz
   ```

2. **Recipe Creation**: References embedded registry
   ```json
   {
     "oci_url": "controller:8080/os-images/ubuntu-rootfs:22.04",
     ...
   }
   ```

3. **Maintenance OS**: Pulls artifacts during provisioning
   ```bash
   oras pull controller:8080/os-images/ubuntu-rootfs:22.04 --output - | tar xpf -
   ```

Milestones and Deliverables

### 1. Storage Backend and Blob Management

**Tasks:**
- Implement OCI Layout storage backend (`internal/provisioner/oci/storage.go`)
- Blob upload: `POST` initiate, `PATCH` chunk, `PUT` finalize with digest verification
- Blob download: `GET` with streaming, `HEAD` for metadata
- Temporary file handling with atomic rename on finalization
- Digest verification (sha256) on all uploads
- Advisory file locks for concurrent access

**Tests:**
- Upload blob via POST→PATCH→PUT sequence
- Upload blob via monolithic POST (single request)
- Download blob and verify digest matches
- Concurrent uploads to different blobs (no corruption)
- Digest mismatch returns 400 and rejects upload
- Large blob handling (>10GB sparse file)

**Files:**
- `internal/provisioner/oci/storage.go` - Storage backend
- `internal/provisioner/oci/storage_test.go` - Storage tests
- `internal/provisioner/oci/blob.go` - Blob upload/download handlers
- `internal/provisioner/oci/blob_test.go` - Blob tests

**Acceptance:**
- Blobs stored content-addressably under `blobs/sha256/<digest>`
- Upload failures leave no partial files
- Concurrent uploads work without corruption
- Digest verification enforced on all writes

### 2. Manifest Management

**Tasks:**
- Implement manifest storage (`internal/provisioner/oci/manifest.go`)
- `PUT /v2/<name>/manifests/<reference>` - Write/update tag or digest
- `GET /v2/<name>/manifests/<reference>` - Retrieve by tag or digest
- `HEAD /v2/<name>/manifests/<reference>` - Check existence
- `DELETE /v2/<name>/manifests/<reference>` - Soft-delete tag (optional)
- Tag-to-digest mapping in `repositories/<name>/refs/<tag>`
- Validate manifest JSON structure (minimal schema2/OCI checks)
- Enforce immutability policy if configured

**Tests:**
- PUT manifest with valid JSON and existing blob references
- GET manifest by tag returns correct content
- GET manifest by digest returns correct content
- HEAD manifest returns correct headers (Content-Length, Docker-Content-Digest)
- PUT manifest with non-existent blob reference returns 400
- DELETE manifest removes tag but not blob
- Concurrent tag updates (last-write-wins or conflict depending on config)

**Files:**
- `internal/provisioner/oci/manifest.go` - Manifest handlers
- `internal/provisioner/oci/manifest_test.go` - Manifest tests
- `internal/provisioner/oci/refs.go` - Tag-to-digest reference management

**Acceptance:**
- Manifests stored and retrieved correctly by tag and digest
- Tag updates atomic and safe under concurrent access
- Deleted tags don't break blob references
- Invalid manifests rejected with clear errors

### 3. HTTP API Handlers and Routing

**Tasks:**
- Implement OCI Distribution API endpoints (`internal/provisioner/oci/handler.go`)
- `GET /v2/` - Ping endpoint (returns 200 if authenticated)
- Blob endpoints: `GET/HEAD/POST/PATCH/PUT /v2/<name>/blobs/*`
- Manifest endpoints: `GET/HEAD/PUT/DELETE /v2/<name>/manifests/<reference>`
- Upload session management (UUID-based upload tracking)
- Range request support for `GET /v2/<name>/blobs/<digest>` (optional but recommended)
- Error responses following OCI spec (JSON error format)
- Integration with controller HTTP mux

**Tests:**
- `GET /v2/` returns 200 (or 401 if auth enabled)
- Upload lifecycle: POST→PATCH→PUT with UUID tracking
- Download blob with Range header returns correct bytes
- Invalid routes return 404 with OCI error JSON
- Upload timeout enforced (configurable)

**Files:**
- `internal/provisioner/oci/handler.go` - HTTP handlers
- `internal/provisioner/oci/handler_test.go` - Handler tests
- `internal/provisioner/oci/routes.go` - Route registration
- `cmd/provisioner-controller/main.go` - Integrate `/v2/*` routes

**Acceptance:**
- All OCI Distribution endpoints respond correctly
- Upload sessions tracked and cleaned up on timeout
- Error responses follow OCI spec format
- Registry endpoints co-exist with provisioning API without conflicts

### 4. Authentication and Authorization

**Tasks:**
- Implement basic authentication middleware (`internal/provisioner/oci/auth.go`)
- Support htpasswd file or database-backed credential store
- Require authentication for all `/v2/*` requests (except ping if configured)
- Authorization policy: allow pull/push based on user credentials
- Optional: repository-prefix based authorization (e.g., `tools/*` read-only)
- Never log credentials or Authorization headers
- Return `401 Unauthorized` with `WWW-Authenticate: Basic realm="Shoal Registry"`

**Tests:**
- Unauthenticated request to `/v2/*` returns 401
- Valid credentials allow push and pull
- Invalid credentials return 401
- Authorization header not logged in access logs
- Optional: repository-prefix policy enforced correctly

**Files:**
- `internal/provisioner/oci/auth.go` - Authentication middleware
- `internal/provisioner/oci/auth_test.go` - Auth tests
- `internal/provisioner/config/config.go` - Add registry auth config fields

**Acceptance:**
- Basic authentication enforced on all registry endpoints
- Credentials validated against htpasswd or database
- Secrets never appear in logs
- Clear error messages on auth failure

### 5. Garbage Collection

**Tasks:**
- Implement GC process (`internal/provisioner/oci/gc.go`)
- Build reachability graph: manifests/tags → blobs
- Mark unreferenced blobs for deletion after grace period
- Safe deletion: quarantine before final removal
- Background goroutine with configurable interval (e.g., 1 hour)
- Admin endpoint to trigger manual GC (optional)
- Metrics: `registry_gc_blobs_deleted`, `registry_gc_duration_seconds`

**Tests:**
- Delete tag → blob remains if referenced by another tag
- Delete all tags → blob deleted after GC run + grace period
- GC runs concurrently with uploads/downloads without corruption
- Quarantine/grace period prevents premature deletion

**Files:**
- `internal/provisioner/oci/gc.go` - Garbage collection logic
- `internal/provisioner/oci/gc_test.go` - GC tests
- `cmd/provisioner-controller/main.go` - Start GC background worker

**Acceptance:**
- Unreferenced blobs deleted after grace period
- Referenced blobs never deleted
- GC process doesn't interfere with active uploads/downloads
- GC metrics exposed

### 6. Integration with oras and podman

**Tasks:**
- Integration tests with real `oras` client
- Integration tests with real `podman` client
- Test artifact push/pull: rootfs tarball, Windows WIM (sparse file)
- Test container image push/pull: tool containers
- Verify maintenance OS can pull artifacts during provisioning workflow
- Update maintenance OS Quadlet container definitions to reference embedded registry
- Document registry usage in provisioning workflows

**Tests:**
- `oras push` rootfs tarball to controller
- `oras pull` rootfs tarball from controller (verify digest)
- `oras push` large WIM (>10GB sparse file)
- `podman push` tool container to controller
- `podman pull` tool container from controller
- Maintenance OS Quadlet pulls from controller during Linux workflow

**Files:**
- `internal/provisioner/integration/oci_registry_test.go` - Integration tests
- `images/maintenance/README.md` - Document registry usage
- `docs/provisioner/embedded_registry.md` - User-facing registry documentation

**Acceptance:**
- `oras` and `podman` clients work seamlessly with embedded registry
- Large artifacts (>10GB) transfer correctly
- Maintenance OS can pull artifacts during provisioning
- Documentation provides clear examples

### 7. Configuration and Observability

**Tasks:**
- Add registry configuration to controller (`internal/provisioner/config/config.go`)
- Environment variables: `ENABLE_REGISTRY`, `REGISTRY_STORAGE`, `REGISTRY_AUTH_MODE`, etc.
- Prometheus metrics: upload/download bytes, request counts, durations, storage usage
- Structured logging for all registry operations (no secrets logged)
- Audit log: push/pull/delete events with user, repo, tag, digest, size
- Health check endpoint: verify storage accessible and writable
- Storage usage monitoring: expose `registry_storage_bytes` and `registry_blob_count`

**Tests:**
- Configuration validation at startup
- Invalid config values rejected with clear errors
- Metrics exposed at `/metrics` endpoint
- Logs structured and secrets redacted
- Audit log records all push/pull/delete events

**Files:**
- `internal/provisioner/config/config.go` - Registry config
- `internal/provisioner/oci/metrics.go` - Prometheus metrics
- `internal/provisioner/oci/logging.go` - Structured logging
- `docs/provisioner/registry_configuration.md` - Configuration guide

**Acceptance:**
- Registry configurable via environment variables
- Metrics exposed and accurate
- Logs contain no secrets
- Audit log provides complete history
- Storage usage monitored

### 8. E2E Validation and Documentation

**Tasks:**
- End-to-end test: push artifact → reference in recipe → provision via maintenance OS
- Test with Linux workflow (rootfs tarball)
- Test with Windows workflow (WIM artifact)
- Performance test: 20GB blob upload/download
- Concurrency test: 8 parallel uploads to different repos
- Documentation: user guide, configuration reference, troubleshooting
- Example workflows: CI pipeline pushing artifacts, recipes using registry

**Tests:**
- E2E Linux: push rootfs → create recipe → provision → verify system boots
- E2E Windows: push WIM → create recipe → provision → verify system boots
- Performance: 20GB blob upload completes without timeout
- Concurrency: 8 parallel uploads without corruption or deadlock
- Failure recovery: controller restart with in-progress uploads

**Files:**
- `internal/provisioner/integration/oci_e2e_test.go` - E2E tests
- `docs/provisioner/embedded_registry.md` - Complete user guide
- `docs/provisioner/registry_troubleshooting.md` - Troubleshooting guide
- `docs/provisioner/recipes/linux_with_registry.json` - Example recipe
- `README.md` - Update Phase 5 status

**Acceptance:**
- Complete provisioning workflows using embedded registry
- Performance meets expectations for large artifacts
- Concurrent operations stable
- Documentation comprehensive and clear
- Controller restarts safely recover state

Acceptance Criteria (Summarized)

All Phase 5 acceptance criteria must be met:

- ✓ Controller serves `/v2/*` endpoints (OCI Distribution API)
- ✓ Blob upload/download with digest verification
- ✓ Manifest storage with tag and digest retrieval
- ✓ Basic authentication enforced (configurable)
- ✓ Storage backend uses OCI Layout on disk
- ✓ Garbage collection removes unreferenced blobs
- ✓ `oras` push/pull works for rootfs tarballs and WIMs
- ✓ `podman` push/pull works for container images
- ✓ Large artifacts (>10GB) handled correctly
- ✓ Concurrent operations safe and corruption-free
- ✓ Maintenance OS Quadlet pulls from embedded registry
- ✓ Metrics and logging provide observability
- ✓ Secrets never logged
- ✓ `go run build.go validate` passes with new tests
- ✓ Integration tests cover happy path and failure scenarios
- ✓ Documentation complete with examples

Testing Strategy (Phase 5)

### Unit Tests

**Storage Backend:**
- Blob write/read with digest verification
- Concurrent blob uploads (different digests)
- Digest mismatch rejection
- Atomic file operations (temp → final)

**Manifest Management:**
- Tag creation and update
- Digest-based retrieval
- Tag deletion (soft-delete)
- Concurrent tag updates

**Authentication:**
- Valid credentials accepted
- Invalid credentials rejected
- Authorization policy enforcement
- No secrets in logs

**Garbage Collection:**
- Reachability graph construction
- Unreferenced blob deletion
- Grace period enforcement
- Concurrent GC and uploads

### Integration Tests

**oras Client:**
- Push rootfs tarball (custom media type)
- Pull rootfs tarball (verify digest)
- Push large WIM (>10GB sparse file)
- Pull large WIM

**podman Client:**
- Push tool container image
- Pull tool container image
- Verify image runs correctly

**Provisioning Workflows:**
- Linux: push rootfs → recipe → provision → boot
- Windows: push WIM → recipe → provision → boot
- Failure: unreachable registry URL → correct error attribution

**Concurrency:**
- 8 parallel uploads to different repos
- 4 uploads + 4 downloads simultaneously
- No deadlocks, no corruption

### Performance Tests

**Large Blob Handling:**
- Upload 20GB blob (streaming, chunked)
- Download 20GB blob (streaming, Range support)
- Verify throughput and resource usage

**Sustained Load:**
- 10 parallel uploads (1GB each)
- Measure latency, CPU, memory
- Verify no resource leaks

### Security Tests

**Authentication:**
- Brute-force attempts rate-limited
- Invalid credentials logged (username only)
- No credential leakage

**Path Traversal:**
- Repository names sanitized
- No escape outside storage root

**Integrity:**
- Corrupted blob upload rejected
- Manifest with missing blobs rejected

Operational Notes

### Storage Requirements

- **Deduplication:** Blobs stored once per digest, shared across repositories
- **Filesystem:** Recommend XFS or ext4 with large file support
- **Capacity:** Plan for 2-5x provisioned artifact size (dedup + manifests + overhead)
- **Example:** 10 rootfs (5GB each) + 5 WIMs (8GB each) ≈ 90GB raw, ~60GB deduplicated

### Performance Considerations

- **Large Blobs:** Streaming uploads/downloads to avoid memory buffering
- **Chunked Uploads:** PATCH continuation for >1GB blobs
- **Range Requests:** Enable for partial downloads (e.g., layer caching)
- **Timeouts:** Generous for large blobs (default: 1 hour upload, 30 min download)
- **Concurrency:** Limit concurrent uploads (default: 8) to prevent I/O saturation

### Security Considerations

- **TLS:** Strongly recommended for production deployments
- **Basic Auth:** Use strong passwords; consider bcrypt hashing
- **Secrets:** Never log Authorization headers, passwords, or user-data
- **Audit Log:** Enable for compliance and forensics
- **Network Isolation:** Registry accessible only from maintenance OS network if possible

### Garbage Collection

- **Grace Period:** 24-48 hours recommended to avoid accidental deletion
- **Scheduling:** Run during low-traffic periods (e.g., nightly)
- **Monitoring:** Alert on high storage usage before GC runs
- **Manual Trigger:** Provide admin endpoint for emergency cleanup

Risks & Mitigations

### Risk: Large Artifact Performance

**Mitigation:** Streaming I/O, chunked uploads, Range support, generous timeouts

### Risk: Concurrent Upload Corruption

**Mitigation:** Advisory file locks, atomic renames, temporary files with UUIDs

### Risk: Storage Exhaustion

**Mitigation:** Enforce max blob size, monitor storage usage, alert on thresholds, GC

### Risk: Authentication Bypass

**Mitigation:** Middleware enforces auth on all endpoints, integration tests verify 401 responses

### Risk: Digest Collision (SHA-256)

**Mitigation:** Use standard Go crypto/sha256, verify on upload, trust collision resistance

Dependencies

### Phase 3 Components (Reused)

- Controller HTTP server (extend routing)
- Configuration system (add registry config)
- Logging and metrics infrastructure

### New Dependencies

- None - use Go standard library (`crypto/sha256`, `os`, `io`, `net/http`)
- Optional: `github.com/opencontainers/go-digest` for digest validation (MIT license, compatible)

### Design Documents

- `design/027_Embedded_OCI_Registry.md` (primary specification)
- `design/020_Provisioner_Architecture.md` (system overview)
- `design/021_Provisioner_Controller_Service.md` (controller integration)

Start Checklist

- [ ] Branch: `feature/provisioner-phase5`
- [ ] Baseline: `go run build.go validate` → PASS on master
- [ ] Designs reviewed: 020, 021, 027, 032
- [ ] Phase 3/4 components understood (Linux/Windows workflows)
- [ ] Storage location planned and accessible
- [ ] Test artifacts prepared (rootfs tarball, sparse WIM)

Success Metrics

- Complete embedded OCI registry implementation
- All unit and integration tests passing
- `oras` and `podman` clients work seamlessly
- Large artifacts (>10GB) handled without issues
- Documentation complete with examples
- Test coverage maintained >50%
- No regression in Phase 3/4 functionality

Future Enhancements (Post-Phase 5)

- Catalog API (`/v2/_catalog`) for browsing repositories
- Bearer token authentication (JWT with scopes)
- Repository-level quotas and limits
- Admin UI for registry management
- Prometheus remote-write integration
- Image signing and verification (Sigstore/Cosign)
- Proxy/mirror mode for external registries
- Replication between controller instances
- S3-backed storage backend (alternative to filesystem)

Change Log

- v0.1 (2025-11-08): Initial Phase 5 plan for Embedded OCI Registry implementation.
