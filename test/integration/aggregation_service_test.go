package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"shoal/pkg/auth"
	"shoal/pkg/models"
)

func TestAggregationServiceRoot(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.Server.URL+"/redfish/v1/AggregationService", nil)
	req.SetBasicAuth("admin", "admin")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if body["Name"] != "Aggregation Service" {
		t.Errorf("expected Name 'Aggregation Service', got %v", body["Name"])
	}
	as, ok := body["AggregationSources"].(map[string]interface{})
	if !ok || as["@odata.id"] != "/redfish/v1/AggregationService/AggregationSources" {
		t.Errorf("expected link to AggregationSources, got %v", body["AggregationSources"])
	}
}

func TestAggregationSourcesCRUD(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	client := &http.Client{}

	// 1) List (should be empty)
	req, _ := http.NewRequest("GET", ts.Server.URL+"/redfish/v1/AggregationService/AggregationSources", nil)
	req.SetBasicAuth("admin", "admin")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var list map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("parse list: %v", err)
	}
	resp.Body.Close()

	// 2) Create
	create := map[string]interface{}{
		"Name":        "bmc-01",
		"HostName":    "192.168.1.50",
		"UserName":    "root",
		"Password":    "calvin",
		"Description": "test node",
		"Enabled":     true,
	}
	buf, _ := json.Marshal(create)
	req, _ = http.NewRequest("POST", ts.Server.URL+"/redfish/v1/AggregationService/AggregationSources", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "admin")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, string(b))
	}
	var created map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("parse create: %v", err)
	}
	resp.Body.Close()

	if _, has := created["Password"]; has {
		t.Errorf("password should not be returned in response")
	}

	memberURL, _ := created["@odata.id"].(string)
	if memberURL == "" {
		t.Fatalf("missing member @odata.id")
	}

	// 3) Read
	req, _ = http.NewRequest("GET", ts.Server.URL+memberURL, nil)
	req.SetBasicAuth("admin", "admin")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("get member failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var member map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&member); err != nil {
		t.Fatalf("parse member: %v", err)
	}
	resp.Body.Close()
	if member["Name"] != "bmc-01" {
		t.Errorf("expected Name bmc-01, got %v", member["Name"])
	}

	// 4) Update
	patch := map[string]interface{}{
		"HostName": "192.168.1.51",
		"Enabled":  false,
	}
	buf, _ = json.Marshal(patch)
	req, _ = http.NewRequest("PATCH", ts.Server.URL+memberURL, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "admin")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("patch failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var updated map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		t.Fatalf("parse patch: %v", err)
	}
	resp.Body.Close()
	if updated["HostName"] != "192.168.1.51" {
		t.Errorf("expected HostName updated, got %v", updated["HostName"])
	}
	if updated["Enabled"] != false {
		t.Errorf("expected Enabled=false, got %v", updated["Enabled"])
	}

	// 5) Delete
	req, _ = http.NewRequest("DELETE", ts.Server.URL+memberURL, nil)
	req.SetBasicAuth("admin", "admin")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 6) Verify 404
	req, _ = http.NewRequest("GET", ts.Server.URL+memberURL, nil)
	req.SetBasicAuth("admin", "admin")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("get after delete failed: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestAggregationSourcesRBAC(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Create a viewer user
	passwordHash, err := auth.HashPassword("view")
	if err != nil {
		t.Fatalf("hash failed: %v", err)
	}
	viewer := &models.User{ID: "viewer-1", Username: "viewer", PasswordHash: passwordHash, Role: models.RoleViewer, Enabled: true}
	if err := ts.DB.CreateUser(context.Background(), viewer); err != nil {
		t.Fatalf("failed to create viewer: %v", err)
	}

	client := &http.Client{}

	// Viewer can GET collection
	req, _ := http.NewRequest("GET", ts.Server.URL+"/redfish/v1/AggregationService/AggregationSources", nil)
	req.SetBasicAuth("viewer", "view")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("viewer GET failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Viewer cannot POST (403)
	create := map[string]interface{}{
		"Name":     "bmc-rbac",
		"HostName": "10.0.0.1",
		"UserName": "root",
		"Password": "pw",
	}
	buf, _ := json.Marshal(create)
	req, _ = http.NewRequest("POST", ts.Server.URL+"/redfish/v1/AggregationService/AggregationSources", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("viewer", "view")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("viewer POST failed: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestManagedNodesRedirect(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Request legacy path; expect redirect to AggregationSources
	req, _ := http.NewRequest("GET", ts.Server.URL+"/redfish/v1/AggregationService/ManagedNodes", nil)
	req.SetBasicAuth("admin", "admin")
	noFollow := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := noFollow.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("expected 307 redirect, got %d", resp.StatusCode)
	}

	loc := resp.Header.Get("Location")
	if loc != "/redfish/v1/AggregationService/AggregationSources" {
		t.Fatalf("unexpected redirect location: %s", loc)
	}
}
