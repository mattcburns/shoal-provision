# 023: Task ISO Builder — Deterministic ISO Generation and Serving

Status: Proposed
Owners: Provisioning Working Group
Last updated: 2025-11-03

Summary

This document defines the design for generating the per‑job task ISO (“task.iso”) and serving it to BMCs as virtual media. The task.iso is a small, deterministic ISO9660 image that contains the user’s recipe and any auxiliary assets (schema, cloud-init user-data, Windows unattend.xml, ESXi ks.cfg). The controller builds this ISO, stores it under a configured directory, serves it via HTTP with Range support, and manages its lifecycle. Determinism ensures identical inputs yield bit-for-bit identical outputs, which simplifies caching, integrity, and debugging.

Related

- 020_Provisioner_Architecture.md (system overview and index)
- 021_Provisioner_Controller_Service.md (APIs, orchestration, persistence)
- 022_Recipe_Schema_and_Validation.md (authoritative schema and limits)
- 027_Embedded_OCI_Registry.md (optional /v2/ handler)
- 028_Redfish_Operations.md (virtual media and reboot flows)
- 032_Error_Handling_and_Webhooks.md (webhook reliability)

Goals

- Deterministic ISO generation given the same inputs (byte-identical output).
- Small ISO (typically < 5 MiB) with ISO9660 + Rock Ridge/Joliet for broad compatibility.
- Fast build (< 1s typical) and predictable performance.
- Simple, secure serving with HTTP Range support and short‑lived signed URLs.
- Robust retention and garbage collection (GC) strategy.

Non‑Goals

- General‑purpose ISO authoring for arbitrary directories.
- Bootable ISO creation (maintenance.iso is bootable; task.iso is data-only).
- Content encryption (HTTPS/TLS termination assumed at controller or network).

ISO Contents and Layout

- Required:
  - /recipe.json: The validated recipe (opaque to controller post-validation).
  - /recipe.schema.json: The exact schema version used to validate.
- Optional (per workflow):
  - /user-data: Cloud‑init user‑data for Linux workflows.
  - /unattend.xml: Windows answer file.
  - /ks.cfg: ESXi kickstart file.
- Constraints:
  - Filenames are lowercase ASCII with hyphens; no spaces.
  - No symlinks, device nodes, or special files.
  - File permissions set to read‑only (0444), owner/group 0:0.
  - Total size ≤ configured limit (e.g., 10 MiB); individual files as per 022.

Inputs → Outputs

Inputs (immutable within a build):
- job_id (uuid)
- recipe_json (string; already validated against schema)
- schema_json (string; controller’s authoritative copy)
- optional user_data (string)
- optional unattend_xml (string)
- optional ks_cfg (string)
- build_parameters:
  - volume_id (derived; see Determinism)
  - source_date_epoch (seconds since epoch; see Determinism)

Outputs:
- on-disk ISO file path: TASK_ISO_DIR/{job_id}/task.iso
- metadata: sha256 digest (hex), size bytes, volume_id, created_at
- DB fields in jobs table updated (task_iso_path, updated_at)

Deterministic Build Strategy

To achieve bit‑for‑bit determinism:

1) Stable file set and ordering
- Include only the defined filenames above.
- Sort inputs lexicographically before writing to staging dir (implied by static names).

2) Stable metadata and timestamps
- Set all file mtimes to SOURCE_DATE_EPOCH.
- Set owner/group to 0:0 and mode 0444 (directories 0555).
- Use a constant timezone/locale (UTC).

3) Stable volume ID and image layout
- Volume ID: TASK_<JOBID_NO_DASH> (uppercased, max 32 chars; truncated deterministically).
- Disable automatic padding that depends on environment; explicitly control padding flags.
- Use a fixed set/order of mkisofs/xorriso flags (documented below).

4) Stable toolchain & flags
- Prefer xorriso/xorrisofs or genisoimage/mkisofs with:
  - Rock Ridge (-R) for POSIX attributes and long names.
  - Joliet (-J) to maximize compatibility (some BMCs browse Joliet trees).
  - Deterministic timestamps via -volume_date/-modification-date or SOURCE_DATE_EPOCH.
  - Disable variable padding where possible (-no-pad, when compatible with BMCs).
- Record tool version in build metadata for traceability.

5) Environmental isolation
- Build in a fresh, per‑job staging directory.
- Avoid locale-dependent behavior (LC_ALL=C).

Recommended Command Lines

Option A: xorriso (preferred when available)
- Flags:
  - -outdev <iso_path>
  - -volumeid <VOLID>
  - -joliet on
  - -rockridge on
  - -set_all_file_dates <ISO_UTC_TIMESTAMP> (from SOURCE_DATE_EPOCH)
  - -uid 0 -gid 0
  - -file_name_limit 31 for ISO9660 tree; Joliet will keep long names.
  - -padding 0 (if BMCs accept; otherwise use small fixed padding)
  - -map <src_dir> / (map staging dir to image root)

