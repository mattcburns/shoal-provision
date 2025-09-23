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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"shoal/internal/database"
	"shoal/pkg/models"
)

func TestAddConnectionMethod(t *testing.T) {
	// Create a test BMC server
	bmcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redfish/v1/":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id":   "/redfish/v1/",
				"@odata.type": "#ServiceRoot.v1_5_0.ServiceRoot",
				"Id":          "RootService",
				"Name":        "Test BMC",
			})
		case "/redfish/v1/Managers":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id":   "/redfish/v1/Managers",
				"@odata.type": "#ManagerCollection.ManagerCollection",
				"Members": []map[string]string{
					{"@odata.id": "/redfish/v1/Managers/BMC"},
				},
			})
		case "/redfish/v1/Managers/BMC":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id":   "/redfish/v1/Managers/BMC",
				"@odata.type": "#Manager.v1_5_0.Manager",
				"Id":          "BMC",
				"Name":        "BMC Manager",
			})
		case "/redfish/v1/Systems":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id":   "/redfish/v1/Systems",
				"@odata.type": "#ComputerSystemCollection.ComputerSystemCollection",
				"Members": []map[string]string{
					{"@odata.id": "/redfish/v1/Systems/System.1"},
				},
			})
		case "/redfish/v1/Systems/System.1":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id":   "/redfish/v1/Systems/System.1",
				"@odata.type": "#ComputerSystem.v1_5_0.ComputerSystem",
				"Id":          "System.1",
				"Name":        "System One",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer bmcServer.Close()

	// Create temporary database
	tempFile, err := os.CreateTemp("", "test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(tempFile.Name()) }()
	_ = tempFile.Close()

	db, err := database.New(tempFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	// Create BMC service
	service := New(db)

	// Test adding a connection method
	t.Run("AddConnectionMethod_Success", func(t *testing.T) {
		method, err := service.AddConnectionMethod(
			ctx,
			"Test BMC Connection",
			bmcServer.URL,
			"admin",
			"password",
		)

		if err != nil {
			t.Fatalf("AddConnectionMethod failed: %v", err)
		}

		if method == nil {
			t.Fatal("Expected connection method, got nil")
		}

		if method.Name != "Test BMC Connection" {
			t.Errorf("Expected name 'Test BMC Connection', got '%s'", method.Name)
		}

		if method.Address != bmcServer.URL {
			t.Errorf("Expected address '%s', got '%s'", bmcServer.URL, method.Address)
		}

		// Verify aggregated data was fetched
		if method.AggregatedManagers == "" {
			t.Error("Expected aggregated managers data")
		}

		if method.AggregatedSystems == "" {
			t.Error("Expected aggregated systems data")
		}

		// Parse and verify the aggregated data
		var managers []map[string]interface{}
		if err := json.Unmarshal([]byte(method.AggregatedManagers), &managers); err != nil {
			t.Errorf("Failed to parse aggregated managers: %v", err)
		}
		if len(managers) != 1 {
			t.Errorf("Expected 1 manager, got %d", len(managers))
		}

		var systems []map[string]interface{}
		if err := json.Unmarshal([]byte(method.AggregatedSystems), &systems); err != nil {
			t.Errorf("Failed to parse aggregated systems: %v", err)
		}
		if len(systems) != 1 {
			t.Errorf("Expected 1 system, got %d", len(systems))
		}
	})

	t.Run("AddConnectionMethod_InvalidBMC", func(t *testing.T) {
		// Try to add a connection to an invalid BMC
		method, err := service.AddConnectionMethod(
			ctx,
			"Invalid BMC",
			"http://invalid.bmc.address:9999",
			"admin",
			"password",
		)

		if err == nil {
			t.Error("Expected error for invalid BMC, got nil")
		}

		if method != nil {
			t.Error("Expected nil method for invalid BMC")
		}
	})
}

func TestFetchAggregatedData(t *testing.T) {
	// Create a test BMC server with more complex data
	bmcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for basic auth
		username, password, ok := r.BasicAuth()
		if !ok || username != "testuser" || password != "testpass" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		switch r.URL.Path {
		case "/redfish/v1/Managers":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id": "/redfish/v1/Managers",
				"Members": []map[string]string{
					{"@odata.id": "/redfish/v1/Managers/BMC1"},
					{"@odata.id": "/redfish/v1/Managers/BMC2"},
				},
			})
		case "/redfish/v1/Managers/BMC1":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id":   "/redfish/v1/Managers/BMC1",
				"@odata.type": "#Manager.v1_5_0.Manager",
				"Id":          "BMC1",
				"Name":        "BMC Manager 1",
				"ManagerType": "BMC",
			})
		case "/redfish/v1/Managers/BMC2":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id":   "/redfish/v1/Managers/BMC2",
				"@odata.type": "#Manager.v1_5_0.Manager",
				"Id":          "BMC2",
				"Name":        "BMC Manager 2",
				"ManagerType": "BMC",
			})
		case "/redfish/v1/Systems":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id": "/redfish/v1/Systems",
				"Members": []map[string]string{
					{"@odata.id": "/redfish/v1/Systems/System1"},
					{"@odata.id": "/redfish/v1/Systems/System2"},
				},
			})
		case "/redfish/v1/Systems/System1":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id":    "/redfish/v1/Systems/System1",
				"@odata.type":  "#ComputerSystem.v1_5_0.ComputerSystem",
				"Id":           "System1",
				"Name":         "Computer System 1",
				"SystemType":   "Physical",
				"Manufacturer": "Test Manufacturer",
			})
		case "/redfish/v1/Systems/System2":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id":    "/redfish/v1/Systems/System2",
				"@odata.type":  "#ComputerSystem.v1_5_0.ComputerSystem",
				"Id":           "System2",
				"Name":         "Computer System 2",
				"SystemType":   "Virtual",
				"Manufacturer": "Test Manufacturer",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer bmcServer.Close()

	// Create temporary database
	tempFile, err := os.CreateTemp("", "test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(tempFile.Name()) }()
	_ = tempFile.Close()

	db, err := database.New(tempFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// Create BMC service
	service := New(db)
	ctx := context.Background()

	// Test fetching aggregated data
	testBMC := &models.BMC{
		Address:  bmcServer.URL,
		Username: "testuser",
		Password: "testpass",
	}

	managers, systems, err := service.FetchAggregatedData(ctx, testBMC)
	if err != nil {
		t.Fatalf("FetchAggregatedData failed: %v", err)
	}

	// Verify managers
	if len(managers) != 2 {
		t.Errorf("Expected 2 managers, got %d", len(managers))
	}

	// Verify systems
	if len(systems) != 2 {
		t.Errorf("Expected 2 systems, got %d", len(systems))
	}

	// Check manager details
	if len(managers) > 0 {
		manager1 := managers[0]
		if id, ok := manager1["Id"].(string); !ok || id != "BMC1" {
			t.Errorf("Expected manager ID 'BMC1', got '%v'", manager1["Id"])
		}
		if name, ok := manager1["Name"].(string); !ok || name != "BMC Manager 1" {
			t.Errorf("Expected manager name 'BMC Manager 1', got '%v'", manager1["Name"])
		}
	}

	// Check system details
	if len(systems) > 0 {
		system1 := systems[0]
		if id, ok := system1["Id"].(string); !ok || id != "System1" {
			t.Errorf("Expected system ID 'System1', got '%v'", system1["Id"])
		}
		if sysType, ok := system1["SystemType"].(string); !ok || sysType != "Physical" {
			t.Errorf("Expected system type 'Physical', got '%v'", system1["SystemType"])
		}
	}
}

func TestConnectionMethodCRUD(t *testing.T) {
	// Create temporary database
	tempFile, err := os.CreateTemp("", "test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(tempFile.Name()) }()
	_ = tempFile.Close()

	db, err := database.New(tempFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	// Create BMC service
	service := New(db)

	// Create test connection methods directly in DB
	method1 := &models.ConnectionMethod{
		ID:                   "test-cm-crud-1",
		Name:                 "Test Method 1",
		ConnectionMethodType: "Redfish",
		Address:              "192.168.1.100",
		Username:             "admin",
		Password:             "password",
		Enabled:              true,
	}
	if err := db.CreateConnectionMethod(ctx, method1); err != nil {
		t.Fatal(err)
	}

	method2 := &models.ConnectionMethod{
		ID:                   "test-cm-crud-2",
		Name:                 "Test Method 2",
		ConnectionMethodType: "Redfish",
		Address:              "192.168.1.101",
		Username:             "admin",
		Password:             "password",
		Enabled:              true,
	}
	if err := db.CreateConnectionMethod(ctx, method2); err != nil {
		t.Fatal(err)
	}

	t.Run("GetConnectionMethods", func(t *testing.T) {
		methods, err := service.GetConnectionMethods(ctx)
		if err != nil {
			t.Fatalf("GetConnectionMethods failed: %v", err)
		}

		if len(methods) != 2 {
			t.Errorf("Expected 2 methods, got %d", len(methods))
		}
	})

	t.Run("GetConnectionMethod", func(t *testing.T) {
		method, err := service.GetConnectionMethod(ctx, "test-cm-crud-1")
		if err != nil {
			t.Fatalf("GetConnectionMethod failed: %v", err)
		}

		if method == nil {
			t.Fatal("Expected method, got nil")
		}

		if method.Name != "Test Method 1" {
			t.Errorf("Expected name 'Test Method 1', got '%s'", method.Name)
		}
	})

	t.Run("RemoveConnectionMethod", func(t *testing.T) {
		err := service.RemoveConnectionMethod(ctx, "test-cm-crud-2")
		if err != nil {
			t.Fatalf("RemoveConnectionMethod failed: %v", err)
		}

		// Verify it was removed
		methods, err := service.GetConnectionMethods(ctx)
		if err != nil {
			t.Fatal(err)
		}

		if len(methods) != 1 {
			t.Errorf("Expected 1 method after removal, got %d", len(methods))
		}

		// Verify the right one was removed
		if methods[0].ID != "test-cm-crud-1" {
			t.Errorf("Wrong method was removed")
		}
	})
}
