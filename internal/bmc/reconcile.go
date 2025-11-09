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
	"context"
	"log/slog"
	"strings"
	"time"
)

// ReconcileState verifies and repairs selected Redfish-managed state on restart.
// Currently covers: ensuring virtual media still mounted if previously set (heuristic) and
// one-time boot override presence if expected. Future: power state expectations.
func (s *Service) ReconcileState(ctx context.Context, bmcName string, expectedImage string, ensureBootOnce bool) error {
	// Attempt minimal queries; tolerate errors.
	bmc, err := s.db.GetBMCByName(ctx, bmcName)
	if err != nil || bmc == nil || !bmc.Enabled {
		return err
	}
	// Virtual media: if expectedImage provided and not mounted, reinsert without changing write-protect settings.
	if expectedImage != "" {
		managerID, merr := s.GetFirstManagerID(ctx, bmcName)
		if merr == nil && managerID != "" {
			slotOID, serr := s.findVirtualMediaSlot(ctx, bmc, managerID)
			if serr == nil && slotOID != "" {
				vmRes, ferr := s.fetchRedfishResource(ctx, bmc, slotOID)
				if ferr == nil && vmRes != nil {
					curImg, _ := vmRes["Image"].(string)
					inserted, _ := vmRes["Inserted"].(bool)
					if !(inserted && curImg == expectedImage) {
						slog.Info("reconcile: reinserting virtual media", "bmc", bmcName, "image", expectedImage)
						// Ignore bootOnce here; caller can set ensureBootOnce
						_ = s.InsertVirtualMedia(ctx, bmcName, expectedImage, false, false)
						// small delay to allow mount before boot override
						timer := time.NewTimer(300 * time.Millisecond)
						select {
						case <-ctx.Done():
							timer.Stop()
							return ctx.Err()
						case <-timer.C:
							// continue
						}
					}
				}
			}
		}
	}
	// Boot override: if ensureBootOnce and not already set Once->Cd (canonical), set it again.
	if ensureBootOnce {
		systemID, serr := s.GetFirstSystemID(ctx, bmcName)
		if serr == nil && systemID != "" {
			sysPath := "/redfish/v1/Systems/" + systemID
			if cur, err := s.fetchRedfishResource(ctx, bmc, sysPath); err == nil && cur != nil {
				if boot, ok := cur["Boot"].(map[string]interface{}); ok {
					enabled, _ := boot["BootSourceOverrideEnabled"].(string)
					target, _ := boot["BootSourceOverrideTarget"].(string)
					if !(strings.EqualFold(enabled, "Once") && strings.EqualFold(target, "Cd")) {
						slog.Info("reconcile: reapplying one-time boot override", "bmc", bmcName)
						_ = s.SetOneTimeBoot(ctx, bmcName, "Cd")
					}
				}
			}
		}
	}
	return nil
}
