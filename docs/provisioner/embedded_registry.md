# Embedded OCI Registry

The Shoal provisioner controller includes an embedded OCI Distribution-compliant registry for hosting provisioning artifacts such as rootfs tarballs, Windows WIM images, and tool container images. This eliminates the need for external registries in air-gapped or isolated environments.

## Overview

The embedded registry:
- Implements the OCI Distribution API (`/v2/*`)
- Supports artifact push/pull via `oras` and `podman` clients
- Provides content-addressable blob storage with automatic deduplication
- Includes garbage collection for unreferenced blobs
- Supports optional basic authentication
- Co-exists with the provisioning API on the same HTTP server

## Architecture

```
┌─────────────────────────────────────┐
│   Provisioner Controller (HTTP)     │
├─────────────────────────────────────┤
│  /api/v1/*      → Provisioning API  │
│  /media/tasks/* → Task ISO serving  │
│  /v2/*          → OCI Registry API  │
└─────────────────────────────────────┘
```

### Storage Layout

The registry uses OCI Image Layout on disk (default: `/var/lib/shoal/oci`):

```
/var/lib/shoal/oci/
├── blobs/
│   └── sha256/
│       ├── abc123... (blob content)
│       └── def456... (blob content)
├── index.json (top-level refs)
├── oci-layout (version marker)
└── repositories/
    └── <repo-name>/
        └── refs/
            └── <tag> → manifest digest
```

Blobs are stored once per digest and shared across all repositories, providing automatic deduplication.

## Getting Started

### Prerequisites

- `oras` CLI (for pushing artifacts): https://oras.land/docs/installation
- `podman` (for container images): https://podman.io/docs/installation

### Basic Usage

#### 1. Push a Rootfs Tarball

```bash
# Create or obtain a rootfs tarball
tar czf ubuntu-22.04-rootfs.tar.gz -C /path/to/rootfs .

# Push to embedded registry
oras push controller.example.com:8080/os-images/ubuntu-rootfs:22.04 \
  --artifact-type application/vnd.shoal.rootfs.tar.gz \
  --plain-http \
  ubuntu-22.04-rootfs.tar.gz
```

#### 2. Push a Windows WIM Image

```bash
# Push WIM image (can be large, e.g., 10GB+)
oras push controller.example.com:8080/os-images/windows-server:2022 \
  --artifact-type application/vnd.shoal.windows.wim \
  --plain-http \
  install.wim
```

#### 3. Push a Tool Container Image

```bash
# Build or pull a container image
podman pull docker.io/library/alpine:latest
podman tag alpine:latest controller.example.com:8080/tools/alpine:latest

# Push to embedded registry
podman push --tls-verify=false \
  controller.example.com:8080/tools/alpine:latest
```

## Configuration

The embedded registry is configured via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `ENABLE_REGISTRY` | `true` | Enable embedded registry |
| `REGISTRY_STORAGE` | `/var/lib/shoal/oci` | Storage root directory |
| `REGISTRY_AUTH_MODE` | `none` | Authentication mode: `none`, `basic`, or `htpasswd` |
| `REGISTRY_HTPASSWD_FILE` | | Path to htpasswd file (required if `auth_mode=htpasswd`) |
| `REGISTRY_GC_INTERVAL` | `1h` | Garbage collection interval |
| `REGISTRY_GC_GRACE_PERIOD` | `24h` | Grace period before blob deletion |

### Example with Authentication

Create an htpasswd file:

```bash
htpasswd -Bbn admin secretpassword > /etc/shoal/registry.htpasswd
```

Start controller with auth enabled:

```bash
export ENABLE_REGISTRY=true
export REGISTRY_AUTH_MODE=htpasswd
export REGISTRY_HTPASSWD_FILE=/etc/shoal/registry.htpasswd
./provisioner-controller
```

Push with authentication:

```bash
oras push controller.example.com:8080/os-images/ubuntu:22.04 \
  --plain-http \
  --username admin \
  --password secretpassword \
  ubuntu-rootfs.tar.gz
```

## Integration with Provisioning Workflows

### Linux Provisioning

Reference the embedded registry in your recipe:

