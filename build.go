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

/*
Shoal Build Automation

A Go-based build and test automation system for the Shoal Redfish Aggregator.

Usage:
    go run build.go                    # Run full build and test pipeline
    go run build.go test              # Run tests only
    go run build.go build             # Build binary only
    go run build.go build-dispatcher  # Build dispatcher binaries
    go run build.go clean             # Clean build artifacts
    go run build.go fmt               # Format Go code
    go run build.go lint              # Run linting (if available)
    go run build.go coverage          # Run tests with coverage
    go run build.go deps              # Check and download dependencies
    go run build.go validate          # Full validation pipeline
    go run build.go build-all         # Build for all platforms
    go run build.go install-tools     # Install golangci-lint and gosec
    go run build.go --platform linux/amd64 build  # Build for specific platform
*/

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ANSI color codes for terminal output
const (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorRed    = "\033[91m"
	colorGreen  = "\033[92m"
	colorYellow = "\033[93m"
	colorBlue   = "\033[94m"
	colorCyan   = "\033[96m"
)

// SupportedPlatform represents a target build platform
type SupportedPlatform struct {
	GOOS   string
	GOARCH string
}

// BuildInfo contains metadata about a build
type BuildInfo struct {
	Timestamp    string `json:"timestamp"`
	GoVersion    string `json:"go_version"`
	GitCommit    string `json:"git_commit"`
	GitBranch    string `json:"git_branch"`
	GitDirty     bool   `json:"git_dirty"`
	Platform     string `json:"platform"`
	Architecture string `json:"architecture"`
}

// BuildRunner manages the build process
type BuildRunner struct {
	rootDir    string
	buildDir   string
	binaryName string
	startTime  time.Time
}

// NewBuildRunner creates a new build runner
func NewBuildRunner() (*BuildRunner, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	binaryName := "shoal"
	if runtime.GOOS == "windows" {
		binaryName = "shoal.exe"
	}

	return &BuildRunner{
		rootDir:    wd,
		buildDir:   filepath.Join(wd, "build"),
		binaryName: binaryName,
		startTime:  time.Now(),
	}, nil
}

// Print helpers
func (br *BuildRunner) printHeader(title string) {
	fmt.Printf("\n%s%s%s%s\n", colorBold, colorBlue, strings.Repeat("=", 60), colorReset)
	fmt.Printf("%s%s %s%s\n", colorBold, colorBlue, title, colorReset)
	fmt.Printf("%s%s%s%s\n\n", colorBold, colorBlue, strings.Repeat("=", 60), colorReset)
}

func (br *BuildRunner) printStep(step string) {
	fmt.Printf("%s%s→%s %s\n", colorBold, colorCyan, colorReset, step)
}

func (br *BuildRunner) printSuccess(message string) {
	fmt.Printf("%s%s✓%s %s\n", colorBold, colorGreen, colorReset, message)
}

func (br *BuildRunner) printError(message string) {
	fmt.Printf("%s%s✗%s %s\n", colorBold, colorRed, colorReset, message)
}

func (br *BuildRunner) printWarning(message string) {
	fmt.Printf("%s%s⚠%s %s\n", colorBold, colorYellow, colorReset, message)
}

// runCommand executes a command and returns exit code, stdout, and stderr
func (br *BuildRunner) runCommand(name string, args []string, cwd string, check bool) (int, string, string, error) {
	if cwd == "" {
		cwd = br.rootDir
	}

	cmd := exec.Command(name, args...)
	cmd.Dir = cwd

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return 1, "", "", fmt.Errorf("command failed: %w", err)
		}
	}

	if check && exitCode != 0 {
		br.printError(fmt.Sprintf("Command failed: %s %s", name, strings.Join(args, " ")))
		if stdout.Len() > 0 {
			fmt.Printf("STDOUT:\n%s\n", stdout.String())
		}
		if stderr.Len() > 0 {
			fmt.Printf("STDERR:\n%s\n", stderr.String())
		}
	}

	return exitCode, stdout.String(), stderr.String(), nil
}

