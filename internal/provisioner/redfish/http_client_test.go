package redfish

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

// Unit tests for the HTTP Redfish client using an in-memory fake Redfish server.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPClient_MountSetBootResetAndUnmount(t *testing.T) {
	fake := newFakeRedfish()
	// Simulate failure on GracefulRestart so client falls back to ForceRestart
	fake.failGraceful = true

	srv := httptest.NewServer(fake)
	defer srv.Close()

	cl, err := NewHTTPClient(Config{
		Endpoint: srv.URL,
		Username: "user",
		Password: "pass",
		Timeout:  2 * time.Second,
		Logger:   nil,
	})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}

	maintISO := "https://controller/media/bootc-maintenance.iso"
	taskISO := "https://controller/media/tasks/job-1/task.iso"

	// Mount maintenance (CD1)
	if err := cl.MountVirtualMedia(newTestCtx(t), 1, maintISO); err != nil {
		t.Fatalf("MountVirtualMedia cd1: %v", err)
	}
	if got := fake.vm[fake.vm1].Image; got != maintISO || !fake.vm[fake.vm1].Inserted {
		t.Fatalf("vm1 state mismatch: inserted=%v image=%q", fake.vm[fake.vm1].Inserted, fake.vm[fake.vm1].Image)
	}

	// Mount task (CD2)
	if err := cl.MountVirtualMedia(newTestCtx(t), 2, taskISO); err != nil {
		t.Fatalf("MountVirtualMedia cd2: %v", err)
	}
	if got := fake.vm[fake.vm2].Image; got != taskISO || !fake.vm[fake.vm2].Inserted {
		t.Fatalf("vm2 state mismatch: inserted=%v image=%q", fake.vm[fake.vm2].Inserted, fake.vm[fake.vm2].Image)
	}

	// Set one-time boot to CD
	if err := cl.SetOneTimeBoot(newTestCtx(t), BootDeviceCD); err != nil {
		t.Fatalf("SetOneTimeBoot: %v", err)
	}
	if !fake.bootPatched || fake.lastBootTarget != "Cd" {
		t.Fatalf("expected boot patched to Cd, got patched=%v target=%q", fake.bootPatched, fake.lastBootTarget)
	}

	// Reboot with fallback (Graceful fails, Force succeeds)
	if err := cl.Reboot(newTestCtx(t), RebootGracefulWithFallback); err != nil {
		t.Fatalf("Reboot: %v", err)
	}
	if fake.lastResetType != "ForceRestart" && fake.lastResetType != "PowerCycle" {
		t.Fatalf("expected fallback reset to ForceRestart/PowerCycle, got %q", fake.lastResetType)
	}
	if fake.resetCalls["GracefulRestart"] == 0 {
		t.Fatalf("expected attempted GracefulRestart first")
	}

	// Unmount task (CD2), then maintenance (CD1)
	if err := cl.UnmountVirtualMedia(newTestCtx(t), 2); err != nil {
		t.Fatalf("UnmountVirtualMedia cd2: %v", err)
	}
	if fake.vm[fake.vm2].Inserted || fake.vm[fake.vm2].Image != "" {
		t.Fatalf("vm2 not ejected: inserted=%v image=%q", fake.vm[fake.vm2].Inserted, fake.vm[fake.vm2].Image)
	}
	if err := cl.UnmountVirtualMedia(newTestCtx(t), 1); err != nil {
		t.Fatalf("UnmountVirtualMedia cd1: %v", err)
	}
	if fake.vm[fake.vm1].Inserted || fake.vm[fake.vm1].Image != "" {
		t.Fatalf("vm1 not ejected: inserted=%v image=%q", fake.vm[fake.vm1].Inserted, fake.vm[fake.vm1].Image)
	}
}

