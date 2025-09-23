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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shoal/pkg/models"
)

func TestAuditsPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("db new: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Create several audit records
	longBody := strings.Repeat("x", 10000)
	a1 := &models.AuditRecord{UserID: "u1", UserName: "admin", BMCName: "b1", Action: "proxy", Method: "GET", Path: "/redfish/v1/Systems", StatusCode: 200, DurationMS: 10, RequestBody: longBody, ResponseBody: longBody}
	if err := db.CreateAudit(ctx, a1); err != nil {
		t.Fatalf("create a1: %v", err)
	}
	// Small delay to ensure ordering by created_at
	time.Sleep(5 * time.Millisecond)
	a2 := &models.AuditRecord{UserID: "u2", UserName: "op", BMCName: "b1", Action: "proxy", Method: "POST", Path: "/redfish/v1/Actions", StatusCode: 204, DurationMS: 15}
	if err := db.CreateAudit(ctx, a2); err != nil {
		t.Fatalf("create a2: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	a3 := &models.AuditRecord{UserID: "u3", UserName: "view", BMCName: "b2", Action: "proxy", Method: "GET", Path: "/redfish/v1/Managers", StatusCode: 200, DurationMS: 12}
	if err := db.CreateAudit(ctx, a3); err != nil {
		t.Fatalf("create a3: %v", err)
	}

	// List without filter (default limit)
	list, err := db.ListAudits(ctx, "", 0)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 audits, got %d", len(list))
	}

	// List with filter and limit
	listB1, err := db.ListAudits(ctx, "b1", 1)
	if err != nil {
		t.Fatalf("list b1: %v", err)
	}
	if len(listB1) != 1 {
		t.Fatalf("expected 1 audit for b1 with limit 1, got %d", len(listB1))
	}
	if listB1[0].BMCName != "b1" {
		t.Fatalf("unexpected bmc name: %s", listB1[0].BMCName)
	}

	// Bodies in list should be truncated to <= 4096
	if len(list[0].RequestBody) > 4096 || len(list[0].ResponseBody) > 4096 {
		t.Fatalf("expected truncated bodies <= 4096, got %d/%d", len(list[0].RequestBody), len(list[0].ResponseBody))
	}

	// Get by ID
	got, err := db.GetAudit(ctx, a2.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got == nil || got.ID != a2.ID || got.Method != "POST" {
		t.Fatalf("unexpected audit: %+v", got)
	}
}