Option B: genisoimage/mkisofs
- Flags:
  - -o <iso_path>
  - -J -R
  - -V <VOLID>
  - -iso-level 3
  - -graft-points .
  - -M or -no-pad depending on compatibility
  - Use SOURCE_DATE_EPOCH for uniform timestamps (honored by many mkisofs variants)

Note: Verify your chosen tool’s support for SOURCE_DATE_EPOCH. If not supported, set timestamps explicitly on files in staging and rely on -R to persist them.

Builder API and Flow

Proposed package: internal/provisioner/iso

Public API (concept):

- BuildTaskISO(ctx, params) (isoPath string, meta Metadata, err error)
  - params:
    - JobID string
    - RecipeJSON []byte
    - SchemaJSON []byte
    - UserData []byte (optional)
    - UnattendXML []byte (optional)
    - KSCfg []byte (optional)
    - SourceDateEpoch time.Time (or int64)
    - VolumeID string (optional; default derived)
    - OutputDir string (TASK_ISO_DIR/<job_id>)
  - returns:
    - path to iso
    - Metadata{SHA256, SizeBytes, VolumeID, Tool, ToolVersion}

Steps:

1) Prepare staging
- Create tmp dir: TASK_ISO_DIR/<job_id>/.build-<rand>
- Write files with exact names:
  - recipe.json
  - recipe.schema.json
  - user-data (if provided)
  - unattend.xml (if provided)
  - ks.cfg (if provided)
- Set file perms 0444, dir perms 0555.
- Set mtime of all files and dirs to SourceDateEpoch.

2) Compute derived VolumeID
- VOLID := "TASK_" + UPPERCASE(JobID without dashes)
- Truncate to tool’s max (often 32 chars).
- Persist VOLID in metadata.

3) Invoke ISO tool
- Prefer xorriso; fallback to mkisofs if not present (configurable).
- Pass flags for Rock Ridge, Joliet, timestamp, UID/GID, padding policy.
- Output to TASK_ISO_DIR/<job_id>/task.iso
- Ensure parent directory exists; write atomically (tmp file + rename).

4) Verify and hash
- stat() image; size > 0.
- sha256(file) → hex digest; store in metadata and DB.
- Optionally, re‑mount locally (loopback) in tests to verify content.

5) Cleanup staging
- Remove .build-* staging dir.
- Leave final iso and a small JSON sidecar (<job_id>/task.iso.meta.json) with metadata (optional).

Serving the ISO

Endpoint

- GET /media/tasks/{job_id}/task.iso
  - Purpose: BMC virtual media URL.
  - Auth: Short‑lived signed URLs or basic auth bound to BMC, depending on deployment policy.
  - Headers:
    - Content-Type: application/x-iso9660-image
    - Content-Length: <bytes>
    - Accept-Ranges: bytes
    - ETag: "sha256:<hex>"
    - Cache-Control: private, max-age=60 (or 0 if using signed URLs)
  - Behavior:
    - Supports Range requests (partial content 206).
    - Validates signature if signed URLs are enabled (see below).
    - Returns 404 if job not found or ISO missing.

Signed URLs (recommended)

- HMAC signature with a controller secret:
  - token = base64url(HMAC-SHA256(secret, job_id + ":" + expires + ":" + sha256))
- Query parameters:
  - ?expires=UNIX_TS&sig=TOKEN
- Validation:
  - Check expires >= now + small skew.
  - Recompute token; must match sig.
- Revocation:
  - Rotate secret to invalidate all outstanding URLs (rare).
  - Delete ISO to invalidate specific job URLs.

Security Considerations

- Sanitize filenames: never include user-controlled path components.
- Deny symlinks and special files in staging (enforce regular files only).
- Size limits: reject when any file or total exceeds configured limits.
- Secrets: do not embed secrets in recipe.json; if present in user_data/unattend.xml, do not log file contents.
- Signed URL logs: redact signature values.
- Store BMC credentials outside of ISO logic (controller servers table).

Compatibility and BMC Quirks

- Some BMCs require Joliet to browse files; include -J in build flags.
- Some BMCs cannot handle zero padding; if issues occur, add fixed padding (e.g., 300k) deterministically.
- Ensure HTTP Range is supported; some BMCs issue partial GETs.
- If a BMC caches aggressively, prefer signed URLs with short expirations to avoid stale content.

Retention and Garbage Collection

- Policy: delete task ISO files for jobs in state=complete older than JOB_RETENTION_DAYS (default 14).
- Background GC:
  - Periodically scan TASK_ISO_DIR:
    - If job_id directory has no active job or is older than retention, delete directory.
  - Emit events for deletions.
- Disk pressure:
  - Optional: high‑watermark GC triggers earlier cleanup if free space below threshold.

