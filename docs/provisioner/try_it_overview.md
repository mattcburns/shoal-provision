# Provisioner Quickstart Overview

This consolidated quickstart links to the detailed workflow guides and provides canonical fixture references. Use this as an entry point for experimenting with Shoal provisioning workflows (Linux, ESXi, Windows preview).

## Fixtures
Sample fixture files (these are embedded into the task ISO depending on recipe contents):

- `fixtures/user-data.yaml` (Linux cloud-init)
- `fixtures/ks.cfg` (ESXi Kickstart; must be present for ESXi workflow)
- `fixtures/unattend.xml` (Windows unattended install preview)

Copy or adapt them when forming recipe JSON. Current size limit for `ks.cfg` is < 64KiB.

## Workflow Guides
Refer to per‑workflow instructions for environment variables, flags, and step-by-step mount & boot orchestration:

- [Linux Workflow](try_it_linux.md)
- [ESXi Dual-ISO Workflow](try_it_esxi.md)
- [Windows Preview Workflow](try_it_windows.md)

## Recipe Structure (Conceptual)
Each provisioning request will POST a JSON recipe describing:

```json
{
  "system_uuid": "<bmc-uuid>",
  "workflow": "linux|esxi|windows-preview",
  "assets": {
    "user_data": "#cloud-config...",           // for linux
    "ks_cfg": "vmaccepteula...",               // for esxi (required)
    "unattend_xml": "<?xml version=...>"        // for windows preview
  }
}
```

Additional knobs will be introduced (disk layout, RAID, boot order overrides, etc.) as future milestones land.

## API Status
The controller currently runs workers that look for queued jobs (future endpoint). Until the job submission API is implemented, use the guides to simulate provisioning logic by adjusting environment and running targeted unit tests.

## Next Steps
- Implement job submission REST endpoint (`POST /api/v1/provisioner/jobs`) accepting the recipe above.
- Extend task ISO builder to include checksum manifest & reproducible timestamps.
- Add validation layer for asset size + schema.

## Troubleshooting
- Missing `ks_cfg` in an ESXi recipe will fail early in the `validate-recipe` step.
- Oversized Kickstart (>64KiB) will also cause validation failure.
- Use `REDfISH_MODE=noop` to run tests without hardware.

## License
All fixture samples are provided under the project AGPLv3; adapt as needed.
