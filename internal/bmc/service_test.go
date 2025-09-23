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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shoal/internal/database"
	"shoal/pkg/models"
)

func TestBuildBMCURL(t *testing.T) {
	// Create a test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	service := New(db)

	tests := []struct {
		name     string
		bmc      *models.BMC
		path     string
		expected string
	}{
		{
			name: "standard BMC with HTTPS prefix",
			bmc: &models.BMC{
				Address: "https://192.168.1.100",
			},
			path:     "/redfish/v1/Systems",
			expected: "https://192.168.1.100/redfish/v1/Systems",
		},
		{
			name: "standard BMC with HTTP prefix",
			bmc: &models.BMC{
				Address: "http://192.168.1.100",
			},
			path:     "/redfish/v1/Systems",
			expected: "http://192.168.1.100/redfish/v1/Systems",
		},
		{
			name: "BMC without protocol prefix (should add https)",
			bmc: &models.BMC{
				Address: "192.168.1.100",
			},
			path:     "/redfish/v1/Systems",
			expected: "https://192.168.1.100/redfish/v1/Systems",
		},
		{
			name: "hostname without protocol",
			bmc: &models.BMC{
				Address: "bmc.example.com",
			},
			path:     "/redfish/v1/Systems",
			expected: "https://bmc.example.com/redfish/v1/Systems",
		},
		{
			name: "mock BMC with path prefix",
			bmc: &models.BMC{
				Address: "https://mock.shoal.cloud/public-rackmount1/",
			},
			path:     "/redfish/v1/Systems",
			expected: "https://mock.shoal.cloud/public-rackmount1/redfish/v1/Systems",
		},
		{
			name: "mock BMC with path prefix (no trailing slash)",
			bmc: &models.BMC{
				Address: "https://mock.shoal.cloud/public-rackmount1",
			},
			path:     "/redfish/v1/Systems",
			expected: "https://mock.shoal.cloud/public-rackmount1/redfish/v1/Systems",
		},
		{
			name: "path without leading slash",
			bmc: &models.BMC{
				Address: "https://192.168.1.100",
			},
			path:     "redfish/v1/Systems",
			expected: "https://192.168.1.100/redfish/v1/Systems",
		},
		{
			name: "BMC with port number",
			bmc: &models.BMC{
				Address: "https://192.168.1.100:8443",
			},
			path:     "/redfish/v1/Systems",
			expected: "https://192.168.1.100:8443/redfish/v1/Systems",
		},
		{
			name: "complex path with actions",
			bmc: &models.BMC{
				Address: "https://mock.shoal.cloud/public-rackmount1",
			},
			path:     "/redfish/v1/Systems/437XR1138R2/Actions/ComputerSystem.Reset",
			expected: "https://mock.shoal.cloud/public-rackmount1/redfish/v1/Systems/437XR1138R2/Actions/ComputerSystem.Reset",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := service.buildBMCURL(tt.bmc, tt.path)
			if err != nil {
				t.Fatalf("buildBMCURL() error = %v", err)
			}
			if result != tt.expected {
				t.Errorf("buildBMCURL() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestPowerControl(t *testing.T) {
	// Create a test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Create a test BMC
	bmc := &models.BMC{
		Name:     "test-bmc",
		Address:  "", // Will be set to test server URL
		Username: "admin",
		Password: "password",
		Enabled:  true,
	}

	// Create a mock BMC server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check authentication
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		switch r.URL.Path {
		case "/redfish/v1/Systems":
			// Return a systems collection
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id": "/redfish/v1/Systems",
				"Members": []map[string]string{
					{"@odata.id": "/redfish/v1/Systems/System1"},
				},
			})
		case "/redfish/v1/Systems/System1/Actions/ComputerSystem.Reset":
			// Validate power action request
			var powerReq models.PowerRequest
			if err := json.NewDecoder(r.Body).Decode(&powerReq); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			// Accept any valid power action
			validActions := []models.PowerAction{
				models.PowerActionOn,
				models.PowerActionForceOff,
				models.PowerActionGracefulShutdown,
				models.PowerActionGracefulRestart,
				models.PowerActionForceRestart,
				models.PowerActionNmi,
			}
			isValid := false
			for _, action := range validActions {
				if powerReq.ResetType == action {
					isValid = true
					break
				}
			}
			if !isValid {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Set the BMC address to the test server URL
	bmc.Address = server.URL

	// Add BMC to database
	if err := db.CreateBMC(ctx, bmc); err != nil {
		t.Fatalf("Failed to create BMC: %v", err)
	}

	service := New(db)

	// Test successful power control
	testCases := []struct {
		name   string
		action models.PowerAction
	}{
		{"Power On", models.PowerActionOn},
		{"Force Off", models.PowerActionForceOff},
		{"Graceful Shutdown", models.PowerActionGracefulShutdown},
		{"Force Restart", models.PowerActionForceRestart},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := service.PowerControl(ctx, "test-bmc", tc.action)
			if err != nil {
				t.Errorf("PowerControl() failed for %s: %v", tc.action, err)
			}
		})
	}

	// Test with non-existent BMC
	t.Run("Non-existent BMC", func(t *testing.T) {
		err := service.PowerControl(ctx, "non-existent", models.PowerActionOn)
		if err == nil {
			t.Error("Expected error for non-existent BMC, got nil")
		}
	})

	// Test with disabled BMC
	t.Run("Disabled BMC", func(t *testing.T) {
		// Disable the BMC
		bmc.Enabled = false
		if err := db.UpdateBMC(ctx, bmc); err != nil {
			t.Fatalf("Failed to update BMC: %v", err)
		}

		err := service.PowerControl(ctx, "test-bmc", models.PowerActionOn)
		if err == nil {
			t.Error("Expected error for disabled BMC, got nil")
		}
	})
}

func TestPowerControlNoSystems(t *testing.T) {
	// Create a test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Create a mock BMC server that returns empty systems
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if r.URL.Path == "/redfish/v1/Systems" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id": "/redfish/v1/Systems",
				"Members":   []map[string]string{},
			})
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Create a test BMC
	bmc := &models.BMC{
		Name:     "test-bmc-no-systems",
		Address:  server.URL,
		Username: "admin",
		Password: "password",
		Enabled:  true,
	}

	if err := db.CreateBMC(ctx, bmc); err != nil {
		t.Fatalf("Failed to create BMC: %v", err)
	}

	service := New(db)

	// Test power control with no systems available
	err = service.PowerControl(ctx, "test-bmc-no-systems", models.PowerActionOn)
	if err == nil {
		t.Error("Expected error for BMC with no systems, got nil")
	}
	if err != nil && err.Error() != "no systems found on BMC" {
		t.Errorf("Expected 'no systems found on BMC' error, got: %v", err)
	}
}

