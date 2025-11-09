// Shoal is a Redfish aggregator service.
// Copyright (C) 2025  Matthew Burns
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

package bmc

import "testing"

func TestGetQuirks_MapsBootTargets(t *testing.T) {
	cases := []struct{ vendor, in, expect string }{
		{"Dell iDRAC", "Cd", "Cd"},
		{"AcmeVendor", "Cd", "UsbCd"},
		{"Supermicro", "Cd", "Cd"},
		{"Unknown", "Cd", "Cd"},
	}
	for _, c := range cases {
		q := getQuirks(c.vendor)
		out := q.mapBootTarget(c.in)
		if out != c.expect {
			t.Fatalf("vendor %s: expected %s got %s", c.vendor, c.expect, out)
		}
	}
}

func TestGetQuirks_DefaultsNonEmpty(t *testing.T) {
	q := getQuirks("Unknown")
	if q.InsertAction == "" || q.EjectAction == "" {
		t.Fatalf("expected default action names set")
	}
	if len(q.BootTargetMap) == 0 {
		t.Fatalf("expected boot target map filled")
	}
}
