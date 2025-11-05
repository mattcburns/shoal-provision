# 036: Release and Deployment

Status: Proposed
Owners: Provisioning Working Group
Last updated: 2025-11-05

Summary

This document defines how to package, release, and deploy the Provisioner Controller and the maintenance OS ISO. It covers deliverables, filesystem layout, configuration, systemd units, reverse proxy and ports, upgrades and rollbacks, database migrations and backups, blue/green and canary strategies, certificate and secret rotation, post-deploy smoke tests, and acceptance criteria.

Scope

- In scope:
  - Packaging and release artifacts (controller binary, maintenance ISO, tool images)
  - Controller deployment (single-node) with optional embedded /v2/ registry
  - Configuration via environment file, systemd unit templates, directory layout
  - Upgrade/rollback procedures including DB migrations and backups
  - Operational hardening (TLS, secrets, retention/GC, metrics/logs)
- Out of scope:
  - Multi-instance horizontal scaling (LB/HA) beyond a brief note
  - Vendor-specific OS tuning of the host system
  - Organization-specific CI/CD mechanics (covered in 034)

1) Deliverables and Versioning

- Controller binary:
  - Single statically linked Go binary (linux/amd64, linux/arm64)
  - Comes with example systemd unit and example env file
  - Checksums and signatures published with the release
- Maintenance OS:
  - bootc-maintenance.iso (bootable ISO)
  - bootc image (optional) for sites that rebuild the ISO locally
- Tool images:
  - Container images for partitioning, imaging, bootloader, config-drive, etc.
- Versioning:
  - Semantic versioning MAJOR.MINOR.PATCH
  - Tags: vX.Y.Z, major.minor, latest (see 034)
  - Controller and maintenance ISO should be compatible within the same minor version line

2) Filesystem Layout (recommended)

- Binary and config:
  - /usr/local/bin/shoal-controller
  - /etc/shoal/shoal.env
- Data and working directories (owner: shoal:shoal, mode 0750):
  - /var/lib/shoal/
    - db/               (SQLite database and migrations)
    - tasks/            (task.iso files per job_id)
    - oci/              (embedded registry storage, optional)
    - static/isos/      (maintenance.iso and vendor ISOs)
- Logs (journalctl preferred; optional file sink if desired):
  - /var/log/shoal/ (owner: shoal:shoal, mode 0750)
- Systemd:
  - /etc/systemd/system/shoal.service

3) Configuration

- The controller supports environment variables and flags. For operational simplicity, use an EnvironmentFile with key=value pairs:
  - CONTROLLER_HTTP_ADDR=:8080
  - TLS_CERT_FILE=/etc/shoal/tls/cert.pem
  - TLS_KEY_FILE=/etc/shoal/tls/key.pem
  - AUTH_MODE=basic
  - BASIC_USER=admin
  - BASIC_PASS_HASH=$2y$...
  - DB_PATH=/var/lib/shoal/db/provisioner.db
  - STORAGE_ROOT=/var/lib/shoal
  - TASK_ISO_DIR=/var/lib/shoal/tasks
  - STATIC_DIR=/var/lib/shoal/static
  - MAINTENANCE_ISO_URL=https://controller.example.org/static/isos/bootc-maintenance.iso
  - ENABLE_REGISTRY=true
  - REGISTRY_STORAGE=/var/lib/shoal/oci
  - REGISTRY_AUTH_MODE=basic
  - REGISTRY_USERS_FILE=/etc/shoal/registry-users.htpasswd
  - WORKER_CONCURRENCY=4
  - REDFISH_TIMEOUT=30s
  - REDFISH_RETRIES=5
  - JOB_LEASE_TTL=10m
  - JOB_STUCK_TIMEOUT=4h
  - WEBHOOK_SECRET=replace-with-long-random
  - SIGNED_URL_SECRET=replace-with-long-random
  - JOB_RETENTION_DAYS=14
  - LOG_LEVEL=info