func TestGetDetailedBMCStatus(t *testing.T) {
	// Create a test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Create a mock BMC server that returns comprehensive test data
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check authentication
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/redfish/v1/Systems":
			// Return systems collection
			json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id": "/redfish/v1/Systems",
				"Members": []map[string]string{
					{"@odata.id": "/redfish/v1/Systems/System1"},
				},
			})
		case "/redfish/v1/Systems/System1":
			// Return system details
			json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id":    "/redfish/v1/Systems/System1",
				"Id":           "System1",
				"Name":         "Test System",
				"SerialNumber": "ABC123456789",
				"SKU":          "SKU-TEST-001",
				"PowerState":   "On",
				"Model":        "Test Server Model",
				"Manufacturer": "Test Manufacturer",
			})
		case "/redfish/v1/Systems/System1/EthernetInterfaces":
			// Return network interfaces collection
			json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id": "/redfish/v1/Systems/System1/EthernetInterfaces",
				"Members": []map[string]string{
					{"@odata.id": "/redfish/v1/Systems/System1/EthernetInterfaces/NIC1"},
					{"@odata.id": "/redfish/v1/Systems/System1/EthernetInterfaces/NIC2"},
				},
			})
		case "/redfish/v1/Systems/System1/EthernetInterfaces/NIC1":
			// Return first NIC details
			json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id":   "/redfish/v1/Systems/System1/EthernetInterfaces/NIC1",
				"Id":          "NIC1",
				"Name":        "Ethernet Interface 1",
				"Description": "Primary Network Interface",
				"MACAddress":  "AA:BB:CC:DD:EE:FF",
				"IPv4Addresses": []map[string]interface{}{
					{"Address": "192.168.1.100"},
					{"Address": "10.0.0.100"},
				},
			})
		case "/redfish/v1/Systems/System1/EthernetInterfaces/NIC2":
			// Return second NIC details
			json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id":   "/redfish/v1/Systems/System1/EthernetInterfaces/NIC2",
				"Id":          "NIC2",
				"Name":        "Ethernet Interface 2",
				"Description": "Secondary Network Interface",
				"MACAddress":  "FF:EE:DD:CC:BB:AA",
				"IPv4Addresses": []map[string]interface{}{
					{"Address": "192.168.2.100"},
				},
			})
		case "/redfish/v1/Systems/System1/Storage":
			// Return storage collection
			json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id": "/redfish/v1/Systems/System1/Storage",
				"Members": []map[string]string{
					{"@odata.id": "/redfish/v1/Systems/System1/Storage/Storage1"},
				},
			})
		case "/redfish/v1/Systems/System1/Storage/Storage1":
			// Return storage controller details
			json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id": "/redfish/v1/Systems/System1/Storage/Storage1",
				"Id":        "Storage1",
				"Name":      "Storage Controller",
				"Drives": []map[string]string{
					{"@odata.id": "/redfish/v1/Systems/System1/Storage/Storage1/Drives/Drive1"},
					{"@odata.id": "/redfish/v1/Systems/System1/Storage/Storage1/Drives/Drive2"},
				},
			})
		case "/redfish/v1/Systems/System1/Storage/Storage1/Drives/Drive1":
			// Return first drive details
			json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id":     "/redfish/v1/Systems/System1/Storage/Storage1/Drives/Drive1",
				"Id":            "Drive1",
				"Name":          "Drive 1",
				"Model":         "TestDrive-SSD-1TB",
				"SerialNumber":  "SSD123456789",
				"CapacityBytes": float64(1000000000000), // 1TB
				"MediaType":     "SSD",
				"Status": map[string]interface{}{
					"Health": "OK",
				},
			})
		case "/redfish/v1/Systems/System1/Storage/Storage1/Drives/Drive2":
			// Return second drive details
			json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id":     "/redfish/v1/Systems/System1/Storage/Storage1/Drives/Drive2",
				"Id":            "Drive2",
				"Name":          "Drive 2",
				"Model":         "TestDrive-HDD-2TB",
				"SerialNumber":  "HDD987654321",
				"CapacityBytes": float64(2000000000000), // 2TB
				"MediaType":     "HDD",
				"Status": map[string]interface{}{
					"Health": "OK",
				},
			})
		case "/redfish/v1/Managers":
			// Return managers collection
			json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id": "/redfish/v1/Managers",
				"Members": []map[string]string{
					{"@odata.id": "/redfish/v1/Managers/Manager1"},
				},
			})
		case "/redfish/v1/Managers/Manager1/LogServices":
			// Return log services collection
			json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id": "/redfish/v1/Managers/Manager1/LogServices",
				"Members": []map[string]string{
					{"@odata.id": "/redfish/v1/Managers/Manager1/LogServices/EventLog"},
				},
			})
		case "/redfish/v1/Managers/Manager1/LogServices/EventLog":
			// Return log service details
			json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id": "/redfish/v1/Managers/Manager1/LogServices/EventLog",
				"Id":        "EventLog",
				"Name":      "Event Log",
			})
		case "/redfish/v1/Managers/Manager1/LogServices/EventLog/Entries":
			// Return log entries collection
			json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id": "/redfish/v1/Managers/Manager1/LogServices/EventLog/Entries",
				"Members": []map[string]string{
					{"@odata.id": "/redfish/v1/Managers/Manager1/LogServices/EventLog/Entries/1"},
					{"@odata.id": "/redfish/v1/Managers/Manager1/LogServices/EventLog/Entries/2"},
				},
			})
		case "/redfish/v1/Managers/Manager1/LogServices/EventLog/Entries/1":
			// Return first log entry
			json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id": "/redfish/v1/Managers/Manager1/LogServices/EventLog/Entries/1",
				"Id":        "1",
				"Message":   "System boot completed successfully",
				"Severity":  "OK",
				"Created":   "2024-01-15T10:30:00Z",
				"EntryType": "Event",
			})
		case "/redfish/v1/Managers/Manager1/LogServices/EventLog/Entries/2":
			// Return second log entry
			json.NewEncoder(w).Encode(map[string]interface{}{
				"@odata.id": "/redfish/v1/Managers/Manager1/LogServices/EventLog/Entries/2",
				"Id":        "2",
				"Message":   "Temperature sensor reading high",
				"Severity":  "Warning",
				"Created":   "2024-01-15T11:45:00Z",
				"EntryType": "Event",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Create a test BMC
	bmc := &models.BMC{
		Name:     "test-bmc-detailed",
		Address:  server.URL,
		Username: "admin",
		Password: "password",
		Enabled:  true,
	}
	if err := db.CreateBMC(ctx, bmc); err != nil {
		t.Fatalf("Failed to create BMC: %v", err)
	}

	service := New(db)

	// Test successful detailed status retrieval
	t.Run("Successful detailed status", func(t *testing.T) {
		details, err := service.GetDetailedBMCStatus(ctx, "test-bmc-detailed")
		if err != nil {
			t.Fatalf("GetDetailedBMCStatus() failed: %v", err)
		}

		// Verify BMC basic info
		if details.BMC.Name != "test-bmc-detailed" {
			t.Errorf("Expected BMC name 'test-bmc-detailed', got '%s'", details.BMC.Name)
		}

		// Verify system information
		if details.SystemInfo == nil {
			t.Fatal("SystemInfo should not be nil")
		}
		if details.SystemInfo.SerialNumber != "ABC123456789" {
			t.Errorf("Expected serial number 'ABC123456789', got '%s'", details.SystemInfo.SerialNumber)
		}
		if details.SystemInfo.SKU != "SKU-TEST-001" {
			t.Errorf("Expected SKU 'SKU-TEST-001', got '%s'", details.SystemInfo.SKU)
		}
		if details.SystemInfo.PowerState != "On" {
			t.Errorf("Expected power state 'On', got '%s'", details.SystemInfo.PowerState)
		}
		if details.SystemInfo.Model != "Test Server Model" {
			t.Errorf("Expected model 'Test Server Model', got '%s'", details.SystemInfo.Model)
		}
		if details.SystemInfo.Manufacturer != "Test Manufacturer" {
			t.Errorf("Expected manufacturer 'Test Manufacturer', got '%s'", details.SystemInfo.Manufacturer)
		}

		// Verify network interfaces
		if len(details.NetworkInterfaces) != 2 {
			t.Errorf("Expected 2 network interfaces, got %d", len(details.NetworkInterfaces))
		}
		if len(details.NetworkInterfaces) > 0 {
			nic1 := details.NetworkInterfaces[0]
			if nic1.Name != "Ethernet Interface 1" {
				t.Errorf("Expected NIC name 'Ethernet Interface 1', got '%s'", nic1.Name)
			}
			if nic1.MACAddress != "AA:BB:CC:DD:EE:FF" {
				t.Errorf("Expected MAC 'AA:BB:CC:DD:EE:FF', got '%s'", nic1.MACAddress)
			}
			if len(nic1.IPAddresses) != 2 {
				t.Errorf("Expected 2 IP addresses, got %d", len(nic1.IPAddresses))
			}
		}

		// Verify storage devices
		if len(details.StorageDevices) != 2 {
			t.Errorf("Expected 2 storage devices, got %d", len(details.StorageDevices))
		}
		if len(details.StorageDevices) > 0 {
			drive1 := details.StorageDevices[0]
			if drive1.Name != "Drive 1" {
				t.Errorf("Expected drive name 'Drive 1', got '%s'", drive1.Name)
			}
			if drive1.Model != "TestDrive-SSD-1TB" {
				t.Errorf("Expected drive model 'TestDrive-SSD-1TB', got '%s'", drive1.Model)
			}
			if drive1.CapacityBytes != 1000000000000 {
				t.Errorf("Expected capacity 1000000000000, got %d", drive1.CapacityBytes)
			}
			if drive1.MediaType != "SSD" {
				t.Errorf("Expected media type 'SSD', got '%s'", drive1.MediaType)
			}
			if drive1.Status != "OK" {
				t.Errorf("Expected status 'OK', got '%s'", drive1.Status)
			}
		}

		// Verify SEL entries
		if len(details.SELEntries) != 2 {
			t.Errorf("Expected 2 SEL entries, got %d", len(details.SELEntries))
		}
		if len(details.SELEntries) > 0 {
			entry1 := details.SELEntries[0]
			if entry1.Message != "System boot completed successfully" {
				t.Errorf("Expected message 'System boot completed successfully', got '%s'", entry1.Message)
			}
			if entry1.Severity != "OK" {
				t.Errorf("Expected severity 'OK', got '%s'", entry1.Severity)
			}
			if entry1.EntryType != "Event" {
				t.Errorf("Expected entry type 'Event', got '%s'", entry1.EntryType)
			}
		}
	})

	// Test with non-existent BMC
	t.Run("Non-existent BMC", func(t *testing.T) {
		_, err := service.GetDetailedBMCStatus(ctx, "non-existent")
		if err == nil {
			t.Error("Expected error for non-existent BMC, got nil")
		}
	})

	// Test with disabled BMC
	t.Run("Disabled BMC", func(t *testing.T) {
		// Disable the BMC
		bmc.Enabled = false
		if err := db.UpdateBMC(ctx, bmc); err != nil {
			t.Fatalf("Failed to update BMC: %v", err)
		}

		_, err := service.GetDetailedBMCStatus(ctx, "test-bmc-detailed")
		if err == nil {
			t.Error("Expected error for disabled BMC, got nil")
		}
	})
}

func TestGetSystemInfo(t *testing.T) {
	// Create a test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Create a mock BMC server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/redfish/v1/Systems":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/System1"}},
			})
		case "/redfish/v1/Systems/System1":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"SerialNumber": "TEST123",
				"SKU":          "TEST-SKU",
				"PowerState":   "Off",
				"Model":        "Test Model",
				"Manufacturer": "Test Mfg",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	service := New(db)
	bmc := &models.BMC{
		Name:     "test-system-info",
		Address:  server.URL,
		Username: "admin",
		Password: "password",
		Enabled:  true,
	}
	if err := db.CreateBMC(ctx, bmc); err != nil {
		t.Fatalf("Failed to create BMC: %v", err)
	}

	t.Run("Successful system info retrieval", func(t *testing.T) {
		systemInfo, err := service.getSystemInfo(ctx, bmc)
		if err != nil {
			t.Fatalf("getSystemInfo() failed: %v", err)
		}

		if systemInfo.SerialNumber != "TEST123" {
			t.Errorf("Expected serial 'TEST123', got '%s'", systemInfo.SerialNumber)
		}
		if systemInfo.SKU != "TEST-SKU" {
			t.Errorf("Expected SKU 'TEST-SKU', got '%s'", systemInfo.SKU)
		}
		if systemInfo.PowerState != "Off" {
			t.Errorf("Expected power state 'Off', got '%s'", systemInfo.PowerState)
		}
		if systemInfo.Model != "Test Model" {
			t.Errorf("Expected model 'Test Model', got '%s'", systemInfo.Model)
		}
		if systemInfo.Manufacturer != "Test Mfg" {
			t.Errorf("Expected manufacturer 'Test Mfg', got '%s'", systemInfo.Manufacturer)
		}
	})
}

