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

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMetadataEndpoint(t *testing.T) {
	handler, db := setupTestAPI(t)
	defer func() { _ = db.Close() }()

	// $metadata should be accessible without auth and return XML + OData header
	req := httptest.NewRequest(http.MethodGet, "/redfish/v1/$metadata", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from $metadata, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/xml" {
		t.Fatalf("expected Content-Type application/xml, got %q", ct)
	}
	if od := rec.Header().Get("OData-Version"); od != "4.0" {
		t.Fatalf("expected OData-Version 4.0, got %q", od)
	}
	if body := rec.Body.String(); body == "" {
		t.Fatalf("expected non-empty metadata body")
	}
}
