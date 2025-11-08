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

package dispatcher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"shoal/internal/logging"
	"shoal/internal/provisioner/api"
	"shoal/internal/provisioner/schema"
)

// Exit codes align with design/025_Dispatcher_Go_Binary.md.
const (
	ExitSuccess         = 0
	ExitDeviceTimeout   = 10
	ExitMountFailure    = 11
	ExitSchemaError     = 12
	ExitRecipeReadError = 13
	ExitSchemaInvalid   = 14
	ExitOutputError     = 15
	ExitSystemdError    = 16
	ExitPrivilegeError  = 17
	ExitSerialError     = 18
	ExitUnexpectedError = 20
)

// Error wraps a dispatcher failure with an exit code.
type Error struct {
	Code int
	Err  error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("dispatcher: %v", e.Err)
}

// Unwrap exposes the underlying error for errors.Is/As.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ExecFunc executes a command and returns combined stdout/stderr.
type ExecFunc func(ctx context.Context, name string, args ...string) ([]byte, error)

// Config controls dispatcher behaviour.
type Config struct {
	TaskDevices    []string
	TaskMount      string
	EnvDir         string
	RecipePath     string
	SchemaPath     string
	TargetOverride string
	SerialSource   string
	SerialEnvKey   string
	SerialOverride string
	Version        string
	NoStart        bool
	SkipRootCheck  bool
	Logger         *slog.Logger
	Exec           ExecFunc
	Now            func() time.Time
}

