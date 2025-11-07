// Shoal is a Redfish aggregator service.
// Copyright (C) 2025 Matthew Burns
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package integration_test

import (
	"testing"
)

// TestLinuxWorkflow_EndToEnd is a placeholder for the Phase 3 Linux workflow E2E test.
// It will validate the sequence: partition → image-linux → bootloader-linux → config-drive,
// and verify success/failure webhook behavior as per design/029_Workflow_Linux.md.
//
// NOTE: This test is intentionally skipped until Phase 3 implementation lands. Keeping it
// present ensures we maintain visibility and acceptance criteria alignment without breaking CI.
func TestLinuxWorkflow_EndToEnd(t *testing.T) {
	t.Skip("Phase 3: E2E Linux workflow pending implementation; see design/039_Provisioner_Phase_3_Plan.md")
}