```json
{
  "task_target": "install-linux.target",
  "target_disk": "/dev/sda",
  "oci_url": "controller.example.com:8080/os-images/ubuntu-rootfs:22.04",
  "partitions": [
    {"mount": "/boot/efi", "size": "512M", "fstype": "vfat"},
    {"mount": "/", "size": "0", "fstype": "ext4"}
  ],
  "cloud_init": {
    "hostname": "server01.example.com",
    "users": [
      {
        "name": "admin",
        "ssh_authorized_keys": ["ssh-rsa AAAA..."]
      }
    ]
  }
}
```

During provisioning, the maintenance OS will:
1. Pull the artifact from the embedded registry: `oras pull controller.example.com:8080/os-images/ubuntu-rootfs:22.04`
2. Extract the rootfs to the target disk
3. Install the bootloader and apply cloud-init configuration

### Windows Provisioning

```json
{
  "task_target": "install-windows.target",
  "target_disk": "/dev/sda",
  "oci_url": "controller.example.com:8080/os-images/windows-server:2022",
  "partitions": [
    {"mount": "MSR", "size": "128M"},
    {"mount": "C:", "size": "0", "fstype": "ntfs"}
  ],
  "unattend_xml": "..."
}
```

The maintenance OS will:
1. Pull the WIM image from the registry
2. Apply the WIM to the target partition using `wimapply`
3. Configure Windows boot files and UEFI entries
4. Place unattend.xml for automated setup

### Tool Containers in Maintenance OS

The maintenance OS can pull tool containers from the embedded registry for specialized tasks:

```bash
# In maintenance OS Quadlet systemd unit
podman pull --tls-verify=false controller:8080/tools/disk-formatter:latest
podman run --rm --privileged controller:8080/tools/disk-formatter:latest /dev/sda
```

## Garbage Collection

The embedded registry includes automatic garbage collection to remove unreferenced blobs.

### How It Works

1. **Reachability Analysis**: The GC process builds a graph from manifests/tags to blobs
2. **Grace Period**: Unreferenced blobs are marked for deletion but kept for the grace period
3. **Deletion**: After the grace period expires, blobs are permanently deleted

### Manual GC Trigger

Trigger GC manually via the admin endpoint (if enabled):

```bash
curl -X POST http://controller.example.com:8080/admin/gc
```

### Storage Monitoring

Monitor registry storage usage:

```bash
# Check disk usage
du -sh /var/lib/shoal/oci

# View Prometheus metrics
curl http://controller.example.com:9090/metrics | grep registry_storage
```

Metrics exposed:
- `registry_storage_bytes` - Total storage used by blobs
- `registry_blob_count` - Number of blobs stored
- `registry_gc_blobs_deleted` - Blobs deleted by GC
- `registry_gc_duration_seconds` - GC run duration

## Best Practices

### Storage Requirements

- **Deduplication**: Blobs are stored once per digest, shared across repositories
- **Filesystem**: Use XFS or ext4 with large file support
- **Capacity Planning**: Provision 2-5x expected artifact size for deduplication overhead
- **Example**: 10 rootfs (5GB each) + 5 WIMs (8GB each) ≈ 90GB raw, ~60GB deduplicated

### Performance Considerations

- **Large Blobs**: Registry uses streaming I/O to avoid memory buffering
- **Chunked Uploads**: Use PATCH continuation for blobs >1GB
- **Timeouts**: Default upload timeout is 1 hour, download timeout is 30 minutes
- **Concurrency**: Limit concurrent uploads (default: 8) to prevent I/O saturation

### Security Recommendations

- **TLS**: Strongly recommended for production deployments
- **Basic Auth**: Use strong passwords with bcrypt hashing
- **Network Isolation**: Restrict registry access to maintenance OS network
- **Audit Logging**: Enable audit logs for compliance and forensics
- **Secrets**: Never log Authorization headers or passwords

### Backup and Recovery

Backup the OCI storage directory:

```bash
# Snapshot storage during low activity
tar czf oci-backup.tar.gz /var/lib/shoal/oci

# Restore
tar xzf oci-backup.tar.gz -C /
```

For incremental backups, use rsync:

```bash
rsync -av /var/lib/shoal/oci/ backup-host:/backups/shoal-oci/
```

