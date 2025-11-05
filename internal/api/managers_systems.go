/*
Shoal is a Redfish aggregator service.
Copyright (C) 2025  Matthew Burns

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"shoal/pkg/redfish"
)

// handleManagersCollection returns the list of managed BMCs as managers.
// Extracted from api.go per design 019. Behavior is unchanged.
func (h *Handler) handleManagersCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		h.writeAllow(w, http.MethodGet)
		return
	}
	if r.Method != http.MethodGet {
		h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
		return
	}

	// Get both regular BMCs and ConnectionMethods
	bmcs, err := h.db.GetBMCs(r.Context())
	if err != nil {
		slog.Error("Failed to get BMCs", "error", err)
		h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to retrieve BMCs")
		return
	}

	methods, err := h.bmcSvc.GetConnectionMethods(r.Context())
	if err != nil {
		slog.Error("Failed to get connection methods", "error", err)
		// Don't fail entirely if we can't get connection methods
	}

	var members []redfish.ODataIDRef

	// Add regular BMCs
	for _, bmc := range bmcs {
		if bmc.Enabled {
			members = append(members, redfish.ODataIDRef{
				ODataID: fmt.Sprintf("/redfish/v1/Managers/%s", bmc.Name),
			})
		}
	}

	// Add aggregated managers from ConnectionMethods
	for _, method := range methods {
		if method.Enabled && method.AggregatedManagers != "" {
			// Parse the aggregated managers JSON
			var managers []map[string]interface{}
			if err := json.Unmarshal([]byte(method.AggregatedManagers), &managers); err == nil {
				for _, manager := range managers {
					if odataID, ok := manager["@odata.id"].(string); ok {
						// Prefix with the connection method ID to make it unique
						modifiedID := fmt.Sprintf("/redfish/v1/Managers/%s%s", method.ID, odataID)
						members = append(members, redfish.ODataIDRef{
							ODataID: modifiedID,
						})
					}
				}
			}
		}
	}

	collection := redfish.Collection{
		ODataContext: "/redfish/v1/$metadata#ManagerCollection.ManagerCollection",
		ODataID:      "/redfish/v1/Managers",
		ODataType:    "#ManagerCollection.ManagerCollection",
		Name:         "Manager Collection",
		Members:      members,
		MembersCount: len(members),
	}

	h.writeJSONResponse(w, http.StatusOK, collection)
}

// handleSystemsCollection returns the list of systems from managed BMCs.
// Extracted from api.go per design 019. Behavior is unchanged.
func (h *Handler) handleSystemsCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		h.writeAllow(w, http.MethodGet)
		return
	}
	if r.Method != http.MethodGet {
		h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
		return
	}

	// Get both regular BMCs and ConnectionMethods
	bmcs, err := h.db.GetBMCs(r.Context())
	if err != nil {
		slog.Error("Failed to get BMCs", "error", err)
		h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to retrieve BMCs")
		return
	}

	methods, err := h.bmcSvc.GetConnectionMethods(r.Context())
	if err != nil {
		slog.Error("Failed to get connection methods", "error", err)
		// Don't fail entirely if we can't get connection methods
	}

	var members []redfish.ODataIDRef

	// Add regular BMCs
	for _, bmc := range bmcs {
		if bmc.Enabled {
			members = append(members, redfish.ODataIDRef{
				ODataID: fmt.Sprintf("/redfish/v1/Systems/%s", bmc.Name),
			})
		}
	}

	// Add aggregated systems from ConnectionMethods
	for _, method := range methods {
		if method.Enabled && method.AggregatedSystems != "" {
			// Parse the aggregated systems JSON
			var systems []map[string]interface{}
			if err := json.Unmarshal([]byte(method.AggregatedSystems), &systems); err == nil {
				for _, system := range systems {
					if odataID, ok := system["@odata.id"].(string); ok {
						// Prefix with the connection method ID to make it unique
						modifiedID := fmt.Sprintf("/redfish/v1/Systems/%s%s", method.ID, odataID)
						members = append(members, redfish.ODataIDRef{
							ODataID: modifiedID,
						})
					}
				}
			}
		}
	}

	collection := redfish.Collection{
		ODataContext: "/redfish/v1/$metadata#ComputerSystemCollection.ComputerSystemCollection",
		ODataID:      "/redfish/v1/Systems",
		ODataType:    "#ComputerSystemCollection.ComputerSystemCollection",
		Name:         "Computer System Collection",
		Members:      members,
		MembersCount: len(members),
	}

	h.writeJSONResponse(w, http.StatusOK, collection)
}