Error Handling and Retries

- ISO build:
  - Disk full, permission denied → terminal error; mark job failed with step=iso.build.
  - Tool not found → configuration error; log once per interval, mark failed.
- Serving:
  - Missing ISO → 404; worker may re‑build if provisioning still in progress.
- Determinism failures:
  - If computed hash changes given identical inputs (should not happen), log critical event with both hashes; keep both artifacts for investigation.

Testing Strategy

Unit tests
- Deterministic build:
  - Given fixed inputs and SOURCE_DATE_EPOCH, repeated builds produce identical SHA256.
- Layout correctness:
  - Mount built ISO (loopback or library) and verify file list, permissions, contents.
- Limits enforcement:
  - Oversized user_data/unattend/ks_cfg rejected.
- Signed URL validation:
  - Valid and invalid signatures; expired token behavior.

Integration tests
- Controller worker path:
  - After POST /api/v1/jobs, a task.iso is created; GET /media/tasks/{job_id}/task.iso returns 200 with correct headers and ETag.
- BMC simulator:
  - Issue Range requests; verify 206 responses and correct slices.
- GC:
  - Advance time; verify old ISOs are removed and DB state remains intact.

Performance tests
- Build latency under concurrent workers (WORKER_CONCURRENCY).
- Throughput of the media server with partial reads for large files (even though task.iso is small).

Acceptance Criteria

- Determinism:
  - Same inputs, same SOURCE_DATE_EPOCH, same tool/flags → identical SHA256 across runs and hosts.
- Compatibility:
  - ISOs are readable by common BMCs (iDRAC/iLO/XCC/Supermicro); files visible and mountable.
- Limits:
  - Enforced per 022; oversized inputs rejected with clear errors.
- Serving:
  - Endpoint supports Range requests and sets Content-Type, ETag, and Content-Length.
  - Signed URLs validated with configurable skew and expiry.
- Lifecycle:
  - ISO path recorded in DB and cleaned up by GC after retention.
- Observability:
  - Events logged for build start/end, size, digest, and serving; errors emit actionable messages.
- Validate pipeline:
  - go run build.go validate passes with unit/integration tests added in subsequent implementation.

Open Questions

- Should we embed an integrity file (e.g., /sha256.txt) inside task.iso for offline verification on maintenance OS? Not strictly required since content is trusted from controller, but could aid debugging.
- Fixed padding size default: 0 vs a small deterministic pad for maximal BMC compatibility (collect data; start with 0 and toggle via config on demand).

Appendix A: Example Build (mkisofs)

```bash
VOLID="TASK_F7F5D2B61F1F4B7C9FCB2A8E1B8E5B4A"   # derived and truncated if needed
EPOCH="${SOURCE_DATE_EPOCH:-1730659200}"        # 2024-11-04 00:00:00 UTC example
STAGING="/var/lib/shoal/tasks/f7f5.../staging"
OUT="/var/lib/shoal/tasks/f7f5.../task.iso"

# Ensure deterministic perms and mtimes
find "$STAGING" -exec chmod 0444 {} \; 2>/dev/null
find "$STAGING" -type d -exec chmod 0555 {} \; 2>/dev/null
find "$STAGING" -exec touch -d "@$EPOCH" {} \;

genisoimage \
  -o "$OUT" \
  -J -R \
  -iso-level 3 \
  -V "$VOLID" \
  -graft-points \
  -quiet \
  "$STAGING"=/  # contents of STAGING at ISO root
```

Appendix B: Example Build (xorriso)

```bash
VOLID="TASK_F7F5D2B61F1F4B7C9FCB2A8E1B8E5B4A"
EPOCH_ISO="$(date -u -d "@${SOURCE_DATE_EPOCH:-1730659200}" +"%Y%m%d%H%M%S00")"
STAGING="/var/lib/shoal/tasks/f7f5.../staging"
OUT="/var/lib/shoal/tasks/f7f5.../task.iso"

xorriso \
  -outdev "$OUT" \
  -volumeid "$VOLID" \
  -joliet on \
  -rockridge on \
  -padding 0 \
  -set_all_file_dates "$EPOCH_ISO" \
  -uid 0 -gid 0 \
  -map "$STAGING" /
```

Appendix C: Example Sidecar Metadata (optional)

```json
{
  "job_id": "f7f5d2b6-1f1f-4b7c-9fcb-2a8e1b8e5b4a",
  "path": "/var/lib/shoal/tasks/f7f5.../task.iso",
  "sha256": "8f14e45fceea167a5a36dedd4bea2543...",
  "size_bytes": 32768,
  "volume_id": "TASK_F7F5D2B61F1F4B7C9FCB2A8E1B8E5B4A",
  "tool": "xorriso",
  "tool_version": "1.5.4",
  "source_date_epoch": 1730659200,
  "created_at": "2025-11-03T20:30:00Z"
}
```
