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
	"path/filepath"
	"testing"
	"time"

	"shoal/pkg/models"
)

func TestNew(t *testing.T) {
	// Create temporary database file
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Verify database file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatalf("Database file was not created")
	}
}

func TestMigrate(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	err = db.Migrate(ctx)
	if err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Verify tables exist by trying to query them
	_, err = db.GetBMCs(ctx)
	if err != nil {
		t.Fatalf("Failed to query BMCs table after migration: %v", err)
	}
}

func TestSettingsPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Seed a BMC
	bmc := &models.BMC{Name: "b1", Address: "https://1.2.3.4", Username: "u", Password: "p", Enabled: true}
	if err := db.CreateBMC(ctx, bmc); err != nil {
		t.Fatalf("create bmc: %v", err)
	}

	// Upsert two descriptors
	d1 := models.SettingDescriptor{ID: "id1", BMCName: "b1", ResourcePath: "/redfish/v1/Systems/S1/Bios", Attribute: "A1", Type: "boolean", CurrentValue: true, SourceTimeISO: time.Now().UTC().Format(time.RFC3339)}
	d2 := models.SettingDescriptor{ID: "id2", BMCName: "b1", ResourcePath: "/redfish/v1/Managers/M1/NetworkProtocol", Attribute: "HTTPS", Type: "object", CurrentValue: map[string]any{"Port": 443.0}, SourceTimeISO: time.Now().UTC().Format(time.RFC3339)}
	if err := db.UpsertSettingDescriptors(ctx, "b1", []models.SettingDescriptor{d1, d2}); err != nil {
		t.Fatalf("upsert descriptors: %v", err)
	}

	// List
	list, err := db.GetSettingsDescriptors(ctx, "b1", "")
	if err != nil {
		t.Fatalf("list descriptors: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 descriptors, got %d", len(list))
	}

	// Filter
	list, err = db.GetSettingsDescriptors(ctx, "b1", "Bios")
	if err != nil {
		t.Fatalf("filter descriptors: %v", err)
	}
	if len(list) != 1 || list[0].ID != "id1" {
		t.Fatalf("expected filter to return id1, got %+v", list)
	}

	// Get by id
	got, err := db.GetSettingDescriptor(ctx, "b1", "id2")
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got == nil || got.ID != "id2" {
		t.Fatalf("expected id2, got %+v", got)
	}
}

// Profile persistence tests removed in Design 014

