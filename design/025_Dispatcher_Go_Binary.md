# 025: Dispatcher Go Binary

Status: Proposed
Owners: Provisioning Working Group
Last updated: 2025-11-03

Summary

This document specifies the design of the on-host Dispatcher (a static Go binary) that runs inside the bootc-based maintenance OS. The binary’s sole responsibility is to deterministically read a job “recipe” from the second virtual media (task.iso), validate it, materialize normalized runtime inputs under /run/provision, and then hand off execution to systemd by starting the systemd target declared in the recipe. The Dispatcher is intentionally tiny, dependency-free at runtime, robust against transient hardware readiness issues, and explicit in its failure semantics so the controller can attribute failures to root causes quickly.

Related

- 020_Provisioner_Architecture.md
- 021_Provisioner_Controller_Service.md
- 022_Recipe_Schema_and_Validation.md
- 023_Task_ISO_Builder.md
- 024_Maintenance_OS_Build_with_bootc.md
- 026_Systemd_and_Quadlet_Orchestration.md
- 032_Error_Handling_and_Webhooks.md

Goals

- Be the single point of logic that translates the job “recipe” into concrete runtime inputs for systemd/Quadlet workflows.
- Be reliable and deterministic: given a valid recipe and hardware that presents the task ISO, always produce the same normalized outputs and start the specified target.
- Provide clear, actionable failure modes, exit codes, and logs suitable for automated troubleshooting and controller attribution.
- Remain self-contained: one static binary, no network access required, no dynamic dependencies.

Non-Goals

- Running provisioning tools or managing containers directly (that’s systemd/Quadlet).
- Long-lived orchestration or any “phone-home” loops beyond optional failure reporting paths.
- Complex policy evaluation or vendor-specific behavior; those are implemented by controller, systemd units, or tool containers.

Responsibilities

- Wait for and mount the task ISO device read-only.
- Load and validate recipe.json against recipe.schema.json (both from the task ISO).
- Normalize and write inputs for downstream units:
  - /run/provision/recipe.env for scalar values
  - /run/provision/layout.json for partitioning layouts
  - /run/provision/user-data, /run/provision/unattend.xml, and other auxiliary files if present
- Resolve and persist the system serial number for webhook usage by other units.
- Start the systemd target specified by task_target.
- Exit with a precise exit code (0 on successful handoff; non-zero on failure).

Runtime Behavior

- Preflight:
  - Ensure the process is running as root (required for mounting).
  - Check for required OS facilities (mount syscall availability, /sys and /dev presence).
- Task ISO availability:
  - Poll for the configured ISO block device (default /dev/sr1) with a bounded timeout (e.g., 120 seconds) and a short polling interval (e.g., 1s).
  - Optional: if the configured device does not appear, optionally probe a small, preconfigured set (e.g., /dev/sr0 and /dev/sr1) in order, then bind-select the one that mounts successfully.
- Mount:
  - Create the mountpoint (default /mnt/task) if missing.
  - Mount read-only with filesystem type “iso9660”; mark the mount as nosuid,nodev,noexec where compatible (noexec is acceptable because no binaries in task.iso are executed by the Dispatcher).
- Validation:
  - Load /mnt/task/recipe.schema.json and compile a validator (draft-07).
  - Load /mnt/task/recipe.json, parse JSON, and validate against the schema.
  - Reject inputs exceeding configured size limits (consistent with 022).
- Normalization:
  - Create /run/provision with mode 0755 if missing.
  - Persist an environment file /run/provision/recipe.env containing key=value pairs:
    - TASK_TARGET, TARGET_DISK, OCI_URL, FIRMWARE_URL, SERIAL_NUMBER (derived), and any other scalar fields the workflows need.
  - Persist any structured data to specific files:
    - /run/provision/layout.json from the recipe partition_layout field (verbatim).
    - /run/provision/user-data and /run/provision/unattend.xml if present and non-empty.
  - Ensure all files are written atomically (write to a temp file and rename).
  - Permissions:
    - env and JSON files: 0644 (readable by root and system service accounts).
  - Compute and log a short content summary (e.g., sizes and SHA256 of large files) for diagnostics; do not log raw content.
- Systemd handoff:
  - Invoke systemctl start <task_target>.
  - Stream stdout/stderr to journald so target start issues are captured.
  - If systemctl returns success, the Dispatcher exits 0.
  - If systemctl returns failure, exit with a dedicated code (see Exit Codes) and log the error.
