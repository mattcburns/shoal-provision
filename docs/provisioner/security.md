# Provisioner Security Guide

This document describes the security features of the Shoal provisioner controller and provides guidance for secure deployment and operation.

## Table of Contents

1. [Security Architecture](#security-architecture)
2. [TLS/HTTPS Configuration](#tlshttps-configuration)
3. [Authentication](#authentication)
4. [Rate Limiting](#rate-limiting)
5. [Signed Media URLs](#signed-media-urls)
6. [Secret Management](#secret-management)
7. [Password Hashing](#password-hashing)
8. [Security Headers](#security-headers)
9. [Deployment Best Practices](#deployment-best-practices)
10. [Monitoring and Auditing](#monitoring-and-auditing)

## Security Architecture

The provisioner controller implements defense-in-depth with multiple security layers:

- **Transport Security**: TLS 1.2+ encryption for all HTTP traffic
- **Authentication**: Basic auth, JWT, or webhook secret validation
- **Rate Limiting**: Token bucket algorithm prevents DoS attacks
- **Input Validation**: Strict validation of all API inputs
- **Credential Protection**: Argon2id password hashing, redaction in logs
- **Media Protection**: HMAC-SHA256 signed URLs for ISO downloads
- **Security Headers**: OWASP-compliant HTTP headers

All security features follow OWASP recommendations and are documented in `design/033_Security_Model.md`.

## TLS/HTTPS Configuration

### Enabling TLS

TLS is **strongly recommended** for production deployments. Enable it with environment variables or command-line flags:

```bash
# Environment variables
export ENABLE_TLS=true
export TLS_CERT_FILE=/path/to/server.crt
export TLS_KEY_FILE=/path/to/server.key

# Or use flags
./provisioner-controller \
  -enable-tls \
  -tls-cert /path/to/server.crt \
  -tls-key /path/to/server.key
```

### Generating Self-Signed Certificates (Development Only)

For development/testing, generate self-signed certificates:

```bash
openssl req -x509 -newkey rsa:4096 -nodes \
  -keyout server.key \
  -out server.crt \
  -days 365 \
  -subj "/CN=provisioner.example.com"
```

**WARNING**: Never use self-signed certificates in production. Use certificates from a trusted Certificate Authority (CA) or Let's Encrypt.

### Production Certificate Setup

For production, obtain certificates from:

1. **Let's Encrypt** (free, automated):
   ```bash
   certbot certonly --standalone -d provisioner.example.com
   ```

2. **Corporate CA**: Request certificates from your organization's PKI team

3. **Commercial CA**: Purchase from DigiCert, Sectigo, etc.

### Certificate Renewal

Certificates expire. Automate renewal:

```bash
# Let's Encrypt auto-renewal (via cron)
0 0 * * * certbot renew --quiet --deploy-hook "systemctl restart provisioner-controller"
```

The controller does **not** support hot-reloading certificates. Restart the service after renewal.

### HSTS (HTTP Strict Transport Security)

When TLS is enabled (`ENABLE_TLS=true`), the controller automatically sets the `Strict-Transport-Security` header with a 1-year max-age. This instructs browsers to always use HTTPS.

**First deployment**: Ensure TLS works correctly before enabling HSTS, as HSTS cannot be easily revoked.

## Authentication

### API Authentication Modes

The controller supports three authentication modes for the Jobs API (`/api/v1/jobs`):

#### 1. None (Development Only)

```bash
export AUTH_MODE=none
```

**WARNING**: Disables authentication. Use only for local development.

#### 2. Basic Authentication

```bash
export AUTH_MODE=basic
export BASIC_USER=admin
export BASIC_PASS=your-secure-password
```

Clients must send `Authorization: Basic <base64(user:pass)>` header.

#### 3. JWT (Recommended for Production)

```bash
export AUTH_MODE=jwt
export JWT_SECRET=your-256-bit-secret
export JWT_AUDIENCE=provisioner-api
export JWT_ISSUER=https://auth.example.com
```

Clients must obtain a JWT from your identity provider and send `Authorization: Bearer <token>`.

**Best practices**:
- Use a strong JWT secret (256-bit minimum, generated with `openssl rand -base64 32`)
- Rotate JWT secrets periodically
- Set short token expiry times (15-60 minutes)
- Validate `aud` and `iss` claims

### Webhook Authentication

Webhooks use shared secret authentication via `X-Webhook-Secret` header:

```bash
export WEBHOOK_SECRET=your-webhook-secret
```

Generate strong secrets:
```bash
openssl rand -base64 32
```

#### Secret Rotation (Zero-Downtime)

To rotate the webhook secret without downtime:

1. **Deploy with both secrets**:
   ```bash
   export WEBHOOK_SECRET=new-secret-here
   export WEBHOOK_SECRET_OLD=old-secret-here
   ```

2. **Update dispatcher configuration** to use `new-secret-here`

3. **Wait for all dispatchers to redeploy** (typically 1 rolling update cycle)

4. **Remove old secret**:
   ```bash
   unset WEBHOOK_SECRET_OLD
   ```

The controller logs when the old secret is used, allowing you to monitor rotation progress.

## Rate Limiting

Rate limiting protects authentication endpoints from brute-force attacks using a token bucket algorithm.

### Configuration

```bash
# Requests per minute per client IP
export RATE_LIMIT_RPM=10

# Burst size (initial tokens available)
export RATE_LIMIT_BURST=5
```

### Behavior

- Each client IP gets `RATE_LIMIT_BURST` initial tokens
- Tokens refill at `RATE_LIMIT_RPM / 60` per second
- When tokens are exhausted, clients receive `429 Too Many Requests` with `Retry-After: 60` header
- Rate limits reset after 2× cleanup interval (10 minutes by default)

### Reverse Proxy Considerations

The rate limiter extracts client IPs from:

1. `X-Forwarded-For` header (first IP)
2. `X-Real-IP` header
3. `RemoteAddr` (fallback)

**IMPORTANT**: When deploying behind a reverse proxy (nginx, HAProxy, Cloudflare), ensure it sets `X-Forwarded-For` correctly. Otherwise, all requests appear to come from the proxy IP.

### Monitoring Rate Limits

Watch for `429` responses in logs:
```bash
grep "429" /var/log/provisioner-controller.log
```

If legitimate users are rate-limited, increase `RATE_LIMIT_RPM` or `RATE_LIMIT_BURST`.

## Signed Media URLs

Task ISO files contain sensitive provisioning data (SSH keys, passwords, configurations). Signed URLs prevent unauthorized access.

### Configuration

```bash
# Enable signed URLs
export MEDIA_SIGNING_SECRET=your-media-signing-secret

# URL expiry (default: 30 minutes)
export MEDIA_SIGNED_URL_TTL=30m

# Optional: bind URLs to client IP
export MEDIA_ENABLE_IP_BIND=true
```

### How It Works

1. Controller generates signed URL with HMAC-SHA256:
   ```
   /media/tasks/{job_id}/task.iso?expires=1699999999&sig=abc123
   ```

2. Dispatcher fetches ISO using signed URL

3. MediaHandler validates:
   - Signature matches (HMAC-SHA256 with `MEDIA_SIGNING_SECRET`)
   - URL not expired (`expires` > current time)
   - Client IP matches (if `MEDIA_ENABLE_IP_BIND=true`)

4. On validation failure, returns `403 Forbidden`

### Signed URL Security

- **Never log signed URLs**: They contain the signature and can be replayed
- **Use short TTLs**: 30 minutes is recommended. Reduce for high-security environments
- **IP binding**: Enable in production to prevent URL sharing. Disable if dispatchers use NAT/proxy pools
- **Secret rotation**: Rotate `MEDIA_SIGNING_SECRET` periodically (requires coordination with active provisioning jobs)

## Secret Management

### Storage Recommendations

**Never** commit secrets to version control. Use:

1. **Environment variables** (systemd, Docker, Kubernetes)
2. **Secret management systems**: HashiCorp Vault, AWS Secrets Manager, Kubernetes Secrets
3. **Configuration files** with restrictive permissions (`chmod 600`)

### Secrets Summary

| Secret | Purpose | Rotation | Strength |
|--------|---------|----------|----------|
| `WEBHOOK_SECRET` | Webhook authentication | Monthly | 256-bit |
| `JWT_SECRET` | JWT signature verification | Quarterly | 256-bit |
| `BASIC_PASS` | Basic auth password | Quarterly | 16+ chars |
| `MEDIA_SIGNING_SECRET` | Signed URL HMAC | Quarterly | 256-bit |
| TLS Private Key | TLS encryption | Yearly (via cert renewal) | RSA 4096+ |

### Secret Generation

Use cryptographically secure random generation:

```bash
# 256-bit secrets (32 bytes base64-encoded)
openssl rand -base64 32

# Passwords (16 alphanumeric characters)
openssl rand -base64 12 | tr -d '/+=' | cut -c1-16
```

### Secret Redaction in Logs

The controller automatically redacts secrets in logs using `pkg/crypto/redact.go`:

- **Secrets**: First 2 + last 2 characters visible
- **Tokens**: First 4 + last 4 characters visible
- **Passwords**: `[REDACTED]`
- **Authorization headers**: Scheme + redacted value
- **URLs**: Passwords in connection strings replaced with `****`

Verify redaction:
```bash
# No plaintext secrets should appear
grep -E 'secret|password|token' /var/log/provisioner-controller.log
```

## Password Hashing

Passwords (for basic auth, registry, etc.) are hashed with **Argon2id** using OWASP-recommended parameters.

### Argon2id Parameters

- **Memory**: 64 MiB
- **Iterations**: 3
- **Parallelism**: 2 (uses 2 CPU cores)
- **Salt**: 16 bytes (random)
- **Output**: 32 bytes

### Hash Format (PHC String)

```
$argon2id$v=19$m=65536,t=3,p=2$<salt>$<hash>
```

### Bcrypt Fallback

For compatibility, bcrypt (cost 10) is supported. The controller automatically upgrades bcrypt hashes to Argon2id on next password verification:

```go
if crypto.NeedsRehash(storedHash) {
    newHash, _ := crypto.HashPassword(plaintextPassword)
    // Update stored hash to newHash
}
```

### Best Practices

- **Never store plaintext passwords**
- **Use Argon2id for new passwords** (`crypto.HashPassword()`)
- **Verify passwords securely** (`crypto.VerifyPassword()`)
- **Upgrade legacy hashes** (`crypto.NeedsRehash()`)

## Security Headers

The controller sets the following security headers on **all responses**:

| Header | Value | Purpose |
|--------|-------|---------|
| `X-Content-Type-Options` | `nosniff` | Prevent MIME sniffing |
| `X-Frame-Options` | `DENY` | Prevent clickjacking |
| `Referrer-Policy` | `no-referrer` | Prevent referrer leakage |
| `Strict-Transport-Security` | `max-age=31536000` | Force HTTPS (when TLS enabled) |

### CORS Configuration (Optional)

CORS is **disabled by default**. Enable if web UI needs cross-origin access:

```bash
export ENABLE_CORS=true
export CORS_ALLOWED_ORIGINS=https://app.example.com,https://admin.example.com
```

**WARNING**: Never use `CORS_ALLOWED_ORIGINS=*` in production. Specify exact origins.

## Deployment Best Practices

### 1. Run as Non-Root User

Create a dedicated service user:

```bash
sudo useradd -r -s /bin/false provisioner
sudo chown -R provisioner:provisioner /var/lib/provisioner
```

Run the controller as this user (systemd example):

```ini
[Service]
User=provisioner
Group=provisioner
ExecStart=/usr/local/bin/provisioner-controller
```

### 2. File Permissions

Restrict access to sensitive files:

```bash
chmod 600 /etc/provisioner/secrets.env
chmod 600 /etc/provisioner/tls/server.key
chmod 755 /var/lib/provisioner
```

### 3. Firewall Configuration

Limit network access:

```bash
# Allow only necessary ports
sudo ufw allow 8443/tcp  # HTTPS
sudo ufw deny 8080/tcp   # Block HTTP in production
```

### 4. Resource Limits

Set resource limits to prevent DoS:

```ini
[Service]
LimitNOFILE=1024
LimitNPROC=64
MemoryLimit=512M
CPUQuota=100%
```

### 5. Logging and Monitoring

- **Centralize logs**: Use syslog, journald, or log aggregation (ELK, Splunk)
- **Monitor for anomalies**: Failed auth attempts, rate limits, webhook failures
- **Alert on security events**: 401/403 spikes, TLS errors, secret rotation issues

### 6. Network Segmentation

- **Isolate provisioner network**: Use VLANs or firewalls to separate provisioner traffic from production
- **Restrict BMC access**: Only allow provisioner to communicate with BMC network
- **Use VPN/bastion**: Require VPN or bastion host for API access

### 7. Regular Updates

- **Update Go runtime**: Security patches for the Go compiler
- **Update dependencies**: `go get -u && go mod tidy`
- **Monitor CVEs**: Subscribe to GitHub security advisories

## Monitoring and Auditing

### Security Metrics

Monitor these Prometheus metrics (exposed at `/metrics`):

- `http_requests_total{status="401"}`: Failed authentication attempts
- `http_requests_total{status="429"}`: Rate limit hits
- `http_requests_total{status="403"}`: Signed URL rejections
- `webhook_requests_total{status="unauthorized"}`: Invalid webhook secrets

### Security Logs

Key log events to monitor:

```bash
# Failed auth attempts
grep "unauthorized" /var/log/provisioner-controller.log

# Rate limit violations
grep "rate limit" /var/log/provisioner-controller.log

# Webhook secret rotation progress
grep "authenticated with old secret" /var/log/provisioner-controller.log

# TLS errors
grep "tls" /var/log/provisioner-controller.log | grep -i error
```

### Audit Checklist

Perform regular security audits:

- [ ] All secrets rotated within policy timeframe
- [ ] TLS certificates valid and not expiring soon
- [ ] No plaintext secrets in logs (run `grep -E 'password|secret' logs`)
- [ ] Rate limiting configured and effective
- [ ] Authentication enabled on all protected endpoints
- [ ] HSTS header present when TLS enabled
- [ ] File permissions correct (secrets 600, binaries 755)
- [ ] Service running as non-root user
- [ ] Firewall rules restrict unnecessary access
- [ ] Monitoring alerts configured
- [ ] Dependency versions up to date

## Security Incident Response

### Compromised Secret

If a secret is compromised:

1. **Rotate immediately** using the zero-downtime procedure
2. **Audit logs** for unauthorized access during compromise window
3. **Notify stakeholders** per incident response plan
4. **Review security controls** to prevent recurrence

### Detected Brute-Force Attack

If rate limiting detects brute-force attempts:

1. **Confirm legitimate traffic** (check source IPs)
2. **Temporarily reduce `RATE_LIMIT_BURST`** to slow attackers
3. **Block source IPs** at firewall level if attack persists
4. **Review auth logs** for successful compromises

### TLS Certificate Expiry

If TLS certificate expires:

1. **Renew certificate immediately**
2. **Restart controller** (`systemctl restart provisioner-controller`)
3. **Implement auto-renewal** to prevent future expiry
4. **Set monitoring alerts** for 30 days before expiry

## Compliance

The provisioner controller implements security controls aligned with:

- **OWASP Top 10** (2021)
- **CIS Benchmarks** (general server hardening)
- **NIST SP 800-63B** (password hashing)
- **RFC 7519** (JWT)
- **RFC 6749** (OAuth 2.0 patterns)

For compliance documentation, see `design/038_Compliance_and_Licensing.md`.

## References

- [OWASP Cheat Sheets](https://cheatsheetseries.owasp.org/)
- [Argon2id RFC 9106](https://www.rfc-editor.org/rfc/rfc9106.html)
- [JWT Best Practices](https://www.rfc-editor.org/rfc/rfc8725.html)
- [HSTS RFC 6797](https://www.rfc-editor.org/rfc/rfc6797.html)
- [TLS 1.3 RFC 8446](https://www.rfc-editor.org/rfc/rfc8446.html)

## Support

For security questions or to report vulnerabilities:

- **Email**: security@example.com (replace with actual contact)
- **GitHub**: Open a security advisory (private disclosure)
- **Emergency**: Contact on-call via PagerDuty (replace with actual process)

**Responsible Disclosure**: We appreciate responsible disclosure of security issues. Please do not open public GitHub issues for security vulnerabilities.
