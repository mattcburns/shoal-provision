# Shoal - Redfish Aggregator

Shoal is a Go-based Redfish aggregator that discovers and manages multiple Baseboard Management Controllers (BMCs) through a single, unified, Redfish‑compliant API. It ships as a single binary with a web UI and REST API for power control, settings discovery, configuration profiles, and auditing.

## Key Features

- Redfish-compliant API and proxy to downstream BMCs
- Aggregates many BMCs into one interface and API
- Web UI for management, status, power control, and users
- Settings discovery for BIOS, NICs, and storage (009)
- Configuration Profiles to snapshot, diff, and apply settings
- Audit logging of proxied requests and key actions

## Documentation

- Getting Started: [docs/1_getting_started.md](docs/1_getting_started.md)
- Usage Guide: [docs/2_usage.md](docs/2_usage.md)
- API Guide: [docs/3_api.md](docs/3_api.md)
- Development: [docs/4_development.md](docs/4_development.md)
- Deployment & Ops: [docs/5_deployment.md](docs/5_deployment.md)
- Audit Logging: [docs/6_auditing.md](docs/6_auditing.md)

## Quick Start

```bash
# Build from source
git clone https://github.com/mattcburns/shoal.git
cd shoal
python3 build.py validate
./build/shoal
```

Open http://localhost:8080 and log in with `admin` / `admin` (change immediately).

Alternatively, download a prebuilt binary from [Releases](https://github.com/mattcburns/shoal/releases), make it executable, and run it.

## License

AGPL-3.0. See `LICENSE`.