Example /etc/shoal/shoal.env

    CONTROLLER_HTTP_ADDR=:8080
    DB_PATH=/var/lib/shoal/db/provisioner.db
    STORAGE_ROOT=/var/lib/shoal
    TASK_ISO_DIR=/var/lib/shoal/tasks
    STATIC_DIR=/var/lib/shoal/static
    MAINTENANCE_ISO_URL=https://controller.example.org/static/isos/bootc-maintenance.iso
    ENABLE_REGISTRY=true
    REGISTRY_STORAGE=/var/lib/shoal/oci
    AUTH_MODE=basic
    BASIC_USER=admin
    BASIC_PASS_HASH=$2y$12$REDACTED_HASH
    REGISTRY_AUTH_MODE=basic
    REGISTRY_USERS_FILE=/etc/shoal/registry-users.htpasswd
    WEBHOOK_SECRET=REDACTED_LONG_RANDOM
    SIGNED_URL_SECRET=REDACTED_LONG_RANDOM
    WORKER_CONCURRENCY=4
    REDFISH_TIMEOUT=30s
    REDFISH_RETRIES=5
    JOB_LEASE_TTL=10m
    JOB_STUCK_TIMEOUT=4h
    JOB_RETENTION_DAYS=14
    LOG_LEVEL=info

Notes
- If TLS is terminated by a reverse proxy, omit TLS_CERT_FILE/TLS_KEY_FILE and bind to localhost.
- BASIC_PASS_HASH is a hash (argon2id/bcrypt), not plaintext. See 033 for password policy.
- SIGNED_URL_SECRET secures /media/tasks signed URLs; rotate periodically (033).

4) Systemd Service

Create a dedicated user and directories:

    useradd --system --home /var/lib/shoal --shell /sbin/nologin shoal
    install -o shoal -g shoal -m 0750 -d /var/lib/shoal/{db,tasks,oci,static/isos}
    install -o shoal -g shoal -m 0750 -d /var/log/shoal
    chown -R shoal:shoal /etc/shoal

Example /etc/systemd/system/shoal.service

    [Unit]
    Description=Shoal Provisioner Controller
    After=network-online.target
    Wants=network-online.target

    [Service]
    User=shoal
    Group=shoal
    EnvironmentFile=/etc/shoal/shoal.env
    WorkingDirectory=/var/lib/shoal
    ExecStart=/usr/local/bin/shoal-controller
    Restart=always
    RestartSec=3
    # Hardenings (tune if binding low ports or using TLS keys)
    NoNewPrivileges=true
    PrivateTmp=true
    ProtectSystem=full
    ProtectHome=true
    ReadWritePaths=/var/lib/shoal /var/log/shoal /etc/shoal
    # Increase limits for large uploads if embedded registry is enabled
    LimitNOFILE=65535

    [Install]
    WantedBy=multi-user.target

Enable and start:

    systemctl daemon-reload
    systemctl enable --now shoal.service
    systemctl status shoal.service

5) Reverse Proxy and Ports

- Default controller listen: :8080
- If serving HTTPS directly, expose 443 and set TLS_CERT_FILE/TLS_KEY_FILE
- If using a reverse proxy (recommended for TLS and buffering):
  - Terminate TLS at proxy
  - Proxy /api/ and /media/ and /v2/ to controller
  - Enable large body uploads and streaming for /v2/ (oras and podman)
  - Set appropriate timeouts for long uploads/downloads

Example nginx (abridged)

    upstream shoal {
      server 127.0.0.1:8080;
    }

    server {
      listen 443 ssl http2;
      server_name controller.example.org;

      # ssl_certificate ...; ssl_certificate_key ...;

      client_max_body_size 0;  # allow large artifact uploads
      proxy_read_timeout 3600s;
      proxy_send_timeout 3600s;

      location / {
        proxy_pass http://shoal;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto https;
        proxy_http_version 1.1;
      }
    }

6) Static Files (Maintenance ISO and Vendor ISOs)