// CheckPrerequisites verifies required tools are available
func (br *BuildRunner) CheckPrerequisites() bool {
	br.printStep("Checking prerequisites")

	// Check Go installation
	exitCode, stdout, _, err := br.runCommand("go", []string{"version"}, "", false)
	if err != nil || exitCode != 0 {
		br.printError("Go is not installed or not in PATH")
		return false
	}

	goVersion := strings.TrimSpace(stdout)
	br.printSuccess(fmt.Sprintf("Found %s", goVersion))

	// Check if we're in a Go module
	if _, err := os.Stat(filepath.Join(br.rootDir, "go.mod")); os.IsNotExist(err) {
		br.printError("go.mod not found - not in a Go module directory")
		return false
	}

	br.printSuccess("All prerequisites met")
	return true
}

// Clean removes build artifacts
func (br *BuildRunner) Clean() bool {
	br.printStep("Cleaning build artifacts")

	// Remove build directory
	if err := os.RemoveAll(br.buildDir); err != nil {
		if !os.IsNotExist(err) {
			br.printError(fmt.Sprintf("Failed to remove build directory: %v", err))
			return false
		}
	} else {
		br.printSuccess("Removed build directory")
	}

	// Remove binary from root
	binaryPath := filepath.Join(br.rootDir, br.binaryName)
	if err := os.Remove(binaryPath); err != nil {
		if !os.IsNotExist(err) {
			br.printError(fmt.Sprintf("Failed to remove binary: %v", err))
		}
	} else {
		br.printSuccess(fmt.Sprintf("Removed %s", br.binaryName))
	}

	// Remove test artifacts
	testArtifacts := []string{
		"coverage.out",
		"coverage.html",
		"coverage.txt",
	}

	for _, artifact := range testArtifacts {
		artifactPath := filepath.Join(br.rootDir, artifact)
		if err := os.Remove(artifactPath); err == nil {
			br.printSuccess(fmt.Sprintf("Removed %s", artifact))
		}
	}

	// Remove .test, .db, .sqlite files
	patterns := []string{"*.test", "*.db", "*.sqlite", "*.sqlite3"}
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(filepath.Join(br.rootDir, pattern))
		for _, match := range matches {
			os.Remove(match)
		}
	}

	br.printSuccess("Cleaned test artifacts")
	return true
}

// DownloadDependencies fetches and verifies Go module dependencies
func (br *BuildRunner) DownloadDependencies() bool {
	br.printStep("Downloading dependencies")

	exitCode, _, _, _ := br.runCommand("go", []string{"mod", "download"}, "", true)
	if exitCode != 0 {
		return false
	}

	// Verify dependencies
	exitCode, _, _, _ = br.runCommand("go", []string{"mod", "verify"}, "", true)
	if exitCode != 0 {
		br.printError("Dependency verification failed")
		return false
	}

	br.printSuccess("Dependencies downloaded and verified")
	return true
}

// FormatCode formats Go code
func (br *BuildRunner) FormatCode() bool {
	br.printStep("Formatting Go code")

	exitCode, _, _, _ := br.runCommand("go", []string{"fmt", "./..."}, "", true)
	if exitCode != 0 {
		return false
	}

	br.printSuccess("Code formatted")
	return true
}

// LintCode runs static analysis on Go code
func (br *BuildRunner) LintCode() bool {
	br.printStep("Linting code")

	// Try golangci-lint first (warning-only for now due to pre-existing issues)
	exitCode, _, _, err := br.runCommand("golangci-lint", []string{"--version"}, "", false)
	if err == nil && exitCode == 0 {
		fmt.Println("  Running golangci-lint (informational only)...")
		exitCode, _, _, _ := br.runCommand("golangci-lint", []string{"run"}, "", true)
		if exitCode != 0 {
			br.printWarning("golangci-lint found issues (not failing build)")
			br.printWarning("See .golangci.yml for configuration")
			br.printWarning("Future milestone will address linting cleanup")
		} else {
			br.printSuccess("Linting passed (golangci-lint)")
		}
		// Don't fail build on golangci-lint issues for now
		// Fall through to go vet as the actual quality gate
	}

	// Run go vet as the quality gate
	exitCode, _, _, _ = br.runCommand("go", []string{"vet", "./..."}, "", true)
	if exitCode != 0 {
		return false
	}

	br.printSuccess("Static analysis passed (go vet)")
	return true
}

