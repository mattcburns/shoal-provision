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
	"strings"

	"shoal/pkg/models"
	"shoal/pkg/redfish"
)

// handleAggregationService handles aggregation service endpoints.
// Extracted from api.go per design 019. Behavior is unchanged.
func (h *Handler) handleAggregationService(w http.ResponseWriter, r *http.Request, path string, user *models.User) {
	// Remove /v1/AggregationService prefix
	subPath := strings.TrimPrefix(path, "/v1/AggregationService")

	if subPath == "" || subPath == "/" {
		// Handle AggregationService root
		if r.Method == http.MethodOptions {
			h.writeAllow(w, http.MethodGet)
			return
		}
		if r.Method != http.MethodGet {
			h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
			return
		}

		aggService := redfish.AggregationService{
			ODataContext: "/redfish/v1/$metadata#AggregationService.AggregationService",
			ODataID:      "/redfish/v1/AggregationService",
			ODataType:    "#AggregationService.v1_0_0.AggregationService",
			ID:           "AggregationService",
			Name:         "Aggregation Service",
			Description:  "BMC Aggregation Service",
			ConnectionMethods: redfish.ODataIDRef{
				ODataID: "/redfish/v1/AggregationService/ConnectionMethods",
			},
		}

		h.writeJSONResponse(w, http.StatusOK, aggService)
		return
	}

	if subPath == "/ConnectionMethods" || subPath == "/ConnectionMethods/" {
		// Handle ConnectionMethods collection
		if r.Method == http.MethodOptions {
			h.writeAllow(w, http.MethodGet, http.MethodPost)
			return
		}
		h.handleConnectionMethodsCollection(w, r, user)
		return
	}

	if strings.HasPrefix(subPath, "/ConnectionMethods/") {
		// Handle individual ConnectionMethod
		parts := strings.Split(strings.TrimPrefix(subPath, "/ConnectionMethods/"), "/")
		if len(parts) == 1 && parts[0] != "" {
			h.handleConnectionMethod(w, r, parts[0], user)
			return
		}
	}

	h.writeErrorResponse(w, http.StatusNotFound, "Base.1.0.ResourceNotFound", "Resource not found")
}

