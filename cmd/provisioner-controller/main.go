package main

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
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"shoal/internal/provisioner/api"
	"shoal/internal/provisioner/iso"
	"shoal/internal/provisioner/jobs"
	"shoal/internal/provisioner/redfish"
	"shoal/internal/provisioner/store"
	"shoal/pkg/provisioner"
)

// Config holds runtime configuration for the provisioner controller.
// Values can be provided via environment variables and/or flags.
// Flags take precedence over environment variables.
type Config struct {
	HTTPAddr          string        // CONTROLLER_HTTP_ADDR
	DBPath            string        // DB_PATH
	StorageRoot       string        // STORAGE_ROOT
	TaskISODir        string        // TASK_ISO_DIR
	MaintenanceISOURL string        // MAINTENANCE_ISO_URL
	EnableRegistry    bool          // ENABLE_REGISTRY
	RegistryStorage   string        // REGISTRY_STORAGE
	AuthMode          string        // AUTH_MODE: basic|jwt|none
	WebhookSecret     string        // WEBHOOK_SECRET (do not log value)
	WorkerConcurrency int           // WORKER_CONCURRENCY
	RedfishTimeout    time.Duration // REDFISH_TIMEOUT
	RedfishRetries    int           // REDFISH_RETRIES
	JobLeaseTTL       time.Duration // JOB_LEASE_TTL
	JobStuckTimeout   time.Duration // JOB_STUCK_TIMEOUT
	LogLevel          string        // LOG_LEVEL: info|debug
}

// defaultConfig returns sane defaults aligned with the design docs.
func defaultConfig() Config {
	return Config{
		HTTPAddr:          ":8080",
		DBPath:            "./provisioner.db",
		StorageRoot:       "./var/shoal",
		TaskISODir:        "./var/shoal/task-isos",
		MaintenanceISOURL: "http://localhost:8080/media/isos/bootc-maintenance.iso",
		EnableRegistry:    false,
		RegistryStorage:   "./var/shoal/oci",
		AuthMode:          "none",
		WebhookSecret:     "",
		WorkerConcurrency: 2,
		RedfishTimeout:    30 * time.Second,
		RedfishRetries:    5,
		JobLeaseTTL:       10 * time.Minute,
		JobStuckTimeout:   4 * time.Hour,
		LogLevel:          "info",
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func getenvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}

func getenvDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

// parseConfig builds the Config from env + flags.
// Flags override environment variables.
func parseConfig() Config {
	def := defaultConfig()

	// Seed from env
	cfg := Config{
		HTTPAddr:          getenv("CONTROLLER_HTTP_ADDR", def.HTTPAddr),
		DBPath:            getenv("DB_PATH", def.DBPath),
		StorageRoot:       getenv("STORAGE_ROOT", def.StorageRoot),
		TaskISODir:        getenv("TASK_ISO_DIR", def.TaskISODir),
		MaintenanceISOURL: getenv("MAINTENANCE_ISO_URL", def.MaintenanceISOURL),
		EnableRegistry:    getenvBool("ENABLE_REGISTRY", def.EnableRegistry),
		RegistryStorage:   getenv("REGISTRY_STORAGE", def.RegistryStorage),
		AuthMode:          getenv("AUTH_MODE", def.AuthMode),
		WebhookSecret:     getenv("WEBHOOK_SECRET", def.WebhookSecret),
		WorkerConcurrency: getenvInt("WORKER_CONCURRENCY", def.WorkerConcurrency),
		RedfishTimeout:    getenvDuration("REDFISH_TIMEOUT", def.RedfishTimeout),
		RedfishRetries:    getenvInt("REDFISH_RETRIES", def.RedfishRetries),
		JobLeaseTTL:       getenvDuration("JOB_LEASE_TTL", def.JobLeaseTTL),
		JobStuckTimeout:   getenvDuration("JOB_STUCK_TIMEOUT", def.JobStuckTimeout),
		LogLevel:          getenv("LOG_LEVEL", def.LogLevel),
	}

	// Flags (override env if provided)
	flag.StringVar(&cfg.HTTPAddr, "addr", cfg.HTTPAddr, "HTTP listen address (env CONTROLLER_HTTP_ADDR)")
	flag.StringVar(&cfg.DBPath, "db", cfg.DBPath, "SQLite DB path (env DB_PATH)")
	flag.StringVar(&cfg.StorageRoot, "storage-root", cfg.StorageRoot, "Storage root directory (env STORAGE_ROOT)")
	flag.StringVar(&cfg.TaskISODir, "task-iso-dir", cfg.TaskISODir, "Task ISO output directory (env TASK_ISO_DIR)")
	flag.StringVar(&cfg.MaintenanceISOURL, "maintenance-iso-url", cfg.MaintenanceISOURL, "Maintenance ISO URL (env MAINTENANCE_ISO_URL)")
	flag.BoolVar(&cfg.EnableRegistry, "enable-registry", cfg.EnableRegistry, "Enable embedded OCI registry (env ENABLE_REGISTRY)")
	flag.StringVar(&cfg.RegistryStorage, "registry-storage", cfg.RegistryStorage, "Embedded registry storage path (env REGISTRY_STORAGE)")
	flag.StringVar(&cfg.AuthMode, "auth-mode", cfg.AuthMode, "Auth mode: basic|jwt|none (env AUTH_MODE)")
	flag.StringVar(&cfg.WebhookSecret, "webhook-secret", cfg.WebhookSecret, "Webhook shared secret (env WEBHOOK_SECRET)")
	flag.IntVar(&cfg.WorkerConcurrency, "workers", cfg.WorkerConcurrency, "Worker concurrency (env WORKER_CONCURRENCY)")
	flag.DurationVar(&cfg.RedfishTimeout, "redfish-timeout", cfg.RedfishTimeout, "Redfish request timeout (env REDFISH_TIMEOUT)")
	flag.IntVar(&cfg.RedfishRetries, "redfish-retries", cfg.RedfishRetries, "Redfish retry count (env REDFISH_RETRIES)")
	flag.DurationVar(&cfg.JobLeaseTTL, "job-lease-ttl", cfg.JobLeaseTTL, "Job lease TTL (env JOB_LEASE_TTL)")
	flag.DurationVar(&cfg.JobStuckTimeout, "job-stuck-timeout", cfg.JobStuckTimeout, "Job stuck timeout (env JOB_STUCK_TIMEOUT)")
	flag.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "Log level: info|debug (env LOG_LEVEL)")

	flag.Parse()
	return cfg
}