func TestHTTPClient_DiscoveryFallback_SystemVirtualMedia(t *testing.T) {
	// Manager has no VirtualMedia; System exposes VirtualMedia collection (same path).
	fake := newFakeRedfish()
	fake.managerHasVM = false
	fake.systemExposesVM = true

	srv := httptest.NewServer(fake)
	defer srv.Close()

	cl, err := NewHTTPClient(Config{
		Endpoint: srv.URL,
		Timeout:  2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}

	iso := "https://controller/media/bootc-maintenance.iso"
	if err := cl.MountVirtualMedia(newTestCtx(t), 1, iso); err != nil {
		t.Fatalf("MountVirtualMedia cd1 (fallback): %v", err)
	}
	if !fake.vm[fake.vm1].Inserted || fake.vm[fake.vm1].Image != iso {
		t.Fatalf("vm1 state mismatch after fallback discovery: inserted=%v image=%q", fake.vm[fake.vm1].Inserted, fake.vm[fake.vm1].Image)
	}
}

func TestHTTPClient_IdempotentMount_SameImageSkipsInsert(t *testing.T) {
	fake := newFakeRedfish()
	srv := httptest.NewServer(fake)
	defer srv.Close()

	cl, err := NewHTTPClient(Config{
		Endpoint: srv.URL,
		Timeout:  2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}

	iso := "https://controller/media/tasks/job-1/task.iso"

	// Pre-populate vm1 with the same image already inserted.
	st := fake.vm[fake.vm1]
	st.Inserted = true
	st.Image = iso
	fake.vm[fake.vm1] = st

	// Attempt mount with the same ISO; should not call InsertMedia again.
	if err := cl.MountVirtualMedia(newTestCtx(t), 1, iso); err != nil {
		t.Fatalf("MountVirtualMedia idempotent: %v", err)
	}
	if calls := fake.insertCalls[fake.vm1]; calls != 0 {
		t.Fatalf("expected 0 insert calls for identical image, got %d", calls)
	}
}

/***************
 Fake Redfish server
****************/

type vmState struct {
	Inserted   bool
	Image      string
	MediaTypes []string
}

type fakeRedfish struct {
	// Config toggles
	managerHasVM     bool
	systemExposesVM  bool
	failGraceful     bool
	systemPath       string
	managerPath      string
	vmCollectionPath string
	vm1              string
	vm2              string

	// State
	vm             map[string]vmState
	insertCalls    map[string]int
	ejectCalls     map[string]int
	resetCalls     map[string]int
	lastResetType  string
	bootPatched    bool
	lastBootTarget string
}

func newFakeRedfish() *fakeRedfish {
	f := &fakeRedfish{
		managerHasVM:     true,
		systemExposesVM:  false,
		failGraceful:     false,
		systemPath:       "/redfish/v1/Systems/1",
		managerPath:      "/redfish/v1/Managers/1",
		vmCollectionPath: "/redfish/v1/Managers/1/VirtualMedia",
		vm1:              "/redfish/v1/Managers/1/VirtualMedia/CD1",
		vm2:              "/redfish/v1/Managers/1/VirtualMedia/CD2",
		vm:               map[string]vmState{},
		insertCalls:      map[string]int{},
		ejectCalls:       map[string]int{},
		resetCalls:       map[string]int{},
	}
	// Default VM instances
	f.vm[f.vm1] = vmState{Inserted: false, Image: "", MediaTypes: []string{"CD"}}
	f.vm[f.vm2] = vmState{Inserted: false, Image: "", MediaTypes: []string{"DVD"}}
	return f
}

func (f *fakeRedfish) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		f.handleGET(w, r)
	case http.MethodPost:
		f.handlePOST(w, r)
	case http.MethodPatch:
		f.handlePATCH(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeRedfish) handleGET(w http.ResponseWriter, r *http.Request) {
	setJSON(w)
	switch r.URL.Path {
	case "/redfish/v1/", "/redfish/v1":
		writeJSON(w, http.StatusOK, map[string]any{
			"Systems":  map[string]any{"@odata.id": "/redfish/v1/Systems"},
			"Managers": map[string]any{"@odata.id": "/redfish/v1/Managers"},
		})
	case "/redfish/v1/Systems":
		writeJSON(w, http.StatusOK, map[string]any{
			"Members": []map[string]any{
				{"@odata.id": f.systemPath},
			},
		})
	case f.systemPath:
		sys := map[string]any{
			"Id":   "1",
			"Name": "FakeSystem",
			"Links": map[string]any{
				"ManagedBy": []map[string]any{{"@odata.id": f.managerPath}},
			},
		}
		if f.systemExposesVM {
			sys["VirtualMedia"] = map[string]any{"@odata.id": f.vmCollectionPath}
		} else {
			sys["VirtualMedia"] = map[string]any{"@odata.id": ""}
		}
		writeJSON(w, http.StatusOK, sys)
	case "/redfish/v1/Managers":
		writeJSON(w, http.StatusOK, map[string]any{
			"Members": []map[string]any{
				{"@odata.id": f.managerPath},
			},
		})
	case f.managerPath:
		vm := ""
		if f.managerHasVM {
			vm = f.vmCollectionPath
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"Id":           "1",
			"Name":         "FakeManager",
			"VirtualMedia": map[string]any{"@odata.id": vm},
		})
	case f.vmCollectionPath:
		writeJSON(w, http.StatusOK, map[string]any{
			"Members": []map[string]any{
				{"@odata.id": f.vm1},
				{"@odata.id": f.vm2},
			},
		})
	default:
		// Maybe a virtual media instance
		if st, ok := f.vm[r.URL.Path]; ok {
			writeJSON(w, http.StatusOK, map[string]any{
				"Id":         lastSeg(r.URL.Path),
				"Name":       lastSeg(r.URL.Path),
				"MediaTypes": st.MediaTypes,
				"Inserted":   st.Inserted,
				"Image":      st.Image,
			})
			return
		}
		http.NotFound(w, r)
	}
}

func (f *fakeRedfish) handlePATCH(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != f.systemPath {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Boot struct {
			BootSourceOverrideEnabled string `json:"BootSourceOverrideEnabled"`
			BootSourceOverrideTarget  string `json:"BootSourceOverrideTarget"`
		} `json:"Boot"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	f.bootPatched = true
	f.lastBootTarget = body.Boot.BootSourceOverrideTarget
	setJSON(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (f *fakeRedfish) handlePOST(w http.ResponseWriter, r *http.Request) {
	setJSON(w)
	// Reset action
	if r.URL.Path == f.systemPath+"/Actions/ComputerSystem.Reset" {
		var body struct {
			ResetType string `json:"ResetType"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		f.resetCalls[body.ResetType]++
		if body.ResetType == "GracefulRestart" && f.failGraceful {
			http.Error(w, "simulated failure", http.StatusInternalServerError)
			return
		}
		f.lastResetType = body.ResetType
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}

	// VM actions: InsertMedia / EjectMedia
	const ins = "/Actions/VirtualMedia.InsertMedia"
	const ej = "/Actions/VirtualMedia.EjectMedia"
	path := r.URL.Path
	switch {
	case strings.HasSuffix(path, ins):
		vmPath := strings.TrimSuffix(path, ins)
		st, ok := f.vm[vmPath]
		if !ok {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Image string `json:"Image"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		st.Inserted = true
		st.Image = body.Image
		f.vm[vmPath] = st
		f.insertCalls[vmPath]++
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	case strings.HasSuffix(path, ej):
		vmPath := strings.TrimSuffix(path, ej)
		st, ok := f.vm[vmPath]
		if !ok {
			http.NotFound(w, r)
			return
		}
		st.Inserted = false
		st.Image = ""
		f.vm[vmPath] = st
		f.ejectCalls[vmPath]++
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	default:
		http.NotFound(w, r)
	}
}

func setJSON(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func lastSeg(p string) string {
	p = strings.TrimSuffix(p, "/")
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

/***************
 Test helpers
****************/

// newTestCtx returns a cancellable context that will be canceled
// by the test harness automatically when the test finishes.
type neverCtx struct{}

func (neverCtx) Deadline() (time.Time, bool) { return time.Time{}, false }
func (neverCtx) Done() <-chan struct{}       { return nil }
func (neverCtx) Err() error                  { return nil }
func (neverCtx) Value(key any) any           { return nil }

func newTestCtx(t *testing.T) neverCtx {
	t.Helper()
	return neverCtx{}
}

func TestHTTPClient_SessionRetryOn401(t *testing.T) {
	type state struct {
		token        string
		issuedToken  bool
		gotBootPatch bool
	}
	st := &state{token: "TEST-TOKEN"}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setJSON(w)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/redfish/v1/SessionService/Sessions":
			// Create session and return token
			w.Header().Set("X-Auth-Token", st.token)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"Id": "sess1"})
			st.issuedToken = true
			return

		case r.Method == http.MethodGet && (r.URL.Path == "/redfish/v1/" || r.URL.Path == "/redfish/v1"):
			if r.Header.Get("X-Auth-Token") != st.token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Systems":  map[string]any{"@odata.id": "/redfish/v1/Systems"},
				"Managers": map[string]any{"@odata.id": "/redfish/v1/Managers"},
			})
			return

		case r.Method == http.MethodGet && r.URL.Path == "/redfish/v1/Systems":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Members": []map[string]any{{"@odata.id": "/redfish/v1/Systems/1"}},
			})
			return

		case r.Method == http.MethodGet && r.URL.Path == "/redfish/v1/Systems/1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Id":   "1",
				"Name": "SessSys",
				"Links": map[string]any{
					"ManagedBy": []map[string]any{{"@odata.id": "/redfish/v1/Managers/1"}},
				},
			})
			return

		case r.Method == http.MethodGet && r.URL.Path == "/redfish/v1/Managers":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Members": []map[string]any{{"@odata.id": "/redfish/v1/Managers/1"}},
			})
			return

		case r.Method == http.MethodGet && r.URL.Path == "/redfish/v1/Managers/1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Id":           "1",
				"Name":         "SessMgr",
				"VirtualMedia": map[string]any{"@odata.id": "/redfish/v1/Managers/1/VirtualMedia"},
			})
			return

		case r.Method == http.MethodGet && r.URL.Path == "/redfish/v1/Managers/1/VirtualMedia":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Members": []map[string]any{{"@odata.id": "/redfish/v1/Managers/1/VirtualMedia/CD"}},
			})
			return

		case r.Method == http.MethodGet && r.URL.Path == "/redfish/v1/Managers/1/VirtualMedia/CD":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Id":         "CD",
				"Name":       "CD",
				"MediaTypes": []string{"CD"},
				"Inserted":   false,
				"Image":      "",
			})
			return

		case r.Method == http.MethodPatch && r.URL.Path == "/redfish/v1/Systems/1":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if _, ok := body["Boot"]; ok {
				st.gotBootPatch = true
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
			return
		}

		http.NotFound(w, r)
	}))
	defer srv.Close()

	cl, err := NewHTTPClient(Config{
		Endpoint: srv.URL,
		Username: "user",
		Password: "pass",
		Timeout:  2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}

	// Call an operation that triggers discovery, which should 401 then create session, then proceed.
	if err := cl.SetOneTimeBoot(newTestCtx(t), BootDeviceCD); err != nil {
		t.Fatalf("SetOneTimeBoot with session retry failed: %v", err)
	}
	if !st.issuedToken || !st.gotBootPatch {
		t.Fatalf("expected session token issuance and boot patch; issued=%v, patched=%v", st.issuedToken, st.gotBootPatch)
	}
}

func TestHTTPClient_RetryBackoffOn5xx_InsertMedia(t *testing.T) {
	type svc struct {
		insertAttempts int
	}
	s := &svc{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setJSON(w)
		switch {
		case r.Method == http.MethodGet && (r.URL.Path == "/redfish/v1/" || r.URL.Path == "/redfish/v1"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Systems":  map[string]any{"@odata.id": "/redfish/v1/Systems"},
				"Managers": map[string]any{"@odata.id": "/redfish/v1/Managers"},
			})
			return
		case r.Method == http.MethodGet && r.URL.Path == "/redfish/v1/Systems":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Members": []map[string]any{{"@odata.id": "/redfish/v1/Systems/1"}},
			})
			return
		case r.Method == http.MethodGet && r.URL.Path == "/redfish/v1/Systems/1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Id":   "1",
				"Name": "RetrySys",
				"Links": map[string]any{
					"ManagedBy": []map[string]any{{"@odata.id": "/redfish/v1/Managers/1"}},
				},
			})
			return
		case r.Method == http.MethodGet && r.URL.Path == "/redfish/v1/Managers":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Members": []map[string]any{{"@odata.id": "/redfish/v1/Managers/1"}},
			})
			return
		case r.Method == http.MethodGet && r.URL.Path == "/redfish/v1/Managers/1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Id":           "1",
				"Name":         "RetryMgr",
				"VirtualMedia": map[string]any{"@odata.id": "/redfish/v1/Managers/1/VirtualMedia"},
			})
			return
		case r.Method == http.MethodGet && r.URL.Path == "/redfish/v1/Managers/1/VirtualMedia":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Members": []map[string]any{{"@odata.id": "/redfish/v1/Managers/1/VirtualMedia/CD"}},
			})
			return
		case r.Method == http.MethodGet && r.URL.Path == "/redfish/v1/Managers/1/VirtualMedia/CD":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Id":         "CD",
				"Name":       "CD",
				"MediaTypes": []string{"CD"},
				"Inserted":   false,
				"Image":      "",
			})
			return
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/Actions/VirtualMedia.InsertMedia"):
			s.insertAttempts++
			if s.insertAttempts < 3 {
				http.Error(w, "temporary failure", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cl, err := NewHTTPClient(Config{
		Endpoint: srv.URL,
		Timeout:  3 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}

	// Mount should retry on 5xx and eventually succeed.
	if err := cl.MountVirtualMedia(newTestCtx(t), 1, "https://controller/media/task.iso"); err != nil {
		t.Fatalf("MountVirtualMedia with retry failed: %v", err)
	}
	if s.insertAttempts < 3 {
		t.Fatalf("expected at least 3 attempts (2 failures + success), got %d", s.insertAttempts)
	}
}

func TestHTTPClient_BootOverrideModeForVendors(t *testing.T) {
	type capReq struct {
		lastMode string
	}
	run := func(t *testing.T, vendor string, expectMode bool) {
		t.Helper()
		cr := &capReq{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			setJSON(w)
			switch {
			case r.Method == http.MethodGet && (r.URL.Path == "/redfish/v1/" || r.URL.Path == "/redfish/v1"):
				_ = json.NewEncoder(w).Encode(map[string]any{
					"Systems":  map[string]any{"@odata.id": "/redfish/v1/Systems"},
					"Managers": map[string]any{"@odata.id": "/redfish/v1/Managers"},
				})
				return
			case r.Method == http.MethodGet && r.URL.Path == "/redfish/v1/Systems":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"Members": []map[string]any{{"@odata.id": "/redfish/v1/Systems/1"}},
				})
				return
			case r.Method == http.MethodGet && r.URL.Path == "/redfish/v1/Systems/1":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"Id":   "1",
					"Name": "ModeSys",
					"Links": map[string]any{
						"ManagedBy": []map[string]any{{"@odata.id": "/redfish/v1/Managers/1"}},
					},
				})
				return
			case r.Method == http.MethodGet && r.URL.Path == "/redfish/v1/Managers":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"Members": []map[string]any{{"@odata.id": "/redfish/v1/Managers/1"}},
				})
				return
			case r.Method == http.MethodGet && r.URL.Path == "/redfish/v1/Managers/1":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"Id":           "1",
					"Name":         "ModeMgr",
					"VirtualMedia": map[string]any{"@odata.id": "/redfish/v1/Managers/1/VirtualMedia"},
				})
				return
			case r.Method == http.MethodGet && r.URL.Path == "/redfish/v1/Managers/1/VirtualMedia":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"Members": []map[string]any{{"@odata.id": "/redfish/v1/Managers/1/VirtualMedia/CD"}},
				})
				return
			case r.Method == http.MethodGet && r.URL.Path == "/redfish/v1/Managers/1/VirtualMedia/CD":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"Id":         "CD",
					"Name":       "CD",
					"MediaTypes": []string{"CD"},
					"Inserted":   false,
					"Image":      "",
				})
				return
			case r.Method == http.MethodPatch && r.URL.Path == "/redfish/v1/Systems/1":
				var body struct {
					Boot struct {
						BootSourceOverrideEnabled string `json:"BootSourceOverrideEnabled"`
						BootSourceOverrideTarget  string `json:"BootSourceOverrideTarget"`
						BootSourceOverrideMode    string `json:"BootSourceOverrideMode"`
					} `json:"Boot"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				cr.lastMode = body.Boot.BootSourceOverrideMode
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
				return
			}
			http.NotFound(w, r)
		}))
		defer srv.Close()

		cl, err := NewHTTPClient(Config{
			Endpoint: srv.URL,
			Vendor:   vendor,
			Timeout:  2 * time.Second,
		})
		if err != nil {
			t.Fatalf("NewHTTPClient: %v", err)
		}
		if err := cl.SetOneTimeBoot(newTestCtx(t), BootDeviceCD); err != nil {
			t.Fatalf("SetOneTimeBoot vendor=%s failed: %v", vendor, err)
		}
		if expectMode && cr.lastMode != "UEFI" {
			t.Fatalf("expected BootSourceOverrideMode UEFI for vendor=%s; got %q", vendor, cr.lastMode)
		}
		if !expectMode && cr.lastMode != "" {
			t.Fatalf("expected no BootSourceOverrideMode for vendor=%s; got %q", vendor, cr.lastMode)
		}
	}

	t.Run("iDRAC", func(t *testing.T) { run(t, "iDRAC", true) })
	t.Run("iLO", func(t *testing.T) { run(t, "iLO", true) })
	t.Run("Supermicro", func(t *testing.T) { run(t, "Supermicro", false) })
}
