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
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"shoal/internal/assets"
	"shoal/pkg/redfish"
)

// handleMetadata serves the OData $metadata CSDL. For Phase 1, return a minimal static shell.
// Moved into its own file per design 019; behavior is unchanged.
func (h *Handler) handleMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		rfWriteAllow(w, http.MethodGet)
		return
	}
	if r.Method != http.MethodGet {
		h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("OData-Version", "4.0")
	// Try to serve embedded metadata.xml from assets; fallback to minimal shell
	staticFS := assets.GetStaticFS()
	if data, err := fs.ReadFile(staticFS, "metadata.xml"); err == nil {
		etag := rfComputeETag(data)
		if match := r.Header.Get("If-None-Match"); match != "" && rfIfNoneMatchMatches(match, etag) {
			w.Header().Set("ETag", etag)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}
	// Fallback minimal CSDL skeleton aligning to entities we expose
	const csdl = `<?xml version="1.0" encoding="UTF-8"?>
<edmx:Edmx Version="4.0" xmlns:edmx="http://docs.oasis-open.org/odata/ns/edmx">
	<edmx:DataServices>
		<Schema Namespace="ServiceRoot" xmlns="http://docs.oasis-open.org/odata/ns/edm">
			@EntityType Name="ServiceRoot">
				<Key><PropertyRef Name="Id"/></Key>
				<Property Name="Id" Type="Edm.String" Nullable="false"/>
			</EntityType>
			<EntityContainer Name="ServiceContainer">
				<EntitySet Name="ServiceRoot" EntityType="ServiceRoot.ServiceRoot"/>
			</EntityContainer>
		</Schema>
	</edmx:DataServices>
</edmx:Edmx>`
	// Fallback content also gets an ETag
	fb := []byte(csdl)
	etag := rfComputeETag(fb)
	if match := r.Header.Get("If-None-Match"); match != "" && rfIfNoneMatchMatches(match, etag) {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", etag)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(fb)
}

// handleRegistriesCollection lists available message registries (minimal: Base)
// Moved into its own file per design 019; behavior is unchanged.
func (h *Handler) handleRegistriesCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		rfWriteAllow(w, http.MethodGet)
		return
	}
	if r.Method != http.MethodGet {
		h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
		return
	}
	// Discover embedded registry files under redfish/ directory
	staticFS := assets.GetStaticFS()
	var members []redfish.ODataIDRef
	if err := fs.WalkDir(staticFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasPrefix(path, "redfish/") && strings.HasSuffix(strings.ToLower(path), ".json") {
			name := strings.TrimSuffix(strings.TrimPrefix(path, "redfish/"), ".json")
			// Only include top-level registry files (e.g., Base.json)
			if !strings.Contains(name, "/") {
				members = append(members, redfish.ODataIDRef{ODataID: "/redfish/v1/Registries/" + name})
			}
		}
		return nil
	}); err != nil {
		slog.Error("Failed to walk registries directory", "error", err)
		// Continue with empty members list rather than failing the request
	}
	coll := redfish.Collection{
		ODataContext: "/redfish/v1/$metadata#MessageRegistryFileCollection.MessageRegistryFileCollection",
		ODataID:      "/redfish/v1/Registries",
		ODataType:    "#MessageRegistryFileCollection.MessageRegistryFileCollection",
		Name:         "Message Registry File Collection",
		Members:      members,
		MembersCount: len(members),
	}
	h.writeJSONResponse(w, http.StatusOK, coll)
}

// handleRegistryFile serves individual registry files from embedded assets.
// Moved into its own file per design 019; behavior is unchanged.
func (h *Handler) handleRegistryFile(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		rfWriteAllow(w, http.MethodGet)
		return
	}
	if r.Method != http.MethodGet {
		h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
		return
	}
	// Expect paths like /redfish/v1/Registries/Base or /redfish/v1/Registries/<Name>/<file>
	name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/redfish/v1/Registries/"), "/")
	if name == "" {
		h.writeErrorResponse(w, http.StatusNotFound, "Base.1.0.ResourceNotFound", "Registry not found")
		return
	}
	// If requesting just the registry name, serve the JSON file from embedded FS
	// Map name -> static/redfish/<name>.json
	filePath := "redfish/" + name + ".json"
	staticFS := assets.GetStaticFS()
	data, err := fs.ReadFile(staticFS, filePath)
	if err == nil {
		etag := rfComputeETag(data)
		// Use centralized JSON writer for consistent headers and conditional handling
		rfWriteJSONResponseWithETag(w, r, http.StatusOK, json.RawMessage(data), etag)
		return
	}
	// Support nested paths like /Registries/Base/Base.json
	if strings.Contains(name, "/") {
		p := "redfish/" + name
		if d, err2 := fs.ReadFile(staticFS, p); err2 == nil {
			etag := rfComputeETag(d)
			rfWriteJSONResponseWithETag(w, r, http.StatusOK, json.RawMessage(d), etag)
			return
		}
	}
	h.writeErrorResponse(w, http.StatusNotFound, "Base.1.0.ResourceNotFound", "Registry not found")
}

// handleSchemaStoreRoot returns a placeholder SchemaStore collection listing embedded JSON schemas.
// Moved into its own file per design 019; behavior is unchanged.
func (h *Handler) handleSchemaStoreRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		rfWriteAllow(w, http.MethodGet)
		return
	}
	if r.Method != http.MethodGet {
		h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
		return
	}
	// Discover embedded JSON schemas under schemas/ if present
	staticFS := assets.GetStaticFS()
	var members []redfish.ODataIDRef
	if err := fs.WalkDir(staticFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasPrefix(path, "schemas/") && strings.HasSuffix(strings.ToLower(path), ".json") {
			name := strings.TrimPrefix(path, "schemas/")
			members = append(members, redfish.ODataIDRef{ODataID: "/redfish/v1/SchemaStore/" + name})
		}
		return nil
	}); err != nil {
		slog.Error("Failed to walk schemas directory", "error", err)
		// Continue with empty members list rather than failing the request
	}
	coll := redfish.Collection{
		ODataContext: "/redfish/v1/$metadata#JsonSchemaFileCollection.JsonSchemaFileCollection",
		ODataID:      "/redfish/v1/SchemaStore",
		ODataType:    "#JsonSchemaFileCollection.JsonSchemaFileCollection",
		Name:         "JSON Schema File Collection",
		Members:      members,
		MembersCount: len(members),
	}
	h.writeJSONResponse(w, http.StatusOK, coll)
}

// handleSchemaFile serves individual schema files from embedded assets.
// Moved into its own file per design 019; behavior is unchanged.
func (h *Handler) handleSchemaFile(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		rfWriteAllow(w, http.MethodGet)
		return
	}
	if r.Method != http.MethodGet {
		h.writeErrorResponse(w, http.StatusMethodNotAllowed, "Base.1.0.MethodNotAllowed", "Method not allowed")
		return
	}
	// Serve files from embedded schemas directory
	name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/redfish/v1/SchemaStore/"), "/")
	if name == "" {
		h.writeErrorResponse(w, http.StatusNotFound, "Base.1.0.ResourceNotFound", "Schema not found")
		return
	}
	p := "schemas/" + name
	staticFS := assets.GetStaticFS()
	if data, err := fs.ReadFile(staticFS, p); err == nil {
		etag := rfComputeETag(data)
		rfWriteJSONResponseWithETag(w, r, http.StatusOK, json.RawMessage(data), etag)
		return
	}
	h.writeErrorResponse(w, http.StatusNotFound, "Base.1.0.ResourceNotFound", "Schema not found")
}
