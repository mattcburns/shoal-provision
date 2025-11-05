# 024: Maintenance OS Build with bootc

Status: Proposed
Owners: Provisioning Working Group
Last updated: 2025-11-03

Summary

This document specifies how to build the immutable maintenance OS image (bootc-maintenance.iso) that boots every target server to execute provisioning and maintenance jobs. The image is based on bootc (OSTree-native container image) and includes:
- A static Go dispatcher binary that reads recipe.json from task.iso and starts the appropriate systemd target.
- Curated systemd units and Quadlet (.container) units that orchestrate partitioning, imaging, bootloader installation, config-drive, firmware updates, and final webhooks.
- Minimal host utilities, podman, and oras. Tooling runs inside containers; the host remains small and consistent.
- Optional pre-bound tool images for offline execution; or configuration to pull artifacts from the controller’s embedded /v2/ registry at runtime.

This design covers composition, configuration, distribution of units, bound images, ISO production, security, and acceptance criteria.

Goals

- Deterministic, reproducible maintenance image builds with minimal host footprint.
- Robust boot behavior and ordering (udev settle → dispatcher → systemd targets).
- Reliable, standalone operation with no dependency on external phone-home services.
- Clear separation between host OS and tool containers for vendor-specific utilities.
- Compatibility across common server platforms (UEFI first; BIOS where necessary).

Non-Goals

- Installing every tool on the host; all tools run in containers via Quadlet.
- Providing SSH or a general-purpose rescue environment (kept minimal).
- Implementing the provisioning controller; this document focuses on the maintenance OS.

References

- 020_Provisioner_Architecture.md (index and high-level design)
- 021_Provisioner_Controller_Service.md (APIs, state machine, storage)
- 022_Recipe_Schema_and_Validation.md (authoritative recipe schema)
- 023_Task_ISO_Builder.md (deterministic task.iso generation)
- 026_Systemd_and_Quadlet_Orchestration.md (targets and service graph)
- 027_Embedded_OCI_Registry.md (optional, single-binary controller + /v2/)
- 032_Error_Handling_and_Webhooks.md (final status reporting)

1. Image Composition

1.1 Base

- Base image: Fedora bootc (e.g., quay.io/fedora/fedora-bootc:40) or compatible EL bootc image with kernel and dracut that support your target hardware set.
- Primary host content:
  - systemd (PID 1)
  - podman (with Quadlet support) and oras
  - minimal diagnostics: coreutils, util-linux, udev, jq
  - networking/dhcp tooling sufficient to reach the controller for webhooks (e.g., NetworkManager or systemd-networkd)
  - Optional: ca-certificates for TLS with the controller and embedded registry

1.2 Dispatcher