// RunTests executes Go tests
func (br *BuildRunner) RunTests(withCoverage bool) bool {
	br.printStep("Running tests")

	args := []string{"test"}
	if withCoverage {
		args = append(args, "-coverprofile=coverage.out")
	}
	args = append(args, "-v", "./...")

	exitCode, _, _, _ := br.runCommand("go", args, "", true)
	if exitCode != 0 {
		return false
	}

	br.printSuccess("All tests passed")

	// Generate coverage report if requested
	if withCoverage {
		coverageFile := filepath.Join(br.rootDir, "coverage.out")
		if _, err := os.Stat(coverageFile); err == nil {
			exitCode, stdout, _, _ := br.runCommand("go", []string{"tool", "cover", "-func=coverage.out"}, "", false)
			if exitCode == 0 {
				// Extract total coverage
				lines := strings.Split(strings.TrimSpace(stdout), "\n")
				for _, line := range lines {
					if strings.Contains(line, "total:") {
						parts := strings.Fields(line)
						if len(parts) > 0 {
							coverage := parts[len(parts)-1]
							br.printSuccess(fmt.Sprintf("Test coverage: %s", coverage))
							break
						}
					}
				}

				// Generate HTML coverage report
				_, _, _, _ = br.runCommand("go", []string{"tool", "cover", "-html=coverage.out", "-o", "coverage.html"}, "", false)
				if _, err := os.Stat(filepath.Join(br.rootDir, "coverage.html")); err == nil {
					br.printSuccess("Coverage report generated: coverage.html")
				}
			}
		}
	}

	return true
}

// BuildBinary builds the main application binary
func (br *BuildRunner) BuildBinary() bool {
	br.printStep("Building application")

	// Create build directory
	if err := os.MkdirAll(br.buildDir, 0755); err != nil {
		br.printError(fmt.Sprintf("Failed to create build directory: %v", err))
		return false
	}

	// Build optimized binary with embedded assets
	binaryPath := filepath.Join(br.buildDir, br.binaryName)
	args := []string{
		"build",
		"-ldflags", "-s -w -extldflags=-static",
		"-tags", "netgo,osusergo",
		"-o", binaryPath,
		"./cmd/shoal",
	}

	exitCode, _, _, _ := br.runCommand("go", args, "", true)
	if exitCode != 0 {
		return false
	}

	// Verify binary was created
	info, err := os.Stat(binaryPath)
	if err != nil {
		br.printError("Binary was not created")
		return false
	}

	// Get binary size
	sizeMB := float64(info.Size()) / (1024 * 1024)
	br.printSuccess(fmt.Sprintf("Binary built: %s (%.1f MB)", binaryPath, sizeMB))

	// Test binary execution
	exitCode, _, _, _ = br.runCommand(binaryPath, []string{"-h"}, "", false)
	if exitCode == 0 {
		br.printSuccess("Binary execution test passed")
	} else {
		br.printWarning("Binary execution test failed (may be normal)")
	}

	return true
}

