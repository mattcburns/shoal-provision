# Phase 4: Windows Server Provisioning Workflow

Status: Planned  
Owners: Provisioning Working Group  
Last updated: 2025-11-08

## Summary

Phase 4 implements the end-to-end Windows Server provisioning workflow inside the maintenance OS, building on the foundation established in Phases 1-3. This phase delivers GPT partitioning (EFI/MSR/NTFS), WIM imaging via wimlib, UEFI boot configuration, and unattend.xml placement for automated first-boot configuration.

## References

- `design/020_Provisioner_Architecture.md` - Overall architecture and roadmap
- `design/030_Workflow_Windows.md` - Windows workflow specifications
- `design/026_Systemd_and_Quadlet_Orchestration.md` - Unit orchestration patterns
- `design/032_Error_Handling_and_Webhooks.md` - Error handling and webhooks
- `plans/001_Phase_3_Provisioner_Plan.md` - Phase 3 implementation (Linux reference)

## Scope (Phase 4)

### In Scope

**Windows Workflow Components:**
- `partition.service` (reused from Phase 3 with Windows GPT layout support)
- `image-windows.service` + `image-windows.container` (WIM apply via wimlib)
- `bootloader-windows.service` + `bootloader-windows.container` (UEFI boot config)
- `install-windows.target` (master orchestration target)

**Tooling:**
- WIM imaging: wimlib-imagex / wimapply for applying Windows install images
- NTFS support: ntfs-3g for mounting and filesystem operations
- UEFI boot: efibootmgr for firmware boot entry creation
- Partition support: EFI System Partition (FAT32), Microsoft Reserved (MSR), NTFS

**Integration:**
- Recipe schema extension for Windows-specific fields (unattend.xml, WIM index)
- Dispatcher updates for Windows layout validation
- Webhook reporting with Windows-specific step names

**Testing:**
- Unit tests for Windows planners (WIM, bootloader, unattend placement)
- Integration tests with test WIM artifacts
- Idempotency validation for all steps

### Out of Scope

- BIOS/Legacy boot (UEFI-only, consistent with Phase 3)
- Domain join or advanced post-install customization beyond unattend.xml
- Windows PE-based bootloader containers (future enhancement)
- Secure Boot enablement (future enhancement)
- ESXi workflow (deferred to Phase 5+)
- Embedded OCI registry (deferred to Phase 5+)

## Architecture Overview

### GPT Partition Layout

Windows requires a specific GPT layout:

1. **EFI System Partition (ESP)**
   - Size: 300-512MB
   - Type GUID: `ef00` (EFI System)
   - Format: FAT32 (vfat)
   - Mount: `/mnt/efi`

2. **Microsoft Reserved Partition (MSR)**
   - Size: 16-128MB (typically 16MB)
   - Type GUID: `0c01` (Microsoft Reserved)
   - Format: None (raw, no filesystem)
   - Purpose: Reserved for Windows disk management

3. **Windows Primary Partition**
   - Size: Remaining space (e.g., 100%)
   - Type GUID: `0700` (Microsoft Basic Data)
   - Format: NTFS
   - Mount: `/mnt/new-windows`

### Workflow Steps

```
install-windows.target
├── partition.service (create EFI/MSR/NTFS layout)
├── image-windows.service (apply WIM → NTFS)
└── bootloader-windows.service (UEFI boot + unattend.xml)
    ├── OnSuccess → provision-success.service
    └── OnFailure → provision-failed@%n.service
```

### Data Flow

1. **Dispatcher** validates recipe, writes:
   - `/run/provision/recipe.env` (TARGET_DISK, OCI_URL, etc.)
   - `/run/provision/layout.json` (EFI/MSR/NTFS schema)
   - `/run/provision/unattend.xml` (Windows answer file)

2. **partition.service** creates GPT layout (reuses Phase 3 planner)