- Cleanup:
  - The Dispatcher does not unmount /mnt/task; keeping it mounted read-only is desirable for subsequent services that may read auxiliary files if needed.
  - No long-running processes remain; the systemd units take over.

Interfaces

- CLI flags (defaults align with 024):
  - --task-iso-device: default /dev/sr1; optional multi-value fallback (comma-separated) such as /dev/sr1,/dev/sr0
  - --task-mount-point: default /mnt/task
  - --env-dir: default /run/provision
  - --schema-path: default recipe.schema.json (relative to task mount)
  - --recipe-path: default recipe.json (relative to task mount)
  - --udev-wait-seconds: default 120
  - --poll-interval: default 1s
  - --log-level: default info (debug|info|warn|error)
  - --serial-source: auto|dmi|dmidecode|env; default auto
  - --serial-env-key: default PROVISIONER_SERIAL (if serial-source=env)
  - --no-start: when set, perform all steps except systemctl start (for testing)
  - --target-override: when set, overrides task_target (for recovery testing)
- Environment variables:
  - PROVISIONER_SERIAL: optional override for serial number detection
  - PROVISIONER_LOG_LEVEL: default log level
  - Others mapped 1:1 with flags for container-friendly configuration
- File system contracts:
  - Input: /dev/srN block device; mount contents include recipe.json and recipe.schema.json
  - Output: /run/provision with files mentioned above
- Data structure (conceptual):
  - ProvisionRecipe:
    - task_target (string, required)
    - target_disk (string, optional)
    - oci_url (string, optional)
    - firmware_url (string, optional)
    - partition_layout (json.RawMessage, optional)
    - user_data (string, optional)
    - unattend_xml (string, optional)
    - Additional optional fields allowed by schema; only a curated subset is exported to recipe.env

Serial Number Resolution

- Purpose: populate SERIAL_NUMBER in /run/provision/recipe.env for webhook services and auditing.
- Order (serial-source=auto):
  1) Environment override: if PROVISIONER_SERIAL is set and non-empty, use it.
  2) DMI sysfs: read /sys/class/dmi/id/product_serial (trim whitespace).
  3) dmidecode fallback: run “dmidecode -s system-serial-number” if available (best-effort).
  4) If still unknown, set SERIAL_NUMBER to “unknown” and log a warning.
- Note: If a server is known to report empty or “To Be Filled By O.E.M.” serials, controller-side mapping can provide the real serial via PROVISIONER_SERIAL.

Failure Modes and Exit Codes

The Dispatcher returns precise exit codes so the failure path (e.g., provision-failed.service) can attribute root causes. Non-exhaustive mapping:

- 0: Success — recipe validated, outputs written, and systemd target started.
- 10: Task ISO device not found within timeout.
- 11: Mount failure (permission denied, wrong filesystem, device error).
- 12: Schema load/compile error (missing or invalid recipe.schema.json).
- 13: Recipe read error (I/O failure, invalid JSON).
- 14: Recipe schema validation error (details logged).
- 15: Output write failure (disk full, permission denied).
- 16: Systemd start failure (systemctl returned non-zero).
- 17: Insufficient privileges / environment (not root, missing mount capability).
- 18: Serial number detection fatal error (extremely unlikely; only if strict mode enabled).
- 20: Unexpected panic (recovered); the Dispatcher logs stack trace, exits non-zero.

Notes:
- On any non-zero exit, systemd marks provision-dispatcher.service as failed, which triggers the failure path defined by 026 (e.g., provision-failed.service).
- The failure path reports failure back to controller via webhook, including the failed unit (expandable with %n or a static step name like “provision-dispatcher.service”).

Logging and Observability

- Output format: human-readable lines with level prefix; optionally JSON logs if configured in future.
- Always include:
  - step: “dispatcher”
  - device and mountpoint used
  - task_target
  - paths written
  - summary of sizes/checksums of large auxiliary files (never raw content)
  - serial source and result (redacted if policy requires)
- Log levels:
  - debug: detailed flow and timings
  - info: major phase transitions
  - warn: non-fatal anomalies (e.g., serial unknown)
  - error: fatal conditions with exit code
- Timers:
  - Measure and log wait time to device availability, schema compile time, and total time-to-handoff.

Security Considerations

- Run as root; drop no capabilities because mounting requires privileges; rely on host being a dedicated maintenance OS.
- Treat all recipe contents as untrusted data:
  - Never execute or interpret arbitrary code.
  - Only write recipe data to files and call systemctl with a literal target name validated by schema constraints (pattern verifying “*.target”).