func TestGetNetworkInterfaces(t *testing.T) {
	// Create a test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Create a mock BMC server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/redfish/v1/Systems":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/System1"}},
			})
		case "/redfish/v1/Systems/System1/EthernetInterfaces":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Members": []map[string]string{
					{"@odata.id": "/redfish/v1/Systems/System1/EthernetInterfaces/NIC1"},
				},
			})
		case "/redfish/v1/Systems/System1/EthernetInterfaces/NIC1":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Name":        "Test NIC",
				"Description": "Test Network Interface",
				"MACAddress":  "11:22:33:44:55:66",
				"IPv4Addresses": []map[string]interface{}{
					{"Address": "10.0.0.1"},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	service := New(db)
	bmc := &models.BMC{
		Name:     "test-nics",
		Address:  server.URL,
		Username: "admin",
		Password: "password",
		Enabled:  true,
	}
	if err := db.CreateBMC(ctx, bmc); err != nil {
		t.Fatalf("Failed to create BMC: %v", err)
	}

	t.Run("Successful NIC retrieval", func(t *testing.T) {
		nics, err := service.getNetworkInterfaces(ctx, bmc)
		if err != nil {
			t.Fatalf("getNetworkInterfaces() failed: %v", err)
		}

		if len(nics) != 1 {
			t.Errorf("Expected 1 NIC, got %d", len(nics))
		}

		if len(nics) > 0 {
			nic := nics[0]
			if nic.Name != "Test NIC" {
				t.Errorf("Expected name 'Test NIC', got '%s'", nic.Name)
			}
			if nic.MACAddress != "11:22:33:44:55:66" {
				t.Errorf("Expected MAC '11:22:33:44:55:66', got '%s'", nic.MACAddress)
			}
			if len(nic.IPAddresses) != 1 || nic.IPAddresses[0] != "10.0.0.1" {
				t.Errorf("Expected IP '10.0.0.1', got %v", nic.IPAddresses)
			}
		}
	})
}

