# 028: Redfish Operations — Virtual Media, Boot Override, Reboot, Vendor Notes

Note for implementers (vendor constants):
- VENDOR_IDRAC = "iDRAC" — Dell iDRAC controllers
- VENDOR_ILO = "iLO" — HPE iLO controllers
- VENDOR_SUPERMICRO = "Supermicro" — Supermicro BMCs

Guidance:
- Use these string constants when mapping configuration/vendor hints in code.
- When the exact vendor string is not provided, tolerate common variations (e.g., case-insensitive matches, substrings like "dell", "hpe"/"hp", "supermicro") for pragmatic detection.
- Boot override: prefer adding Boot.BootSourceOverrideMode="UEFI" for iDRAC and iLO; omit for Supermicro unless required by a specific firmware.
- InsertMedia: include Inserted:true and TransferProtocolType:"URI" for iDRAC/iLO; WriteProtected:true is recommended across vendors.


Status: In Progress
Owners: Provisioning Working Group
Last updated: 2025-11-05

Summary

Progress (2025-11-05)
- Phase 1: A Redfish Noop client is implemented and used by the controller workers to simulate virtual media mount, one-time boot, and reboot. This unblocks end-to-end orchestration and testing without real BMCs.
- Next phases: Implement the real Redfish client per this document (virtual media, boot override, power operations), add vendor capability profiles and retries/backoff, and replace the noop client in integration tests as hardware or mocks become available.

This document defines the Redfish operations used by the Provisioner Controller to orchestrate bare‑metal provisioning via dual virtual media at Layer 3. It specifies how to reliably locate Redfish resources, insert/eject virtual media, set one‑time boot to CD, initiate a reboot, and perform cleanup. It also documents idempotency rules, timeouts/retries, polling heuristics, and vendor-specific observations (iDRAC, iLO, XCC, Supermicro) to maximize compatibility.

Related

- 020_Provisioner_Architecture.md
- 021_Provisioner_Controller_Service.md
- 022_Recipe_Schema_and_Validation.md
- 023_Task_ISO_Builder.md
- 024_Maintenance_OS_Build_with_bootc.md
- 025_Dispatcher_Go_Binary.md
- 026_Systemd_and_Quadlet_Orchestration.md
- 027_Embedded_OCI_Registry.md
- 032_Error_Handling_and_Webhooks.md
- 035_Test_Strategy.md

Goals

- Provide a minimal, reliable Redfish control loop supporting:
  - Insert virtual media (maintenance.iso, task.iso)
  - Set one‑time boot to CD
  - Reset system (graceful with safe fallback)
  - Eject media during cleanup
- Be idempotent and vendor-tolerant through discovery, conditional logic, and retries.
- Offer clear failure attribution for job events (e.g., redfish.mount, redfish.boot‑override, redfish.reset, redfish.unmount).

Non‑Goals

- Full hardware inventory or lifecycle management beyond what provisioning needs.
- Advanced vendor OEM actions not required for generic virtual media/boot control.

Terminology

- System: Redfish ComputerSystem resource (/redfish/v1/Systems/{id})
- Manager: Redfish Manager resource (/redfish/v1/Managers/{id})
- VirtualMedia: Collection & instances under a Manager (commonly) or under a System on some vendors.

1. Authentication, Sessions, and TLS

- Authentication
  - Support HTTP Basic and Session-based auth.
  - Prefer sessions when available to avoid repeated credential exchange.
- Sessions
  - Create a session at /redfish/v1/SessionService/Sessions (POST) and reuse the X-Auth-Token.
  - Handle 401 by refreshing the session (single retry) before failing the step.
- TLS
  - Prefer HTTPS; allow a deployment toggle for self-signed BMC certs if necessary.
  - Never log credentials, tokens, or Authorization headers.

2. Resource Discovery (do not hardcode)

- Discover the System and Manager resources:
  1) GET /redfish/v1/
  2) GET ServiceRoot.Systems and enumerate to pick the intended ComputerSystem (single‑system BMCs commonly expose one).
  3) From System.Links.ManagedBy or via ServiceRoot.Managers, resolve the Manager resource for VirtualMedia operations.
- Discover VirtualMedia:
  - Prefer Manager.VirtualMedia collection (widely supported).
  - Some implementations expose VirtualMedia under System; detect and fallback accordingly.
  - Enumerate VirtualMedia instances and select a “CD”/“DVD” like target by inspecting MediaTypes and Id/Name.

