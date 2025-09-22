# Deployment and Operations

This guide covers deployment, security, and troubleshooting.

## Deployment

Shoal is designed for simple deployment as a single, self-contained binary with no external dependencies. For detailed instructions on production builds, systemd services, and Docker, see `DEPLOYMENT.md`.

**Quick Deployment (from source):**
```bash
# Build for production
python3 build.py build

# Copy and run on target server
scp build/shoal user@server:/opt/shoal/
ssh user@server '/opt/shoal/shoal -port 8080 -db /var/lib/shoal/shoal.db'
```

## Releases

Download pre-built binaries from the project's [GitHub Releases](https://github.com/mattcburns/shoal/releases) page.

```bash
# Linux AMD64
curl -L -o shoal "https://github.com/mattcburns/shoal/releases/latest/download/shoal-linux-amd64"
chmod +x shoal && ./shoal
```

## Security

### Password Security

**User Passwords**:
- Hashed using bcrypt.
- Original passwords are never stored or logged.

**BMC Password Encryption**:
- Shoal supports AES-256-GCM encryption for BMC passwords stored in the database.
- To enable, provide an encryption key via the `SHOAL_ENCRYPTION_KEY` environment variable or the `--encryption-key` flag.
- If no key is provided, passwords are stored in plaintext (not recommended for production).
- **IMPORTANT**: The same key must be used consistently. Losing the key means losing access to all BMC passwords.

```bash
# Using environment variable (recommended)
export SHOAL_ENCRYPTION_KEY="your-secret-encryption-key"
./build/shoal
```

### BMC Requirements

- BMCs must support DMTF Redfish API (v1.6.0 or compatible).
- Network connectivity from the Shoal server to BMC management interfaces.
- Valid BMC credentials (username/password).
- Certificate validation is disabled by default to support self-signed certificates, which are common in BMC environments.

## Troubleshooting

### Common Issues

1.  **BMC Connection Failed**:
    - Verify the BMC IP address and network connectivity.
    - Check the BMC credentials.
    - Ensure the Redfish service is enabled on the BMC.

2.  **Database Errors**:
    - Check file permissions for the database file.
    - Verify disk space availability.

3.  **Authentication Issues**:
    - Verify admin credentials (`admin`/`admin` by default).
    - Check if a session token has expired.

### Debug Logging

Enable debug logging to get detailed information about requests and internal operations.

```bash
# Enable debug logging
./build/shoal -log-level debug
```
