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
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"shoal/internal/database"
	"shoal/pkg/models"
)

func TestReconcileState_ReinsertsMediaAndSetsBootOnce(t *testing.T) {
	var insertCalled int32
	var bootPatched int32

	mux := http.NewServeMux()
	mux.HandleFunc("/redfish/v1/Managers", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Members": []map[string]string{{"@odata.id": "/redfish/v1/Managers/mgr-1"}},
		})
	})
	mux.HandleFunc("/redfish/v1/Managers/mgr-1", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Id":           "mgr-1",
			"Manufacturer": "AcmeVendor",
			"VirtualMedia": map[string]any{"@odata.id": "/redfish/v1/Managers/mgr-1/VirtualMedia"},
		})
	})
	mux.HandleFunc("/redfish/v1/Managers/mgr-1/VirtualMedia", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Members": []map[string]string{{"@odata.id": "/redfish/v1/Managers/mgr-1/VirtualMedia/cd"}},
		})
	})
	mux.HandleFunc("/redfish/v1/Managers/mgr-1/VirtualMedia/cd", func(w http.ResponseWriter, r *http.Request) {
		// Simulate not inserted
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Id":         "cd",
			"Inserted":   false,
			"Image":      "",
			"MediaTypes": []string{"CD"},
			"Actions": map[string]any{
				"#VirtualMedia.InsertMedia": map[string]any{"target": "/redfish/v1/Managers/mgr-1/VirtualMedia/cd/Actions/VirtualMedia.InsertMedia"},
			},
		})
	})
	mux.HandleFunc("/redfish/v1/Managers/mgr-1/VirtualMedia/cd/Actions/VirtualMedia.InsertMedia", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&insertCalled, 1)
		// basic validation of payload
		b, _ := io.ReadAll(r.Body)
		if !bytes.Contains(b, []byte("\"Inserted\":true")) {
			t.Fatalf("expected Inserted:true in payload: %s", string(b))
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/redfish/v1/Systems", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/sys-1"}},
		})
	})
	mux.HandleFunc("/redfish/v1/Systems/sys-1", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Id":   "sys-1",
				"Boot": map[string]any{"BootSourceOverrideEnabled": "Disabled"},
			})
		case http.MethodPatch:
			atomic.AddInt32(&bootPatched, 1)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	db, err := database.New(":memory:")
	if err != nil {
		t.Fatalf("db new: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	b := &models.BMC{Name: "b", Address: srv.URL, Username: "u", Password: "p", Enabled: true}
	if err := db.CreateBMC(context.Background(), b); err != nil {
		t.Fatalf("create bmc: %v", err)
	}

	svc := New(db)
	if err := svc.ReconcileState(context.Background(), b.Name, "http://example/iso", true); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if atomic.LoadInt32(&insertCalled) == 0 {
		t.Fatalf("expected insert action to be called")
	}
	if atomic.LoadInt32(&bootPatched) == 0 {
		t.Fatalf("expected boot override to be patched")
	}
}