func TestBMCOperations(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Test CreateBMC
	bmc := &models.BMC{
		Name:        "test-bmc-1",
		Address:     "192.168.1.100",
		Username:    "admin",
		Password:    "password",
		Description: "Test BMC",
		Enabled:     true,
	}

	err = db.CreateBMC(ctx, bmc)
	if err != nil {
		t.Fatalf("Failed to create BMC: %v", err)
	}

	if bmc.ID == 0 {
		t.Fatal("BMC ID was not set after creation")
	}

	// Test GetBMC
	retrievedBMC, err := db.GetBMC(ctx, bmc.ID)
	if err != nil {
		t.Fatalf("Failed to get BMC: %v", err)
	}

	if retrievedBMC == nil {
		t.Fatal("Retrieved BMC is nil")
	}

	if retrievedBMC.Name != bmc.Name {
		t.Errorf("Expected BMC name %s, got %s", bmc.Name, retrievedBMC.Name)
	}

	// Test GetBMCByName
	retrievedBMCByName, err := db.GetBMCByName(ctx, bmc.Name)
	if err != nil {
		t.Fatalf("Failed to get BMC by name: %v", err)
	}

	if retrievedBMCByName == nil {
		t.Fatal("Retrieved BMC by name is nil")
	}

	if retrievedBMCByName.ID != bmc.ID {
		t.Errorf("Expected BMC ID %d, got %d", bmc.ID, retrievedBMCByName.ID)
	}

	// Test GetBMCs
	bmcs, err := db.GetBMCs(ctx)
	if err != nil {
		t.Fatalf("Failed to get BMCs: %v", err)
	}

	if len(bmcs) != 1 {
		t.Errorf("Expected 1 BMC, got %d", len(bmcs))
	}

	// Test UpdateBMC
	bmc.Description = "Updated description"
	bmc.Enabled = false

	err = db.UpdateBMC(ctx, bmc)
	if err != nil {
		t.Fatalf("Failed to update BMC: %v", err)
	}

	updatedBMC, err := db.GetBMC(ctx, bmc.ID)
	if err != nil {
		t.Fatalf("Failed to get updated BMC: %v", err)
	}

	if updatedBMC.Description != "Updated description" {
		t.Errorf("Expected description 'Updated description', got %s", updatedBMC.Description)
	}

	if updatedBMC.Enabled != false {
		t.Error("Expected BMC to be disabled")
	}

	// Test UpdateBMCLastSeen
	err = db.UpdateBMCLastSeen(ctx, bmc.ID)
	if err != nil {
		t.Fatalf("Failed to update BMC last seen: %v", err)
	}

	updatedBMC, err = db.GetBMC(ctx, bmc.ID)
	if err != nil {
		t.Fatalf("Failed to get BMC after updating last seen: %v", err)
	}

	if updatedBMC.LastSeen == nil {
		t.Error("LastSeen should not be nil after update")
	}

	// Test DeleteBMC
	err = db.DeleteBMC(ctx, bmc.ID)
	if err != nil {
		t.Fatalf("Failed to delete BMC: %v", err)
	}

	deletedBMC, err := db.GetBMC(ctx, bmc.ID)
	if err != nil {
		t.Fatalf("Failed to check deleted BMC: %v", err)
	}

	if deletedBMC != nil {
		t.Error("BMC should be nil after deletion")
	}
}

func TestSessionOperations(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Disable foreign key constraints for testing
	if err := db.DisableForeignKeys(); err != nil {
		t.Fatalf("Failed to disable foreign keys: %v", err)
	}

	// Test CreateSession
	session := &models.Session{
		ID:        "test-session-1",
		UserID:    "user-123",
		Token:     "test-token-123",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
	}

	err = db.CreateSession(ctx, session)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Test GetSessionByToken
	retrievedSession, err := db.GetSessionByToken(ctx, session.Token)
	if err != nil {
		t.Fatalf("Failed to get session by token: %v", err)
	}

	if retrievedSession == nil {
		t.Fatal("Retrieved session is nil")
	}

	if retrievedSession.ID != session.ID {
		t.Errorf("Expected session ID %s, got %s", session.ID, retrievedSession.ID)
	}

	// Test DeleteSession
	err = db.DeleteSession(ctx, session.Token)
	if err != nil {
		t.Fatalf("Failed to delete session: %v", err)
	}

	deletedSession, err := db.GetSessionByToken(ctx, session.Token)
	if err != nil {
		t.Fatalf("Failed to check deleted session: %v", err)
	}

	if deletedSession != nil {
		t.Error("Session should be nil after deletion")
	}
}

func TestCleanupExpiredSessions(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Disable foreign key constraints for testing
	if err := db.DisableForeignKeys(); err != nil {
		t.Fatalf("Failed to disable foreign keys: %v", err)
	}

	// Create an expired session
	expiredSession := &models.Session{
		ID:        "expired-session",
		UserID:    "user-123",
		Token:     "expired-token",
		ExpiresAt: time.Now().Add(-1 * time.Hour), // Already expired
		CreatedAt: time.Now().Add(-2 * time.Hour),
	}

	err = db.CreateSession(ctx, expiredSession)
	if err != nil {
		t.Fatalf("Failed to create expired session: %v", err)
	}

	// Create a valid session
	validSession := &models.Session{
		ID:        "valid-session",
		UserID:    "user-123",
		Token:     "valid-token",
		ExpiresAt: time.Now().Add(1 * time.Hour), // Valid for 1 hour
		CreatedAt: time.Now(),
	}

	err = db.CreateSession(ctx, validSession)
	if err != nil {
		t.Fatalf("Failed to create valid session: %v", err)
	}

	// Cleanup expired sessions
	err = db.CleanupExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("Failed to cleanup expired sessions: %v", err)
	}

	// Count sessions directly to verify cleanup worked
	var expiredCount, validCount int

	// Check if expired session exists
	err = db.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions WHERE token = ?", expiredSession.Token).Scan(&expiredCount)
	if err != nil {
		t.Fatalf("Failed to check expired session count: %v", err)
	}

	if expiredCount != 0 {
		t.Error("Expired session should have been cleaned up")
	}

	// Check if valid session exists
	err = db.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions WHERE token = ?", validSession.Token).Scan(&validCount)
	if err != nil {
		t.Fatalf("Failed to check valid session count: %v", err)
	}

	if validCount != 1 {
		t.Error("Valid session should still exist")
	}
}