func TestGetStorageDevices(t *testing.T) {
	// Create a test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Create a mock BMC server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/redfish/v1/Systems":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/System1"}},
			})
		case "/redfish/v1/Systems/System1/Storage":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Members": []map[string]string{
					{"@odata.id": "/redfish/v1/Systems/System1/Storage/Storage1"},
				},
			})
		case "/redfish/v1/Systems/System1/Storage/Storage1":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Drives": []map[string]string{
					{"@odata.id": "/redfish/v1/Systems/System1/Storage/Storage1/Drives/Drive1"},
				},
			})
		case "/redfish/v1/Systems/System1/Storage/Storage1/Drives/Drive1":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Name":          "Test Drive",
				"Model":         "TestModel-500GB",
				"SerialNumber":  "TESTSERIAL123",
				"CapacityBytes": float64(500000000000),
				"MediaType":     "HDD",
				"Status": map[string]interface{}{
					"Health": "OK",
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	service := New(db)
	bmc := &models.BMC{
		Name:     "test-storage",
		Address:  server.URL,
		Username: "admin",
		Password: "password",
		Enabled:  true,
	}
	if err := db.CreateBMC(ctx, bmc); err != nil {
		t.Fatalf("Failed to create BMC: %v", err)
	}

	t.Run("Successful storage retrieval", func(t *testing.T) {
		devices, err := service.getStorageDevices(ctx, bmc)
		if err != nil {
			t.Fatalf("getStorageDevices() failed: %v", err)
		}

		if len(devices) != 1 {
			t.Errorf("Expected 1 storage device, got %d", len(devices))
		}

		if len(devices) > 0 {
			device := devices[0]
			if device.Name != "Test Drive" {
				t.Errorf("Expected name 'Test Drive', got '%s'", device.Name)
			}
			if device.Model != "TestModel-500GB" {
				t.Errorf("Expected model 'TestModel-500GB', got '%s'", device.Model)
			}
			if device.CapacityBytes != 500000000000 {
				t.Errorf("Expected capacity 500000000000, got %d", device.CapacityBytes)
			}
			if device.Status != "OK" {
				t.Errorf("Expected status 'OK', got '%s'", device.Status)
			}
		}
	})
}

