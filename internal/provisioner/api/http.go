package api

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

// Package api implements HTTP request/response models and handlers for the
// Provisioner Controller Service as described in the 021 design document.
// In Phase 1 this wires POST/GET endpoints to the persistence store with
// a stubbed recipe validation step (022 to be integrated later).
//
// Endpoints implemented in this file:
//   - POST /api/v1/jobs
//   - GET  /api/v1/jobs/{id}
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"shoal/pkg/provisioner"
)

// JobStore defines the persistence methods the API needs.
// The internal store implementation (internal/provisioner/store.Store)
// should satisfy this interface.
type JobStore interface {
	// Servers
	GetServerBySerial(ctx context.Context, serial string) (*provisioner.Server, error)

	// Jobs
	InsertJob(ctx context.Context, job *provisioner.Job) error
	GetJobByID(ctx context.Context, id string) (*provisioner.Job, error)
	MarkJobStatus(ctx context.Context, id string, status provisioner.JobStatus, failedStep *string) error

	// Events
	ListJobEvents(ctx context.Context, jobID string, limit int) ([]provisioner.JobEvent, error)
	AppendJobEvent(ctx context.Context, ev provisioner.JobEvent) error
}

// API is the HTTP layer for the provisioner controller.
type API struct {
	Store             JobStore
	MaintenanceISOURL string

	// Logger is optional; if nil, logging is suppressed.
	Logger *log.Logger
	// Now allows tests to control timestamps.
	Now func() time.Time
}

// New constructs an API with its required dependencies.
func New(store JobStore, maintenanceISOURL string, logger *log.Logger) *API {
	return &API{
		Store:             store,
		MaintenanceISOURL: maintenanceISOURL,
		Logger:            logger,
		Now:               func() time.Time { return time.Now().UTC() },
	}
}

// Register attaches the API handlers to a mux under the expected routes.
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/jobs", a.jobsHandler)
	mux.HandleFunc("/api/v1/jobs/", a.jobByIDHandler)
}

// --------------- Models ---------------

// CreateJobRequest is the payload for POST /api/v1/jobs.
type CreateJobRequest struct {
	ServerSerial string          `json:"server_serial"`
	Recipe       json.RawMessage `json:"recipe"`
}

// CreateJobResponse is returned for POST /api/v1/jobs upon success (202).
type CreateJobResponse struct {
	JobID        string                 `json:"job_id"`
	Status       provisioner.JobStatus  `json:"status"`
	ServerSerial string                 `json:"server_serial"`
	CreatedAt    time.Time              `json:"created_at"`
	Extra        map[string]interface{} `json:"-"` // reserved for future fields
}

// GetJobResponse is returned for GET /api/v1/jobs/{id}.
type GetJobResponse struct {
	JobID        string                `json:"job_id"`
	ServerSerial string                `json:"server_serial"`
	Status       provisioner.JobStatus `json:"status"`
	FailedStep   *string               `json:"failed_step,omitempty"`
	CreatedAt    time.Time             `json:"created_at"`
	LastUpdate   time.Time             `json:"last_update"`
	Events       []JobEventDTO         `json:"events"`
}

// JobEventDTO is a user-facing event entry for GetJobResponse.
type JobEventDTO struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
	Step    *string   `json:"step,omitempty"`
}

// jsonError is a simple error envelope for API responses.
type jsonError struct {
	Error   string            `json:"error"`
	Message string            `json:"message,omitempty"`
	Details []ValidationError `json:"details,omitempty"`
}

