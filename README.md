# Shoal - Redfish Aggregator

Shoal is a Go-based Redfish aggregator that discovers and manages multiple Baseboard Management Controllers (BMCs) through a single, unified, Redfish‑compliant API. It ships as a single binary with a web UI and REST API for power control and settings discovery.

## Key Features

- Redfish-compliant API and proxy to downstream BMCs
- Aggregates many BMCs into one interface and API
- Web UI for management, status, power control, and users
- Settings discovery for BIOS, NICs, and storage (009)


## Documentation

- Getting Started: [docs/1_getting_started.md](docs/1_getting_started.md)
- Usage Guide: [docs/2_usage.md](docs/2_usage.md)
- API Guide: [docs/3_api.md](docs/3_api.md)
- Development: [docs/4_development.md](docs/4_development.md)
- Deployment & Ops: [docs/5_deployment.md](docs/5_deployment.md)

### Provisioner

The bare-metal provisioner provides automated server provisioning via Redfish and a bootable maintenance OS:

**Status:**
- **Phase 1 (Controller & API):** Complete ✓
- **Phase 2 (Maintenance OS & Dispatcher):** Complete ✓
- **Phase 3 (Linux Workflow):** Complete ✓
  - Partitioning, imaging, bootloader installation, cloud-init
  - End-to-end integration tests and webhook delivery
  - CI for building maintenance ISO
- **Phase 4 (Windows Workflow):** Planned
- **Phase 5 (Embedded OCI Registry):** Complete ✓
  - OCI Distribution API for artifacts and container images
  - Support for oras and podman clients
  - Content-addressable storage with garbage collection
  - Integration with Linux and Windows provisioning workflows
- **Phase 6 (Hardening & ESXi Handoff):** In Progress ⚙
  - ESXi dual-ISO handoff workflow (vendor installer ISO + task.iso with /ks.cfg)
  - Redfish operation robustness (timeouts, retries, idempotency)
  - Expanded test strategy & operational docs

**Documentation:**
- Architecture: `design/020_Provisioner_Architecture.md`
- Controller: `design/021_Provisioner_Controller_Service.md`
- Recipe Schema & Validation: `design/022_Recipe_Schema_and_Validation.md`
- ESXi Workflow: `design/031_Workflow_ESXi.md` and `docs/provisioner/esxi_handoff.md`
- Linux Workflow: `design/029_Workflow_Linux.md`
- Redfish Operations Hardening: `design/028_Redfish_Operations.md`
- Registry: `docs/provisioner/embedded_registry.md`
- Plans: `plans/003_Phase_5_Provisioner_Plan.md`, `plans/004_Phase_6_Provisioner_Plan.md`
- Webhook Examples: `docs/webhook_examples/`

Fixture samples for quick starts live in `docs/provisioner/fixtures/` (cloud-init `user-data.yaml`, ESXi `ks.cfg`, Windows `unattend.xml`). See the overview below.

## Quick Start

```bash
# Build from source
git clone https://github.com/mattcburns/shoal.git
cd shoal
go run build.go validate
./build/shoal
```

Open http://localhost:8080 and log in with `admin` / `admin` (change immediately).

### Try it quick previews

- Overview: `docs/provisioner/try_it_overview.md`
- Linux workflow: `docs/provisioner/try_it_linux.md`
- ESXi handoff (Phase 6): `docs/provisioner/try_it_esxi.md`
- Windows workflow (planned): `docs/provisioner/try_it_windows.md`

Alternatively, download a prebuilt binary from [Releases](https://github.com/mattcburns/shoal/releases), make it executable, and run it.

## License

AGPL-3.0. See `LICENSE`.