- Do not log sensitive content (user-data, unattend.xml); instead log sizes and hashes.
- Mount task ISO read-only with restrictive mount flags where compatible: ro,nosuid,nodev,noexec.
- Enforce upper bounds on input sizes (consistent with 022) to avoid memory pressure or excessive /run/provision usage.
- Avoid creating world-writable files; only 0644 for outputs and 0755 for directories.

Determinism and Idempotency

- Given the same task ISO, the Dispatcher produces byte-identical outputs in /run/provision (except for metadata like file mtime if not standardized; adjust to set mtime to now or a fixed epoch if required).
- Idempotency:
  - Re-running the Dispatcher overwrites outputs atomically with the same contents and reattempts systemctl start.
  - If the target is already active, systemctl start is effectively a no-op; the Dispatcher logs and exits 0.

Testing Strategy

- Unit tests:
  - JSON schema validation success/failure cases (using the embedded schema compiler).
  - Parsing and normalization of recipe inputs into env and file outputs.
  - Serial number resolution across all sources (mocked).
  - Exit code mapping for each simulated failure path.
- Integration tests (VM/containerized):
  - Simulate appearing /dev/sr1 with a loopback-mounted ISO image populated with recipe.json and recipe.schema.json.
  - Verify /run/provision file outputs and that systemctl is invoked with the specified target (in test, use --no-start or a stub).
  - Negative tests for device timeout, mount error, invalid JSON, and schema violations.
- Performance:
  - Validate time-to-handoff targets (e.g., within a few seconds after device availability).
- CI:
  - Tests run under go run build.go validate.
  - No network requirement.

Tuning and Configuration

- Timeouts:
  - udev wait: default 120s, override via --udev-wait-seconds; log remaining time while waiting.
- Device probing:
  - If multi-device fallback is enabled, only probe a small, fixed list to avoid accidental mounting of unrelated media.
- Start behavior:
  - --target-override for recovery/testing scenarios; log a warning that it overrides the recipe.
  - --no-start for dry-run validation and provisioning prep.

Interaction with Systemd and Webhooks

- The Dispatcher only starts the target; it does not wait for completion.
- Success and failure notifications are handled by systemd units:
  - OnSuccess of the master target triggers provision-success.service.
  - OnFailure of the master target triggers provision-failed.service.
- Preflight failure reporting:
  - Preferred: rely on systemd’s OnFailure of provision-dispatcher.service to trigger the provision-failed.service which sends a webhook.
  - Optional fallback: the Dispatcher may directly POST failure to controller if configured with a webhook URL and secret; prefer the systemd approach to keep the binary network-agnostic.

Pseudocode (high-level)

- Parse flags and env
- Ensure root, log version and config
- Wait for task device up to timeout
- Mount read-only to /mnt/task
- Load and compile schema
- Load recipe and validate
- Resolve serial number
- Write /run/provision outputs (env, layout, auxiliary files)
- Start systemd target (systemctl start <task_target>)
- Exit 0 on success; otherwise exit with precise code

Open Questions

- Should we enforce that task_target must be one of an allowlist shipped with the maintenance OS (defense-in-depth), rather than only a pattern match? Answer: likely yes; implement a simple allowlist by scanning available *.target files under /etc/systemd/system and failing otherwise (configurable).
- Should we stamp a build/version file to /run/provision (e.g., dispatcher version, commit SHA) for troubleshooting? Answer: yes; include /run/provision/build-info.txt containing dispatcher version and schema $id.
- Should we standardize file timestamps for determinism in /run/provision? Answer: not required for function; can be implemented if helpful for golden-tests.

Acceptance Criteria

- Given a valid task ISO, the Dispatcher:
  - Mounts the ISO read-only.
  - Validates recipe.json against recipe.schema.json.
  - Writes /run/provision/recipe.env, layout.json, and auxiliary files if present.
  - Resolves SERIAL_NUMBER and persists it to recipe.env.
  - Starts the declared systemd target and exits 0.
- On invalid or missing inputs, the Dispatcher exits non-zero with a precise exit code and clear logs; systemd failure path is triggered.
- No network access is required for normal operation; optional direct webhook on preflight failure is feature-flagged and off by default.
- Unit and integration tests cover success and major failure modes; go run build.go validate passes.

Change Log

- v0.1 (2025-11-03): Initial design for dispatcher behavior, interfaces, and failure modes.