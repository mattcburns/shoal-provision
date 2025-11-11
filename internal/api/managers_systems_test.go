// Shoal is a Redfish aggregator service.
// Copyright (C) 2025 Matthew Burns
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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"shoal/internal/auth"
	"shoal/internal/bmc"
	"shoal/internal/database"
	"shoal/pkg/models"
	"shoal/pkg/redfish"
)

func TestHandleManagersCollection(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Initialize the database schema
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

	authSvc := auth.New(db)
	bmcSvc := bmc.New(db)
	handler := &Handler{
		db:     db,
		auth:   authSvc,
		bmcSvc: bmcSvc,
	}

	ctx := context.Background()

	// Test 1: OPTIONS request
	req := httptest.NewRequest(http.MethodOptions, "/redfish/v1/Managers", nil)
	rec := httptest.NewRecorder()
	handler.handleManagersCollection(rec, req)
	if rec.Code != http.StatusNoContent && rec.Code != http.StatusOK {
		t.Errorf("OPTIONS: expected status 200 or 204, got %d", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != "GET" {
		t.Errorf("OPTIONS: expected Allow header 'GET', got '%s'", allow)
	}

	// Test 2: Invalid method (POST)
	req = httptest.NewRequest(http.MethodPost, "/redfish/v1/Managers", nil)
	rec = httptest.NewRecorder()
	handler.handleManagersCollection(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: expected status 405, got %d", rec.Code)
	}

	// Test 3: GET with no BMCs
	req = httptest.NewRequest(http.MethodGet, "/redfish/v1/Managers", nil)
	rec = httptest.NewRecorder()
	handler.handleManagersCollection(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET (empty): expected status 200, got %d", rec.Code)
	}

	var collection redfish.Collection
	if err := json.NewDecoder(rec.Body).Decode(&collection); err != nil {
		t.Fatalf("GET (empty): failed to decode response: %v", err)
	}
	if collection.MembersCount != 0 {
		t.Errorf("GET (empty): expected 0 members, got %d", collection.MembersCount)
	}

	// Test 4: GET with BMCs
	testBMC := &models.BMC{
		Name:     "test-bmc",
		Address:  "192.168.1.100",
		Username: "admin",
		Password: "password",
		Enabled:  true,
	}
	if err := db.CreateBMC(ctx, testBMC); err != nil {
		t.Fatalf("failed to create test BMC: %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/redfish/v1/Managers", nil)
	rec = httptest.NewRecorder()
	handler.handleManagersCollection(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET (with BMC): expected status 200, got %d", rec.Code)
	}

	if err := json.NewDecoder(rec.Body).Decode(&collection); err != nil {
		t.Fatalf("GET (with BMC): failed to decode response: %v", err)
	}
	if collection.MembersCount != 1 {
		t.Errorf("GET (with BMC): expected 1 member, got %d", collection.MembersCount)
	}
	if len(collection.Members) > 0 && collection.Members[0].ODataID != "/redfish/v1/Managers/test-bmc" {
		t.Errorf("GET (with BMC): unexpected member ID: %s", collection.Members[0].ODataID)
	}

	// Test 5: Disabled BMC should not appear
	disabledBMC := &models.BMC{
		Name:     "disabled-bmc",
		Address:  "192.168.1.101",
		Username: "admin",
		Password: "password",
		Enabled:  false,
	}
	if err := db.CreateBMC(ctx, disabledBMC); err != nil {
		t.Fatalf("failed to create disabled BMC: %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/redfish/v1/Managers", nil)
	rec = httptest.NewRecorder()
	handler.handleManagersCollection(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET (disabled BMC): expected status 200, got %d", rec.Code)
	}

	if err := json.NewDecoder(rec.Body).Decode(&collection); err != nil {
		t.Fatalf("GET (disabled BMC): failed to decode response: %v", err)
	}
	if collection.MembersCount != 1 {
		t.Errorf("GET (disabled BMC): expected 1 member (disabled should be filtered), got %d", collection.MembersCount)
	}
}

func TestHandleSystemsCollection(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Initialize the database schema
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

	authSvc := auth.New(db)
	bmcSvc := bmc.New(db)
	handler := &Handler{
		db:     db,
		auth:   authSvc,
		bmcSvc: bmcSvc,
	}

	ctx := context.Background()

	// Test 1: OPTIONS request
	req := httptest.NewRequest(http.MethodOptions, "/redfish/v1/Systems", nil)
	rec := httptest.NewRecorder()
	handler.handleSystemsCollection(rec, req)
	if rec.Code != http.StatusNoContent && rec.Code != http.StatusOK {
		t.Errorf("OPTIONS: expected status 200 or 204, got %d", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != "GET" {
		t.Errorf("OPTIONS: expected Allow header 'GET', got '%s'", allow)
	}

	// Test 2: Invalid method (DELETE)
	req = httptest.NewRequest(http.MethodDelete, "/redfish/v1/Systems", nil)
	rec = httptest.NewRecorder()
	handler.handleSystemsCollection(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE: expected status 405, got %d", rec.Code)
	}

	// Test 3: GET with no systems
	req = httptest.NewRequest(http.MethodGet, "/redfish/v1/Systems", nil)
	rec = httptest.NewRecorder()
	handler.handleSystemsCollection(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET (empty): expected status 200, got %d", rec.Code)
	}

	var collection redfish.Collection
	if err := json.NewDecoder(rec.Body).Decode(&collection); err != nil {
		t.Fatalf("GET (empty): failed to decode response: %v", err)
	}
	if collection.MembersCount != 0 {
		t.Errorf("GET (empty): expected 0 members, got %d", collection.MembersCount)
	}

	// Test 4: GET with systems
	testBMC := &models.BMC{
		Name:     "test-system",
		Address:  "192.168.1.200",
		Username: "admin",
		Password: "password",
		Enabled:  true,
	}
	if err := db.CreateBMC(ctx, testBMC); err != nil {
		t.Fatalf("failed to create test BMC: %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/redfish/v1/Systems", nil)
	rec = httptest.NewRecorder()
	handler.handleSystemsCollection(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET (with system): expected status 200, got %d", rec.Code)
	}

	if err := json.NewDecoder(rec.Body).Decode(&collection); err != nil {
		t.Fatalf("GET (with system): failed to decode response: %v", err)
	}
	if collection.MembersCount != 1 {
		t.Errorf("GET (with system): expected 1 member, got %d", collection.MembersCount)
	}
	if len(collection.Members) > 0 && collection.Members[0].ODataID != "/redfish/v1/Systems/test-system" {
		t.Errorf("GET (with system): unexpected member ID: %s", collection.Members[0].ODataID)
	}

	// Test 5: Verify collection metadata
	if collection.ODataContext != "/redfish/v1/$metadata#ComputerSystemCollection.ComputerSystemCollection" {
		t.Errorf("unexpected @odata.context: %s", collection.ODataContext)
	}
	if collection.ODataType != "#ComputerSystemCollection.ComputerSystemCollection" {
		t.Errorf("unexpected @odata.type: %s", collection.ODataType)
	}
	if collection.Name != "Computer System Collection" {
		t.Errorf("unexpected Name: %s", collection.Name)
	}
}
