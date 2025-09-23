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

package database

import (
	"context"
	"os"
	"testing"
	"time"

	"shoal/pkg/models"
)

func TestConnectionMethodOperations(t *testing.T) {
	// Create a temporary database for testing
	tempFile, err := os.CreateTemp("", "test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(tempFile.Name()) }()
	_ = tempFile.Close()

	db, err := New(tempFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// Run migrations
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	t.Run("CreateConnectionMethod", func(t *testing.T) {
		method := &models.ConnectionMethod{
			ID:                   "test-cm-1",
			Name:                 "Test Connection Method",
			ConnectionMethodType: "Redfish",
			Address:              "192.168.1.100",
			Username:             "admin",
			Password:             "password",
			Enabled:              true,
		}

		err := db.CreateConnectionMethod(ctx, method)
		if err != nil {
			t.Errorf("CreateConnectionMethod failed: %v", err)
		}

		// Verify CreatedAt and UpdatedAt are set
		if method.CreatedAt.IsZero() {
			t.Error("CreatedAt was not set")
		}
		if method.UpdatedAt.IsZero() {
			t.Error("UpdatedAt was not set")
		}
	})

	t.Run("GetConnectionMethod", func(t *testing.T) {
		method, err := db.GetConnectionMethod(ctx, "test-cm-1")
		if err != nil {
			t.Errorf("GetConnectionMethod failed: %v", err)
		}
		if method == nil {
			t.Fatal("Expected connection method, got nil")
		}
		if method.Name != "Test Connection Method" {
			t.Errorf("Expected name 'Test Connection Method', got '%s'", method.Name)
		}
		if method.Address != "192.168.1.100" {
			t.Errorf("Expected address '192.168.1.100', got '%s'", method.Address)
		}
		if method.Username != "admin" {
			t.Errorf("Expected username 'admin', got '%s'", method.Username)
		}
		if method.Password != "password" {
			t.Errorf("Expected password 'password', got '%s'", method.Password)
		}
	})

	t.Run("GetConnectionMethod_NotFound", func(t *testing.T) {
		method, err := db.GetConnectionMethod(ctx, "non-existent")
		if err != nil {
			t.Errorf("GetConnectionMethod should not error for non-existent ID: %v", err)
		}
		if method != nil {
			t.Error("Expected nil for non-existent connection method")
		}
	})

	t.Run("GetConnectionMethods", func(t *testing.T) {
		// Add another connection method
		method2 := &models.ConnectionMethod{
			ID:                   "test-cm-2",
			Name:                 "Another Test Method",
			ConnectionMethodType: "Redfish",
			Address:              "192.168.1.101",
			Username:             "user",
			Password:             "pass",
			Enabled:              false,
		}
		if err := db.CreateConnectionMethod(ctx, method2); err != nil {
			t.Fatal(err)
		}

		methods, err := db.GetConnectionMethods(ctx)
		if err != nil {
			t.Errorf("GetConnectionMethods failed: %v", err)
		}
		if len(methods) != 2 {
			t.Errorf("Expected 2 connection methods, got %d", len(methods))
		}

		// Methods should be ordered by name
		if methods[0].Name != "Another Test Method" {
			t.Errorf("Expected first method to be 'Another Test Method', got '%s'", methods[0].Name)
		}
		if methods[1].Name != "Test Connection Method" {
			t.Errorf("Expected second method to be 'Test Connection Method', got '%s'", methods[1].Name)
		}
	})

	t.Run("UpdateConnectionMethodAggregatedData", func(t *testing.T) {
		managersJSON := `[{"@odata.id": "/redfish/v1/Managers/1"}]`
		systemsJSON := `[{"@odata.id": "/redfish/v1/Systems/1"}]`

		err := db.UpdateConnectionMethodAggregatedData(ctx, "test-cm-1", managersJSON, systemsJSON)
		if err != nil {
			t.Errorf("UpdateConnectionMethodAggregatedData failed: %v", err)
		}

		// Verify the data was updated
		method, err := db.GetConnectionMethod(ctx, "test-cm-1")
		if err != nil {
			t.Fatal(err)
		}
		if method.AggregatedManagers != managersJSON {
			t.Errorf("Expected managers JSON '%s', got '%s'", managersJSON, method.AggregatedManagers)
		}
		if method.AggregatedSystems != systemsJSON {
			t.Errorf("Expected systems JSON '%s', got '%s'", systemsJSON, method.AggregatedSystems)
		}
	})

	t.Run("UpdateConnectionMethodLastSeen", func(t *testing.T) {
		err := db.UpdateConnectionMethodLastSeen(ctx, "test-cm-1")
		if err != nil {
			t.Errorf("UpdateConnectionMethodLastSeen failed: %v", err)
		}

		method, err := db.GetConnectionMethod(ctx, "test-cm-1")
		if err != nil {
			t.Fatal(err)
		}
		if method.LastSeen == nil {
			t.Error("LastSeen was not set")
		}
		if time.Since(*method.LastSeen) > 5*time.Second {
			t.Error("LastSeen timestamp is too old")
		}
	})

	t.Run("DeleteConnectionMethod", func(t *testing.T) {
		err := db.DeleteConnectionMethod(ctx, "test-cm-2")
		if err != nil {
			t.Errorf("DeleteConnectionMethod failed: %v", err)
		}

		// Verify it was deleted
		method, err := db.GetConnectionMethod(ctx, "test-cm-2")
		if err != nil {
			t.Fatal(err)
		}
		if method != nil {
			t.Error("Connection method should have been deleted")
		}

		// Verify other methods still exist
		methods, err := db.GetConnectionMethods(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(methods) != 1 {
			t.Errorf("Expected 1 connection method after deletion, got %d", len(methods))
		}
	})

	t.Run("CreateConnectionMethod_DuplicateName", func(t *testing.T) {
		// Try to create a method with duplicate name
		method := &models.ConnectionMethod{
			ID:                   "test-cm-3",
			Name:                 "Test Connection Method", // Same as test-cm-1
			ConnectionMethodType: "Redfish",
			Address:              "192.168.1.102",
			Username:             "admin",
			Password:             "password",
			Enabled:              true,
		}

		err := db.CreateConnectionMethod(ctx, method)
		if err == nil {
			t.Error("Expected error for duplicate name, got nil")
		}
	})
}

func TestConnectionMethodPasswordEncryption(t *testing.T) {
	// Create a temporary database with encryption
	tempFile, err := os.CreateTemp("", "test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(tempFile.Name()) }()
	_ = tempFile.Close()

	// Use a test encryption key
	encryptionKey := "test-encryption-key-32-bytes-long!"
	db, err := NewWithEncryption(tempFile.Name(), encryptionKey)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// Run migrations
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	// Create a connection method with password
	method := &models.ConnectionMethod{
		ID:                   "test-encrypted-cm",
		Name:                 "Encrypted Method",
		ConnectionMethodType: "Redfish",
		Address:              "192.168.1.200",
		Username:             "admin",
		Password:             "secret-password",
		Enabled:              true,
	}

	err = db.CreateConnectionMethod(ctx, method)
	if err != nil {
		t.Fatalf("Failed to create connection method: %v", err)
	}

	// Retrieve the method and verify password is decrypted correctly
	retrieved, err := db.GetConnectionMethod(ctx, "test-encrypted-cm")
	if err != nil {
		t.Fatalf("Failed to get connection method: %v", err)
	}

	if retrieved.Password != "secret-password" {
		t.Errorf("Expected password 'secret-password', got '%s'", retrieved.Password)
	}

	// Verify password is actually encrypted in database by querying directly
	var storedPassword string
	row := db.conn.QueryRow("SELECT password FROM connection_methods WHERE id = ?", "test-encrypted-cm")
	if err := row.Scan(&storedPassword); err != nil {
		t.Fatal(err)
	}

	// Encrypted password should start with "ENC:"
	if storedPassword == "secret-password" {
		t.Error("Password was not encrypted in database")
	}
	if len(storedPassword) <= len("secret-password") {
		t.Error("Encrypted password should be longer than plaintext")
	}
}