func TestGetSimpleStorageDevices(t *testing.T) {
	// Create a test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Create a mock BMC server that has SimpleStorage but no Storage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/redfish/v1/Systems":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/System1"}},
			})
		case "/redfish/v1/Systems/System1/Storage":
			// Storage collection doesn't exist - return 404
			w.WriteHeader(http.StatusNotFound)
		case "/redfish/v1/Systems/System1/SimpleStorage":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Members": []map[string]string{
					{"@odata.id": "/redfish/v1/Systems/System1/SimpleStorage/1"},
				},
			})
		case "/redfish/v1/Systems/System1/SimpleStorage/1":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Name":        "Simple Storage Controller",
				"Description": "System SATA",
				"Devices": []map[string]interface{}{
					{
						"Name":          "SATA Bay 1",
						"Manufacturer":  "Contoso",
						"Model":         "3000GT8",
						"CapacityBytes": float64(8000000000000),
						"Status": map[string]interface{}{
							"State":  "Enabled",
							"Health": "OK",
						},
					},
					{
						"Name":          "SATA Bay 2",
						"Manufacturer":  "Contoso",
						"Model":         "3000GT7",
						"CapacityBytes": float64(4000000000000),
						"Status": map[string]interface{}{
							"State":  "Enabled",
							"Health": "Warning",
						},
					},
					{
						"Name": "SATA Bay 3",
						"Status": map[string]interface{}{
							"State": "Absent",
						},
					},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	service := New(db)
	bmc := &models.BMC{
		Name:     "test-simple-storage",
		Address:  server.URL,
		Username: "admin",
		Password: "password",
		Enabled:  true,
	}
	if err := db.CreateBMC(ctx, bmc); err != nil {
		t.Fatalf("Failed to create BMC: %v", err)
	}

	t.Run("Successful SimpleStorage retrieval", func(t *testing.T) {
		devices, err := service.getStorageDevices(ctx, bmc)
		if err != nil {
			t.Fatalf("getStorageDevices() failed: %v", err)
		}

		// Should get 2 devices (SATA Bay 1 and SATA Bay 2, but not SATA Bay 3 which is Absent)
		if len(devices) != 2 {
			t.Errorf("Expected 2 storage devices, got %d", len(devices))
		}

		if len(devices) >= 2 {
			// Check first device
			device1 := devices[0]
			if device1.Name != "SATA Bay 1" {
				t.Errorf("Expected name 'SATA Bay 1', got '%s'", device1.Name)
			}
			if device1.Model != "Contoso 3000GT8" {
				t.Errorf("Expected model 'Contoso 3000GT8', got '%s'", device1.Model)
			}
			if device1.CapacityBytes != 8000000000000 {
				t.Errorf("Expected capacity 8000000000000, got %d", device1.CapacityBytes)
			}
			if device1.Status != "OK" {
				t.Errorf("Expected status 'OK', got '%s'", device1.Status)
			}

			// Check second device
			device2 := devices[1]
			if device2.Name != "SATA Bay 2" {
				t.Errorf("Expected name 'SATA Bay 2', got '%s'", device2.Name)
			}
			if device2.Model != "Contoso 3000GT7" {
				t.Errorf("Expected model 'Contoso 3000GT7', got '%s'", device2.Model)
			}
			if device2.CapacityBytes != 4000000000000 {
				t.Errorf("Expected capacity 4000000000000, got %d", device2.CapacityBytes)
			}
			if device2.Status != "Warning" {
				t.Errorf("Expected status 'Warning', got '%s'", device2.Status)
			}
		}
	})
}

func TestGetSELEntries(t *testing.T) {
	// Create a test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Create a mock BMC server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/redfish/v1/Managers":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Members": []map[string]string{{"@odata.id": "/redfish/v1/Managers/Manager1"}},
			})
		case "/redfish/v1/Managers/Manager1/LogServices":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Members": []map[string]string{
					{"@odata.id": "/redfish/v1/Managers/Manager1/LogServices/EventLog"},
				},
			})
		case "/redfish/v1/Managers/Manager1/LogServices/EventLog":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Name": "Event Log",
			})
		case "/redfish/v1/Managers/Manager1/LogServices/EventLog/Entries":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Members": []map[string]string{
					{"@odata.id": "/redfish/v1/Managers/Manager1/LogServices/EventLog/Entries/1"},
				},
			})
		case "/redfish/v1/Managers/Manager1/LogServices/EventLog/Entries/1":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Id":        "1",
				"Message":   "Test log message",
				"Severity":  "Warning",
				"Created":   "2024-01-01T00:00:00Z",
				"EntryType": "Event",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	service := New(db)
	bmc := &models.BMC{
		Name:     "test-sel",
		Address:  server.URL,
		Username: "admin",
		Password: "password",
		Enabled:  true,
	}
	if err := db.CreateBMC(ctx, bmc); err != nil {
		t.Fatalf("Failed to create BMC: %v", err)
	}

	t.Run("Successful SEL retrieval", func(t *testing.T) {
		entries, err := service.getSELEntries(ctx, bmc)
		if err != nil {
			t.Fatalf("getSELEntries() failed: %v", err)
		}

		if len(entries) != 1 {
			t.Errorf("Expected 1 SEL entry, got %d", len(entries))
		}

		if len(entries) > 0 {
			entry := entries[0]
			if entry.ID != "1" {
				t.Errorf("Expected ID '1', got '%s'", entry.ID)
			}
			if entry.Message != "Test log message" {
				t.Errorf("Expected message 'Test log message', got '%s'", entry.Message)
			}
			if entry.Severity != "Warning" {
				t.Errorf("Expected severity 'Warning', got '%s'", entry.Severity)
			}
		}
	})
}