func TestGetSetSetting(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Test setting and getting a value
	key := "test_setting"
	value := "test_value"

	err = db.SetSetting(ctx, key, value)
	if err != nil {
		t.Fatalf("SetSetting failed: %v", err)
	}

	// Retrieve the setting
	retrieved, err := db.GetSetting(ctx, key)
	if err != nil {
		t.Fatalf("GetSetting failed: %v", err)
	}

	if retrieved != value {
		t.Errorf("Expected value %q, got %q", value, retrieved)
	}

	// Test updating an existing setting
	newValue := "updated_value"
	err = db.SetSetting(ctx, key, newValue)
	if err != nil {
		t.Fatalf("SetSetting (update) failed: %v", err)
	}

	retrieved, err = db.GetSetting(ctx, key)
	if err != nil {
		t.Fatalf("GetSetting (after update) failed: %v", err)
	}

	if retrieved != newValue {
		t.Errorf("Expected updated value %q, got %q", newValue, retrieved)
	}

	// Test getting non-existent setting
	retrieved, err = db.GetSetting(ctx, "nonexistent_key")
	if err != nil {
		t.Fatalf("GetSetting for non-existent key failed: %v", err)
	}
	// GetSetting returns empty string for non-existent keys, not an error
	if retrieved != "" {
		t.Errorf("Expected empty string for non-existent setting, got %q", retrieved)
	}
}

func TestEnsureServiceUUID(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// First call should create a UUID
	uuid1, err := db.EnsureServiceUUID(ctx)
	if err != nil {
		t.Fatalf("EnsureServiceUUID failed: %v", err)
	}

	if uuid1 == "" {
		t.Fatal("UUID should not be empty")
	}

	// Second call should return the same UUID
	uuid2, err := db.EnsureServiceUUID(ctx)
	if err != nil {
		t.Fatalf("EnsureServiceUUID (second call) failed: %v", err)
	}

	if uuid1 != uuid2 {
		t.Errorf("UUID should be consistent: got %q then %q", uuid1, uuid2)
	}
}

