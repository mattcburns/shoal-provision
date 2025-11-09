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

import (
	"strings"
	"time"
)

// Quirks captures vendor-specific behaviors and field/value differences.
type Quirks struct {
	// BootTargetMap maps canonical targets (Cd, Pxe, Hdd, Usb) to vendor-specific values.
	BootTargetMap map[string]string
	// VirtualMediaPreference is a list of substring hints to prefer in member Id/MediaTypes.
	VirtualMediaPreference []string
	// InsertAction overrides the default Insert action name when vendors deviate.
	InsertAction string // e.g., "VirtualMedia.InsertMedia" (default)
	// EjectAction overrides the default Eject action name when vendors deviate.
	EjectAction string // e.g., "VirtualMedia.EjectMedia" (default)
	// RequiresWriteProtected requests WriteProtected=true on insert for this vendor.
	RequiresWriteProtected bool
	// DelayAfterInsert requests a small delay after successful insert before next step.
	DelayAfterInsert time.Duration
}

func (q *Quirks) mapBootTarget(target string) string {
	if q == nil || len(q.BootTargetMap) == 0 {
		return target
	}
	// Normalize canonical key capitalization
	key := target
	if m, ok := q.BootTargetMap[key]; ok && m != "" {
		return m
	}
	// Try case-insensitive
	for k, v := range q.BootTargetMap {
		if strings.EqualFold(k, target) && v != "" {
			return v
		}
	}
	return target
}

func getQuirks(vendor string) *Quirks {
	v := strings.ToLower(strings.TrimSpace(vendor))
	// Defaults
	q := &Quirks{
		BootTargetMap: map[string]string{
			"Cd":  "Cd",
			"Pxe": "Pxe",
			"Hdd": "Hdd",
			"Usb": "Usb",
		},
		VirtualMediaPreference: []string{"cd", "dvd"},
		InsertAction:           "VirtualMedia.InsertMedia",
		EjectAction:            "VirtualMedia.EjectMedia",
		RequiresWriteProtected: false,
		DelayAfterInsert:       0,
	}

	switch {
	case strings.Contains(v, "dell") || strings.Contains(v, "idrac"):
		// Dell iDRAC tends to use standard targets; sometimes needs slight delay after insert.
		q.DelayAfterInsert = 500 * time.Millisecond
	case strings.Contains(v, "hewlett") || strings.Contains(v, "hpe") || strings.Contains(v, "ilo"):
		// HPE iLO is close to spec; prefer cd hints
		q.VirtualMediaPreference = []string{"cd", "dvd"}
	case strings.Contains(v, "supermicro"):
		// Some Supermicro models expose USB-CD style; map canonical Cd to Cd if unknown.
		q.BootTargetMap["Cd"] = "Cd"
		q.VirtualMediaPreference = []string{"cd", "usb", "dvd"}
	case strings.Contains(v, "lenovo"):
		// Lenovo XCC tends to be spec-compliant; keep defaults.
	case strings.Contains(v, "acme"):
		// Example vendor mapping to exercise tests; map Cd -> UsbCd
		q.BootTargetMap["Cd"] = "UsbCd"
		q.VirtualMediaPreference = []string{"usb", "cd"}
	}
	return q
}
