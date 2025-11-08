# Embedded OCI Registry - Troubleshooting Guide

This guide provides detailed troubleshooting steps for common issues with the Shoal embedded OCI registry.

## Table of Contents

1. [Push/Pull Failures](#pushpull-failures)
2. [Authentication Issues](#authentication-issues)
3. [Storage Problems](#storage-problems)
4. [Performance Issues](#performance-issues)
5. [Garbage Collection](#garbage-collection)
6. [Networking Problems](#networking-problems)
7. [Debug Logging](#debug-logging)
8. [Common Error Codes](#common-error-codes)

## Push/Pull Failures

### BLOB_UPLOAD_INVALID

**Symptoms:**
- Push fails with error: `BLOB_UPLOAD_INVALID: blob upload invalid`
- Upload completes but finalization fails

**Causes:**
- Digest mismatch between computed and provided digest
- File corruption during transfer
- Interrupted upload left partial data

**Solutions:**

1. Verify local file integrity:
```bash
# Calculate digest of local file
sha256sum artifact.tar.gz

# Compare with digest reported by oras
oras push controller:8080/repo:tag artifact.tar.gz --plain-http --debug
```

2. Check controller logs for digest mismatch:
```bash
grep "digest mismatch" /var/log/shoal/controller.log
```

3. Clear partial uploads and retry:
```bash
# Remove temporary upload directories (controller must be stopped)
sudo rm -rf /var/lib/shoal/oci/uploads/*

# Restart controller
sudo systemctl restart provisioner-controller

# Retry push
oras push controller:8080/repo:tag artifact.tar.gz --plain-http
```

4. Verify network stability:
```bash
# Test connection stability during large transfers
ping -c 100 -i 0.2 controller.example.com

# Check for packet loss
mtr controller.example.com
```

### MANIFEST_UNKNOWN

**Symptoms:**
- Pull fails with error: `MANIFEST_UNKNOWN: manifest unknown`
- Tag or digest not found in registry

**Causes:**
- Tag doesn't exist (typo or not yet pushed)
- Tag was deleted
- Manifest was corrupted

**Solutions:**

1. Verify tag exists using oras discover:
```bash
oras discover controller:8080/os-images/ubuntu-rootfs:22.04 --plain-http
```

2. List available tags (if catalog endpoint is enabled):
```bash
curl http://controller:8080/v2/os-images/ubuntu-rootfs/tags/list
```

3. Check manifest file on disk:
```bash
# List repositories
ls /var/lib/shoal/oci/repositories/

# List tags for a repository
ls /var/lib/shoal/oci/repositories/os-images/ubuntu-rootfs/refs/

# Read tag reference
cat /var/lib/shoal/oci/repositories/os-images/ubuntu-rootfs/refs/22.04
```

4. Verify manifest blob exists:
```bash
# Get manifest digest from tag ref
DIGEST=$(cat /var/lib/shoal/oci/repositories/os-images/ubuntu-rootfs/refs/22.04)

# Check if manifest blob exists
DIGEST_PATH=$(echo $DIGEST | sed 's/sha256://')
ls -lh /var/lib/shoal/oci/blobs/sha256/$DIGEST_PATH
```

### BLOB_UNKNOWN

**Symptoms:**
- Pull fails with error: `BLOB_UNKNOWN: blob unknown to registry`
- Manifest references blob that doesn't exist

**Causes:**
- Incomplete push (manifest pushed but blob failed)
- Garbage collection deleted blob prematurely
- Storage corruption

**Solutions:**

1. Verify blob exists in storage:
```bash
# Extract blob digest from error message
BLOB_DIGEST="sha256:abc123..."
DIGEST_PATH=$(echo $BLOB_DIGEST | sed 's/sha256://')

# Check if blob exists
ls -lh /var/lib/shoal/oci/blobs/sha256/$DIGEST_PATH
```

2. Check GC logs for premature deletion:
```bash
grep "deleted blob" /var/log/shoal/controller.log | grep $BLOB_DIGEST
```

3. Re-push the artifact:
```bash
oras push controller:8080/repo:tag artifact.tar.gz --plain-http
```

## Authentication Issues

### 401 Unauthorized

**Symptoms:**
- All requests to `/v2/*` return `401 Unauthorized`
- Authentication required but credentials not provided or invalid

**Causes:**
- Registry has authentication enabled but no credentials provided
- Invalid username/password
- htpasswd file misconfigured
- Wrong authentication mode

**Solutions:**

1. Verify authentication configuration:
```bash
# Check controller environment
grep REGISTRY_AUTH /etc/systemd/system/provisioner-controller.service

# Expected values: none, basic, htpasswd
```

2. Test htpasswd file:
```bash
# Verify file format
cat /etc/shoal/registry.htpasswd

# Test password (htpasswd -v requires apache2-utils package)
htpasswd -v /etc/shoal/registry.htpasswd admin
```

3. Provide credentials to oras/podman:
```bash
# oras with credentials
oras push controller:8080/repo:tag \
  --plain-http \
  --username admin \
  --password secretpassword \
  artifact.tar.gz

# podman with credentials
podman login --tls-verify=false controller:8080
podman push controller:8080/repo:tag
```

4. Check controller logs for auth failures:
```bash
grep "authentication failed" /var/log/shoal/controller.log
```

### Credentials Not Working

**Symptoms:**
- Credentials provided but still get `401 Unauthorized`
- Password known to be correct

**Causes:**
- htpasswd file has incorrect permissions
- htpasswd file not found by controller
- Password hashing algorithm mismatch

**Solutions:**

1. Check file permissions:
```bash
# htpasswd file must be readable by controller process
ls -la /etc/shoal/registry.htpasswd

# Fix if needed
sudo chown shoal:shoal /etc/shoal/registry.htpasswd
sudo chmod 640 /etc/shoal/registry.htpasswd
```

2. Recreate htpasswd file with correct algorithm:
```bash
# Use bcrypt (-B flag) for strong hashing
htpasswd -Bbn admin newpassword > /tmp/registry.htpasswd.new

# Backup old file
sudo cp /etc/shoal/registry.htpasswd /etc/shoal/registry.htpasswd.backup

# Replace with new file
sudo mv /tmp/registry.htpasswd.new /etc/shoal/registry.htpasswd

# Restart controller
sudo systemctl restart provisioner-controller
```

3. Test authentication directly:
```bash
# Test basic auth with curl
curl -v -u admin:secretpassword http://controller:8080/v2/
```

## Storage Problems

### Storage Space Exhausted

**Symptoms:**
- Push fails with error about disk space
- Controller logs show "no space left on device"
- Cannot upload new artifacts

**Causes:**
- Storage partition full
- Too many unreferenced blobs
- GC not running or misconfigured

**Solutions:**

1. Check storage usage:
```bash
# Check filesystem usage
df -h /var/lib/shoal/oci

# Check OCI storage size
du -sh /var/lib/shoal/oci

# Breakdown by directory
du -sh /var/lib/shoal/oci/*
```

2. Trigger manual garbage collection:
```bash
# Via admin endpoint (if enabled)
curl -X POST http://controller:8080/admin/gc

# Check GC logs
tail -f /var/log/shoal/controller.log | grep "gc:"
```

3. Manually clean up old blobs (controller must be stopped):
```bash
# Stop controller
sudo systemctl stop provisioner-controller

# Identify unreferenced blobs (requires manual script or go tool)
# WARNING: Only delete blobs you're certain are unreferenced

# Restart controller
sudo systemctl start provisioner-controller
```

4. Increase storage capacity:
```bash
# Add storage or expand partition
sudo lvextend -L +50G /dev/vg/shoal-oci
sudo resize2fs /dev/vg/shoal-oci

# Or move OCI storage to larger partition
sudo systemctl stop provisioner-controller
sudo mv /var/lib/shoal/oci /mnt/large-storage/oci
sudo ln -s /mnt/large-storage/oci /var/lib/shoal/oci
sudo systemctl start provisioner-controller
```

### Corrupted Storage

**Symptoms:**
- Registry returns unexpected errors
- Files missing from storage
- Blobs exist but can't be read

**Causes:**
- Filesystem corruption
- Incomplete writes during crash
- Incorrect permissions

**Solutions:**

1. Check filesystem integrity:
```bash
# Unmount and check filesystem (requires downtime)
sudo systemctl stop provisioner-controller
sudo umount /var/lib/shoal/oci
sudo fsck -f /dev/vg/shoal-oci
sudo mount /var/lib/shoal/oci
```

2. Verify file permissions:
```bash
# Check ownership
ls -laR /var/lib/shoal/oci | head -20

# Fix if needed
sudo chown -R shoal:shoal /var/lib/shoal/oci
sudo find /var/lib/shoal/oci -type d -exec chmod 755 {} \;
sudo find /var/lib/shoal/oci -type f -exec chmod 644 {} \;
```

3. Validate OCI layout structure:
```bash
# Verify required files exist
test -f /var/lib/shoal/oci/oci-layout && echo "OK" || echo "MISSING"
test -f /var/lib/shoal/oci/index.json && echo "OK" || echo "MISSING"

# Check oci-layout version
cat /var/lib/shoal/oci/oci-layout
# Should output: {"imageLayoutVersion":"1.0.0"}
```

4. Restore from backup:
```bash
# Stop controller
sudo systemctl stop provisioner-controller

# Restore from latest backup
sudo rm -rf /var/lib/shoal/oci
sudo tar xzf /backups/oci-backup-latest.tar.gz -C /var/lib/shoal/

# Verify structure
ls -la /var/lib/shoal/oci

# Restart controller
sudo systemctl start provisioner-controller
```

## Performance Issues

### Slow Upload/Download

**Symptoms:**
- Large artifacts take excessively long to upload/download
- Transfer speeds much lower than expected
- Timeouts on large files

**Causes:**
- Network bandwidth limitations
- CPU or I/O bottleneck on controller
- Storage backend slow (e.g., network filesystem)
- Concurrent operations saturating resources

**Solutions:**

1. Measure actual network performance:
```bash
# Test bandwidth to controller
iperf3 -c controller.example.com

# Or use simple curl test
time curl -o /dev/null http://controller:8080/v2/
```

2. Check controller resource usage:
```bash
# CPU and memory
ssh controller "top -b -n 1 | head -20"

# I/O wait
ssh controller "iostat -x 1 5"

# Network usage
ssh controller "iftop -i eth0"
```

3. Optimize storage backend:
```bash
# Check if storage is on network filesystem
df -T /var/lib/shoal/oci

# Consider moving to local SSD
# Or tune mount options for performance
```

4. Adjust concurrency limits:
```bash
# Edit controller configuration
# Set REGISTRY_MAX_CONCURRENT_UPLOADS=4

# Restart controller
sudo systemctl restart provisioner-controller
```

5. Increase timeouts:
```bash
# For oras client
export ORAS_TIMEOUT=3600  # 1 hour

# For podman
podman push --timeout=3600s controller:8080/repo:tag
```

### High Memory Usage

**Symptoms:**
- Controller process using excessive memory
- OOM killer terminates controller
- System becomes unresponsive during large transfers

**Causes:**
- Large files being buffered in memory
- Memory leak in registry code
- Too many concurrent uploads

**Solutions:**

1. Verify streaming is working:
```bash
# Check controller logs during upload
tail -f /var/log/shoal/controller.log | grep "streaming"

# Should NOT see "buffering entire file" messages
```

2. Limit concurrent uploads:
```bash
# Reduce concurrent upload limit
export REGISTRY_MAX_CONCURRENT_UPLOADS=2

# Restart controller
sudo systemctl restart provisioner-controller
```

3. Monitor memory usage during transfers:
```bash
# Watch memory in real-time
watch -n 1 'ps aux | grep provisioner-controller | grep -v grep'

# Or use htop
htop -p $(pgrep provisioner-controller)
```

4. Adjust systemd memory limits if needed:
```bash
# Edit service file
sudo systemctl edit provisioner-controller

# Add:
[Service]
MemoryMax=4G
MemoryHigh=3G

# Reload and restart
sudo systemctl daemon-reload
sudo systemctl restart provisioner-controller
```

## Garbage Collection

### GC Not Running

**Symptoms:**
- Storage continues to grow despite deleting tags
- Unreferenced blobs not being deleted
- No GC activity in logs

**Causes:**
- GC disabled in configuration
- GC interval too long
- GC process crashing

**Solutions:**

1. Verify GC configuration:
```bash
# Check environment variables
grep REGISTRY_GC /etc/systemd/system/provisioner-controller.service

# Should see:
# REGISTRY_GC_INTERVAL=1h
# REGISTRY_GC_GRACE_PERIOD=24h
```

2. Check GC logs:
```bash
# Look for GC activity
grep "gc: " /var/log/shoal/controller.log | tail -20

# Should see periodic "gc: started" and "gc: completed" messages
```

3. Trigger manual GC:
```bash
curl -X POST http://controller:8080/admin/gc

# Watch logs for GC activity
tail -f /var/log/shoal/controller.log | grep "gc:"
```

4. Check for GC errors:
```bash
# Look for errors during GC
grep "gc:.*error" /var/log/shoal/controller.log
```

### GC Deleting Blobs Too Aggressively

**Symptoms:**
- Pull fails immediately after push
- Blobs deleted while still in use
- `BLOB_UNKNOWN` errors shortly after upload

**Causes:**
- Grace period too short
- Tag update race condition
- Manifest not properly linked to blobs

**Solutions:**

1. Increase grace period:
```bash
# Edit configuration
export REGISTRY_GC_GRACE_PERIOD=48h

# Restart controller
sudo systemctl restart provisioner-controller
```

2. Verify manifest-blob linkage:
```bash
# Get manifest for a tag
MANIFEST=$(curl -s http://controller:8080/v2/repo/manifests/tag)

# Extract blob digests from manifest
echo "$MANIFEST" | jq -r '.layers[].digest'

# Verify each blob exists in storage
```

3. Disable GC temporarily:
```bash
# Set very long interval to effectively disable
export REGISTRY_GC_INTERVAL=87600h  # 10 years

# Restart controller
sudo systemctl restart provisioner-controller
```

## Networking Problems

### Cannot Connect to Registry

**Symptoms:**
- Connection refused or timeout when accessing registry
- `oras push/pull` fails with network errors
- curl to `/v2/` fails

**Causes:**
- Controller not running
- Firewall blocking port
- Wrong hostname or port
- TLS certificate issues

**Solutions:**

1. Verify controller is running:
```bash
# Check process
ssh controller "pgrep provisioner-controller"

# Check service status
ssh controller "systemctl status provisioner-controller"
```

2. Test connectivity:
```bash
# Basic ping
ping controller.example.com

# TCP connection to registry port
nc -zv controller.example.com 8080

# HTTP request
curl -v http://controller.example.com:8080/v2/
```

3. Check firewall rules:
```bash
# On controller, check if port is open
sudo firewall-cmd --list-ports

# Add port if needed
sudo firewall-cmd --permanent --add-port=8080/tcp
sudo firewall-cmd --reload
```

4. Verify TLS configuration:
```bash
# Test TLS connection
openssl s_client -connect controller.example.com:8443

# Check certificate validity
echo | openssl s_client -connect controller.example.com:8443 2>/dev/null | openssl x509 -noout -dates
```

### DNS Resolution Issues

**Symptoms:**
- Registry hostname doesn't resolve
- Works with IP but not hostname
- Intermittent failures

**Causes:**
- DNS misconfiguration
- `/etc/hosts` missing entry
- DNS cache issues

**Solutions:**

1. Test DNS resolution:
```bash
# Lookup hostname
nslookup controller.example.com

# Or use dig
dig controller.example.com
```

2. Add to /etc/hosts as workaround:
```bash
# Add entry
echo "192.168.1.100 controller.example.com" | sudo tee -a /etc/hosts

# Verify
ping controller.example.com
```

3. Clear DNS cache:
```bash
# On client (systemd-resolved)
sudo systemd-resolve --flush-caches

# On client (dnsmasq)
sudo systemctl restart dnsmasq
```

## Debug Logging

Enable detailed debug logging to troubleshoot complex issues.

### Enable Debug Logging

```bash
# Edit controller environment
sudo systemctl edit provisioner-controller

# Add:
[Service]
Environment="LOG_LEVEL=debug"

# Reload and restart
sudo systemctl daemon-reload
sudo systemctl restart provisioner-controller
```

### Useful Log Patterns

```bash
# Watch all registry operations
tail -f /var/log/shoal/controller.log | grep "registry:"

# Monitor blob uploads
tail -f /var/log/shoal/controller.log | grep "blob upload"

# Track manifest operations
tail -f /var/log/shoal/controller.log | grep "manifest:"

# Watch authentication decisions
tail -f /var/log/shoal/controller.log | grep "auth:"

# GC activity
tail -f /var/log/shoal/controller.log | grep "gc:"
```

### Enable HTTP Request Logging

```bash
# Enable verbose HTTP logging
export REGISTRY_LOG_HTTP_REQUESTS=true

# See every request to /v2/*
tail -f /var/log/shoal/controller.log | grep "http:"
```

## Common Error Codes

| Error Code | Meaning | Common Causes |
|------------|---------|---------------|
| `BLOB_UNKNOWN` | Blob not found | Incomplete push, GC deleted blob, corruption |
| `BLOB_UPLOAD_INVALID` | Upload failed validation | Digest mismatch, corruption, interrupted transfer |
| `BLOB_UPLOAD_UNKNOWN` | Upload session not found | Session expired, wrong UUID, controller restart |
| `DIGEST_INVALID` | Malformed digest | Client bug, wrong digest format |
| `MANIFEST_BLOB_UNKNOWN` | Manifest references missing blob | Incomplete push (blob failed but manifest succeeded) |
| `MANIFEST_INVALID` | Malformed manifest | Invalid JSON, unsupported schema version |
| `MANIFEST_UNKNOWN` | Manifest/tag not found | Tag doesn't exist, deleted, or wrong repository name |
| `NAME_INVALID` | Invalid repository name | Special characters, invalid format |
| `NAME_UNKNOWN` | Repository not found | Never pushed to this repository |
| `SIZE_INVALID` | Content-Length mismatch | Network issue, proxy interference |
| `TAG_INVALID` | Invalid tag name | Special characters, too long |
| `UNAUTHORIZED` | Authentication required or failed | No credentials, wrong password, auth misconfigured |
| `DENIED` | Authorization failed | User has no permission for this operation |
| `UNSUPPORTED` | Operation not supported | Unsupported media type, unsupported method |

## Getting Help

If you've tried the troubleshooting steps and still have issues:

1. **Gather diagnostic information:**
```bash
# Controller version
./provisioner-controller --version

# Configuration
grep REGISTRY_ /etc/systemd/system/provisioner-controller.service

# Recent logs
tail -100 /var/log/shoal/controller.log

# Storage status
df -h /var/lib/shoal/oci
du -sh /var/lib/shoal/oci/*

# OCI layout validation
test -f /var/lib/shoal/oci/oci-layout && cat /var/lib/shoal/oci/oci-layout
```

2. **Enable debug logging and reproduce the issue**

3. **File an issue with:**
   - Description of the problem
   - Steps to reproduce
   - Diagnostic information gathered above
   - Relevant log excerpts (with secrets redacted)

4. **Resources:**
   - GitHub Issues: https://github.com/mattburns/shoal/issues
   - Design docs: `design/027_Embedded_OCI_Registry.md`
   - User guide: `docs/provisioner/embedded_registry.md`
   - OCI Distribution Spec: https://github.com/opencontainers/distribution-spec