func TestSessionCRUD(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Create a user first (sessions have foreign key to users)
	user := &models.User{
		Username:     "testuser",
		PasswordHash: "hashed_password",
		Role:         "admin",
		Enabled:      true,
	}
	err = db.CreateUser(ctx, user)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Test GetSession (should return nil for non-existent)
	result, err := db.GetSession(ctx, "nonexistent-id")
	if err != nil {
		t.Fatalf("GetSession should not error for non-existent: %v", err)
	}
	if result != nil {
		t.Error("Expected nil session for non-existent ID")
	}

	// Test GetSessions (should be empty initially)
	sessions, err := db.GetSessions(ctx)
	if err != nil {
		t.Fatalf("GetSessions failed: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("Expected 0 sessions, got %d", len(sessions))
	}

	// Create a session via CreateSession
	session := &models.Session{
		ID:        "session-test-123", // ID must be set by caller
		Token:     "test-token-123",
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(1 * time.Hour),
		CreatedAt: time.Now(),
	}
	err = db.CreateSession(ctx, session)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Test GetSession (by ID)
	retrieved, err := db.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if retrieved == nil {
		t.Fatal("GetSession returned nil")
	}
	if retrieved.Token != session.Token {
		t.Errorf("Expected token %q, got %q", session.Token, retrieved.Token)
	}
	if retrieved.UserID != session.UserID {
		t.Errorf("Expected UserID %q, got %q", session.UserID, retrieved.UserID)
	}

	// Test GetSessions
	sessions, err = db.GetSessions(ctx)
	if err != nil {
		t.Fatalf("GetSessions failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("Expected 1 session, got %d", len(sessions))
	}

	// Test DeleteSessionByID
	err = db.DeleteSessionByID(ctx, retrieved.ID)
	if err != nil {
		t.Fatalf("DeleteSessionByID failed: %v", err)
	}

	// Verify deletion
	sessions, err = db.GetSessions(ctx)
	if err != nil {
		t.Fatalf("GetSessions (after delete) failed: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("Expected 0 sessions after delete, got %d", len(sessions))
	}
}

func TestUserCRUD(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Test GetUsers (empty initially)
	users, err := db.GetUsers(ctx)
	if err != nil {
		t.Fatalf("GetUsers failed: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("Expected 0 users initially, got %d", len(users))
	}

	// Test CountUsers (should be 0)
	count, err := db.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers failed: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected count 0, got %d", count)
	}

	// Test CreateUser
	user := &models.User{
		ID:           "user-test-123", // ID must be set by caller
		Username:     "testuser",
		PasswordHash: "hashed_password_123",
		Role:         "admin",
		Enabled:      true,
	}
	err = db.CreateUser(ctx, user)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Test GetUser
	retrieved, err := db.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	if retrieved.Username != user.Username {
		t.Errorf("Expected username %q, got %q", user.Username, retrieved.Username)
	}

	// Test GetUserByUsername
	byUsername, err := db.GetUserByUsername(ctx, user.Username)
	if err != nil {
		t.Fatalf("GetUserByUsername failed: %v", err)
	}
	if byUsername.ID != user.ID {
		t.Errorf("Expected user ID %q, got %q", user.ID, byUsername.ID)
	}

	// Test GetUsers (should have 1)
	users, err = db.GetUsers(ctx)
	if err != nil {
		t.Fatalf("GetUsers failed: %v", err)
	}
	if len(users) != 1 {
		t.Errorf("Expected 1 user, got %d", len(users))
	}

	// Test CountUsers (should be 1)
	count, err = db.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected count 1, got %d", count)
	}

	// Test UpdateUser
	user.Role = "operator"
	user.Enabled = false
	err = db.UpdateUser(ctx, user)
	if err != nil {
		t.Fatalf("UpdateUser failed: %v", err)
	}

	updated, err := db.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUser (after update) failed: %v", err)
	}
	if updated.Role != "operator" {
		t.Errorf("Expected role 'operator', got %q", updated.Role)
	}
	if updated.Enabled {
		t.Error("Expected Enabled=false")
	}

	// Test DeleteUser
	err = db.DeleteUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("DeleteUser failed: %v", err)
	}

	// Verify deletion - GetUser should return nil (no error, just nil user)
	deleted, err := db.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUser after delete returned error: %v", err)
	}
	if deleted != nil {
		t.Error("Expected nil user after deletion")
	}

	count, err = db.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers (after delete) failed: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected count 0 after delete, got %d", count)
	}
}

func BenchmarkCreateBMC(b *testing.B) {
	tmpDir := b.TempDir()
	dbPath := filepath.Join(tmpDir, "benchmark.db")

	db, err := New(dbPath)
	if err != nil {
		b.Fatalf("Failed to create database: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		b.Fatalf("Migration failed: %v", err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		bmc := &models.BMC{
			Name:        "bench-bmc",
			Address:     "192.168.1.1",
			Username:    "admin",
			Password:    "password",
			Description: "Benchmark BMC",
			Enabled:     true,
		}

		err := db.CreateBMC(ctx, bmc)
		if err != nil {
			b.Fatalf("Failed to create BMC: %v", err)
		}

		// Clean up for next iteration
		_ = db.DeleteBMC(ctx, bmc.ID)
	}
}