// Run executes the dispatcher workflow.
func Run(ctx context.Context, cfg Config) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &Error{Code: ExitUnexpectedError, Err: fmt.Errorf("panic: %v", r)}
		}
	}()

	logger := cfg.Logger
	if logger == nil {
		logger = logging.New("info")
	}

	execFn := cfg.Exec
	if execFn == nil {
		execFn = defaultExec
	}

	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}

	if !cfg.SkipRootCheck && os.Geteuid() != 0 {
		return &Error{Code: ExitPrivilegeError, Err: errors.New("dispatcher must run as root")}
	}

	taskMount := cfg.TaskMount
	if strings.TrimSpace(taskMount) == "" {
		taskMount = "/mnt/task"
	}
	envDir := cfg.EnvDir
	if strings.TrimSpace(envDir) == "" {
		envDir = "/run/provision"
	}
	recipePath := cfg.RecipePath
	if strings.TrimSpace(recipePath) == "" {
		recipePath = "recipe.json"
	}
	schemaPath := cfg.SchemaPath
	if strings.TrimSpace(schemaPath) == "" {
		schemaPath = "recipe.schema.json"
	}

	attrs := []slog.Attr{
		slog.String("task_mount", taskMount),
		slog.String("env_dir", envDir),
	}
	if len(cfg.TaskDevices) > 0 {
		attrs = append(attrs, slog.String("task_devices", strings.Join(cfg.TaskDevices, ",")))
	}
	args := make([]any, 0, len(attrs))
	for _, attr := range attrs {
		args = append(args, attr)
	}
	logger.Info("dispatcher starting", args...)

	schemaBytes, err := os.ReadFile(filepath.Join(taskMount, schemaPath))
	if err != nil {
		return &Error{Code: ExitSchemaError, Err: fmt.Errorf("read schema: %w", err)}
	}
	if len(bytes.TrimSpace(schemaBytes)) == 0 {
		return &Error{Code: ExitSchemaError, Err: errors.New("schema file is empty")}
	}

	recipeBytes, err := os.ReadFile(filepath.Join(taskMount, recipePath))
	if err != nil {
		return &Error{Code: ExitRecipeReadError, Err: fmt.Errorf("read recipe: %w", err)}
	}
	if len(bytes.TrimSpace(recipeBytes)) == 0 {
		return &Error{Code: ExitRecipeReadError, Err: errors.New("recipe file is empty")}
	}

	validator := api.NewDefaultValidator()
	errs, valErr := validator.ValidateRecipe(recipeBytes)
	if valErr != nil {
		return &Error{Code: ExitRecipeReadError, Err: valErr}
	}
	if len(errs) > 0 {
		msg := errs[0]
		return &Error{Code: ExitSchemaInvalid, Err: fmt.Errorf("recipe validation failed: %s: %s", msg.Field, msg.Message)}
	}

	var rec recipe
	if err := json.Unmarshal(recipeBytes, &rec); err != nil {
		return &Error{Code: ExitRecipeReadError, Err: fmt.Errorf("parse recipe: %w", err)}
	}

	taskTarget := rec.TaskTarget
	if strings.TrimSpace(cfg.TargetOverride) != "" {
		logger.Warn("overriding task target", slog.String("original", taskTarget), slog.String("override", cfg.TargetOverride))
		taskTarget = cfg.TargetOverride
	}
	if strings.TrimSpace(taskTarget) == "" {
		return &Error{Code: ExitSchemaInvalid, Err: errors.New("task_target is empty")}
	}

	serial, serialSource, serr := resolveSerial(ctx, cfg, execFn)
	if serr != nil {
		return &Error{Code: ExitSerialError, Err: serr}
	}
	logger.Info("resolved serial", slog.String("source", serialSource))

	if err := os.MkdirAll(envDir, 0o755); err != nil {
		return &Error{Code: ExitOutputError, Err: fmt.Errorf("create env dir: %w", err)}
	}

	schemaID := extractSchemaID(schemaBytes)
	if schemaID == "" {
		// Fallback to embedded schema ID if available
		schemaID = extractSchemaID(schema.Recipe())
	}

	ts := nowFn().UTC().Format(time.RFC3339)

	envValues := map[string]string{
		"TASK_TARGET":         taskTarget,
		"TARGET_DISK":         strings.TrimSpace(rec.TargetDisk),
		"SERIAL_NUMBER":       serial,
		"WORKFLOW_STARTED_AT": ts,
	}
	if cfg.Version != "" {
		envValues["DISPATCHER_VERSION"] = cfg.Version
	}
	if rec.SchemaVersion != "" {
		envValues["SCHEMA_VERSION"] = rec.SchemaVersion
	}
	if schemaID != "" {
		envValues["SCHEMA_ID"] = schemaID
	}
	if strings.TrimSpace(rec.OCIURL) != "" {
		envValues["OCI_URL"] = rec.OCIURL
	}
	if strings.TrimSpace(rec.FirmwareURL) != "" {
		envValues["FIRMWARE_URL"] = rec.FirmwareURL
	}
	if rec.WIMIndex > 0 {
		envValues["WIM_INDEX"] = fmt.Sprintf("%d", rec.WIMIndex)
	}

	reserved := map[string]struct{}{}
	for k := range envValues {
		reserved[k] = struct{}{}
	}
	for k, v := range rec.Env {
		key := strings.ToUpper(strings.TrimSpace(k))
		if key == "" {
			continue
		}
		if _, exists := reserved[key]; exists {
			logger.Warn("ignoring env override", slog.String("key", key))
			continue
		}
		envValues[key] = v
	}

	if err := writeEnvFile(filepath.Join(envDir, "recipe.env"), envValues); err != nil {
		return &Error{Code: ExitOutputError, Err: fmt.Errorf("write recipe.env: %w", err)}
	}

	if len(bytes.TrimSpace(rec.PartitionLayout)) > 0 {
		if err := writeJSON(filepath.Join(envDir, "layout.json"), rec.PartitionLayout); err != nil {
			return &Error{Code: ExitOutputError, Err: fmt.Errorf("write layout.json: %w", err)}
		}
	}

	if data, err := decodePayload(rec.UserData); err != nil {
		return &Error{Code: ExitSchemaInvalid, Err: fmt.Errorf("user_data: %w", err)}
	} else if len(data) > 0 {
		if err := writeAtomic(filepath.Join(envDir, "user-data"), data, 0o644); err != nil {
			return &Error{Code: ExitOutputError, Err: fmt.Errorf("write user-data: %w", err)}
		}
	}

	if data, err := decodePayload(rec.UnattendXML); err != nil {
		return &Error{Code: ExitSchemaInvalid, Err: fmt.Errorf("unattend_xml: %w", err)}
	} else if len(data) > 0 {
		if err := writeAtomic(filepath.Join(envDir, "unattend.xml"), data, 0o644); err != nil {
			return &Error{Code: ExitOutputError, Err: fmt.Errorf("write unattend.xml: %w", err)}
		}
	}

	if data, err := decodePayload(rec.Kickstart); err != nil {
		return &Error{Code: ExitSchemaInvalid, Err: fmt.Errorf("ks.cfg: %w", err)}
	} else if len(data) > 0 {
		if err := writeAtomic(filepath.Join(envDir, "ks.cfg"), data, 0o644); err != nil {
			return &Error{Code: ExitOutputError, Err: fmt.Errorf("write ks.cfg: %w", err)}
		}
	}

	logger.Info("env files written", slog.String("target", taskTarget))

	if cfg.NoStart {
		logger.Info("no-start flag set; skipping systemctl invocation")
		return nil
	}

	if out, err := execFn(ctx, "systemctl", "start", taskTarget); err != nil {
		detail := strings.TrimSpace(string(out))
		if detail != "" {
			err = fmt.Errorf("%w: %s", err, detail)
		}
		return &Error{Code: ExitSystemdError, Err: fmt.Errorf("systemctl start %s: %w", taskTarget, err)}
	}

	logger.Info("systemctl start complete", slog.String("target", taskTarget))
	return nil
}

