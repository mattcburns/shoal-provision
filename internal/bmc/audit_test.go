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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"shoal/internal/database"
	"shoal/pkg/models"
)

func TestProxyRequestCreatesAudit(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("db new: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Seed a BMC
	b := &models.BMC{Name: "b1", Address: "", Username: "admin", Password: "password", Enabled: true}
	if err := db.CreateBMC(ctx, b); err != nil {
		t.Fatalf("create bmc: %v", err)
	}

	// Mock downstream BMC that expects basic auth and echoes JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// Return a small json body
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	// Update BMC address to server URL
	b.Address = server.URL
	if err := db.UpdateBMC(ctx, b); err != nil {
		t.Fatalf("update bmc: %v", err)
	}

	svc := New(db)

	// Prepare a request with a body containing a secret key to test redaction
	body := map[string]any{"Password": "supersecret", "note": "x"}
	bb, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/redfish/v1/anything", bytes.NewReader(bb))
	req.Header.Set("Content-Type", "application/json")

	// Put user into context to test attribution
	user := &models.User{ID: "u1", Username: "admin"}
	ctx = context.WithValue(req.Context(), "user", user)
	req = req.WithContext(ctx)

	// Execute proxy
	resp, err := svc.ProxyRequest(ctx, "b1", "/redfish/v1/Managers", req)
	if err != nil {
		t.Fatalf("proxy request failed: %v", err)
	}
	if resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected response: %+v", resp)
	}

	// Verify an audit was recorded
	audits, err := db.ListAudits(ctx, "", 10)
	if err != nil {
		t.Fatalf("list audits: %v", err)
	}
	if len(audits) != 1 {
		t.Fatalf("expected 1 audit, got %d", len(audits))
	}
	a := audits[0]
	if a.BMCName != "b1" || a.Method != http.MethodPost || a.Path != "/redfish/v1/Managers" {
		t.Fatalf("unexpected audit fields: %+v", a)
	}
	if a.UserID != "u1" || a.UserName != "admin" {
		t.Fatalf("missing user attribution: %+v", a)
	}
	// Ensure redaction occurred for Password
	if bytes.Contains([]byte(a.RequestBody), []byte("supersecret")) {
		t.Fatalf("expected password to be redacted, got: %s", a.RequestBody)
	}
}
