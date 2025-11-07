package store

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

// Tests for the store layer: migrations, server CRUD, and active provisioning job lookup.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"shoal/pkg/provisioner"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	s, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open store failed: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenAndMigrations_ServerCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Upsert server
	now := time.Now().UTC().Add(-time.Hour)
	ser := provisioner.Server{
		Serial:     "SERIAL-123",
		BMCAddress: "https://bmc.local",
		BMCUser:    "root",
		BMCPass:    "toor",
		Vendor:     "acme",
		LastSeen:   &now,
	}
	if err := s.UpsertServer(ctx, ser); err != nil {
		t.Fatalf("UpsertServer failed: %v", err)
	}

	// Read it back
	got, err := s.GetServerBySerial(ctx, ser.Serial)
	if err != nil {
		t.Fatalf("GetServerBySerial failed: %v", err)
	}
	if got.Serial != ser.Serial || got.BMCAddress != ser.BMCAddress || got.BMCUser != ser.BMCUser || got.BMCPass != ser.BMCPass || got.Vendor != ser.Vendor {
		t.Fatalf("server mismatch:\n got: %+v\nwant: %+v", got, ser)
	}
	if got.LastSeen == nil || !got.LastSeen.UTC().Equal(now.UTC()) {
		t.Fatalf("server LastSeen mismatch: got=%v want=%v", got.LastSeen, now)
	}

	// Update fields and upsert again
	ser.BMCAddress = "https://bmc2.local"
	ser.BMCUser = "admin"
	ser.Vendor = "contoso"
	if err := s.UpsertServer(ctx, ser); err != nil {
		t.Fatalf("UpsertServer (update) failed: %v", err)
	}
	got2, err := s.GetServerBySerial(ctx, ser.Serial)
	if err != nil {
		t.Fatalf("GetServerBySerial (after update) failed: %v", err)
	}
	if got2.BMCAddress != "https://bmc2.local" || got2.BMCUser != "admin" || got2.Vendor != "contoso" {
		t.Fatalf("server update not applied: %+v", got2)
	}
}

func TestInsertJobRequiresExistingServer(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// No server seeded; inserting job should fail due to FK constraint.
	j := provisioner.NewJob("MISSING-SERIAL", json.RawMessage(`{"task_target":"install-linux.target"}`), "http://controller/media/maintenance.iso")
	j.ID = "job-1"

	err := s.InsertJob(ctx, &j)
	if err == nil {
		t.Fatalf("InsertJob succeeded unexpectedly; expected FK error without server")
	}
}

func TestJobInsertGetAndStatusTransition(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Seed server
	ser := provisioner.Server{
		Serial:     "SER-ABC",
		BMCAddress: "https://bmc.example",
		BMCUser:    "root",
		BMCPass:    "pw",
		Vendor:     "acme",
	}
	if err := s.UpsertServer(ctx, ser); err != nil {
		t.Fatalf("UpsertServer failed: %v", err)
	}

	// Insert job (queued)
	recipe := json.RawMessage(`{"task_target":"install-linux.target","target_disk":"/dev/sda"}`)
	j := provisioner.NewJob(ser.Serial, recipe, "http://controller/media/maintenance.iso")
	j.ID = "job-xyz"
	if err := s.InsertJob(ctx, &j); err != nil {
		t.Fatalf("InsertJob failed: %v", err)
	}

	// Fetch job
	got, err := s.GetJobByID(ctx, j.ID)
	if err != nil {
		t.Fatalf("GetJobByID failed: %v", err)
	}
	if got.ID != j.ID || got.ServerSerial != ser.Serial || got.Status != provisioner.JobStatusQueued {
		t.Fatalf("job mismatch: got=%+v want.id=%s want.serial=%s want.status=%s", got, j.ID, ser.Serial, provisioner.JobStatusQueued)
	}
	if string(got.Recipe) != string(recipe) {
		t.Fatalf("job recipe mismatch: got=%s want=%s", string(got.Recipe), string(recipe))
	}

	// Transition to provisioning, then to succeeded, then complete
	if err := s.MarkJobStatus(ctx, j.ID, provisioner.JobStatusProvisioning, nil); err != nil {
		t.Fatalf("MarkJobStatus provisioning failed: %v", err)
	}
	if err := s.MarkJobStatus(ctx, j.ID, provisioner.JobStatusSucceeded, nil); err != nil {
		t.Fatalf("MarkJobStatus succeeded failed: %v", err)
	}
	if err := s.MarkJobStatus(ctx, j.ID, provisioner.JobStatusComplete, nil); err != nil {
		t.Fatalf("MarkJobStatus complete failed: %v", err)
	}
	got2, err := s.GetJobByID(ctx, j.ID)
	if err != nil {
		t.Fatalf("GetJobByID after transitions failed: %v", err)
	}
	if got2.Status != provisioner.JobStatusComplete {
		t.Fatalf("job status mismatch after transitions: got=%s want=%s", got2.Status, provisioner.JobStatusComplete)
	}
}