// recipe mirrors the JSON structure we care about.
type recipe struct {
	SchemaVersion   string            `json:"schema_version"`
	TaskTarget      string            `json:"task_target"`
	TargetDisk      string            `json:"target_disk"`
	OCIURL          string            `json:"oci_url"`
	FirmwareURL     string            `json:"firmware_url"`
	WIMIndex        int               `json:"wim_index"`
	PartitionLayout json.RawMessage   `json:"partition_layout"`
	UserData        json.RawMessage   `json:"user_data"`
	UnattendXML     json.RawMessage   `json:"unattend_xml"`
	Kickstart       json.RawMessage   `json:"ks.cfg"`
	Env             map[string]string `json:"env"`
}

// resolveSerial attempts to determine the system serial number.
func resolveSerial(ctx context.Context, cfg Config, execFn ExecFunc) (string, string, error) {
	if strings.TrimSpace(cfg.SerialOverride) != "" {
		return strings.TrimSpace(cfg.SerialOverride), "override", nil
	}

	envKey := cfg.SerialEnvKey
	if strings.TrimSpace(envKey) == "" {
		envKey = "PROVISIONER_SERIAL"
	}
	if val := strings.TrimSpace(os.Getenv(envKey)); val != "" {
		return val, fmt.Sprintf("env:%s", envKey), nil
	}
	if cfg.SerialSource == "env" {
		return "", "env", fmt.Errorf("serial env key %s not set", envKey)
	}

	if serial, ok := readSysSerial(); ok {
		return serial, "sysfs", nil
	}

	if cfg.SerialSource == "dmi" {
		return "", "dmi", errors.New("serial not available via sysfs")
	}

	if execFn != nil {
		if out, err := execFn(ctx, "dmidecode", "-s", "system-serial-number"); err == nil {
			serial := strings.TrimSpace(string(out))
			if serial != "" && !isPlaceholderSerial(serial) {
				return serial, "dmidecode", nil
			}
		}
	}

	return "unknown", "fallback", nil
}

func readSysSerial() (string, bool) {
	data, err := os.ReadFile("/sys/class/dmi/id/product_serial")
	if err != nil {
		return "", false
	}
	serial := strings.TrimSpace(string(data))
	if serial == "" || isPlaceholderSerial(serial) {
		return "", false
	}
	return serial, true
}

func isPlaceholderSerial(v string) bool {
	lower := strings.ToLower(strings.TrimSpace(v))
	switch lower {
	case "", "unknown", "not specified", "to be filled by o.e.m.":
		return true
	default:
		return false
	}
}

func writeEnvFile(path string, values map[string]string) error {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	for _, k := range keys {
		buf.WriteString(k)
		buf.WriteByte('=')
		buf.WriteString(sanitizeEnvValue(values[k]))
		buf.WriteByte('\n')
	}
	return writeAtomic(path, buf.Bytes(), 0o644)
}

func sanitizeEnvValue(v string) string {
	v = strings.TrimSpace(v)
	v = strings.ReplaceAll(v, "\r", " ")
	v = strings.ReplaceAll(v, "\n", " ")
	return v
}

func writeJSON(path string, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	formatted := formatJSON(raw)
	return writeAtomic(path, formatted, 0o644)
}

func formatJSON(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return nil
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		buf.Reset()
		buf.Write(raw)
	}
	if !bytes.HasSuffix(buf.Bytes(), []byte("\n")) {
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

func decodePayload(raw json.RawMessage) ([]byte, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}

	var inline string
	if err := json.Unmarshal(raw, &inline); err == nil {
		return []byte(inline), nil
	}

	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("unsupported payload shape")
	}

	if content, ok := obj["content"].(string); ok {
		return []byte(content), nil
	}
	if url, ok := obj["url"].(string); ok {
		return nil, fmt.Errorf("payload URL not supported yet (%s)", url)
	}
	if path, ok := obj["path"].(string); ok {
		return nil, fmt.Errorf("payload path not supported yet (%s)", path)
	}

	return nil, fmt.Errorf("payload missing content/url/path")
}

func extractSchemaID(schemaBytes []byte) string {
	if len(schemaBytes) == 0 {
		return ""
	}
	var obj map[string]any
	if err := json.Unmarshal(schemaBytes, &obj); err != nil {
		return ""
	}
	if id, ok := obj["$id"].(string); ok {
		return strings.TrimSpace(id)
	}
	return ""
}

func writeAtomic(path string, content []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()

	if _, err := tmp.Write(content); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func defaultExec(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}
