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

package plan

import "testing"

func TestQuote(t *testing.T) {
	cases := map[string]string{
		"simple":      "simple",
		"needs space": "'needs space'",
		"d'quote":     "'d'\\''quote'",
	}

	for input, want := range cases {
		if got := Quote(input); got != want {
			t.Fatalf("Quote(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCommandShell(t *testing.T) {
	cmd := Command{Program: "sgdisk", Args: []string{"-n", "1:0:+512M", "/dev/sda"}}
	if got := cmd.Shell(); got != "sgdisk -n 1:0:+512M /dev/sda" {
		t.Fatalf("unexpected shell: %s", got)
	}

	cmd = Command{Program: "bash", Args: []string{"-c", "echo hello world"}}
	if got := cmd.Shell(); got != "bash -c 'echo hello world'" {
		t.Fatalf("unexpected shell with quoting: %s", got)
	}
}