func TestGetActiveProvisioningJobBySerial_OrderAndNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Seed server
	ser := provisioner.Server{
		Serial:     "SER-ORDER",
		BMCAddress: "https://bmc.order",
		BMCUser:    "root",
		BMCPass:    "pw",
	}
	if err := s.UpsertServer(ctx, ser); err != nil {
		t.Fatalf("UpsertServer failed: %v", err)
	}

	// Initially, no active job
	if _, err := s.GetActiveProvisioningJobBySerial(ctx, ser.Serial); err == nil {
		t.Fatalf("expected not found for active job when none exist")
	}

	base := time.Now().UTC().Add(-2 * time.Hour)

	// Insert an older provisioning job
	j1 := provisioner.Job{
		ID:                "job-old",
		ServerSerial:      ser.Serial,
		Status:            provisioner.JobStatusProvisioning,
		FailedStep:        nil,
		Recipe:            json.RawMessage(`{"x":1}`),
		CreatedAt:         base.Add(10 * time.Minute),
		UpdatedAt:         base.Add(10 * time.Minute),
		PickedAt:          ptrTime(base.Add(10 * time.Minute)),
		WorkerID:          ptrString("w1"),
		LeaseExpiresAt:    ptrTime(base.Add(20 * time.Minute)),
		TaskISOPath:       nil,
		MaintenanceISOURL: "http://controller/maint.iso",
	}
	if err := s.InsertJob(ctx, &j1); err != nil {
		t.Fatalf("InsertJob j1 failed: %v", err)
	}

	// Insert a newer provisioning job
	j2 := provisioner.Job{
		ID:                "job-new",
		ServerSerial:      ser.Serial,
		Status:            provisioner.JobStatusProvisioning,
		FailedStep:        nil,
		Recipe:            json.RawMessage(`{"x":2}`),
		CreatedAt:         base.Add(30 * time.Minute),
		UpdatedAt:         base.Add(30 * time.Minute),
		PickedAt:          ptrTime(base.Add(30 * time.Minute)),
		WorkerID:          ptrString("w2"),
		LeaseExpiresAt:    ptrTime(base.Add(40 * time.Minute)),
		TaskISOPath:       nil,
		MaintenanceISOURL: "http://controller/maint.iso",
	}
	if err := s.InsertJob(ctx, &j2); err != nil {
		t.Fatalf("InsertJob j2 failed: %v", err)
	}

	// Lookup should return the newest provisioning job (j2)
	got, err := s.GetActiveProvisioningJobBySerial(ctx, ser.Serial)
	if err != nil {
		t.Fatalf("GetActiveProvisioningJobBySerial failed: %v", err)
	}
	if got.ID != "job-new" {
		t.Fatalf("expected newest provisioning job 'job-new', got %q", got.ID)
	}

	// Transition j2 to succeeded; active job should now be the older provisioning job
	if err := s.MarkJobStatus(ctx, "job-new", provisioner.JobStatusSucceeded, nil); err != nil {
		t.Fatalf("MarkJobStatus succeeded failed: %v", err)
	}
	got2, err := s.GetActiveProvisioningJobBySerial(ctx, ser.Serial)
	if err != nil {
		t.Fatalf("expected active job after transitioning newest out; got error: %v", err)
	}
	if got2.ID != "job-old" {
		t.Fatalf("expected remaining provisioning job 'job-old', got %q", got2.ID)
	}
	// Transition j1 out of provisioning; now expect not found
	if err := s.MarkJobStatus(ctx, "job-old", provisioner.JobStatusSucceeded, nil); err != nil {
		t.Fatalf("MarkJobStatus succeeded (job-old) failed: %v", err)
	}
	if _, err := s.GetActiveProvisioningJobBySerial(ctx, ser.Serial); err == nil {
		t.Fatalf("expected not found after transitioning all jobs out of provisioning")
	}
}

func TestListJobsByStatusAndRequeueProvisioning(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	ser := provisioner.Server{
		Serial:     "SER-RECQ",
		BMCAddress: "https://bmc.requeue",
		BMCUser:    "root",
		BMCPass:    "pw",
	}
	if err := s.UpsertServer(ctx, ser); err != nil {
		t.Fatalf("UpsertServer failed: %v", err)
	}

	provisioning := provisioner.NewJob(ser.Serial, json.RawMessage(`{"task_target":"install-linux.target"}`), "http://controller/maint.iso")
	provisioning.ID = "job-prov"
	if err := s.InsertJob(ctx, &provisioning); err != nil {
		t.Fatalf("InsertJob provisioning failed: %v", err)
	}
	if _, err := s.AcquireQueuedJob(ctx, "worker-old", time.Minute); err != nil {
		t.Fatalf("AcquireQueuedJob failed: %v", err)
	}

	queued := provisioner.NewJob(ser.Serial, json.RawMessage(`{"task_target":"install-linux.target"}`), "http://controller/maint.iso")
	queued.ID = "job-queued"
	if err := s.InsertJob(ctx, &queued); err != nil {
		t.Fatalf("InsertJob queued failed: %v", err)
	}

	jobs, err := s.ListJobsByStatus(ctx, provisioner.JobStatusProvisioning)
	if err != nil {
		t.Fatalf("ListJobsByStatus failed: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != provisioning.ID {
		t.Fatalf("expected one provisioning job %s, got %+v", provisioning.ID, jobs)
	}

	if err := s.RequeueProvisioningJob(ctx, provisioning.ID); err != nil {
		t.Fatalf("RequeueProvisioningJob failed: %v", err)
	}

	requeued, err := s.GetJobByID(ctx, provisioning.ID)
	if err != nil {
		t.Fatalf("GetJobByID failed: %v", err)
	}
	if requeued.Status != provisioner.JobStatusQueued {
		t.Fatalf("expected status queued after requeue, got %s", requeued.Status)
	}
	if requeued.WorkerID != nil || requeued.LeaseExpiresAt != nil || requeued.PickedAt != nil {
		t.Fatalf("expected lease fields cleared after requeue: %+v", requeued)
	}
	if requeued.TaskISOPath != nil {
		t.Fatalf("expected task iso path cleared after requeue")
	}

	if err := s.RequeueProvisioningJob(ctx, provisioning.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound requeueing non-provisioning job, got %v", err)
	}
}

func ptrString(s string) *string { return &s }

func ptrTime(ti time.Time) *time.Time { return &ti }
