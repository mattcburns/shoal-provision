// Shoal is a Redfish aggregator service.package provisionerdispatcher

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
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"shoal/internal/logging"
	"shoal/internal/provisioner/dispatcher"
)

var version = "dev"

func main() {
	var (
		taskDevices    = flag.String("task-iso-device", "/dev/sr1", "Comma-separated list of task ISO block devices to probe")
		taskMount      = flag.String("task-mount-point", "/mnt/task", "Mount point for the task ISO contents")
		envDir         = flag.String("env-dir", "/run/provision", "Directory for normalized recipe outputs")
		recipePath     = flag.String("recipe-path", "recipe.json", "Relative path to recipe.json within the task mount")
		schemaPath     = flag.String("schema-path", "recipe.schema.json", "Relative path to recipe.schema.json within the task mount")
		noStart        = flag.Bool("no-start", false, "Validate but do not start the target (testing)")
		targetOverride = flag.String("target-override", "", "Override the task_target from the recipe")
		serialSource   = flag.String("serial-source", "auto", "Serial detection source: auto|env|dmi")
		serialEnvKey   = flag.String("serial-env-key", "PROVISIONER_SERIAL", "Environment variable for serial when serial-source=env or override present")
		logLevel       = flag.String("log-level", "info", "Log level: debug|info|warn|error")
		printVersion   = flag.Bool("version", false, "Print version and exit")
	)

	flag.Parse()

	if *printVersion {
		fmt.Println(version)
		return
	}

	logger := logging.New(*logLevel)
	logger = logger.With(slog.String("component", "dispatcher"))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := dispatcher.Config{
		TaskMount:      *taskMount,
		EnvDir:         *envDir,
		RecipePath:     *recipePath,
		SchemaPath:     *schemaPath,
		TargetOverride: strings.TrimSpace(*targetOverride),
		SerialSource:   strings.ToLower(strings.TrimSpace(*serialSource)),
		SerialEnvKey:   strings.TrimSpace(*serialEnvKey),
		NoStart:        *noStart,
		Version:        version,
		Logger:         logger,
	}

	if trimmed := strings.TrimSpace(*taskDevices); trimmed != "" {
		cfg.TaskDevices = splitComma(trimmed)
	}

	if err := dispatcher.Run(ctx, cfg); err != nil {
		if derr, ok := err.(*dispatcher.Error); ok {
			logger.Error("dispatcher failed", slog.Int("exit_code", derr.Code), slog.Any("err", derr.Err))
			os.Exit(derr.Code)
		}
		logger.Error("dispatcher failed", slog.Any("err", err))
		os.Exit(1)
	}

	logger.Info("dispatcher completed successfully")
}

func splitComma(input string) []string {
	parts := strings.Split(input, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