// handleConnectionMethodsCollection handles the ConnectionMethods collection.
// Extracted from api.go per design 019. Behavior is unchanged.
func (h *Handler) handleConnectionMethodsCollection(w http.ResponseWriter, r *http.Request, user *models.User) {
	if r.Method == http.MethodOptions {
		h.writeAllow(w, http.MethodGet, http.MethodPost)
		return
	}
	switch r.Method {
	case http.MethodGet:
		// Get all connection methods
		methods, err := h.bmcSvc.GetConnectionMethods(r.Context())
		if err != nil {
			slog.Error("Failed to get connection methods", "error", err)
			h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to retrieve connection methods")
			return
		}

		var members []redfish.ODataIDRef
		for _, method := range methods {
			members = append(members, redfish.ODataIDRef{
				ODataID: fmt.Sprintf("/redfish/v1/AggregationService/ConnectionMethods/%s", method.ID),
			})
		}

		collection := redfish.Collection{
			ODataContext: "/redfish/v1/$metadata#ConnectionMethodCollection.ConnectionMethodCollection",
			ODataID:      "/redfish/v1/AggregationService/ConnectionMethods",
			ODataType:    "#ConnectionMethodCollection.ConnectionMethodCollection",
			Name:         "Connection Method Collection",
			Members:      members,
			MembersCount: len(members),
		}

		etag := connectionMethodsCollectionETag(methods)
		if match := r.Header.Get("If-None-Match"); match != "" && ifNoneMatchMatches(match, etag) {
			w.Header().Set("ETag", etag)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		h.writeJSONResponseWithETag(w, http.StatusOK, collection, etag)

	case http.MethodPost:
		// Create a new connection method
		var req struct {
			Name                 string `json:"Name"`
			ConnectionMethodType string `json:"ConnectionMethodType"`
			Address              string `json:"ConnectionMethodVariant.Address"`
			Authentication       struct {
				Username string `json:"Username"`
				Password string `json:"Password"`
			} `json:"ConnectionMethodVariant.Authentication"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.writeErrorResponse(w, http.StatusBadRequest, "Base.1.0.MalformedJSON", "Invalid JSON in request body")
			return
		}

		// Validate required fields
		if req.Name == "" || req.Address == "" || req.Authentication.Username == "" || req.Authentication.Password == "" {
			h.writeErrorResponse(w, http.StatusBadRequest, "Base.1.0.PropertyMissing", "Required properties missing")
			return
		}

		// Default to Redfish if not specified
		if req.ConnectionMethodType == "" {
			req.ConnectionMethodType = "Redfish"
		}

		// Create the connection method
		method, err := h.bmcSvc.AddConnectionMethod(
			r.Context(),
			req.Name,
			req.Address,
			req.Authentication.Username,
			req.Authentication.Password,
		)
		if err != nil {
			slog.Error("Failed to create connection method", "error", err)
			if strings.Contains(err.Error(), "failed to connect") {
				h.writeErrorResponse(w, http.StatusBadRequest, "Base.1.0.ResourceCannotBeCreated", "Unable to connect to BMC")
			} else {
				h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to create connection method")
			}
			return
		}

		// Return the created connection method
		connMethod := redfish.ConnectionMethod{
			ODataContext:         "/redfish/v1/$metadata#ConnectionMethod.ConnectionMethod",
			ODataID:              fmt.Sprintf("/redfish/v1/AggregationService/ConnectionMethods/%s", method.ID),
			ODataType:            "#ConnectionMethod.v1_0_0.ConnectionMethod",
			ID:                   method.ID,
			Name:                 method.Name,
			ConnectionMethodType: method.ConnectionMethodType,
			ConnectionMethodVariant: redfish.ConnectionMethodVariant{
				ODataType: "#ConnectionMethod.v1_0_0.ConnectionMethodVariant",
				Address:   method.Address,
			},
		}

		etag := connectionMethodETag(method)
		w.Header().Set("Location", fmt.Sprintf("/redfish/v1/AggregationService/ConnectionMethods/%s", method.ID))
		h.writeJSONResponseWithETag(w, http.StatusCreated, connMethod, etag)

	default:
		h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
	}
}

// handleConnectionMethod handles a specific ConnectionMethod resource.
// Extracted from api.go per design 019. Behavior is unchanged.
func (h *Handler) handleConnectionMethod(w http.ResponseWriter, r *http.Request, id string, user *models.User) {
	if r.Method == http.MethodOptions {
		h.writeAllow(w, http.MethodGet, http.MethodDelete)
		return
	}
	switch r.Method {
	case http.MethodGet:
		method, err := h.bmcSvc.GetConnectionMethod(r.Context(), id)
		if err != nil {
			slog.Error("Failed to get connection method", "id", id, "error", err)
			h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to retrieve connection method")
			return
		}
		if method == nil {
			h.writeErrorResponse(w, http.StatusNotFound, "Base.1.0.ResourceNotFound", "Connection method not found")
			return
		}

		connMethod := redfish.ConnectionMethod{
			ODataContext:         "/redfish/v1/$metadata#ConnectionMethod.ConnectionMethod",
			ODataID:              fmt.Sprintf("/redfish/v1/AggregationService/ConnectionMethods/%s", method.ID),
			ODataType:            "#ConnectionMethod.v1_0_0.ConnectionMethod",
			ID:                   method.ID,
			Name:                 method.Name,
			ConnectionMethodType: method.ConnectionMethodType,
			ConnectionMethodVariant: redfish.ConnectionMethodVariant{
				ODataType: "#ConnectionMethod.v1_0_0.ConnectionMethodVariant",
				Address:   method.Address,
			},
		}

		etag := connectionMethodETag(method)
		if match := r.Header.Get("If-None-Match"); match != "" && ifNoneMatchMatches(match, etag) {
			w.Header().Set("ETag", etag)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		h.writeJSONResponseWithETag(w, http.StatusOK, connMethod, etag)

	case http.MethodDelete:
		err := h.bmcSvc.RemoveConnectionMethod(r.Context(), id)
		if err != nil {
			slog.Error("Failed to delete connection method", "id", id, "error", err)
			h.writeErrorResponse(w, http.StatusInternalServerError, "Base.1.0.InternalError", "Failed to delete connection method")
			return
		}

		w.WriteHeader(http.StatusNoContent)

	default:
		h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
	}
}