type jsonError struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func readyHandler(w http.ResponseWriter, r *http.Request) {
	// Placeholder: in future, validate DB connectivity, storage, worker readiness, etc.
	writeJSON(w, http.StatusOK, map[string]any{"ready": true})
}

func notImplementedHandler(msg string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotImplemented, jsonError{
			Error:   "not_implemented",
			Message: msg,
		})
	}
}

func jobsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		notImplementedHandler("POST /api/v1/jobs is not implemented yet")(w, r)
	default:
		http.NotFound(w, r)
	}
}

func jobByIDHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	// Extract job_id from path: /api/v1/jobs/{id}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/jobs/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	jobID := parts[0]
	_ = jobID
	notImplementedHandler("GET /api/v1/jobs/{id} is not implemented yet")(w, r)
}

func webhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	// Extract serial from path: /api/v1/status-webhook/{serial}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/status-webhook/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	serial := parts[0]
	_ = serial
	notImplementedHandler("POST /api/v1/status-webhook/{serial} is not implemented yet")(w, r)
}

func tasksMediaHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// GET /media/tasks/{job_id}/task.iso
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		trim := strings.TrimPrefix(r.URL.Path, "/media/tasks/")
		parts := strings.Split(trim, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] != "task.iso" {
			http.NotFound(w, r)
			return
		}
		jobID := parts[0]
		fpath := filepath.Join(cfg.TaskISODir, jobID, "task.iso")
		http.ServeFile(w, r, fpath)
	}
}

func registryHandler(w http.ResponseWriter, r *http.Request) {
	// Placeholder for embedded OCI Distribution API
	notImplementedHandler("Embedded registry /v2/ is not implemented yet")(w, r)
}

func redactedSecret(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		return "****"
	}
	return s[:2] + strings.Repeat("*", len(s)-4) + s[len(s)-2:]
}

func logConfig(cfg Config) {
	// Do not log secret values
	log.Printf("provisioner-controller configuration:")
	log.Printf("  addr=%s", cfg.HTTPAddr)
	log.Printf("  db=%s", cfg.DBPath)
	log.Printf("  storage_root=%s", cfg.StorageRoot)
	log.Printf("  task_iso_dir=%s", cfg.TaskISODir)
	log.Printf("  maintenance_iso_url=%s", cfg.MaintenanceISOURL)
	log.Printf("  enable_registry=%v", cfg.EnableRegistry)
	log.Printf("  registry_storage=%s", cfg.RegistryStorage)
	log.Printf("  auth_mode=%s", cfg.AuthMode)
	log.Printf("  webhook_secret=%s", redactedSecret(cfg.WebhookSecret))
	log.Printf("  workers=%d", cfg.WorkerConcurrency)
	log.Printf("  redfish_timeout=%s", cfg.RedfishTimeout)
	log.Printf("  redfish_retries=%d", cfg.RedfishRetries)
	log.Printf("  job_lease_ttl=%s", cfg.JobLeaseTTL)
	log.Printf("  job_stuck_timeout=%s", cfg.JobStuckTimeout)
	log.Printf("  log_level=%s", cfg.LogLevel)
}

