# 033: Security Model

Status: Proposed
Owners: Provisioning Working Group
Last updated: 2025-11-05

Summary

This document defines the end-to-end security model for the Layer 3 bare‑metal provisioner. It establishes security objectives, trust boundaries, authentication/authorization strategies, transport security (TLS), secrets and key management, signed URL access control for task ISOs, hardening for the controller and maintenance OS, supply chain assurances, a practical threat model, and acceptance criteria.

Scope

- In scope:
  - Controller service security (HTTP API, webhook endpoint, embedded registry, media server)
  - Transport security (HTTPS to and from controller; Redfish to BMCs)
  - Authentication/authorization for APIs and registry
  - Secrets handling (BMC creds, webhook secret, registry creds)
  - Signed URLs for task.iso
  - Maintenance OS hardening and container privilege boundaries
  - Supply-chain integrity (images and artifacts)
  - Threat model and mitigations
- Out of scope:
  - Full enterprise IAM integration (SSO/OIDC/LDAP) beyond a placeholder
  - Hardware attestation or TPM-based boot attestation
  - Deep DLP or PII/GDPR policies (no personal data is expected in normal operation)

Security Objectives

- Confidentiality: Protect credentials and sensitive config (unattend.xml, user-data) in transit and at rest; minimize exposure time and blast radius.
- Integrity: Prevent tampering of artifacts and instructions (task.iso, recipes, registry content). Ensure idempotent, verifiable provisioning flows.
- Authenticity: Ensure only authorized principals can:
  - Start jobs (user API auth)
  - Report job outcomes (webhook auth)
  - Pull/push registry artifacts (registry auth)
  - Mount task.iso (signed URL validation)
- Availability: Maintain resilient controller service and limit DoS amplification, with bounded retries and backoff.
- Auditability: Provide sufficient logs and audit trails without leaking secrets.

System Overview and Trust Boundaries

- Controller service:
  - Exposes:
    - /api/v1/jobs (user API)
    - /api/v1/jobs/{id} (status)
    - /api/v1/status-webhook/{serial} (internal webhook)
    - /media/tasks/{job_id}/task.iso (media)
    - /v2/... (optional embedded registry)
  - Stores:
    - SQLite DB with jobs, events, servers (BMC mapping/creds)
    - Task ISO files (short-lived)
    - Registry storage (OCI layout on disk)
- BMCs (out-of-band):
  - Redfish endpoints reachable over IP; optionally HTTPS with self-signed certs
  - Trust boundary: external device; treat as untrusted network peer
- Maintenance OS (on target):
  - No inbound services; only egress to controller for webhook
  - Trust boundary: runs privileged tooling in containers; content from controller’s task.iso only
- Clients/Users:
  - Trusted only after authentication/authorization at controller
- CI/Artifact publishers:
  - Push to registry; require authentication

Assets to Protect

- BMC credentials (username/password, tokens)
- Webhook shared secret or client certs
- API user credentials or tokens
- Task ISO contents: recipe.json, unattend.xml, user-data, ks.cfg
- Registry artifacts: tool containers, rootfs tarballs, WIMs
- Controller private TLS keys
- Logs (which may contain metadata, but never secrets)

Authentication and Authorization

User API (/api/v1)
- Modes: Basic (default), JWT/OIDC (future), mTLS (optional)
- Configuration:
  - AUTH_MODE=basic|jwt|mtls
  - For Basic: bcrypt/argon2id hashed passwords in DB or file
  - For JWT: verify issuer/audience/signing keys (future)
  - For mTLS: verify client cert signed by configured CA (future)
- Authorization:
  - Minimal roles: admin (create jobs), reader (GET job status)
  - Later: per-server allowlists by serial or tags

Webhook (/api/v1/status-webhook/{server_serial})
- Use a shared secret header or mTLS client certificate
  - Header: X-Webhook-Secret
  - Secret injected into maintenance OS build, or provided in recipe.env (prefer image-baked or secure source)
- Requests without valid secret/cert → 401/403
- Idempotency: duplicate deliveries accepted (200 OK) without double transitions

Embedded OCI Registry (/v2)
- Modes:
  - Basic auth: REGISTRY_AUTH_MODE=basic
  - None (development/testing only)
