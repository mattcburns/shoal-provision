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

package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"path/filepath"
	"testing"
	"time"

	"shoal/internal/provisioner/store"
	"shoal/pkg/provisioner"
)

func newTestStoreForController(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	db := filepath.Join(dir, "controller.db")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	st, err := store.Open(ctx, db)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestReconcileProvisioningJobs(t *testing.T) {
	st := newTestStoreForController(t)
	ctx := context.Background()

	server := provisioner.Server{
		Serial:     "SER-RECON",
		BMCAddress: "https://bmc.recon",
		BMCUser:    "root",
		BMCPass:    "pw",
	}
	if err := st.UpsertServer(ctx, server); err != nil {
		t.Fatalf("UpsertServer: %v", err)
	}

	job := provisioner.NewJob(server.Serial, json.RawMessage(`{"task_target":"install-linux.target"}`), "http://controller/maint.iso")
	job.ID = "job-recon"
	if err := st.InsertJob(ctx, &job); err != nil {
		t.Fatalf("InsertJob: %v", err)
	}

	if _, err := st.AcquireQueuedJob(ctx, "worker-old", time.Minute); err != nil {
		t.Fatalf("AcquireQueuedJob: %v", err)
	}

	logger := log.New(io.Discard, "", 0)
	if err := reconcileProvisioningJobs(ctx, st, logger); err != nil {
		t.Fatalf("reconcileProvisioningJobs: %v", err)
	}

	updated, err := st.GetJobByID(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJobByID: %v", err)
	}
	if updated.Status != provisioner.JobStatusQueued {
		t.Fatalf("expected job status queued after reconcile, got %s", updated.Status)
	}
	if updated.WorkerID != nil || updated.LeaseExpiresAt != nil || updated.PickedAt != nil {
		t.Fatalf("expected lease fields cleared after reconcile: %+v", updated)
	}

	events, err := st.ListJobEvents(ctx, job.ID, 0)
	if err != nil {
		t.Fatalf("ListJobEvents: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("expected reconciliation event to be recorded")
	}
	last := events[len(events)-1]
	if last.Step == nil || *last.Step != "reconcile" {
		t.Fatalf("expected reconcile step, got %+v", last)
	}
	if last.Message == "" || last.Level != provisioner.EventLevelInfo {
		t.Fatalf("unexpected reconciliation event contents: %+v", last)
	}
}