// computeTaskMediaBase derives the base URL used by workers to mount
// /media/tasks/{job_id}/task.iso, preferring the scheme/host from the
// configured MaintenanceISOURL. Falls back to http://127.0.0.1:<port>.
func computeTaskMediaBase(cfg Config) string {
	if u, err := url.Parse(cfg.MaintenanceISOURL); err == nil && u.Scheme != "" && u.Host != "" {
		return fmt.Sprintf("%s://%s/media/tasks", u.Scheme, u.Host)
	}
	host := cfg.HTTPAddr
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	return "http://" + host + "/media/tasks"
}

func newMux(cfg Config, ap *api.API, webhook http.Handler) *http.ServeMux {
	mux := http.NewServeMux()

	// Health/ready
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/readyz", readyHandler)

	// API endpoints
	if ap != nil {
		ap.Register(mux)
	}
	mux.Handle("/api/v1/status-webhook/", webhook)

	// Media serving (task ISO)
	mux.HandleFunc("/media/tasks/", tasksMediaHandler(cfg))

	// Embedded registry (optional)
	if cfg.EnableRegistry {
		mux.HandleFunc("/v2/", registryHandler)
	}

	// Root banner
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"name":        "shoal-provisioner-controller",
			"status":      "starting",
			"description": "Provisioner controller scaffold. Endpoints not implemented yet.",
			"docs":        "See design/021_Provisioner_Controller_Service.md",
		})
	})

	return mux
}

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC | log.Lmsgprefix)
	log.SetPrefix("[provisioner-controller] ")

	cfg := parseConfig()
	logConfig(cfg)

	// Open SQLite store and construct API
	st, err := store.Open(context.Background(), cfg.DBPath)
	if err != nil {
		log.Printf("failed to open store: %v", err)
		os.Exit(1)
	}
	defer st.Close()

	ap := api.New(st, cfg.MaintenanceISOURL, log.Default())
	wbh := api.NewWebhookHandler(st, cfg.WebhookSecret, log.Default(), nil)
	// Ensure task ISO directory exists
	_ = os.MkdirAll(cfg.TaskISODir, 0o755)
	// Start workers
	workerCtx, workerCancel := context.WithCancel(context.Background())
	mediaBase := computeTaskMediaBase(cfg)
	builder := iso.NewFileBuilder(cfg.TaskISODir)
	rfFactory := func(ctx context.Context, server *provisioner.Server) (redfish.Client, error) {
		rfcfg := redfish.Config{
			Endpoint: server.BMCAddress,
			Username: server.BMCUser,
			Password: server.BMCPass,
			Vendor:   server.Vendor,
			Timeout:  cfg.RedfishTimeout,
			Logger:   log.Default(),
		}
		// add a small artificial delay to simulate I/O
		return redfish.NewNoopClient(rfcfg, 200*time.Millisecond), nil
	}
	for i := 0; i < cfg.WorkerConcurrency; i++ {
		wcfg := jobs.WorkerConfig{
			WorkerID:          fmt.Sprintf("worker-%d", i+1),
			PollInterval:      2 * time.Second,
			LeaseTTL:          cfg.JobLeaseTTL,
			ExtendLeaseEvery:  cfg.JobLeaseTTL / 2,
			JobStuckTimeout:   cfg.JobStuckTimeout,
			RedfishTimeout:    cfg.RedfishTimeout,
			TaskISOMediaBase:  mediaBase,
			LogEveryHeartbeat: false,
		}
		w := jobs.NewWorker(st, builder, rfFactory, wcfg, log.Default())
		go w.Run(workerCtx)
	}

	// Prepare server with conservative timeouts
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           newMux(cfg, ap, wbh),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Start server
	errCh := make(chan error, 1)
	go func() {
		log.Printf("HTTP server listening on %s", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("http server error: %w", err)
		}
	}()

	// Graceful shutdown on SIGINT/SIGTERM
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("received signal: %s, initiating graceful shutdown...", sig)
	case err := <-errCh:
		log.Printf("server error: %v", err)
	}

	workerCancel()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	} else {
		log.Printf("server stopped gracefully")
	}
}
