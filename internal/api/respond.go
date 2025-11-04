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
	"log/slog"
	"net/http"
	"strings"
)

// NOTE: These helpers were extracted as part of design 019 to centralize
// response handling for JSON, headers, and Allow. They intentionally use
// rf*-prefixed names to avoid symbol conflicts while call sites are migrated.

// rfWriteJSONResponse writes a JSON response with standard headers applied.
func rfWriteJSONResponse(w http.ResponseWriter, status int, data interface{}) {
	rfWriteJSONResponseWithETag(w, nil, status, data, "")
}

// rfWriteJSONResponseWithETag writes a JSON response and optionally applies
// a provided ETag. If an ETag is provided and the request's If-None-Match
// matches it, a 304 is returned and no body is written.
// To preserve existing behavior during migration, this helper does not
// compute an ETag when the input etag is empty.
func rfWriteJSONResponseWithETag(w http.ResponseWriter, r *http.Request, status int, data interface{}, etag string) {
	// When a strong/weak ETag is provided and the request supplies If-None-Match,
	// short-circuit on 304 Not Modified.
	if r != nil && etag != "" {
		if inm := r.Header.Get("If-None-Match"); strings.TrimSpace(inm) != "" {
			if rfIfNoneMatchMatches(inm, etag) {
				w.Header().Set("ETag", etag)
				// Maintain OData-Version consistency even for 304 responses.
				w.Header().Set("OData-Version", "4.0")
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
	}

	body, err := json.Marshal(data)
	if err != nil {
		slog.Error("Failed to marshal JSON response", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if etag != "" {
		w.Header().Set("ETag", etag)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("OData-Version", "4.0")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		slog.Warn("Failed to write JSON response body", "error", err)
	}
}

// rfWriteAllow responds to an HTTP OPTIONS request by advertising allowed methods,
// deduplicating while preserving order. It also applies OData-Version for consistency.
func rfWriteAllow(w http.ResponseWriter, methods ...string) {
	// Deduplicate while preserving order.
	seen := make(map[string]bool, len(methods))
	ordered := make([]string, 0, len(methods))
	for _, m := range methods {
		if !seen[m] {
			seen[m] = true
			ordered = append(ordered, m)
		}
	}
	w.Header().Set("Allow", strings.Join(ordered, ", "))
	// Maintain OData header consistency even for 204.
	w.Header().Set("OData-Version", "4.0")
	w.WriteHeader(http.StatusNoContent)
}