3. Operations (Happy Path)

3.1 Insert maintenance.iso (CD1) and task.iso (CD2)

- POST InsertMedia action on the appropriate VirtualMedia instances (two separate calls), where payload typically includes:
  - Image: fully qualified HTTP(S) URL to the ISO
  - Inserted: true (if supported)
  - WriteProtected: true (recommended)
  - TransferProtocolType: "URI" (some vendors require this)
- Avoid assuming instance names. Use discovery; if there are two “CD” slots, prefer stable ordering by Id or index.

```/dev/null/insert_media.json#L1-20
{
  "Image": "https://controller.internal:8080/media/tasks/<job_id>/task.iso",
  "Inserted": true,
  "WriteProtected": true,
  "TransferProtocolType": "URI",
  "UserName": null,
  "Password": null
}
```

3.2 Set one‑time boot to CD

- PATCH /redfish/v1/Systems/{id} with:
  - Boot.BootSourceOverrideEnabled = "Once"
  - Boot.BootSourceOverrideTarget = "Cd"
- Optional: set Boot.BootSourceOverrideMode (e.g., "UEFI") if required by vendor.

```/dev/null/patch_boot_override.json#L1-12
{
  "Boot": {
    "BootSourceOverrideEnabled": "Once",
    "BootSourceOverrideTarget": "Cd",
    "BootSourceOverrideMode": "UEFI"
  }
}
```

3.3 Reset (reboot) the host

- POST /redfish/v1/Systems/{id}/Actions/ComputerSystem.Reset with preferred reset type:
  - ResetType: "GracefulRestart"
  - Fallbacks if not supported or timeouts:
    - "ForceRestart"
    - "PowerCycle"
- After issuing Reset, begin polling for BMC/API readiness (see Section 5).

```/dev/null/reset_system.json#L1-6
{
  "ResetType": "GracefulRestart"
}
```

3.4 Cleanup (after success/failure webhook, or ESXi polling)

- EjectMedia (both virtual drives) with POST VirtualMedia.EjectMedia.
- Optionally clear Boot.BootSourceOverrideTarget to "None" if the vendor persists state longer than expected (idempotency-safe).
- Reboot to production OS (same reset semantics as above).

4. Idempotency, Timeouts, and Retries

4.1 Idempotency checks

- InsertMedia:
  - Read VirtualMedia properties; if an identical Image is already inserted (and not Stale), skip reinsert.
  - If a different Image is present, EjectMedia then InsertMedia.
- Boot override:
  - Read System.Boot; if already set to Once/Cd, skipping PATCH is acceptable.
- Reset:
  - Ensure we didn’t just reset moments ago (debounce timer).
- Eject:
  - EjectMedia is safe to repeat; ignore “already ejected” responses.

4.2 Timeouts and retries (controller defaults; configurable)

- Per‑request timeout: 30s.
- Retries: up to 5 attempts with exponential backoff (e.g., 500ms → 8s), with jitter.
- Classified retryable errors:
  - 5xx responses, network timeouts, connection resets.
  - 429 / rate limit (respect Retry-After if present).
- Non‑retryable:
  - 4xx (except 409/429 on some vendors), schema/URI errors (after discovery fallback attempts), auth errors.
- Step‑level timeouts:
  - Mount/boot‑override phase: 20 minutes aggregate budget.
  - Post‑reset BMC availability: 15 minutes (see polling).
  - Cleanup: 10 minutes best‑effort; mark complete even if some unmounts fail.

4.3 Failure attribution (job.failed_step)

- redfish.discover: failure locating required resources (Systems, Managers, VirtualMedia).
- redfish.mount.maintenance or redfish.mount.task: InsertMedia failure per drive.
- redfish.boot-override: failed to PATCH Boot override.
- redfish.reset: failed to reset host to start provisioning.
- redfish.cleanup.unmount: EjectMedia failure during cleanup.
- redfish.cleanup.reset: final reset failure.

5. Polling Strategy (Post‑Reset and ESXi Flow)

- After Reset, the host reboots and the BMC/API may be briefly unavailable. Poll:
  1) Redfish ServiceRoot (GET /redfish/v1/) until HTTP 200 (with backoff).
  2) Then GET Systems/{id} to confirm API is stable.