## Troubleshooting

### Common Issues

#### Push fails with "BLOB_UPLOAD_INVALID"

**Cause**: Digest mismatch during upload.

**Solution**: Verify file integrity, check for corruption during transfer.

```bash
# Verify local file digest
sha256sum artifact.tar.gz
```

#### Pull fails with "MANIFEST_UNKNOWN"

**Cause**: Tag or digest doesn't exist in registry.

**Solution**: Verify the tag exists:

```bash
oras discover controller.example.com:8080/os-images/ubuntu-rootfs:22.04 --plain-http
```

#### Storage space exhausted

**Cause**: Too many unreferenced blobs, GC not running.

**Solution**: Trigger manual GC:

```bash
curl -X POST http://controller.example.com:8080/admin/gc
```

Check GC configuration:

```bash
# Verify GC is enabled and running
grep "registry_gc" /var/log/shoal/controller.log
```

#### Authentication failures

**Cause**: Invalid credentials or htpasswd file issues.

**Solution**: Verify htpasswd file format:

```bash
# Test htpasswd file
htpasswd -v /etc/shoal/registry.htpasswd admin
```

Check controller logs for auth errors:

```bash
grep "auth" /var/log/shoal/controller.log
```

### Debug Logging

Enable debug logging for the registry:

```bash
export LOG_LEVEL=debug
./provisioner-controller
```

Debug logs include:
- Blob upload/download events with sizes and digests
- Manifest operations (push/pull/delete)
- GC reachability analysis and deletion events
- Auth decisions (credentials redacted)

## API Reference

The embedded registry implements the OCI Distribution Specification v2. Full API documentation:

https://github.com/opencontainers/distribution-spec/blob/main/spec.md

### Key Endpoints

- `GET /v2/` - Registry ping (health check)
- `GET /v2/<name>/blobs/<digest>` - Download blob
- `HEAD /v2/<name>/blobs/<digest>` - Check blob existence
- `POST /v2/<name>/blobs/uploads/` - Initiate blob upload
- `PATCH /v2/<name>/blobs/uploads/<uuid>` - Upload blob chunk
- `PUT /v2/<name>/blobs/uploads/<uuid>?digest=<digest>` - Finalize blob upload
- `GET /v2/<name>/manifests/<reference>` - Retrieve manifest
- `PUT /v2/<name>/manifests/<reference>` - Push manifest
- `DELETE /v2/<name>/manifests/<reference>` - Delete manifest/tag

## Examples

### CI/CD Integration

Push artifacts from CI pipeline:

```yaml
# GitHub Actions example
- name: Push rootfs to Shoal registry
  run: |
    oras push ${{ secrets.SHOAL_REGISTRY }}/os-images/ubuntu-rootfs:${{ github.sha }} \
      --artifact-type application/vnd.shoal.rootfs.tar.gz \
      --plain-http \
      --username ${{ secrets.REGISTRY_USER }} \
      --password ${{ secrets.REGISTRY_PASS }} \
      ubuntu-rootfs.tar.gz
```

### Automated Build Pipeline

Complete workflow for building and pushing artifacts:

```bash
#!/bin/bash
set -euo pipefail

# Build rootfs
sudo debootstrap --arch=amd64 jammy /tmp/rootfs http://archive.ubuntu.com/ubuntu/
sudo tar czf ubuntu-22.04-rootfs.tar.gz -C /tmp/rootfs .

# Push to registry
oras push controller.example.com:8080/os-images/ubuntu-rootfs:$(date +%Y%m%d) \
  --artifact-type application/vnd.shoal.rootfs.tar.gz \
  --plain-http \
  ubuntu-22.04-rootfs.tar.gz

# Tag as latest
oras tag controller.example.com:8080/os-images/ubuntu-rootfs:$(date +%Y%m%d) latest

# Cleanup
sudo rm -rf /tmp/rootfs ubuntu-22.04-rootfs.tar.gz
```

## Support

For issues, questions, or feature requests related to the embedded registry:

- File an issue: https://github.com/mattburns/shoal/issues
- Review design docs: `design/027_Embedded_OCI_Registry.md`
- Check provisioner docs: `docs/provisioner/`