- Push/pull policy:
  - Authenticated users can pull/push
  - Optional repository allowlists for push (e.g., tools/*, os-images/*)
  - Optional immutable tags toggle (first-writer wins)
- Password storage: bcrypt/argon2id hashes in a users file or DB

Task ISO Media (/media/tasks/{job_id}/task.iso)
- Access: signed URLs (recommended), short expiry (e.g., 10–30 minutes)
- Optional IP-binding (BMC mgmt IP) if stable; trade-off with NATs and proxies
- Without signed URLs, restrict via Basic auth or IP allowlists on the media path (defense-in-depth)

Redfish (Controller → BMC)
- Prefer Redfish sessions (X-Auth-Token) to repeated Basic auth
- Store BMC credentials securely; avoid logs; rotate when possible

Transport Security (TLS)

Controller endpoints
- Run HTTPS by default on a configurable port with provided certificate and key
  - TLS_CERT_FILE=/etc/shoal/tls/cert.pem
  - TLS_KEY_FILE=/etc/shoal/tls/key.pem
- Strong cipher suites and TLS1.2+ minimum
- HSTS and secure cookies (if any UI/API cookies are introduced later)
- Maintenance OS must trust the controller’s CA; bake CA into maintenance image or distribute via recipe if appropriate

BMC Redfish
- Prefer HTTPS; allow policy-controlled insecure mode for legacy BMCs
  - REDFISH_INSECURE=false|true (true only in tightly controlled networks)
- If a BMC uses a self-signed cert:
  - Option 1: trust on first use (record fingerprint in servers table; alert on change)
  - Option 2: provision BMC CA fingerprints/config out-of-band

Maintenance OS Egress
- TLS to controller for webhook (and to embedded registry if used)
- Bake controller CA into maintenance OS trust store during build for validation

Secrets and Key Management

Secrets Inventory
- BMC credentials per server
- Webhook secret (shared across fleet or per-cluster)
- API user passwords or tokens
- Registry auth credentials
- TLS private keys (controller)

Storage and Rotation
- Database:
  - Encrypt at rest via OS/disk encryption where possible
  - Optional: replace plaintext with references to external secret stores (future)
- Filesystem:
  - Strict permissions (owner-only), never commit secrets to VCS
- Rotation:
  - Webhook secret rotation procedure (dual-accept during transition)
  - API user passwords rotation policy; enforce minimum password strength
  - Registry credentials rotation via CI
  - TLS certs: renew before expiry; support hot reload on SIGHUP (future)

Password Hashing (controller and registry users)
- Prefer Argon2id with memory/time cost tuned for environment
- Accept bcrypt as fallback
- Never store plaintext; never echo back; rate-limit login attempts

Signed URLs for Media Access

Objective
- Restrict task.iso access to temporary, pre-authorized requests valid only for a short window and specific job

Mechanism
- HMAC-SHA256 signature over canonical string:
  - sign_input = method + "\n" + path + "\n" + expires + "\n" + sha256(task.iso)
  - signature = base64url(HMAC(secret, sign_input))
- Request format:
  - GET /media/tasks/{job_id}/task.iso?expires=UNIX_TS&sig=BASE64URL
- Validation:
  - Verify current_time <= expires + skew
  - Recompute signature using stored secret and known iso hash
  - Reject if mismatch or expired
- Optional binders (toggleable):
  - client_ip in sign_input to restrict to known BMC mgmt IP (beware NAT)
- Rotation:
  - Rotate signing secret with dual-accept period
- Logging:
  - Redact sig; log job_id, expiry, client IP, and success/failure

Logging, Auditing, and Privacy

- Structured logs with correlation fields: job_id, server_serial, step, duration_ms, outcome
- Never log secrets or full recipe content; log sizes/hashes for sensitive payloads (unattend.xml, user-data)
- Access logs for:
  - API endpoints (user, route, status)
  - Webhook endpoint (anonymized, include success/failure)
  - Media requests (job_id only; signature redacted)
  - Registry (user, repo, digest/tag, bytes)
- Audit trail:
  - job_events table records security-relevant transitions and errors
  - Optional registry audit file with push/pull events (user, repo, digest)

Hardening: Controller

- Rate limiting:
  - Login endpoints (Basic/JWT) and webhook endpoint (per-source/minute)
- Timeouts:
  - Per-request read/write timeouts
  - Backend timeouts (Redfish, ISO builder)
- Input validation:
  - Strict JSON schema for recipes (reject unknown fields by default)
- Headers and CORS:
  - Strict CORS (API for same-origin; disable broadly unless UI requires)
  - Security headers (X-Content-Type-Options, X-Frame-Options, Referrer-Policy)
- Process:
  - Least privilege filesystem access; run non-root when feasible (bind privileged ports via systemd)
- DoS mitigation:
  - Bounded worker concurrency; per-server serialization of jobs; backpressure on media/registry I/O
- Dependency hygiene:
  - Pin dependencies; scan for CVEs; update regularly

Hardening: Maintenance OS

- No inbound services (sshd disabled)
- Firewall default deny inbound; allow egress to controller/registry only
- Quadlet containers:
  - Run privileged only when necessary (partitioning/bootloader)
  - Minimize capabilities as you gain confidence (SYS_ADMIN, SYS_RAWIO only if required)
  - Mount only required host paths; /run/provision read-only where possible
- Secrets handling:
  - WEBHOOK_SECRET provided via image bake or task.env; do not log
- Telemetry:
  - Log step boundaries and hashes only; never log unattend.xml/user-data contents
- Supply chain:
  - Prefer pulling tool images from controller’s registry; optionally pin by digest
  - Pre-bind images for air-gapped deployments

Supply Chain Security

- Controller binary and dependencies:
  - Reproducible builds; checksums for releases
  - SBOM generation (future)
- Registry artifacts:
  - Prefer pinned digests in recipes; tags allowed but verify digest on pull
  - Optional image signing (cosign) and policy enforcement (future)
- Maintenance OS image:
  - Signed bootc images (if available) and verifiable provenance
- CI pipeline:
  - Use dedicated credentials with minimal scope to push artifacts
  - Store CI secrets securely; rotate regularly

Threat Model (STRIDE)

- Spoofing:
  - Threat: Fake client posting jobs; fake maintenance OS posting webhooks
  - Mitigations: API auth (Basic/JWT/mTLS); webhook shared secret or mTLS; signed media URLs
- Tampering:
  - Threat: Task ISO or registry artifacts modified in transit
  - Mitigations: HTTPS; signed URLs; artifact digest verification (oras/podman); optional image signing
- Repudiation:
  - Threat: User denies creating a job or pushing an artifact
  - Mitigations: Authenticated endpoints; audit logs for create/push; event timestamps
- Information Disclosure:
  - Threat: Leaking unattend.xml/user-data or credentials
  - Mitigations: Never log contents; encrypt at rest; HTTPS; minimal retention; restrict media access; role-based API
- Denial of Service:
  - Threat: Flood of job/webhook/media/registry requests
  - Mitigations: Rate limiting; worker/backpressure; timeouts; keep task.iso small; GC for retention
- Elevation of Privilege:
  - Threat: Container escaping or privilege escalation on maintenance OS
  - Mitigations: Minimal host; privileged only when necessary; tightened capabilities; no inbound services; update base images

Secure Defaults and Configuration

- HTTPS enabled by default for controller endpoints
- AUTH_MODE=basic; strong password policy; bcrypt/argon2id
- WEBHOOK_SECRET required; long random; rotated periodically
- Signed URLs enabled for /media by default; expiry ≤ 30 minutes
- REGISTRY_AUTH_MODE=basic; no anonymous push; pull allowed only to authenticated users by default
- REDFISH_INSECURE=false by default; enable only with explicit policy and compensating controls
- JOB_RETENTION_DAYS minimal; purge completed jobs and ISOs after retention
- Logs redact secrets; debug logs disabled in production

Compliance and Policy Notes

- AGPLv3 licensing and compatible dependencies only
- Keep a dependency license inventory and scan regularly
- If handling regulated data in user-data/unattend.xml, enforce org policy (encryption-at-rest, retention, access controls)

Testing and Validation

- Unit:
  - Auth middlewares; signed URL validation; JSON schema validation errors
- Integration:
  - TLS termination and CA trust validation from maintenance OS
  - Webhook auth (secret mismatch, duplicates)
  - Registry push/pull auth; large blob handling
- Security tests:
  - Secret scanning in repository
  - Rate-limiting and brute-force tests against auth endpoints
  - Fuzz request parsers for APIs that accept complex JSON
- Chaos/Resilience:
  - Expired certs, rotated secrets, controller restart mid-provisioning
- Tooling:
  - Static analysis; CVE scanning for images and dependencies

Operational Procedures

- Key rotation:
  - Webhook secret: generate new; accept both old/new for a window; update maintenance OS image at next release
  - TLS: renew certs; reload server
  - Registry credentials: rotate per CI schedule
- Incident response:
  - Revoke/rotate compromised secrets; invalidate signed URLs (secret rotation)
  - Audit logs to identify misuse; purge sensitive task ISOs; pause provisioning if needed
- BMC compromise:
  - Rotate BMC creds; verify firmware versions; isolate device
- Data retention:
  - Keep only necessary job metadata; purge ISO files post-retention

Acceptance Criteria

- Controller enforces authentication on user API and registry; rejects unauthenticated/unauthorized requests
- Webhook endpoint requires secret or mTLS; correctly handles duplicates; never logs secrets
- Media endpoint validates signed URLs and expires them; secrets redacted from logs
- TLS is enabled end-to-end; maintenance OS trusts controller CA; Redfish HTTPS preferred
- Secrets at rest are not world-readable; password hashes are strong (argon2id/bcrypt)
- Logs and audits capture sufficient events without leaking sensitive content
- Default configuration is secure; insecure modes require explicit opt-in and are documented
- go run build.go validate (including security-focused tests) passes when implemented

Open Questions

- Should we mandate mTLS for webhook in high-security deployments (config toggle)?
- Enforce immutable tags for os-images/* by default to avoid silent image drift?
- Introduce IP pinning in signed URLs as optional defense (document NAT caveats)?
- Integrate image signing (cosign) and policy enforcement (cosign verify) for tool/OS images?

Change Log

- v0.1 (2025-11-05): Initial security model covering auth, TLS, secrets, signed URLs, and threat model.
