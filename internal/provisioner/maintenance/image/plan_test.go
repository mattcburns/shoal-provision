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

package image

import "testing"

func TestPlan(t *testing.T) {
	cmds, err := Plan(Options{OCIURL: "controller:8080/os-images/ubuntu-rootfs:22.04", RootPath: "/mnt/new-root"})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(cmds))
	}
	if got := cmds[0].Shell(); got != "mkdir -p /mnt/new-root" {
		t.Fatalf("unexpected mkdir command: %s", got)
	}
	want := "bash -c 'oras pull controller:8080/os-images/ubuntu-rootfs:22.04 --output - | tar -xpf - -C /mnt/new-root'"
	if got := cmds[1].Shell(); got != want {
		t.Fatalf("unexpected stream command:\n got: %s\nwant: %s", got, want)
	}
}

func TestPlanRequiresURL(t *testing.T) {
	if _, err := Plan(Options{}); err == nil {
		t.Fatalf("expected error for missing url")
	}
}