func TestDiscoverSettingsBasic(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Mock BMC server with BIOS and ManagerNetworkProtocol and @Redfish.Settings
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/redfish/v1/Systems":
			json.NewEncoder(w).Encode(map[string]any{
				"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/Sys1"}},
			})
		case "/redfish/v1/Systems/Sys1/Bios":
			json.NewEncoder(w).Encode(map[string]any{
				"@Redfish.Settings": map[string]any{
					"SettingsObject":      map[string]any{"@odata.id": "/redfish/v1/Systems/Sys1/Bios/Settings"},
					"SupportedApplyTimes": []string{"OnReset"},
				},
				"Attributes": map[string]any{
					"ProcTurboMode":        "Enabled",
					"LogicalProc":          true,
					"SomeNumericAttribute": float64(5),
				},
			})
		case "/redfish/v1/Managers":
			json.NewEncoder(w).Encode(map[string]any{
				"Members": []map[string]string{{"@odata.id": "/redfish/v1/Managers/Mgr1"}},
			})
		case "/redfish/v1/Managers/Mgr1/NetworkProtocol":
			json.NewEncoder(w).Encode(map[string]any{
				"@Redfish.Settings": map[string]any{"SettingsObject": map[string]any{"@odata.id": "/redfish/v1/Managers/Mgr1/NetworkProtocol/Settings"}},
				"NTP":               map[string]any{"ProtocolEnabled": true},
				"HTTP":              map[string]any{"Port": float64(80)},
				"HTTPS":             map[string]any{"Port": float64(443)},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	b := &models.BMC{
		Name:     "bmc1",
		Address:  server.URL,
		Username: "admin",
		Password: "password",
		Enabled:  true,
	}
	if err := db.CreateBMC(ctx, b); err != nil {
		t.Fatalf("Failed to create BMC: %v", err)
	}

	svc := New(db)
	descs, err := svc.DiscoverSettings(ctx, "bmc1", "")
	if err != nil {
		t.Fatalf("DiscoverSettings failed: %v", err)
	}
	if len(descs) == 0 {
		t.Fatalf("expected some settings descriptors, got 0")
	}
	// Ensure some known attributes are present
	hasTurbo := false
	for _, d := range descs {
		if d.Attribute == "ProcTurboMode" {
			hasTurbo = true
			if d.Type == "" || d.BMCName != "bmc1" || d.ResourcePath == "" {
				t.Fatalf("descriptor missing fields: %+v", d)
			}
		}
	}
	if !hasTurbo {
		t.Fatalf("expected ProcTurboMode descriptor")
	}
}

func TestDiscoverSettings009_NetworkAndStorage(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Mock BMC with EthernetInterfaces and Storage resources exposing @Redfish.Settings
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/redfish/v1/Systems":
			json.NewEncoder(w).Encode(map[string]any{
				"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/Sys1"}},
			})
		case "/redfish/v1/Managers":
			json.NewEncoder(w).Encode(map[string]any{
				"Members": []map[string]string{{"@odata.id": "/redfish/v1/Managers/Mgr1"}},
			})
		case "/redfish/v1/Systems/Sys1/EthernetInterfaces":
			json.NewEncoder(w).Encode(map[string]any{
				"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/Sys1/EthernetInterfaces/NIC1"}},
			})
		case "/redfish/v1/Systems/Sys1/EthernetInterfaces/NIC1":
			json.NewEncoder(w).Encode(map[string]any{
				"@Redfish.Settings": map[string]any{
					"SettingsObject": map[string]any{"@odata.id": "/redfish/v1/Systems/Sys1/EthernetInterfaces/NIC1/Settings"},
				},
				"DHCPv4":     map[string]any{"Enabled": true},
				"VLAN":       map[string]any{"Enabled": false, "Id": float64(100)},
				"MACAddress": "AA:BB:CC:DD:EE:FF",
			})
		case "/redfish/v1/Systems/Sys1/Storage":
			json.NewEncoder(w).Encode(map[string]any{
				"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/Sys1/Storage/Stor1"}},
			})
		case "/redfish/v1/Systems/Sys1/Storage/Stor1":
			json.NewEncoder(w).Encode(map[string]any{
				"@Redfish.Settings": map[string]any{
					"SettingsObject": map[string]any{"@odata.id": "/redfish/v1/Systems/Sys1/Storage/Stor1/Settings"},
				},
				"ControllerMode": "RAID",
				"Drives":         []map[string]string{{"@odata.id": "/redfish/v1/Systems/Sys1/Storage/Stor1/Drives/Drive1"}},
			})
		case "/redfish/v1/Systems/Sys1/Storage/Stor1/Drives/Drive1":
			json.NewEncoder(w).Encode(map[string]any{
				"@Redfish.Settings": map[string]any{
					"SettingsObject": map[string]any{"@odata.id": "/redfish/v1/Systems/Sys1/Storage/Stor1/Drives/Drive1/Settings"},
				},
				"WriteCache": "Enabled",
				"Model":      "TestDrive",
			})
		case "/redfish/v1/Systems/Sys1/Storage/Stor1/Volumes":
			json.NewEncoder(w).Encode(map[string]any{
				"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/Sys1/Storage/Stor1/Volumes/Vol1"}},
			})
		case "/redfish/v1/Systems/Sys1/Storage/Stor1/Volumes/Vol1":
			json.NewEncoder(w).Encode(map[string]any{
				"@Redfish.Settings": map[string]any{
					"SettingsObject": map[string]any{"@odata.id": "/redfish/v1/Systems/Sys1/Storage/Stor1/Volumes/Vol1/Settings"},
				},
				"Name":     "Volume1",
				"RAIDType": "RAID1",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	b := &models.BMC{Name: "bmc009", Address: server.URL, Username: "admin", Password: "password", Enabled: true}
	if err := db.CreateBMC(ctx, b); err != nil {
		t.Fatalf("create bmc: %v", err)
	}

	svc := New(db)
	descs, err := svc.DiscoverSettings(ctx, "bmc009", "")
	if err != nil {
		t.Fatalf("DiscoverSettings: %v", err)
	}
	if len(descs) == 0 {
		t.Fatalf("expected descriptors from 009 resources")
	}

	// Assert presence of representative attributes
	var hasDHCPv4, hasVLAN, hasControllerMode, hasWriteCache, hasVolume bool
	for _, d := range descs {
		switch d.Attribute {
		case "DHCPv4":
			hasDHCPv4 = true
		case "VLAN":
			hasVLAN = true
		case "ControllerMode":
			hasControllerMode = true
		case "WriteCache":
			hasWriteCache = true
		case "RAIDType":
			hasVolume = true
		}
		if d.ActionTarget == "" {
			t.Fatalf("missing action target for %s", d.Attribute)
		}
	}
	if !(hasDHCPv4 && hasVLAN && hasControllerMode && hasWriteCache && hasVolume) {
		t.Fatalf("missing expected attributes: DHCPv4=%v VLAN=%v ControllerMode=%v WriteCache=%v RAIDType=%v",
			hasDHCPv4, hasVLAN, hasControllerMode, hasWriteCache, hasVolume)
	}

	// Verify resource filter works for EthernetInterfaces
	descs2, err := svc.DiscoverSettings(ctx, "bmc009", "EthernetInterfaces")
	if err != nil {
		t.Fatalf("DiscoverSettings filter: %v", err)
	}
	// Expect at least NIC-related settings; not asserting counts to avoid flakiness
	var onlyNICs = true
	for _, d := range descs2 {
		if d.ResourcePath == "" || !strings.Contains(d.ResourcePath, "/EthernetInterfaces/") {
			onlyNICs = false
			break
		}
	}
	if !onlyNICs {
		t.Fatalf("filter did not limit to EthernetInterfaces: %+v", descs2)
	}
}

func TestTestConnection(t *testing.T) {
	// Create a test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// Create a mock BMC server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if r.URL.Path == "/redfish/v1/" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"@odata.id": "/redfish/v1/",
				"Name":      "Test BMC",
			})
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	service := New(db)

	// Test successful connection
	t.Run("Successful connection", func(t *testing.T) {
		bmc := &models.BMC{
			Address:  server.URL,
			Username: "admin",
			Password: "password",
		}
		err := service.TestConnection(context.Background(), bmc)
		if err != nil {
			t.Errorf("TestConnection() failed: %v", err)
		}
	})

	// Test with wrong credentials
	t.Run("Wrong credentials", func(t *testing.T) {
		bmc := &models.BMC{
			Address:  server.URL,
			Username: "admin",
			Password: "wrong",
		}
		err := service.TestConnection(context.Background(), bmc)
		if err == nil {
			t.Error("Expected error for wrong credentials, got nil")
		}
	})

	// Test with unreachable BMC
	t.Run("Unreachable BMC", func(t *testing.T) {
		bmc := &models.BMC{
			Address:  "https://192.0.2.1", // TEST-NET-1, should be unreachable
			Username: "admin",
			Password: "password",
		}
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond) // Very short timeout
		defer cancel()
		err := service.TestConnection(ctx, bmc)
		if err == nil {
			t.Error("Expected error for unreachable BMC, got nil")
		}
	})
}

