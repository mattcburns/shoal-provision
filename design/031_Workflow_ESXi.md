# 031: Workflow — VMware ESXi Install

Status: Proposed
Owners: Provisioning Working Group
Last updated: 2025-11-05

Summary

This document defines the end-to-end ESXi provisioning workflow using the dual‑ISO approach controlled by Redfish. Unlike Linux/Windows workflows, the maintenance OS does not run on the target host for ESXi. Instead, the controller mounts the official VMware ESXi installer ISO (CD1) and a small, dynamic task.iso (CD2) that contains a Kickstart file (ks.cfg). The ESXi installer performs a fully unattended installation, reboots, and the controller detects completion by polling the BMC. The controller then unmounts media and issues a final reboot to boot into the newly installed ESXi system.

Related

- 020_Provisioner_Architecture.md (system overview)
- 021_Provisioner_Controller_Service.md (APIs, state machine, storage)
- 022_Recipe_Schema_and_Validation.md (recipe contract; ks_cfg field)
- 023_Task_ISO_Builder.md (task.iso with /ks.cfg at root)
- 027_Embedded_OCI_Registry.md (optional artifact hosting; not required for ESXi flow)
- 028_Redfish_Operations.md (virtual media, boot override, reset, polling)
- 032_Error_Handling_and_Webhooks.md (error taxonomy; no webhooks in this flow)

1. Overview and Goals

- Provide a fully unattended ESXi installation using:
  - CD1: Official VMware ESXi Installer ISO
  - CD2: task.iso containing a Kickstart file at /ks.cfg
- Use Redfish only (Layer 3) for all control (mount, boot, reboot, unmount).
- Avoid modifying the vendor ISO or runtime boot options interactively.
- Detect installation completion by observing BMC/API availability and power state transitions; no in-guest webhook is used.
- Ensure idempotency, robust failure attribution, and clear observability.

Non-Goals

- Customizing or rebuilding the vendor ESXi ISO.
- Injecting kernel boot parameters dynamically (assume installer auto-discovers ks.cfg on CD2 per validated versions).
- Managing ESXi post-install configuration beyond what Kickstart performs.

2. Inputs and Outputs

2.1 Inputs (from POST /api/v1/jobs recipe)

- task_target: "install-esxi.target" (used for routing; maintenance OS not used)
- ks_cfg: string (required; Kickstart contents)
- server_serial: string

2.2 Controller configuration

- ESXI_INSTALLER_URL: URL to the vendor ESXi installer ISO (CD1)
  - Example: https://controller.internal:8080/static/VMware-VMvisor-Installer-8.0U2.iso
- TASK_ISO_DIR: storage for generated task ISOs

2.3 Outputs

- Target host boots the ESXi installer and consumes /ks.cfg from CD2.
- ESXi installs to the first selected disk and reboots (per Kickstart).
- Controller detects reboot completion, unmounts both media, and reboots host to boot from installed ESXi.

3. Task ISO and Kickstart

3.1 task.iso contents

- /ks.cfg (Kickstart file; root of ISO required by this workflow)
- Optional: /recipe.json and /recipe.schema.json may also be included for auditing, but are not used by the ESXi installer.

3.2 Determinism

- task.iso is built deterministically (see 023_Task_ISO_Builder.md) to simplify caching and integrity.

3.3 Kickstart expectations

- The ESXi installer locates /ks.cfg on the second CD automatically (validated for target ESXi versions).
- If a platform requires explicit kernel args (e.g., ks=cdrom:/KS.CFG), that is out of scope for this baseline and must be handled as a vendor compatibility variant (see §10 Open Questions).

4. Controller Orchestration Flow

1) Validate recipe
- Ensure ks_cfg present and within size limits (see 022).
- Persist job with status=queued → provisioning.

2) Build task.iso
- Place ks.cfg at ISO root.
- Store iso path as jobs.task_iso_path.

3) Redfish mounting and boot
- Mount ESXI_INSTALLER_URL as CD1 (maintenance ISO not used).
- Mount task.iso URL as CD2.
- Set one‑time boot to CD.
- Reset system (GracefulRestart with fallbacks).

4) Detect installer progress/reboot (no webhook)
- Poll BMC service root and system resource (see 028):
  - Expect transient unavailability and power state transitions.
  - Wait for a stable “On” state after the installer reboots the host.

5) Cleanup and finalize
- Eject virtual media (both CD1 and CD2).
- Reset system again (optional, recommended) to ensure boot from installed ESXi.
- Mark job complete.

5. Reboot Detection Strategy

- Since the ESXi workflow has no maintenance OS/webhook, the controller uses polling:
  - Poll /redfish/v1/ for availability after the initial reset.
  - Monitor power state transitions (On → cycling → On).
  - After a stable “On” period (e.g., 90 seconds with no transitional power state or API flapping), assume installation completed and proceed to cleanup.
- Time budgets:
  - Installation window: configurable (e.g., up to 90 minutes).
  - Poll interval with backoff and jitter: start at ~1s, back off to ~10–15s.

6. Idempotency and Recovery