- ESXi installer path (no webhook):
  - Poll System.PowerState and/or LastResetTime.
  - Detect transition from On → cycling → On.
  - After installer-driven reboot, proceed to cleanup (eject ISOs).
  - Vendor variability exists; fallback to a time‑based heuristic if power state is unreliable.

```/dev/null/polling_pseudocode.txt#L1-30
loop until deadline:
  if GET /redfish/v1/ returns 200:
    if GET /redfish/v1/Systems/{id} returns 200:
      break
  sleep(backoff)

# ESXi flow post-install:
wait_for_reboot_window(deadline=30m):
  observe power state transitions or periodic NotAvailable errors
  on stable On for >=N seconds:
    proceed to cleanup
```

6. VirtualMedia: URIs and Actions (Reference)

- Discovering VirtualMedia (typical):
  - GET /redfish/v1/Managers/{managerId}/VirtualMedia/
  - Enumerate Members[*].@odata.id
  - For each, GET instance and inspect:
    - MediaTypes: contains "CD", "DVD", or "USBStick"
    - Actions: VirtualMedia.InsertMedia, VirtualMedia.EjectMedia
- InsertMedia:
  - POST {VirtualMediaId}/Actions/VirtualMedia.InsertMedia
- EjectMedia:
  - POST {VirtualMediaId}/Actions/VirtualMedia.EjectMedia

```/dev/null/virtualmedia_example.http#L1-40
# Discover VirtualMedia
GET /redfish/v1/Managers/iDRAC.Embedded.1/VirtualMedia/
Accept: application/json

# Example member
GET /redfish/v1/Managers/iDRAC.Embedded.1/VirtualMedia/CD

# Insert
POST /redfish/v1/Managers/iDRAC.Embedded.1/VirtualMedia/CD/Actions/VirtualMedia.InsertMedia
Content-Type: application/json

{ "Image": "https://controller/media/tasks/<job_id>/task.iso",
  "Inserted": true, "WriteProtected": true, "TransferProtocolType": "URI" }

# Eject
POST /redfish/v1/Managers/iDRAC.Embedded.1/VirtualMedia/CD/Actions/VirtualMedia.EjectMedia
Content-Type: application/json

{}
```

7. Boot Override: URIs and Payloads (Reference)

- System PATCH payload (common):
  - Boot.BootSourceOverrideEnabled = Once
  - Boot.BootSourceOverrideTarget = Cd
  - Optional: Boot.BootSourceOverrideMode = UEFI
- URIs:
  - PATCH /redfish/v1/Systems/{id}
- Verify with GET after PATCH; confirm property values.

8. Reset: URIs and Payloads (Reference)

- URIs:
  - POST /redfish/v1/Systems/{id}/Actions/ComputerSystem.Reset
- Preferred ResetType:
  - "GracefulRestart"
- Fallbacks:
  - "ForceRestart"
  - "PowerCycle"

9. Vendor Notes and Quirks

These are common patterns to handle; actual behaviors vary by firmware version. The implementation must discover and adapt rather than assume.

- Dell iDRAC
  - VirtualMedia typically under Managers (e.g., iDRAC.Embedded.1).
  - InsertMedia often accepts Image + Inserted + WriteProtected (+ TransferProtocolType="URI").
  - Boot override target usually "Cd".
  - Post‑reset API availability can lag; include generous backoff.
- HPE iLO
  - VirtualMedia commonly under Managers.
  - Some generations require explicit "Inserted": true in InsertMedia payload.
  - BootSourceOverrideTarget "Cd" and Mode "UEFI" often honored.
  - Rate limiting (429) may appear; honor Retry-After.
- Lenovo XCC
  - VirtualMedia instances may appear as "CD", "CD1", "CD2"—enumerate and prefer stable ordering.
  - InsertMedia supports HTTP/HTTPS URIs.
  - Boot override "Cd" generally accepted; verify after PATCH.
- Supermicro
  - VirtualMedia under Managers; accept minimal InsertMedia payloads.
  - Basic auth often used; session service may be less consistent across versions.
  - Boot override "Cd"; allow extra retries on PATCH/Reset.
- General
  - Some BMCs keep “Inserted” state across resets; always check before inserting/ejecting.
  - Some require Manager reset to apply VirtualMedia changes; avoid unless strictly necessary.

10. Security Considerations