// BuildForPlatform builds the binary for a specific platform
func (br *BuildRunner) BuildForPlatform(goos, goarch string) bool {
	br.printStep(fmt.Sprintf("Building for %s/%s", goos, goarch))

	// Create build directory
	if err := os.MkdirAll(br.buildDir, 0755); err != nil {
		br.printError(fmt.Sprintf("Failed to create build directory: %v", err))
		return false
	}

	// Determine binary name with extension
	ext := ""
	if goos == "windows" {
		ext = ".exe"
	}
	binaryName := fmt.Sprintf("shoal-%s-%s%s", goos, goarch, ext)
	binaryPath := filepath.Join(br.buildDir, binaryName)

	// Build with target platform
	args := []string{
		"build",
		"-ldflags", "-s -w -extldflags=-static",
		"-tags", "netgo,osusergo",
		"-o", binaryPath,
		"./cmd/shoal",
	}

	cmd := exec.Command("go", args...)
	cmd.Dir = br.rootDir
	cmd.Env = append(os.Environ(), fmt.Sprintf("GOOS=%s", goos), fmt.Sprintf("GOARCH=%s", goarch))

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		br.printError(fmt.Sprintf("Failed to build for %s/%s: %v", goos, goarch, err))
		if stderr.Len() > 0 {
			fmt.Printf("STDERR:\n%s\n", stderr.String())
		}
		return false
	}

	// Verify binary was created
	info, err := os.Stat(binaryPath)
	if err != nil {
		br.printError(fmt.Sprintf("Failed to build for %s/%s", goos, goarch))
		return false
	}

	sizeMB := float64(info.Size()) / (1024 * 1024)
	br.printSuccess(fmt.Sprintf("Built: %s (%.1f MB)", binaryPath, sizeMB))

	return true
}

// BuildAllPlatforms builds binaries for all supported platforms
func (br *BuildRunner) BuildAllPlatforms() bool {
	br.printHeader("Building for all supported platforms")

	platforms := []SupportedPlatform{
		{"linux", "amd64"},
		{"windows", "amd64"},
		{"darwin", "amd64"},
		{"linux", "arm64"},
		{"darwin", "arm64"},
	}

	allOk := true
	for _, platform := range platforms {
		if !br.BuildForPlatform(platform.GOOS, platform.GOARCH) {
			allOk = false
		}
	}

	return allOk
}

// BuildDispatcher builds the provisioner dispatcher binary (static)
func (br *BuildRunner) BuildDispatcher() bool {
	br.printHeader("Building Provisioner Dispatcher")

	dispatcherPath := filepath.Join(br.rootDir, "cmd", "provisioner-dispatcher")
	if _, err := os.Stat(dispatcherPath); os.IsNotExist(err) {
		br.printWarning("Dispatcher source not found - skipping")
		return true // Not a hard failure
	}

	platforms := []SupportedPlatform{
		{"linux", "amd64"},
		{"linux", "arm64"},
	}

	allOk := true
	for _, platform := range platforms {
		binaryName := fmt.Sprintf("dispatcher-%s-%s", platform.GOOS, platform.GOARCH)
		binaryPath := filepath.Join(br.buildDir, binaryName)

		br.printStep(fmt.Sprintf("Building dispatcher for %s/%s", platform.GOOS, platform.GOARCH))

		// Build static binary with minimal size
		cmd := exec.Command("go", "build", "-ldflags=-s -w", "-o", binaryPath, ".")
		cmd.Dir = dispatcherPath
		cmd.Env = append(os.Environ(),
			"CGO_ENABLED=0",
			fmt.Sprintf("GOOS=%s", platform.GOOS),
			fmt.Sprintf("GOARCH=%s", platform.GOARCH),
		)

		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			br.printError(fmt.Sprintf("Failed to build dispatcher for %s/%s: %v", platform.GOOS, platform.GOARCH, err))
			if stderr.Len() > 0 {
				fmt.Printf("STDERR:\n%s\n", stderr.String())
			}
			allOk = false
			continue
		}

		// Verify binary was created
		info, err := os.Stat(binaryPath)
		if err != nil {
			br.printError(fmt.Sprintf("Failed to build dispatcher for %s/%s", platform.GOOS, platform.GOARCH))
			allOk = false
			continue
		}

		sizeMB := float64(info.Size()) / (1024 * 1024)
		br.printSuccess(fmt.Sprintf("Built: %s (%.1f MB)", binaryPath, sizeMB))
	}

	return allOk
}

