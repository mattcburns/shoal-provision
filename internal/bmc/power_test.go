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
	"sync/atomic"
	"testing"

	"shoal/internal/database"
	"shoal/pkg/models"
)

func TestPowerControl_RetriesOnServerError(t *testing.T) {
	var resetAttempts int32
	// Fake Redfish server
	mux := http.NewServeMux()
	mux.HandleFunc("/redfish/v1/Systems", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/sys-1"}},
		})
	})
	mux.HandleFunc("/redfish/v1/Systems/sys-1/Actions/ComputerSystem.Reset", func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&resetAttempts, 1) < 2 {
			http.Error(w, "transient", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Setup test DB
	db, err := database.New(":memory:")
	if err != nil {
		t.Fatalf("db new: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("db migrate: %v", err)
	}
	b := &models.BMC{
		Name:     "test-bmc",
		Address:  srv.URL, // already includes scheme
		Username: "user",
		Password: "pass",
		Enabled:  true,
	}
	if err := db.CreateBMC(context.Background(), b); err != nil {
		t.Fatalf("create bmc: %v", err)
	}

	svc := New(db)
	if err := svc.PowerControl(context.Background(), b.Name, models.PowerActionForceRestart); err != nil {
		t.Fatalf("PowerControl returned error: %v", err)
	}
	if atomic.LoadInt32(&resetAttempts) != 2 {
		t.Fatalf("expected 2 attempts on reset, got %d", resetAttempts)
	}
}