- Use HTTPS to BMC when supported; allow a deployment control for cert validation policy.
- Never embed credentials in ISO URLs; task.iso is public only to the BMC via signed URLs.
- If signed URLs are used for task.iso, generate short expirations and verify signature server‑side.
- Redact secrets in logs; log only endpoints, timings, and status codes.

11. Observability and Events

- Log structure per operation:
  - op: discover | mount.maintenance | mount.task | boot-override | reset | cleanup.unmount | cleanup.reset
  - target: URIs and resource ids (no secrets)
  - attempt, elapsed_ms, status_code
- Emit job_events for:
  - Discovery results (selected System/Manager/VirtualMedia IDs)
  - Insert success (per drive) + effective image URL
  - Boot override results (pre/post state)
  - Reset issued + reboot detection timing
  - Cleanup (eject both drives) and final reset outcome
- Metrics:
  - redfish_request_duration_seconds{op,vendor}
  - redfish_requests_total{op,code,vendor}
  - redfish_retries_total{op,vendor}
  - provisioning_phase_duration_seconds{phase}

12. Testing Strategy

- Mock Redfish servers per vendor profile:
  - Happy paths for Insert/PATCH/Reset
  - 5xx/timeout injection to test retries and backoff
  - 401 to validate session refresh logic
  - 429 with Retry‑After
- Contract tests:
  - Ensure discovery adapts to VirtualMedia under Managers or Systems
  - Verify idempotency logic when media already inserted
- ESXi path:
  - Simulate power state transitions and API downtime; ensure cleanup triggers after stable reboot
- Recovery:
  - Restart controller mid‑provisioning; verify job reconciliation re‑discovers state and continues safely.

13. Acceptance Criteria

- The controller can:
  - Discover System, Manager, and VirtualMedia without hardcoded URIs.
  - Insert maintenance.iso and task.iso (or skip when already inserted and identical).
  - Set one‑time boot to CD and verify properties post‑PATCH.
  - Reset host and detect BMC/API availability after reboot.
  - Eject media and perform final reset during cleanup.
- Idempotency:
  - Re-running any step causes no harm; duplicates are safely detected and skipped or overwritten as designed.
- Resilience:
  - Transient failures are retried with backoff; terminal errors are reported with clear failed_step codes.
- Compatibility:
  - Works against representative mocks for iDRAC, iLO, XCC, and Supermicro models.

Appendix A: Example Step Sequence (Pseudo)

```/dev/null/sequence.txt#L1-60
# 1. Discovery
sys = pick_system(GET /redfish/v1/Systems)
mgr = pick_manager(sys or ServiceRoot)
vms = list_virtual_media(mgr or sys)
cd1, cd2 = select_cd_slots(vms)

# 2. Mount media
insert_media(cd1, maintenance_iso_url)
insert_media(cd2, task_iso_url)

# 3. Boot override
set_boot_once_cd(sys)

# 4. Reset
reset_system(sys, preferred="GracefulRestart", fallbacks=["ForceRestart","PowerCycle"])

# 5. Poll until BMC/API up
wait_until_service_root_ok()
wait_until_system_ok(sys)

# 6. Later (on webhook or ESXi polling) — cleanup
eject_media(cd1)
eject_media(cd2)
clear_boot_override_if_needed(sys)
reset_system(sys, preferred="GracefulRestart")
```

Appendix B: Example Insert/Eject Endpoints

```/dev/null/endpoints.http#L1-40
# Insert
POST /redfish/v1/Managers/{mgrId}/VirtualMedia/{cdId}/Actions/VirtualMedia.InsertMedia
Content-Type: application/json

{ "Image": "https://controller/media/bootc-maintenance.iso",
  "Inserted": true,
  "WriteProtected": true,
  "TransferProtocolType": "URI" }

# Eject
POST /redfish/v1/Managers/{mgrId}/VirtualMedia/{cdId}/Actions/VirtualMedia.EjectMedia
Content-Type: application/json

{}
```

## Phase 6 Hardening Additions (2025-11-08)

- Standardized retry/backoff with jitter and Prometheus metrics for Redfish calls.
- Idempotent virtual media insert/eject and one-time boot override semantics.
- Vendor quirks registry (boot target mapping, slot preferences, optional timing hints).
- Restart reconciliation helper to reassert media/boot state when desired.
- New unit tests covering transient failures, vendor profiles, and reconciliation.

See also: `docs/redfish_hardening.md`.