3. **image-windows.service** pulls WIM and applies to NTFS:
   ```bash
   oras pull ${OCI_URL} --output - | wimapply - /mnt/new-windows --index=1
   ```

4. **bootloader-windows.service** configures UEFI:
   - Copies Windows boot files from applied image to ESP
   - Creates firmware boot entry via efibootmgr
   - Places unattend.xml at `/Windows/Panther/Unattend.xml`

## Milestones and Deliverables

### 1. Recipe Schema Extension

**Tasks:**
- Extend `recipe.schema.json` with Windows-specific fields:
  - `unattend_xml` (required for Windows, string or base64)
  - `wim_index` (optional, default: 1)
- Add validation for Windows partition layout (EFI/MSR/NTFS)
- Update dispatcher to handle Windows layout types

**Tests:**
- Schema validation accepts valid Windows recipes
- Schema rejects missing unattend.xml for Windows target
- MSR partition type validation (0c01, no format field)

**Acceptance:**
- Valid Windows recipe passes validation
- Invalid layouts return clear error messages

### 2. Partition Planner Extensions

**Tasks:**
- Extend partition planner to support MSR partition type (0c01)
- Handle MSR-specific logic (no mkfs, only sgdisk)
- Update partition tests for EFI/MSR/NTFS layouts

**Tests:**
- Plan generation for EFI/MSR/NTFS layout
- MSR partition created without filesystem
- Idempotency with existing Windows layout

**Files:**
- `internal/provisioner/maintenance/partition/plan.go` (extend)
- `internal/provisioner/maintenance/partition/plan_test.go` (new cases)

**Acceptance:**
- Partition planner emits correct sgdisk/mkfs commands for Windows layout
- MSR partition created without formatting

### 3. WIM Imaging Planner and Wrapper

**Tasks:**
- Implement `internal/provisioner/maintenance/image/windows.go`
- Create `cmd/image-windows-plan/main.go` CLI
- Implement `scripts/image-windows-wrapper.sh`:
  - Mount NTFS via ntfs-3g
  - Pull WIM via oras
  - Apply WIM via wimapply with configurable index
  - Record digest stamp for idempotency
- Create `image-windows.service` and `image-windows.container` Quadlet unit

**Tests:**
- WIM plan generation (oras → wimapply command)
- Index selection logic (default 1, configurable)
- Idempotency via digest stamp
- Error handling for unreachable OCI URL

**Files:**
- `internal/provisioner/maintenance/image/windows.go`
- `internal/provisioner/maintenance/image/windows_test.go`
- `cmd/image-windows-plan/main.go`
- `internal/provisioner/maintenance/scripts/image-windows-wrapper.sh`
- `internal/provisioner/maintenance/systemd/image-windows.service`
- `internal/provisioner/maintenance/quadlet/image-windows.container`

**Acceptance:**
- WIM successfully applied to NTFS partition
- Idempotent re-runs skip application if digest matches
- Clear error messages on failure

### 4. Bootloader and Unattend Configuration

**Tasks:**
- Implement `internal/provisioner/maintenance/bootloader/windows.go`
- Create `cmd/bootloader-windows-plan/main.go` CLI
- Implement `scripts/bootloader-windows-wrapper.sh`:
  - Copy Windows boot files from `/mnt/new-windows/Windows/Boot/EFI` to ESP
  - Create fallback boot path `/EFI/Boot/bootx64.efi`
  - Run efibootmgr to create firmware boot entry
  - Place unattend.xml at `/Windows/Panther/Unattend.xml`
- Create `bootloader-windows.service` systemd unit

**Tests:**
- Boot file copy plan generation
- efibootmgr command generation
- Unattend.xml placement logic
- Idempotency (skip if files match)
- Security: unattend.xml hashed but never logged

**Files:**
- `internal/provisioner/maintenance/bootloader/windows.go`
- `internal/provisioner/maintenance/bootloader/windows_test.go`
- `cmd/bootloader-windows-plan/main.go`
- `internal/provisioner/maintenance/scripts/bootloader-windows-wrapper.sh`
- `internal/provisioner/maintenance/systemd/bootloader-windows.service`