func (a *API) logf(format string, args ...any) {
	if a.Logger != nil {
		a.Logger.Printf(format, args...)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// --------------- Handlers ---------------

func (a *API) jobsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		a.handleCreateJob(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (a *API) jobByIDHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	// Path format: /api/v1/jobs/{id} (no trailing segments)
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/jobs/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	a.handleGetJob(w, r, id)
}

// --------------- POST /api/v1/jobs ---------------

func (a *API) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req CreateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, jsonError{
			Error:   "invalid_json",
			Message: "Request body could not be parsed as JSON",
		})
		return
	}
	if strings.TrimSpace(req.ServerSerial) == "" {
		writeJSON(w, http.StatusBadRequest, jsonError{
			Error:   "invalid_request",
			Message: "server_serial is required",
		})
		return
	}
	if len(req.Recipe) == 0 || string(req.Recipe) == "null" {
		writeJSON(w, http.StatusBadRequest, jsonError{
			Error:   "invalid_request",
			Message: "recipe is required",
		})
		return
	}

	// Real recipe validation per 022 (schema-backed)
	if verrs, verr := ValidateRecipe(req.Recipe); verr != nil {
		writeJSON(w, http.StatusBadRequest, jsonError{
			Error:   "invalid_recipe",
			Message: "failed to validate recipe",
		})
		return
	} else if len(verrs) > 0 {
		writeJSON(w, http.StatusBadRequest, jsonError{
			Error:   "invalid_recipe",
			Message: "recipe does not conform to schema",
			Details: verrs,
		})
		return
	}

	// Ensure server exists and is provisionable.
	if _, err := a.Store.GetServerBySerial(ctx, req.ServerSerial); err != nil {
		writeError(w, err, "unknown server_serial: %s", req.ServerSerial)
		return
	}

	job := provisioner.NewJob(req.ServerSerial, req.Recipe, a.MaintenanceISOURL)
	job.ID = uuid.NewString()

	if err := a.Store.InsertJob(ctx, &job); err != nil {
		a.logf("failed to insert job %s for serial %s: %v", job.ID, job.ServerSerial, err)
		writeJSON(w, http.StatusInternalServerError, jsonError{
			Error:   "server_error",
			Message: "failed to create job",
		})
		return
	}

	resp := CreateJobResponse{
		JobID:        job.ID,
		Status:       job.Status,
		ServerSerial: job.ServerSerial,
		CreatedAt:    job.CreatedAt,
	}
	// Return 202 Accepted to reflect queued status per design.
	writeJSON(w, http.StatusAccepted, resp)
}

// --------------- GET /api/v1/jobs/{id} ---------------

func (a *API) handleGetJob(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()

	job, err := a.Store.GetJobByID(ctx, id)
	if err != nil {
		writeError(w, err, "job not found: %s", id)
		return
	}

	events, err := a.Store.ListJobEvents(ctx, id, 0)
	if err != nil {
		a.logf("failed to list events for job %s: %v", id, err)
		writeJSON(w, http.StatusInternalServerError, jsonError{
			Error:   "server_error",
			Message: "failed to load job events",
		})
		return
	}

	resp := GetJobResponse{
		JobID:        job.ID,
		ServerSerial: job.ServerSerial,
		Status:       job.Status,
		FailedStep:   job.FailedStep,
		CreatedAt:    job.CreatedAt,
		LastUpdate:   job.UpdatedAt,
		Events:       toEventDTOs(events),
	}

	writeJSON(w, http.StatusOK, resp)
}

// --------------- Helpers ---------------

func toEventDTOs(evts []provisioner.JobEvent) []JobEventDTO {
	out := make([]JobEventDTO, 0, len(evts))
	for _, e := range evts {
		out = append(out, JobEventDTO{
			Time:    e.Time,
			Level:   e.Level.String(),
			Message: e.Message,
			Step:    e.Step,
		})
	}
	return out
}

func writeError(w http.ResponseWriter, err error, notFoundMsgFmt string, args ...any) {
	// Supports mapping a known not-found semantic from the store layer.
	if isNotFound(err) {
		writeJSON(w, http.StatusNotFound, jsonError{
			Error:   "not_found",
			Message: fmt.Sprintf(notFoundMsgFmt, args...),
		})
		return
	}
	writeJSON(w, http.StatusInternalServerError, jsonError{
		Error:   "server_error",
		Message: "internal error",
	})
}

func isNotFound(err error) bool {
	// Allow both sentinel errors and wrapped variants.
	return errors.Is(err, errors.New("not found")) ||
		strings.Contains(strings.ToLower(err.Error()), "not found")
}

// validateRecipeStub is a temporary placeholder for schema validation per 022.
// It performs only very rudimentary checks to guard obviously invalid inputs.
func validateRecipeStub(raw json.RawMessage) error {
	trim := strings.TrimSpace(string(raw))
	if trim == "" || trim == "null" {
		return errors.New("recipe must be a non-empty JSON object")
	}
	// Basic object check: should start with { and end with } for now.
	if !(strings.HasPrefix(trim, "{") && strings.HasSuffix(trim, "}")) {
		return errors.New("recipe must be a JSON object")
	}
	// No further validation yet.
	return nil
}