- Place maintenance ISO and any vendor ISOs under STATIC_DIR:
  - /var/lib/shoal/static/isos/bootc-maintenance.iso
- Configure MAINTENANCE_ISO_URL to point to a reachable HTTPS URL:
  - https://controller.example.org/static/isos/bootc-maintenance.iso
- The controller can expose /static/ if implemented, or serve via the reverse proxy/web server directly.

7) Database and Migrations

- SQLite DB path: /var/lib/shoal/db/provisioner.db
- Backups:
  - Quiesce or snapshot-friendly backup during low activity windows
  - For hot backup, use sqlite3 .backup or filesystem snapshot (LVM/ZFS)
- Migrations:
  - Forward-only migrations run on startup
  - Before upgrades, take a DB backup; downgrade typically requires restore (no down migrations)
- Integrity and vacuum:
  - Periodic VACUUM and integrity check in maintenance windows

8) Upgrades

General policy
- Back up DB and /etc/shoal/shoal.env
- Drain new work or allow zero-downtime restarts
- Ensure maintenance ISO and tool images are compatible with controller minor version

In-place restart (single-node)
1. Drain (optional but recommended):
   - Set an env flag to stop picking new jobs (if supported) and/or set WORKER_CONCURRENCY=0, reload unit
   - Wait for provisioning jobs to finish or reach a safe point
2. Stop service:
   - systemctl stop shoal
3. Replace binary:
   - install -m 0755 shoal-controller /usr/local/bin/shoal-controller
   - verify checksum/signature against release assets
4. Start service:
   - systemctl start shoal
   - monitor logs for migration success and readiness
5. Re-enable workers:
   - Restore WORKER_CONCURRENCY; systemctl reload shoal (or restart)

Blue/Green (behind a reverse proxy/LB)
- Run two controller instances (A and B) on different ports or hosts pointing to the same DB and storage (use with caution)
- Shift traffic from A to B after B passes health checks
- Ensure only one instance performs job picking at a time (feature flag or leader election needed to avoid contention); otherwise, prefer single instance

Canary
- Upgrade a staging environment with representative artifacts and BMC simulators
- Run a small set of real jobs before promoting to production

9) Rollbacks

- Keep previous controller binary available
- If a new binary applied DB migrations, rollback requires restoring the DB backup taken pre-upgrade
- Procedure:
  1) Stop service
  2) Restore DB and configuration backup
  3) Replace binary with previous version
  4) Start service; verify readiness and jobs status

10) Certificate and Secret Rotation

- TLS certs:
  - If terminating TLS in controller: replace TLS_CERT_FILE/TLS_KEY_FILE; systemctl reload shoal (if supported) or restart
  - If proxy-terminated: rotate at proxy; no controller change required
- WEBHOOK_SECRET:
  - Rotate with a dual-accept period: controller accepts old and new; maintenance OS adopts new secret on next ISO rollout
- SIGNED_URL_SECRET:
  - Rotate similarly; old URLs will fail post-rotation; schedule rotations outside active jobs window
- BASIC/REGISTRY users:
  - Update htpasswd or password hashes; restart/reload controller

11) Retention, GC, and Disk Management

- Task ISOs:
  - JOB_RETENTION_DAYS controls deletion of per-job task.iso after completion
  - Periodic GC removes old files; ensure sufficient disk for concurrent jobs
- Registry storage:
  - Enable GC for unreferenced blobs; retain the last N versions per policy (see 027, 034)
- Logs:
  - Use journald rotation, or logrotate if writing to files
- Monitoring disk usage thresholds; alert before exhaustion

12) Health Checks and Smoke Tests

After (re)deploy:
- HTTP ping:
  - GET /api/v1/jobs/nonexistent → expect 404 with JSON; confirms API reachable
- Registry ping (if enabled):
  - GET /v2/ → 200
- Static ISO:
  - HEAD MAINTENANCE_ISO_URL via proxy; check Content-Length
