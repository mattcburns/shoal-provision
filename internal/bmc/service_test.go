package bmc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
