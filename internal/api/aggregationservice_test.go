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
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"shoal/pkg/models"
)

func TestConnectionMethodsETags(t *testing.T) {
	handler, db := setupTestAPI(t)
	defer func() { _ = db.Close() }()

	adminToken := loginAndGetToken(t, handler, "admin", "admin")

	method := &models.ConnectionMethod{
		ID:                   "cm-test",
		Name:                 "Test Connection Method",
		ConnectionMethodType: "Redfish",
		Address:              "https://example.com",
		Username:             "admin",
		Password:             "secret",
		Enabled:              true,
	}
	if err := db.CreateConnectionMethod(context.Background(), method); err != nil {
		t.Fatalf("failed to seed connection method: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/redfish/v1/AggregationService/ConnectionMethods", nil)
	req.Header.Set("X-Auth-Token", adminToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from connection methods collection, got %d", rec.Code)
	}
	collectionETag := rec.Header().Get("ETag")
	if collectionETag == "" {
		t.Fatalf("expected ETag header on connection methods collection")
	}

	req = httptest.NewRequest(http.MethodGet, "/redfish/v1/AggregationService/ConnectionMethods", nil)
	req.Header.Set("X-Auth-Token", adminToken)
	req.Header.Set("If-None-Match", collectionETag)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("expected 304 when collection ETag matches, got %d", rec.Code)
	}

	resourceURL := "/redfish/v1/AggregationService/ConnectionMethods/" + method.ID
	req = httptest.NewRequest(http.MethodGet, resourceURL, nil)
	req.Header.Set("X-Auth-Token", adminToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when fetching connection method, got %d", rec.Code)
	}
	resourceETag := rec.Header().Get("ETag")
	if resourceETag == "" {
		t.Fatalf("expected ETag header on connection method resource")
	}

	req = httptest.NewRequest(http.MethodGet, resourceURL, nil)
	req.Header.Set("X-Auth-Token", adminToken)
	req.Header.Set("If-None-Match", resourceETag)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("expected 304 when resource ETag matches, got %d", rec.Code)
	}

	time.Sleep(time.Second)

	managers := `[{"@odata.id": "/redfish/v1/Managers/M1"}]`
	systems := `[{"@odata.id": "/redfish/v1/Systems/S1"}]`
	if err := db.UpdateConnectionMethodAggregatedData(context.Background(), method.ID, managers, systems); err != nil {
		t.Fatalf("failed to update aggregated data: %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, resourceURL, nil)
	req.Header.Set("X-Auth-Token", adminToken)
	req.Header.Set("If-None-Match", resourceETag)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when using stale ETag after update, got %d", rec.Code)
	}
	updatedETag := rec.Header().Get("ETag")
	if updatedETag == resourceETag {
		t.Fatalf("expected connection method ETag to change after update")
	}
}