- Media server signed URL:
  - Generate a short-lived URL (admin tool or API) and GET; expect 200 and ETag
- Synthetic job (using a mock Redfish target):
  - POST /api/v1/jobs with a tiny Linux recipe; verify job transitions queued → provisioning → succeeded → complete
- Logs:
  - journalctl -u shoal --since -10m for errors
- Ports:
  - Verify listening on expected address; reverse proxy upstream passes long requests

13) Capacity Planning

- CPU:
  - 2–4 vCPU sufficient for low concurrency; higher when embedded registry handles large artifacts
- Memory:
  - 2–8 GiB typical; increase for higher concurrency and large artifact buffering by clients
- Disk:
  - REGISTRY_STORAGE sized for maximum artifact set (consider multi-10s to 100s of GiB)
  - TASK_ISO_DIR small (MiBs per job) but consider burst concurrency
  - Fast disk recommended for registry
- Network:
  - Ensure sufficient egress/ingress for large artifact pushes/pulls; tune proxy and kernel socket buffers

14) Observability and Troubleshooting

- Logs:
  - journalctl -u shoal; increase LOG_LEVEL=debug temporarily to triage
- Metrics:
  - Expose and scrape metrics (if implemented); track job durations, Redfish latencies, registry throughput
- Common issues:
  - 401 on webhook: secret mismatch; rotate or correct WEBHOOK_SECRET
  - Redfish timeouts: BMC reachability; vendor quirks; adjust REDFISH_TIMEOUT/RETRIES
  - Disk full: task.iso or registry storage saturated; run GC and increase capacity
  - Permission errors: ensure shoal user owns /var/lib/shoal and /etc/shoal
  - TLS errors: CA trust in maintenance OS; install controller CA into maintenance image (024, 033)

15) Security Posture (ops)

- Enforce TLS for clients and webhook
- Restrict inbound access to controller and registry (firewall, security groups)
- Run as non-root; least-privilege file permissions
- Regularly rotate secrets and update to patched releases
- Keep maintenance OS image and tool containers updated; rebuild ISO after updates

16) Acceptance Criteria

- Packaging:
  - Release includes controller binaries (amd64/arm64), checksums/signatures, example systemd unit, example env file, and maintenance ISO
- Deployment:
  - A fresh host can be configured following this doc to start the controller and serve maintenance ISO
- Functionality:
  - API, media, and (if enabled) /v2/ registry respond correctly
  - A synthetic job against a mock Redfish completes end-to-end
- Operations:
  - Upgrade and rollback procedures validated in staging
  - DB backup/restore procedure documented and tested
  - Secret and certificate rotation steps verified
- Hygiene:
  - GC and retention jobs operate and free disk as configured
  - Logs contain no secrets; security headers and auth enforced

Appendix A: Minimal Operator Checklist

- Pre-flight
  - Size disks; open firewall to controller from BMC mgmt network and maintenance OS network
  - Obtain TLS certs, set up reverse proxy (optional)
  - Prepare shoal.env with strong secrets and correct paths
- Install
  - Create shoal user; directories with correct ownership
  - Install controller binary and systemd unit
  - Place maintenance ISO under STATIC_DIR and verify URL
- Start
  - Enable and start service; verify logs and endpoints
  - Push tool images to embedded/external registry as needed
- Test
  - Run synthetic job; verify success and cleanup
- Operate
  - Monitor metrics/logs; schedule regular backups and GC
  - Plan periodic upgrades; test rollback in staging

Appendix B: Sample Commands

- Verify controller is listening:

    ss -ltnp | grep shoal

- Tail logs:

    journalctl -u shoal -f

- Validate env file syntax (simple):

    env -i bash -c 'set -a; source /etc/shoal/shoal.env; set +a; echo OK'

- Backup DB (offline):

    systemctl stop shoal
    sqlite3 /var/lib/shoal/db/provisioner.db ".backup '/var/lib/shoal/db/provisioner-$(date +%F).db.bak'"
    systemctl start shoal