# Maintenance OS Assets (Phase 3)

This directory contains the systemd units, Quadlet container definitions, and wrapper scripts that will be baked into the maintenance OS during Provisioner Phase 3.

```
maintenance/
├── systemd/           # Host systemd units (.service/.target)
├── quadlet/           # Quadlet container definitions (.container)
└── scripts/           # Helper scripts invoked by the units and containers
```

## Packaging expectations

- Scripts are installed to `/opt/shoal/bin/` inside the maintenance OS image.
- Systemd units land under `/etc/systemd/system/`.
- Quadlet files land under `/etc/containers/systemd/` (requires `systemctl daemon-reload`).

## Current status

The wrapper scripts currently operate in dry-run mode. They assert environment prerequisites and emit diagnostic output so we can wire end-to-end orchestration before implementing destructive operations (partitioning, image extraction, bootloader install, config-drive authoring). Subsequent Phase 3 milestones will replace the placeholders with real logic.

See `design/039_Provisioner_Phase_3_Plan.md` for the roadmap and acceptance criteria.