- Mount operations are idempotent: re‑insert same image is safe; eject already‑ejected is safe.
- Boot override to CD can be re-applied if needed.
- Reboot/reset can be retried with backoff.
- Controller restarts:
  - On startup, reconcile provisioning jobs: detect mounted media and continue polling if within time budgets; otherwise fail with redfish.poll timeout.

7. Error Handling and Attribution

- Controller-centric step keys (see 032):
  - iso.build (task.iso build failure)
  - redfish.mount.maintenance (here: vendor installer as CD1)
  - redfish.mount.task (task.iso with ks.cfg as CD2)
  - redfish.boot-override
  - redfish.reset
  - redfish.poll (timeout while waiting for post-install reboot/API readiness)
  - redfish.cleanup.unmount
  - redfish.cleanup.reset
- On terminal error, transition job to failed with failed_step set to the step key. Then best-effort cleanup (eject media), and mark complete or leave failed depending on policy (recommended: failed → cleanup → complete).

8. Observability

- Job events:
  - ISO build path, size, hash
  - Selected VirtualMedia instances for CD1/CD2
  - URLs used (without secrets)
  - Reset and polling milestones (API up/down, power state transitions)
  - Cleanup results
- Metrics:
  - redfish_request_duration_seconds{op,vendor}
  - provisioning_phase_duration_seconds{phase=install,detect,cleanup}
  - iso_build_duration_seconds
- Logging:
  - Structured logs with job_id, server_serial, op, attempt, duration_ms; no secrets.

9. Security Considerations

- Serve vendor ISO and task.iso over HTTPS with valid controller CA; or restrict by network policy if HTTP is used in trusted, isolated networks.
- No credentials inside ks.cfg; if secrets are required (e.g., license keys), do not log contents—log size and a non-reversible hash only.
- Enforce short-lived signed URLs for task.iso if possible.
- Do not store BMC credentials in logs. Redfish sessions preferred over basic auth.

10. Compatibility Notes and Open Questions

- ESXi versions: validate automatic /ks.cfg discovery path for supported versions (e.g., 7.x, 8.x). If a version requires kernel arguments, consider a vendor-profile feature to add boot overrides (may require ISO remastering or unsupported boot param injection via virtual console).
- Disk selection: Kickstart should specify disk selection (e.g., --firstdisk) and overwrite behavior to ensure predictability.
- Post-install customization: Use Kickstart to set management network, passwords, NTP, license (as allowed by policy).
- Secure Boot: out of scope here.

11. Testing Strategy

11.1 Unit (controller)
- ISO builder creates a task.iso with /ks.cfg at root; deterministic hash.
- Redfish mock: InsertMedia (CD1/CD2), boot override, reset, polling with simulated unavailability and power transitions.
- Cleanup robustness: eject failures are non-fatal post-finalization.

11.2 Integration (lab/VM)
- Mount official ESXi installer ISO as CD1 and generated task.iso as CD2.
- Verify installer consumes ks.cfg (unattended install proceeds).
- Observe host reboot; controller polling detects completion; media unmounted; final reboot issued.

11.3 Negative cases
- ks.cfg missing/invalid → installer stops; controller times out on redfish.poll and reports failure.
- Redfish unmount failure → controller logs cleanup issue; job still completes with warning.

12. Acceptance Criteria

- Given a valid recipe with ks_cfg, the controller:
  - Builds task.iso with /ks.cfg
  - Mounts vendor ISO (CD1) and task.iso (CD2)
  - Sets one‑time boot to CD and resets host
  - Detects post-install reboot via polling within the configured window
  - Ejects media and performs a final reboot
- Failure cases:
  - Network/Redfish errors are retried with backoff; terminal errors set failed_step appropriately.
  - Installer never completes → redfish.poll timeout and failed outcome.
- Observability:
  - Events and metrics cover each step; logs contain correlation fields and no secrets.

13. Example Kickstart (ks.cfg)

```/dev/null/ks.cfg#L1-80
# VMware ESXi Kickstart Example (adjust to your environment and policy)
vmaccepteula

# Set root password (example uses locked; replace per policy)
# rootpw --iscrypted <hash>
rootpw myS3cur3Pwd!

# Install to the first available disk and overwrite VMFS if present
install --firstdisk --overwritevmfs

# Use DHCP for management network on default NIC
network --bootproto=dhcp

# Set hostname (optional)
# Note: If DHCP provides hostname, this can be omitted.
%post --interpreter=busybox
/usr/bin/esxcli system hostname set --host=esxi-host
%end

# Optional timezone
# keyboard US-default; timezone UTC

# Enable SSH and ESXi Shell (optional; follow your security policy)
%firstboot --interpreter=busybox
vim-cmd hostsvc/enable_ssh
vim-cmd hostsvc/start_ssh
vim-cmd hostsvc/enable_esx_shell
vim-cmd hostsvc/start_esx_shell
%end

# Reboot after installation completes
reboot
```

14. Change Log

- v0.1 (2025-11-05): Initial ESXi workflow covering dual‑ISO flow, Kickstart delivery via task.iso, reboot detection, and cleanup.