func TestDiscoverSettings_ApplyTimesAndActionTarget(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/redfish/v1/Systems":
			json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/Sys1"}}})
		case "/redfish/v1/Managers":
			json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Managers/Mgr1"}}})
		case "/redfish/v1/Systems/Sys1/Bios":
			json.NewEncoder(w).Encode(map[string]any{
				"@Redfish.Settings": map[string]any{
					"SettingsObject":      map[string]any{"@odata.id": "/redfish/v1/Systems/Sys1/Bios/Settings"},
					"SupportedApplyTimes": []string{"OnReset"},
				},
				"Attributes": map[string]any{"ProcTurboMode": "Enabled"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	b := &models.BMC{Name: "bmc1", Address: server.URL, Username: "u", Password: "p", Enabled: true}
	if err := db.CreateBMC(context.Background(), b); err != nil {
		t.Fatalf("create bmc: %v", err)
	}

	svc := New(db)
	descs, err := svc.DiscoverSettings(context.Background(), "bmc1", "")
	if err != nil {
		t.Fatalf("DiscoverSettings: %v", err)
	}
	var found bool
	for _, d := range descs {
		if d.Attribute == "ProcTurboMode" {
			found = true
			if d.ActionTarget != "/redfish/v1/Systems/Sys1/Bios/Settings" {
				t.Fatalf("action_target mismatch: %q", d.ActionTarget)
			}
			if len(d.ApplyTimes) == 0 || d.ApplyTimes[0] != "OnReset" {
				t.Fatalf("apply_times missing: %+v", d.ApplyTimes)
			}
		}
	}
	if !found {
		t.Fatalf("did not find ProcTurboMode descriptor")
	}
}

func TestAttributeRegistryEnrichment(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Mock server with BIOS + AttributeRegistry and registry payload under Registries
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/redfish/v1/Systems":
			json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/Sys1"}}})
		case "/redfish/v1/Managers":
			json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Managers/Mgr1"}}})
		case "/redfish/v1/Systems/Sys1/Bios":
			json.NewEncoder(w).Encode(map[string]any{
				"@Redfish.Settings": map[string]any{
					"SettingsObject": map[string]any{"@odata.id": "/redfish/v1/Systems/Sys1/Bios/Settings"},
				},
				"AttributeRegistry": "Test.Registry",
				"Attributes":        map[string]any{"ProcTurboMode": "Enabled"},
			})
		case "/redfish/v1/Registries/Test.Registry":
			json.NewEncoder(w).Encode(map[string]any{
				"OwningEntity": "DMTF",
				"RegistryEntries": map[string]any{
					"Attributes": []any{
						map[string]any{
							"AttributeName": "ProcTurboMode",
							"DisplayName":   "Turbo Mode",
							"HelpText":      "Enable or disable Turbo",
							"Type":          "String",
							"ReadOnly":      false,
							"Value":         []any{"Enabled", "Disabled"},
						},
					},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	b := &models.BMC{Name: "bmc1", Address: server.URL, Username: "u", Password: "p", Enabled: true}
	if err := db.CreateBMC(context.Background(), b); err != nil {
		t.Fatalf("create bmc: %v", err)
	}

	svc := New(db)
	descs, err := svc.DiscoverSettings(context.Background(), "bmc1", "")
	if err != nil {
		t.Fatalf("DiscoverSettings: %v", err)
	}
	var got *models.SettingDescriptor
	for i := range descs {
		if descs[i].Attribute == "ProcTurboMode" {
			got = &descs[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("missing ProcTurboMode descriptor")
	}
	if got.DisplayName != "Turbo Mode" {
		t.Fatalf("DisplayName not enriched: %q", got.DisplayName)
	}
	if !got.ReadOnly && (len(got.EnumValues) != 2 || got.EnumValues[0] != "Enabled") {
		t.Fatalf("EnumValues not enriched: %+v", got.EnumValues)
	}
}

func TestActionInfoEnrichment(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/redfish/v1/Systems":
			json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/Sys1"}}})
		case "/redfish/v1/Managers":
			json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Managers/Mgr1"}}})
		case "/redfish/v1/Managers/Mgr1/NetworkProtocol":
			json.NewEncoder(w).Encode(map[string]any{
				"@Redfish.Settings": map[string]any{
					"SettingsObject": map[string]any{"@odata.id": "/redfish/v1/Managers/Mgr1/NetworkProtocol/Settings"},
				},
				"NTP": map[string]any{"ProtocolEnabled": true},
				"Actions": map[string]any{
					"#ManagerNetworkProtocol.Modify": map[string]any{
						"target":              "/redfish/v1/Managers/Mgr1/NetworkProtocol/Settings",
						"@Redfish.ActionInfo": "/redfish/v1/Managers/Mgr1/NetworkProtocol/ActionInfo",
					},
				},
			})
		case "/redfish/v1/Managers/Mgr1/NetworkProtocol/ActionInfo":
			json.NewEncoder(w).Encode(map[string]any{
				"Parameters": []any{
					map[string]any{
						"Name":            "NTP",
						"AllowableValues": []any{"Enabled", "Disabled"},
					},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	b := &models.BMC{Name: "bmc1", Address: server.URL, Username: "u", Password: "p", Enabled: true}
	if err := db.CreateBMC(context.Background(), b); err != nil {
		t.Fatalf("create bmc: %v", err)
	}
	svc := New(db)
	descs, err := svc.DiscoverSettings(context.Background(), "bmc1", "")
	if err != nil {
		t.Fatalf("DiscoverSettings: %v", err)
	}

	var ntp *models.SettingDescriptor
	for i := range descs {
		if descs[i].Attribute == "NTP" {
			ntp = &descs[i]
			break
		}
	}
	if ntp == nil {
		t.Fatalf("missing NTP descriptor")
	}
	if len(ntp.EnumValues) != 2 || ntp.EnumValues[0] != "Enabled" {
		t.Fatalf("ActionInfo enums not applied: %+v", ntp.EnumValues)
	}
}

func TestDiscoverSettings_RefreshBypassesCache(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Backing variable to simulate state change on the BMC
	value := "Enabled"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/redfish/v1/Systems":
			json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/Sys1"}}})
		case "/redfish/v1/Managers":
			json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Managers/Mgr1"}}})
		case "/redfish/v1/Systems/Sys1/Bios":
			json.NewEncoder(w).Encode(map[string]any{
				"@Redfish.Settings": map[string]any{
					"SettingsObject": map[string]any{"@odata.id": "/redfish/v1/Systems/Sys1/Bios/Settings"},
				},
				"Attributes": map[string]any{"ProcTurboMode": value},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	b := &models.BMC{Name: "bmc1", Address: server.URL, Username: "u", Password: "p", Enabled: true}
	if err := db.CreateBMC(context.Background(), b); err != nil {
		t.Fatalf("create bmc: %v", err)
	}

	svc := New(db)

	// First discovery to populate DB and caches
	ctx := context.Background()
	descs1, err := svc.DiscoverSettings(ctx, "bmc1", "")
	if err != nil {
		t.Fatalf("discover1: %v", err)
	}
	var d1 *models.SettingDescriptor
	for i := range descs1 {
		if descs1[i].Attribute == "ProcTurboMode" {
			d1 = &descs1[i]
			break
		}
	}
	if d1 == nil {
		t.Fatalf("missing ProcTurboMode after first discover")
	}
	if v, ok := d1.CurrentValue.(string); !ok || v != "Enabled" {
		t.Fatalf("unexpected first value: %+v", d1.CurrentValue)
	}

	// Change backend value
	value = "Disabled"

	// Without refresh, descriptor should return cached DB value from GetSettingsDescriptors; call DB method to confirm
	stored, err := db.GetSettingsDescriptors(ctx, "bmc1", "Bios")
	if err != nil {
		t.Fatalf("get from db: %v", err)
	}
	var sd *models.SettingDescriptor
	for i := range stored {
		if stored[i].Attribute == "ProcTurboMode" {
			sd = &stored[i]
			break
		}
	}
	if sd == nil {
		t.Fatalf("missing stored descriptor")
	}
	if v, ok := sd.CurrentValue.(string); !ok || v != "Enabled" {
		t.Fatalf("expected cached value 'Enabled' before refresh, got: %+v", sd.CurrentValue)
	}

	// With refresh context flag, re-discovery should update DB to new value
	ctxRefresh := context.WithValue(ctx, interface{}("refresh"), true)
	descs2, err := svc.DiscoverSettings(ctxRefresh, "bmc1", "")
	if err != nil {
		t.Fatalf("discover2: %v", err)
	}
	var d2 *models.SettingDescriptor
	for i := range descs2 {
		if descs2[i].Attribute == "ProcTurboMode" {
			d2 = &descs2[i]
			break
		}
	}
	if d2 == nil {
		t.Fatalf("missing ProcTurboMode after refresh discover")
	}
	if v, ok := d2.CurrentValue.(string); !ok || v != "Disabled" {
		t.Fatalf("expected refreshed value 'Disabled', got: %+v", d2.CurrentValue)
	}
}

func TestDiscoverSettings013_BootOrder(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Mock BMC with System exposing Boot.BootOrder and allowable values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "u" || password != "p" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/redfish/v1/Systems":
			json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/Sys1"}}})
		case "/redfish/v1/Managers":
			json.NewEncoder(w).Encode(map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Managers/M1"}}})
		case "/redfish/v1/Systems/Sys1":
			json.NewEncoder(w).Encode(map[string]any{
				"Boot": map[string]any{
					"BootOrder": []any{"Pxe", "Hdd", "DVD"},
				},
				"BootOrder@Redfish.AllowableValues": []any{"Pxe", "Hdd", "DVD", "USB"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	b := &models.BMC{Name: "bmc013", Address: server.URL, Username: "u", Password: "p", Enabled: true}
	if err := db.CreateBMC(context.Background(), b); err != nil {
		t.Fatalf("create bmc: %v", err)
	}

	svc := New(db)
	// Full discovery
	descs, err := svc.DiscoverSettings(context.Background(), "bmc013", "")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	var boot *models.SettingDescriptor
	for i := range descs {
		if descs[i].Attribute == "Boot.BootOrder" {
			boot = &descs[i]
			break
		}
	}
	if boot == nil {
		t.Fatalf("missing Boot.BootOrder descriptor")
	}
	if boot.Type != "array" {
		t.Fatalf("expected array type, got %q", boot.Type)
	}
	if boot.ActionTarget == "" || !strings.Contains(boot.ActionTarget, "/redfish/v1/Systems/") {
		t.Fatalf("unexpected action target: %q", boot.ActionTarget)
	}
	if len(boot.ApplyTimes) == 0 || boot.ApplyTimes[0] != "OnReset" {
		t.Fatalf("expected OnReset apply time, got %+v", boot.ApplyTimes)
	}
	if len(boot.EnumValues) < 3 {
		t.Fatalf("expected allowable values, got %+v", boot.EnumValues)
	}

	// Filtered discovery by keyword 'Boot' should also include it
	descs2, err := svc.DiscoverSettings(context.Background(), "bmc013", "Boot")
	if err != nil {
		t.Fatalf("discover2: %v", err)
	}
	var found bool
	for i := range descs2 {
		if descs2[i].Attribute == "Boot.BootOrder" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Boot filter did not include Boot.BootOrder")
	}
}