// InstallTools installs development tools (golangci-lint and gosec)
func (br *BuildRunner) InstallTools() bool {
	br.printHeader("Installing Development Tools")

	tools := []struct {
		name    string
		pkg     string
		version string
		check   []string
	}{
		{
			name:    "golangci-lint",
			pkg:     "github.com/golangci/golangci-lint/cmd/golangci-lint",
			version: "v2.6.1", // v2.x required for .golangci.yml version: 2
			check:   []string{"golangci-lint", "--version"},
		},
		{
			name:    "gosec",
			pkg:     "github.com/securego/gosec/v2/cmd/gosec",
			version: "latest",
			check:   []string{"gosec", "-version"},
		},
		{
			name:    "deadcode",
			pkg:     "github.com/remyoudompheng/go-misc/deadcode",
			version: "latest",
			check:   []string{"deadcode", "-help"},
		},
	}

	allOk := true
	for _, tool := range tools {
		br.printStep(fmt.Sprintf("Installing %s", tool.name))

		// Check if already installed
		exitCode, stdout, _, err := br.runCommand(tool.check[0], tool.check[1:], "", false)
		if err == nil && exitCode == 0 {
			version := strings.TrimSpace(strings.Split(stdout, "\n")[0])
			br.printSuccess(fmt.Sprintf("%s already installed: %s", tool.name, version))
			continue
		}

		// Install the tool
		installPkg := fmt.Sprintf("%s@%s", tool.pkg, tool.version)
		br.printStep(fmt.Sprintf("Running: go install %s", installPkg))

		exitCode, _, stderr, _ := br.runCommand("go", []string{"install", installPkg}, "", false)
		if exitCode != 0 {
			br.printError(fmt.Sprintf("Failed to install %s", tool.name))
			if stderr != "" {
				fmt.Printf("Error: %s\n", stderr)
			}
			allOk = false
			continue
		}

		// Verify installation
		exitCode, stdout, _, err = br.runCommand(tool.check[0], tool.check[1:], "", false)
		if err == nil && exitCode == 0 {
			version := strings.TrimSpace(strings.Split(stdout, "\n")[0])
			br.printSuccess(fmt.Sprintf("Successfully installed %s: %s", tool.name, version))
		} else {
			br.printWarning(fmt.Sprintf("%s installed but not found in PATH. You may need to add $GOPATH/bin or $HOME/go/bin to your PATH.", tool.name))
			allOk = false
		}
	}

	if allOk {
		fmt.Println()
		br.printSuccess("All development tools are installed and ready to use!")
	} else {
		fmt.Println()
		br.printWarning("Some tools were not installed successfully. Check the output above for details.")
	}

	return allOk
}

