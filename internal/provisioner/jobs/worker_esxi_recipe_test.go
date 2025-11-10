package jobs

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

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"shoal/internal/provisioner/iso"
	"shoal/internal/provisioner/redfish"
	"shoal/pkg/provisioner"
)

// fakeStoreESXi is a minimal store for processJob tests.
type fakeStoreESXi struct {
	job    *provisioner.Job
	srv    *provisioner.Server
	events []provisioner.JobEvent
}

func (f *fakeStoreESXi) GetServerBySerial(ctx context.Context, serial string) (*provisioner.Server, error) {
	if f.srv == nil || f.srv.Serial != serial {
		return nil, errors.New("not found")
	}
	return f.srv, nil
}
func (f *fakeStoreESXi) AcquireQueuedJob(ctx context.Context, workerID string, leaseTTL time.Duration) (*provisioner.Job, error) {
	return nil, errors.New("unused")
}
func (f *fakeStoreESXi) ExtendLease(ctx context.Context, jobID, workerID string, leaseTTL time.Duration) (bool, error) {
	return true, nil
}
func (f *fakeStoreESXi) GetJobByID(ctx context.Context, id string) (*provisioner.Job, error) {
	return f.job, nil
}
func (f *fakeStoreESXi) MarkJobStatus(ctx context.Context, id string, status provisioner.JobStatus, failedStep *string) error {
	if f.job != nil && f.job.ID == id {
		f.job.Status = status
		f.job.FailedStep = failedStep
		return nil
	}
	return errors.New("not found")
}
func (f *fakeStoreESXi) UpdateJobTaskISOPath(ctx context.Context, id, path string) error { return nil }
func (f *fakeStoreESXi) AppendJobEvent(ctx context.Context, ev provisioner.JobEvent) error {
	f.events = append(f.events, ev)
	return nil
}

type spyISOBuilder struct{ lastAssets iso.Assets }

func (s *spyISOBuilder) BuildTaskISO(ctx context.Context, jobID string, recipe json.RawMessage, assets iso.Assets) (*iso.Result, error) {
	s.lastAssets = assets
	return &iso.Result{Path: "/tmp/placeholder.iso", Size: 123, SHA256: "deadbeef"}, nil
}

type okRFClient struct{}

func (okRFClient) Ping(ctx context.Context) error { return nil }
func (okRFClient) SystemPowerState(ctx context.Context) (redfish.PowerState, error) {
	return redfish.PowerStateOn, nil
}
func (okRFClient) MountVirtualMedia(ctx context.Context, cd int, url string) error { return nil }
func (okRFClient) SetOneTimeBoot(ctx context.Context, d redfish.BootDevice) error  { return nil }
func (okRFClient) Reboot(ctx context.Context, m redfish.RebootMode) error          { return nil }
func (okRFClient) UnmountVirtualMedia(ctx context.Context, cd int) error           { return nil }
func (okRFClient) Close() error                                                    { return nil }

func TestProcessJob_ESXiMissingKickstartFailsEarly(t *testing.T) {
	job := &provisioner.Job{ID: "j1", ServerSerial: "s1", Status: provisioner.JobStatusQueued, Recipe: json.RawMessage(`{"task_target":"install-esxi.target"}`)}
	store := &fakeStoreESXi{job: job, srv: &provisioner.Server{Serial: "s1", BMCAddress: "https://bmc.local", BMCUser: "u", BMCPass: "p"}}
	builder := &spyISOBuilder{}
	w := NewWorker(store, builder, func(context.Context, *provisioner.Server) (redfish.Client, error) { return okRFClient{}, nil }, WorkerConfig{ESXIInstallerURL: "https://controller/VMware.iso"}, nil)

	if err := w.processJob(context.Background(), job); err == nil {
		t.Fatalf("expected error due to missing ks_cfg, got nil")
	}
	if job.Status != provisioner.JobStatusFailed {
		t.Fatalf("expected job failed, got %s", job.Status)
	}
	if job.FailedStep == nil || *job.FailedStep != "validate-recipe" {
		t.Fatalf("expected failed_step validate-recipe, got %v", job.FailedStep)
	}
}

func TestProcessJob_ESXiEmbedsKickstartInISO(t *testing.T) {
	recipe := json.RawMessage(`{"task_target":"install-esxi.target","ks_cfg":"vmaccepteula\ninstall --firstdisk --overwritevmfs\nreboot\n"}`)
	job := &provisioner.Job{ID: "j2", ServerSerial: "s2", Status: provisioner.JobStatusQueued, Recipe: recipe}
	store := &fakeStoreESXi{job: job, srv: &provisioner.Server{Serial: "s2", BMCAddress: "https://bmc.local", BMCUser: "u", BMCPass: "p"}}
	builder := &spyISOBuilder{}
	w := NewWorker(store, builder, func(context.Context, *provisioner.Server) (redfish.Client, error) { return okRFClient{}, nil }, WorkerConfig{ESXIInstallerURL: "https://controller/VMware.iso", RedfishTimeout: 50 * time.Millisecond, TaskISOMediaBase: "http://controller/media/tasks"}, nil)

	// processJob should proceed and embed ks.cfg into ISO assets
	if err := w.processJob(context.Background(), job); err != nil {
		t.Fatalf("processJob returned error: %v", err)
	}
	if len(builder.lastAssets.Kickstart) == 0 {
		t.Fatalf("expected Kickstart content to be embedded in ISO assets")
	}
}

func TestProcessJob_ESXiKickstartTooLargeFails(t *testing.T) {
	// Create a ks.cfg larger than 64KiB
	big := strings.Repeat("x", 64*1024+1)
	recipe := json.RawMessage(`{"task_target":"install-esxi.target","ks_cfg":"` + big + `"}`)
	job := &provisioner.Job{ID: "j3", ServerSerial: "s3", Status: provisioner.JobStatusQueued, Recipe: recipe}
	store := &fakeStoreESXi{job: job, srv: &provisioner.Server{Serial: "s3", BMCAddress: "https://bmc.local", BMCUser: "u", BMCPass: "p"}}
	builder := &spyISOBuilder{}
	w := NewWorker(store, builder, func(context.Context, *provisioner.Server) (redfish.Client, error) { return okRFClient{}, nil }, WorkerConfig{ESXIInstallerURL: "https://controller/VMware.iso"}, nil)

	if err := w.processJob(context.Background(), job); err == nil {
		t.Fatalf("expected error due to oversized ks_cfg, got nil")
	}
	if job.Status != provisioner.JobStatusFailed {
		t.Fatalf("expected job failed, got %s", job.Status)
	}
	if job.FailedStep == nil || *job.FailedStep != "validate-recipe" {
		t.Fatalf("expected failed_step validate-recipe, got %v", job.FailedStep)
	}
}