**Acceptance:**
- Windows boot files present on ESP
- Firmware boot entry created pointing to bootmgfw.efi
- Fallback bootx64.efi present
- Unattend.xml placed correctly

### 5. Master Target and Integration

**Tasks:**
- Create `install-windows.target` systemd target
- Wire OnSuccess/OnFailure webhooks
- Update dispatcher to support Windows target
- Add Windows workflow to integration tests

**Files:**
- `internal/provisioner/maintenance/systemd/install-windows.target`
- `internal/provisioner/integration/windows_workflow_integration_test.go`

**Tests:**
- End-to-end Windows workflow (mock WIM)
- Failure attribution for each step
- Webhook delivery on success/failure
- Idempotent re-run validation

**Acceptance:**
- Complete workflow executes partition → image → bootloader
- Failures report precise unit names
- Idempotent operations safe to re-run

### 6. Maintenance OS Updates

**Tasks:**
- Add wimlib-utils to maintenance OS Containerfile
- Add ntfs-3g to maintenance OS Containerfile
- Update maintenance OS build script if needed
- Add Windows tool container images (or pre-bind wimlib)

**Files:**
- `images/maintenance/Containerfile` (add packages)
- `images/maintenance/README.md` (document Windows support)

**Acceptance:**
- Maintenance OS includes wimlib and ntfs-3g
- Build script produces ISO with Windows tooling

### 7. Documentation and Examples

**Tasks:**
- Create Windows recipe example in documentation
- Document Windows-specific partition layout requirements
- Add troubleshooting guide for Windows workflows
- Update provisioner README with Phase 4 status

**Files:**
- `docs/provisioner/windows_provisioning.md` (new)
- `docs/provisioner/recipes/windows_example.json` (new)
- `README.md` (update Phase 4 status)

**Acceptance:**
- Complete Windows recipe example provided
- Documentation covers EFI/MSR/NTFS requirements

### 8. E2E Validation

**Tasks:**
- VM-based test with Windows WIM (test/mock image)
- Validate GPT layout (ef00, 0c01, 0700)
- Verify boot files and firmware entry
- Verify unattend.xml placement
- Test idempotency (re-run workflow)
- Test failure scenarios (missing WIM, bad layout)

**Acceptance:**
- End-to-end Windows provisioning completes successfully
- System boots to Windows (manual validation or boot detection)
- Unattend.xml processed on first boot
- Failures attribute correct unit names

## Acceptance Criteria (Summarized)

All Phase 4 acceptance criteria must be met:

- ✓ End-to-end Windows workflow provisions successfully
- ✓ GPT layout created: EFI (vfat), MSR (raw), Windows (ntfs)
- ✓ WIM applied to NTFS partition via wimapply
- ✓ UEFI boot configured with firmware entry and fallback path
- ✓ Unattend.xml placed at `/Windows/Panther/Unattend.xml`
- ✓ Failure attribution reports precise unit names
- ✓ Idempotent operations (re-runs safe and mostly no-ops)
- ✓ `go run build.go validate` passes with new tests
- ✓ Integration tests cover happy path and failure scenarios
- ✓ Unattend.xml content never logged (security)

## Testing Strategy (Phase 4)

### Unit Tests

**Partition Planner:**
- EFI/MSR/NTFS layout translation
- MSR partition handling (no filesystem)
- Idempotency checks

**WIM Imaging Planner:**
- oras → wimapply command generation
- Index selection logic
- Digest stamp creation

**Bootloader Planner:**
- Boot file copy plan
- efibootmgr command generation
- Unattend.xml path creation
- Content hashing (no logging)

### Integration Tests

**Full Windows Workflow:**
- Dispatcher → partition → image → bootloader
- Success webhook with all metadata
- Failure webhook with unit attribution
- Digest stamp persistence
- Idempotent re-run (fast path)