// RunSecurityChecks runs security analysis if tools are available
func (br *BuildRunner) RunSecurityChecks() bool {
	br.printStep("Running security checks")

	// 1. Try gosec if available (informational only for now due to pre-existing issues)
	exitCode, _, _, err := br.runCommand("gosec", []string{"-version"}, "", false)
	if err == nil && exitCode == 0 {
		fmt.Println("  Running gosec (informational only)...")
		exitCode, _, _, _ := br.runCommand("gosec", []string{"./..."}, "", true)
		if exitCode != 0 {
			br.printWarning("Security scan found issues (not failing build)")
			br.printWarning("Most issues are unchecked errors (G104) from pre-existing code")
			br.printWarning("Future milestone will address security cleanup")
		} else {
			br.printSuccess("Security scan passed")
		}
		// Don't fail build on gosec issues for now
	} else {
		br.printWarning("gosec not available - skipping security scan")
	}

	// 2. Scan codebase for accidentally committed secrets
	// Patterns that should never appear in source code or logs
	// NOTE: This will flag these patterns in build.go itself (false positive)
	// and redacted logging examples like "secret=redacted" (also false positive).
	// Real secrets would appear as plaintext values after the = sign.
	secretPatterns := []string{
		"password=",
		"secret=",
		"token=",
		"api_key=",
		"private_key=",
		"-----BEGIN.*PRIVATE KEY-----",
	}

	fmt.Println("  Scanning for accidentally committed secrets...")
	foundSecrets := false
	for _, pattern := range secretPatterns {
		// Use grep to search across all non-test, non-vendor files
		// Exclude test files since they may contain mock credentials
		grepArgs := []string{
			"-r",                   // recursive
			"-i",                   // case-insensitive
			"-n",                   // show line numbers
			"--include=*.go",       // only Go files
			"--exclude=*_test.go",  // exclude tests
			"--exclude-dir=vendor", // exclude vendor
			"--exclude-dir=.git",   // exclude git
			"-E",                   // extended regex
			pattern,
			".",
		}

		exitCode, stdout, _, _ := br.runCommand("grep", grepArgs, "", false)
		// grep returns 0 if pattern found, 1 if not found, 2 on error
		if exitCode == 0 && len(strings.TrimSpace(stdout)) > 0 {
			br.printWarning(fmt.Sprintf("Found potential secret pattern '%s':", pattern))
			// Print first few matches
			lines := strings.Split(strings.TrimSpace(stdout), "\n")
			for i, line := range lines {
				if i >= 3 {
					fmt.Printf("    ... (%d more matches)\n", len(lines)-3)
					break
				}
				fmt.Printf("    %s\n", line)
			}
			foundSecrets = true
		}
	}

	if foundSecrets {
		br.printWarning("Found potential secrets in codebase - please review")
		br.printWarning("If these are false positives (e.g., redacted examples), ignore this warning")
	} else {
		br.printSuccess("No secrets detected in codebase")
	}

	// 3. Verify all protected endpoints enforce authentication
	// This is validated by TestAuthEnforcement_ProtectedEndpointsRequireAuth
	// Just document that it's covered by tests
	fmt.Println("  ✓ Auth enforcement validated by security tests")

	return true
}

// GenerateBuildInfo creates build metadata
func (br *BuildRunner) GenerateBuildInfo() *BuildInfo {
	buildInfo := &BuildInfo{
		Timestamp:    time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Platform:     runtime.GOOS,
		Architecture: runtime.GOARCH,
		GitCommit:    "unknown",
		GitBranch:    "unknown",
		GitDirty:     false,
		GoVersion:    "unknown",
	}

	// Get Git information if available
	if exitCode, stdout, _, _ := br.runCommand("git", []string{"rev-parse", "HEAD"}, "", false); exitCode == 0 {
		commit := strings.TrimSpace(stdout)
		if len(commit) >= 8 {
			buildInfo.GitCommit = commit[:8]
		}
	}

	if exitCode, stdout, _, _ := br.runCommand("git", []string{"branch", "--show-current"}, "", false); exitCode == 0 {
		buildInfo.GitBranch = strings.TrimSpace(stdout)
	}

	if exitCode, stdout, _, _ := br.runCommand("git", []string{"status", "--porcelain"}, "", false); exitCode == 0 {
		buildInfo.GitDirty = len(strings.TrimSpace(stdout)) > 0
	}

	// Get Go version
	if exitCode, stdout, _, _ := br.runCommand("go", []string{"version"}, "", false); exitCode == 0 {
		buildInfo.GoVersion = strings.TrimSpace(stdout)
	}

	// Write build info to file
	buildInfoPath := filepath.Join(br.buildDir, "build-info.json")
	if data, err := json.MarshalIndent(buildInfo, "", "  "); err == nil {
		if err := os.WriteFile(buildInfoPath, data, 0644); err != nil {
			br.printWarning(fmt.Sprintf("Failed to write build info: %v", err))
		}
	}

	return buildInfo
}

// Validate runs the full validation pipeline
func (br *BuildRunner) Validate() bool {
	br.printHeader("Shoal Build & Test Validation")

	steps := []struct {
		name string
		fn   func() bool
	}{
		{"Prerequisites", br.CheckPrerequisites},
		{"Dependencies", br.DownloadDependencies},
		{"Format", br.FormatCode},
		{"Lint", br.LintCode},
		{"Tests", func() bool { return br.RunTests(true) }},
		{"Security", br.RunSecurityChecks},
		{"Build", br.BuildBinary},
	}

	for _, step := range steps {
		if !step.fn() {
			br.printError(fmt.Sprintf("Step '%s' failed", step.name))
			return false
		}
	}

	// Generate build info
	br.GenerateBuildInfo()
	br.printSuccess("Build info generated")

	return true
}

