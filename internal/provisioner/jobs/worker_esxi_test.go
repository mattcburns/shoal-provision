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
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"shoal/internal/provisioner/redfish"
	"shoal/pkg/provisioner"
)

type esxiStubClient struct {
	mu          sync.Mutex
	pingErrs    int
	powerStates []redfish.PowerState
	powerErrors []error
	idx         int
}

func (c *esxiStubClient) Ping(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pingErrs > 0 {
		c.pingErrs--
		return errors.New("offline")
	}
	return nil
}

func (c *esxiStubClient) SystemPowerState(ctx context.Context) (redfish.PowerState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.powerErrors) > 0 {
		err := c.powerErrors[0]
		c.powerErrors = c.powerErrors[1:]
		if err != nil {
			return redfish.PowerStateUnknown, err
		}
	}
	if len(c.powerStates) == 0 {
		return redfish.PowerStateUnknown, errors.New("no state")
	}
	if c.idx >= len(c.powerStates) {
		return c.powerStates[len(c.powerStates)-1], nil
	}
	st := c.powerStates[c.idx]
	c.idx++
	return st, nil
}

func (c *esxiStubClient) MountVirtualMedia(context.Context, int, string) error     { return nil }
func (c *esxiStubClient) SetOneTimeBoot(context.Context, redfish.BootDevice) error { return nil }
func (c *esxiStubClient) Reboot(context.Context, redfish.RebootMode) error         { return nil }
func (c *esxiStubClient) UnmountVirtualMedia(context.Context, int) error           { return nil }
func (c *esxiStubClient) Close() error                                             { return nil }

func TestRunESXiCompletion_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)

	job := &provisioner.Job{ID: "job-esxi", Status: provisioner.JobStatusProvisioning}
	fs := &fakeStore{job: job}

	w := &Worker{
		store: fs,
		cfg: WorkerConfig{
			ESXIInstallTimeout:    2 * time.Second,
			ESXIStableWindow:      80 * time.Millisecond,
			ESXIPollIntervalStart: 10 * time.Millisecond,
			ESXIPollIntervalMax:   40 * time.Millisecond,
		},
		now: func() time.Time { return time.Now() },
	}

	rf := &esxiStubClient{
		pingErrs: 1,
		powerStates: []redfish.PowerState{
			redfish.PowerStatePoweringOn,
			redfish.PowerStatePoweringOn,
			redfish.PowerStateOn,
			redfish.PowerStateOn,
		},
	}

	status, err := w.runESXiCompletion(ctx, job, rf)
	if err != nil {
		t.Fatalf("runESXiCompletion returned error: %v", err)
	}
	if status != provisioner.JobStatusSucceeded {
		t.Fatalf("expected status succeeded, got %s", status)
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.job.Status != provisioner.JobStatusSucceeded {
		t.Fatalf("expected job status succeeded, got %s", fs.job.Status)
	}
	if len(fs.events) == 0 {
		t.Fatalf("expected events to be recorded")
	}
	foundAwait := false
	foundPoll := false
	for _, ev := range fs.events {
		if strings.Contains(ev.Message, "op=esxi.await_bmc") && strings.Contains(ev.Message, "status=success") {
			foundAwait = true
		}
		if strings.Contains(ev.Message, "op=esxi.poll_power") && strings.Contains(ev.Message, "status=success") {
			foundPoll = true
		}
	}
	if !foundAwait {
		t.Fatalf("expected await_bmc success event, events=%+v", fs.events)
	}
	if !foundPoll {
		t.Fatalf("expected poll_power success event, events=%+v", fs.events)
	}
}

func TestRunESXiCompletion_Timeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)

	job := &provisioner.Job{ID: "job-timeout", Status: provisioner.JobStatusProvisioning}
	fs := &fakeStore{job: job}

	w := &Worker{
		store: fs,
		cfg: WorkerConfig{
			ESXIInstallTimeout:    200 * time.Millisecond,
			ESXIStableWindow:      80 * time.Millisecond,
			ESXIPollIntervalStart: 10 * time.Millisecond,
			ESXIPollIntervalMax:   20 * time.Millisecond,
		},
		now: func() time.Time { return time.Now() },
	}

	rf := &esxiStubClient{
		powerStates: []redfish.PowerState{
			redfish.PowerStatePoweringOn,
			redfish.PowerStateResetting,
			redfish.PowerStatePoweringOn,
		},
	}

	status, err := w.runESXiCompletion(ctx, job, rf)
	if err == nil {
		t.Fatalf("expected error but got nil")
	}
	if status != provisioner.JobStatusFailed {
		t.Fatalf("expected status failed, got %s", status)
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.job.Status != provisioner.JobStatusFailed {
		t.Fatalf("expected job status failed, got %s", fs.job.Status)
	}
	if len(fs.events) == 0 {
		t.Fatalf("expected failure event to be recorded")
	}
	foundPollErr := false
	var pollMsg string
	for _, ev := range fs.events {
		if strings.Contains(ev.Message, "op=esxi.poll_power") {
			pollMsg = ev.Message
			if strings.Contains(ev.Message, "status=error") {
				foundPollErr = true
				break
			}
		}
	}
	if !foundPollErr {
		t.Fatalf("expected poll_power error event, events=%+v", fs.events)
	}
	if !strings.Contains(pollMsg, "error=") {
		t.Fatalf("expected poll_power event to include error details, got %q", pollMsg)
	}
}