- A single static Go binary: /usr/sbin/provisioner
  - Responsibilities: mount /dev/sr1 (task.iso), validate /recipe.json against /recipe.schema.json, write /run/provision/* files, and start the systemd target declared in task_target.
  - No external dependencies at runtime.
- Service: provision-dispatcher.service (oneshot; After=local-fs.target systemd-udev-settle.service; Wants=network-online.target for webhooks in failure paths)

1.3 Systemd Units and Targets

- Targets:
  - install-linux.target
  - install-windows.target
  - install-esxi.target (if using Kickstart handoff)
  - supermicro-update.target (illustrative ad-hoc maintenance)
- Services:
  - partition.service, image-linux.service, image-windows.service, bootloader-linux.service, bootloader-windows.service, config-drive.service
  - provision-success.service and provision-failed.service (final webhooks)
- Placement: /etc/systemd/system/*.target and *.service
- Ordering:
  - Dispatcher runs once, writes /run/provision/*, then systemctl start $TASK_TARGET (from recipe)
  - Targets Require= the appropriate services; OnSuccess=provision-success.service; OnFailure=provision-failed.service

1.4 Quadlet (.container) Units

- Placement: /etc/containers/systemd/*.container
- Conventions:
  - Each .container runs a single tool or step (Type=oneshot, Remove=true)
  - EnvironmentFile=/run/provision/recipe.env for scalar inputs (TARGET_DISK, OCI_URL, etc.)
  - BindMounts: /run/provision:/run/provision:ro (and additional mounts for disks, EFI, etc.)
  - Device and privilege requirements are explicitly declared (AddDevice=/dev:/dev:rwm; AddCapability=ALL when warranted)
- Examples:
  - partition.container: wraps sgdisk/mkfs tools
  - image-linux.container: oras pull rootfs.tar.gz | tar -x to /mnt/new-root
  - bootloader-linux.container: chroot to install GRUB
  - config-drive.container: create CIDATA partition with user-data
  - image-windows.container: oras pull .wim | wimapply to /mnt/new-windows, set up EFI boot
  - vendor-firmware.container: curl firmware, run flasher

1.5 Bound Images (Offline Mode)

- bootc bound images provide logical binding between the host image and the tool images it needs, enabling pre-fetch into the final ISO:
  - Directory: /usr/lib/bootc/bound-images.d/
  - Implementation: symlinks to Quadlet .container files or helper manifests to force inclusion of referenced images during bootc build/publish steps
- Strategy:
  - If operating fully air-gapped: bind all tool images and rely on podman’s local store seeded by bootc
  - If controller’s embedded /v2/ is reachable: keep host minimal and pull tools on-demand from controller

1.6 Registry and Trust

- If using the controller’s embedded /v2/, configure registries.conf to prefer controller.internal:PORT as an insecure or TLS-verified mirror (prefer TLS with controller CA).
- Install controller CA at build time (e.g., /etc/pki/ca-trust/source/anchors/controller-ca.crt) and update CA trust.
- oras uses the same trust store; ensure parity with podman’s transport configuration.

2. Build Pipeline

2.1 Source Layout (suggested)

- cmd/provisioner/ (Go dispatcher main)
- systemd/ (targets and services)
- quadlets/ (.container units)
- Containerfile (multi-stage: build dispatcher → assemble final bootc image)
- scripts/ (build wrappers; optional)
- docs/design (this series)

2.2 Containerfile Composition (high level)

- Stage 1 (builder): golang:1.22-alpine
  - Build static dispatcher: CGO_ENABLED=0 GOOS=linux
- Stage 2 (final): fedora-bootc base
  - Install host tools: podman, oras, ca-certificates, jq, udev, NetworkManager (or systemd-networkd)
  - Copy dispatcher to /usr/sbin/provisioner
  - Copy systemd/ → /etc/systemd/system/
  - Copy quadlets/ → /etc/containers/systemd/
  - Enable provision-dispatcher.service
  - Create bound images mapping (/usr/lib/bootc/bound-images.d) if pre-binding
  - Optional: copy controller CA and update trust

2.3 Producing the ISO

- Use bootc-image-builder (or equivalent tooling) to convert the bootc image into a bootable ISO:
  - UEFI-first; enable legacy BIOS boot only if required
  - Embed kernel/initramfs generated by the base (dracut)
  - Ensure the ISO contains the full ostree commit and pre-bound containers if applicable
- Deliverable: bootc-maintenance.iso, published at a stable controller URL (e.g., https://controller/api/static/bootc-maintenance.iso)

3. Boot Behavior and Ordering

- boot sequence:
  1) Kernel → initramfs → systemd
  2) Local filesystems → udev settle
  3) provision-dispatcher.service (oneshot)
     - mount /dev/sr1 at /mnt/task
     - validate /recipe.json vs /recipe.schema.json
     - write /run/provision/recipe.env and other files
     - systemctl start $TASK_TARGET
  4) Target orchestrates services via Requires=/After=
  5) Final state:
     - Success: provision-success.service sends webhook {"status":"success"}
     - Failure: provision-failed.service sends webhook {"status":"failed","failed_step":"<unit>"}

Notes:
- network-online.target is required before success/failure services run
- dispatch failure before target start should trigger provision-failed.service (via OnFailure or wrapper)

4. Files and Directories (within the maintenance OS)

- /usr/sbin/provisioner (dispatcher)
- /etc/systemd/system/
  - provision-dispatcher.service
  - install-linux.target, install-windows.target, install-esxi.target, supermicro-update.target
  - partition.service, image-linux.service, image-windows.service, bootloader-linux.service, bootloader-windows.service, config-drive.service
  - provision-success.service, provision-failed.service
- /etc/containers/systemd/
  - partition.container, image-linux.container, image-windows.container, bootloader-linux.container, bootloader-windows.container, config-drive.container, vendor-firmware.container
- /usr/lib/bootc/bound-images.d/
  - Symlinks to container unit references to force inclusion/binding (optional; only when air-gapped/offline desired)
- /run/provision/ (runtime)
  - recipe.env (ENV key=value)
  - layout.json (partition schema)
  - user-data, unattend.xml (if present)
  - other aux files from task.iso

5. Networking and Webhooks

- DHCP or static addressing as configured by the base; ensure default route and DNS to reach controller.
- CA trust installed for HTTPS webhook endpoint (recommended).
- Webhook service:
  - Post to controller /api/v1/status-webhook/{server_serial}
  - Include secret header configured at image build (or injected at runtime from task.iso if preferred).

6. Security and Hardening

- No inbound services (disable sshd, cockpit, etc.).
- Firewall default deny inbound; allow egress to controller and artifact source (if not pre-bound).
- Run containers privileged only when required; otherwise use tightened capabilities.
- Redact secrets from logs; never log recipe contents verbatim.
- Optional: sign the bootc image and verify signatures on boot (supply-chain hardening).
- Optional: FIPS mode if organizational policy requires it (verify tool compatibility first).

7. Compatibility and Hardware Support

- UEFI support is required; add legacy BIOS support only if necessary (cost: increased surface area).
- Kernel modules for common storage/network hardware included in the base; keep host minimal and push vendor tools into containers.
- Validate that dracut includes required drivers for target platforms.

8. Observability and Troubleshooting

- journald logs available via local console; persistent logs are optional (prefer RAM-based to avoid disk writes).
- Provide a "collect logs" container/unit that can:
  - Archive /var/log/journal and relevant /run/provision/* into task.iso mount path or POST to controller for debugging.
- Emit clear systemd unit descriptions and standardized log prefixes:
  - [provisioner], [partition], [imager], [bootloader], [config-drive], [firmware], [webhook]

9. Performance Considerations

- Keep host image small; avoid heavy tools on host OS.
- Use pre-bound images for air-gapped or slow networks to reduce total provisioning time.
- For very large artifacts (e.g., Windows WIM), ensure oras buffering and container memory limits are adequate.
- Consider tmpfs mounts for intermediate extraction to reduce device I/O overhead, if memory allows.

10. Build/Release Process (recommended)

- CI stages:
  1) Build dispatcher (unit tests and static binary)
  2) Build bootc image (copy dispatcher, units, Quadlets; enable services)
  3) Optionally resolve/bind tool containers (air-gapped mode)
  4) Produce bootc-maintenance.iso via image builder
  5) Publish artifacts: bootc-maintenance.iso + SBOM/signature (optional)
- Versioning:
  - Tag bootc image and ISO with semantic versions matching controller release
  - Record unit versions and dispatcher version in /etc/os-release or a dedicated build info file

11. Acceptance Criteria

- Boots reliably on supported hardware into systemd with dispatcher started automatically.
- Dispatcher:
  - Mounts /dev/sr1, validates recipe.json against recipe.schema.json, writes /run/provision/*, and starts the requested systemd target.
  - On validation failure, triggers the failure path (provision-failed.service).
- Quadlet tool units:
  - Can run in privileged mode when necessary and succeed on representative hardware (disk ops, EFI ops, firmware ops).
- Success/Failure webhooks:
  - Send correct payloads to controller, and retry on transient network errors.
- Offline mode:
  - When bound images are enabled, provisioning completes without pulling images from the network.
- Security:
  - No inbound services listening by default; only egress to controller/registry is necessary.
- Observability:
  - Clear logs for each step; failure attribution includes the failing systemd unit name.

12. Test Plan

- Unit tests (dispatcher):
  - Good/invalid recipe validation
  - /run/provision outputs and target start invocation
- Integration tests (VM-based):
  - Mount two ISOs (maintenance + task), confirm full Linux workflow completes and success webhook observed
  - Negative path: partition failure → provision-failed.service webhook and accurate failed_step
- Air-gapped test:
  - Disable network egress; verify pre-bound images path succeeds
- Cross-vendor smoke:
  - Boot on representative platforms (e.g., iDRAC, iLO, XCC, Supermicro) and verify virtual media mount behavior and network readiness for webhook
- Size and performance:
  - Host image size within target bounds; cold boot to dispatcher < 30s typical on modern hardware

Change Log

- v0.1 (2025-11-03): Initial maintenance OS build and unit distribution design.