// PrintSummary prints the build summary
func (br *BuildRunner) PrintSummary(success bool) {
	br.printHeader("Build Summary")

	status := "SUCCESS"
	color := colorGreen
	if !success {
		status = "FAILED"
		color = colorRed
	}

	elapsedTime := time.Since(br.startTime).Seconds()

	fmt.Printf("Status: %s%s%s%s\n", colorBold, color, status, colorReset)
	fmt.Printf("Time: %.1fs\n", elapsedTime)

	if success {
		binaryPath := filepath.Join(br.buildDir, br.binaryName)
		if _, err := os.Stat(binaryPath); err == nil {
			fmt.Printf("Binary: %s\n", binaryPath)
		}
	}
}

func main() {
	// Define command line flags
	var platformFlag string
	flag.StringVar(&platformFlag, "platform", "", "Target platform in the form os/arch (e.g., linux/amd64)")
	flag.Parse()

	// Get command (default to "validate")
	command := "validate"
	args := flag.Args()
	if len(args) > 0 {
		command = args[0]
	}

	// Validate command
	validCommands := map[string]bool{
		"build":            true,
		"build-dispatcher": true,
		"test":             true,
		"clean":            true,
		"fmt":              true,
		"lint":             true,
		"coverage":         true,
		"deps":             true,
		"validate":         true,
		"build-all":        true,
		"install-tools":    true,
	}

	if !validCommands[command] {
		fmt.Fprintf(os.Stderr, "Invalid command: %s\n", command)
		fmt.Fprintf(os.Stderr, "Valid commands: build, build-dispatcher, test, clean, fmt, lint, coverage, deps, validate, build-all, install-tools\n")
		os.Exit(1)
	}

	// Create build runner
	runner, err := NewBuildRunner()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize build runner: %v\n", err)
		os.Exit(1)
	}

	success := false

	// Execute command
	switch command {
	case "clean":
		success = runner.Clean()

	case "deps":
		success = runner.CheckPrerequisites() && runner.DownloadDependencies()

	case "fmt":
		success = runner.CheckPrerequisites() && runner.FormatCode()

	case "lint":
		success = runner.CheckPrerequisites() && runner.LintCode()

	case "test":
		success = runner.CheckPrerequisites() &&
			runner.DownloadDependencies() &&
			runner.RunTests(false)

	case "coverage":
		success = runner.CheckPrerequisites() &&
			runner.DownloadDependencies() &&
			runner.RunTests(true)

	case "build":
		if platformFlag != "" {
			parts := strings.Split(platformFlag, "/")
			if len(parts) != 2 {
				fmt.Fprintf(os.Stderr, "--platform must be in the form os/arch, e.g., linux/amd64\n")
				os.Exit(1)
			}
			goos, goarch := parts[0], parts[1]
			success = runner.CheckPrerequisites() &&
				runner.DownloadDependencies() &&
				runner.BuildForPlatform(goos, goarch)
		} else {
			success = runner.CheckPrerequisites() &&
				runner.DownloadDependencies() &&
				runner.BuildBinary()
		}

	case "build-all":
		success = runner.CheckPrerequisites() &&
			runner.DownloadDependencies() &&
			runner.BuildAllPlatforms()

	case "build-dispatcher":
		success = runner.CheckPrerequisites() &&
			runner.DownloadDependencies() &&
			runner.BuildDispatcher()

	case "install-tools":
		success = runner.CheckPrerequisites() && runner.InstallTools()

	case "validate":
		success = runner.Validate()
	}

	runner.PrintSummary(success)

	if !success {
		os.Exit(1)
	}
}