**Negative Cases:**
- Invalid layout (missing MSR) → partition.service fails
- Unreachable OCI_URL → image-windows.service fails
- Missing unattend.xml → bootloader-windows.service fails
- No EFI partition → bootloader-windows.service fails

### VM/Hardware Tests

- Boot test WIM (small, test image)
- Verify GPT layout post-provision
- Verify boot files on ESP
- Verify firmware boot entry
- Verify unattend.xml presence

## Operational Notes

### WIM Artifacts

- Use test WIM images during development (<500MB)
- Production WIMs can be 4-10GB; adjust timeouts accordingly
- Consider WIM streaming to avoid temp file overhead

### Tooling Requirements

**Maintenance OS packages:**
- `wimlib-utils` (wimapply, wimlib-imagex)
- `ntfs-3g` (NTFS mount support)
- `efibootmgr` (already present from Phase 3)

**Container Images (if using Quadlet):**
- Option 1: Install tools in maintenance OS base image
- Option 2: Create windows-tools container with wimlib + ntfs-3g

### Security Considerations

- **Unattend.xml secrets:** Never log content; only log hash/size
- **Product keys:** Ensure unattend.xml is not persisted in logs or artifacts
- **Administrator passwords:** Use hashed passwords in unattend.xml
- Consider encrypting unattend.xml at rest in controller storage

### Performance Considerations

- **Large WIMs:** 4-10GB typical for Windows Server
  - Use streaming (oras → wimapply pipe)
  - Timeout: 120 minutes for imaging step
- **NTFS mount:** Use ntfs-3g with `compression` and `no_def_opts`
- **CPU:** wimlib is CPU-intensive for decompression

## Risks & Mitigations

### Risk: WIM Format Variations

**Mitigation:** Test with multiple Windows Server versions (2019, 2022, 2025)

### Risk: BCD Store Compatibility

**Mitigation:** Copied boot files include default BCD; Windows finalizes on first boot. Document limitations and consider Windows PE bootloader in future.

### Risk: Large Artifact Timeouts

**Mitigation:** Generous timeouts (120m for imaging); document size limits and performance expectations

### Risk: NTFS Mount Reliability

**Mitigation:** Use ntfs-3g with tested mount options; handle mount failures gracefully

### Risk: Firmware Boot Entry Conflicts

**Mitigation:** efibootmgr handles duplicates; document boot order considerations

## Dependencies

### Phase 3 Components (Reused)

- Partition planner (extend for MSR)
- Dispatcher (extend for Windows)
- Webhook services (reuse)
- Integration test framework (extend)

### New Dependencies

- `wimlib-utils` (AGPLv3-compatible, LGPLv3+)
- `ntfs-3g` (GPL-2.0+, compatible with AGPL)

### Design Documents

- `design/030_Workflow_Windows.md` (primary specification)
- `design/026_Systemd_and_Quadlet_Orchestration.md` (unit patterns)
- `design/022_Recipe_Schema_and_Validation.md` (schema extensions)

## Start Checklist

- [ ] Branch: `feature/provisioner-phase4`
- [ ] Baseline: `go run build.go validate` → PASS on master
- [ ] Designs reviewed: 020, 022, 026, 030, 032
- [ ] Phase 3 components understood (Linux reference)
- [ ] Test WIM artifacts prepared or identified

## Success Metrics

- Complete Windows Server provisioning workflow
- All unit and integration tests passing
- Documentation complete with examples
- Test coverage maintained >50%
- No regression in Phase 3 functionality

## Future Enhancements (Post-Phase 4)

- Windows PE-based bootloader container (alternative to copy + efibootmgr)
- Secure Boot support for Windows
- BitLocker configuration automation
- Driver injection support
- Multiple WIM index selection
- ARM64 Windows support

## Change Log

- v0.1 (2025-11-08): Initial Phase 4 plan for Windows Server provisioning workflow.
