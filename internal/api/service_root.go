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
	"net/http"

	"shoal/pkg/redfish"
)

// handleServiceRoot returns the Redfish service root.
// Moved into its own file per design 019; behavior is unchanged.
func (h *Handler) handleServiceRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		h.writeAllow(w, http.MethodGet)
		return
	}
	if r.Method != http.MethodGet {
		h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
		return
	}

	// Fetch or generate a stable service UUID
	uuid, _ := h.db.EnsureServiceUUID(r.Context())
	serviceRoot := redfish.ServiceRoot{
		ODataContext:   "/redfish/v1/$metadata#ServiceRoot.ServiceRoot",
		ODataID:        "/redfish/v1/",
		ODataType:      "#ServiceRoot.v1_5_0.ServiceRoot",
		ID:             "RootService",
		Name:           "Shoal Redfish Aggregator",
		RedfishVersion: "1.6.0",
		UUID:           uuid,
		Links: redfish.ServiceRootLinks{
			Sessions: redfish.ODataIDRef{ODataID: "/redfish/v1/SessionService/Sessions"},
		},
		Systems:            redfish.ODataIDRef{ODataID: "/redfish/v1/Systems"},
		Managers:           redfish.ODataIDRef{ODataID: "/redfish/v1/Managers"},
		SessionService:     redfish.ODataIDRef{ODataID: "/redfish/v1/SessionService"},
		AggregationService: &redfish.ODataIDRef{ODataID: "/redfish/v1/AggregationService"},
	}

	// Compliance navigation links (already implemented)
	serviceRoot.Registries = &redfish.ODataIDRef{ODataID: "/redfish/v1/Registries"}
	serviceRoot.JsonSchemas = &redfish.ODataIDRef{ODataID: "/redfish/v1/SchemaStore"}
	serviceRoot.AccountService = &redfish.ODataIDRef{ODataID: "/redfish/v1/AccountService"}
	serviceRoot.EventService = &redfish.ODataIDRef{ODataID: "/redfish/v1/EventService"}
	serviceRoot.TaskService = &redfish.ODataIDRef{ODataID: "/redfish/v1/TaskService"}

	h.writeJSONResponse(w, http.StatusOK, serviceRoot)
}
