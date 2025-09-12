#!/usr/bin/env python3
"""
Shoal Build Automation

A simple Python-based build and test automation system for the Shoal Redfish Aggregator.
Requires Python 3.12+ with no external dependencies.

Usage:
    python build.py                    # Run full build and test pipeline
    python build.py test              # Run tests only
    python build.py build             # Build binary only
    python build.py clean             # Clean build artifacts
    python build.py fmt               # Format Go code
    python build.py lint              # Run linting (if available)
    python build.py coverage          # Run tests with coverage
    python build.py deps              # Check and download dependencies
    python build.py validate          # Full validation pipeline
"""

import argparse
import json
import os
import platform
import shutil
import subprocess
import sys
import time
from pathlib import Path
from typing import Dict, List, Optional, Tuple


class Colors:
    """ANSI color codes for terminal output."""
    RESET = '\033[0m'
    BOLD = '\033[1m'
    RED = '\033[91m'
    GREEN = '\033[92m'
    YELLOW = '\033[93m'
    BLUE = '\033[94m'
    PURPLE = '\033[95m'
    CYAN = '\033[96m'


class BuildRunner:
    SUPPORTED_PLATFORMS = [
        ("linux", "amd64"),
        ("windows", "amd64"),
        ("darwin", "amd64"),
        ("linux", "arm64"),
        ("darwin", "arm64"),
    ]

    def build_for_platform(self, goos: str, goarch: str) -> bool:
        """Build the application binary for a specific platform."""
        self.print_step(f"Building for {goos}/{goarch}")
        self.build_dir.mkdir(exist_ok=True)
        ext = ".exe" if goos == "windows" else ""
        binary_name = f"shoal-{goos}-{goarch}{ext}"
        binary_path = self.build_dir / binary_name
        env = os.environ.copy()
        env["GOOS"] = goos
        env["GOARCH"] = goarch
        cmd = [
            "go", "build",
            "-ldflags", "-s -w -extldflags=-static",
            "-tags", "netgo,osusergo",
            "-o", str(binary_path),
            "./cmd/shoal"
        ]
        ret_code, _, _ = self.run_command(cmd, check=True)
        if ret_code != 0 or not binary_path.exists():
            self.print_error(f"Failed to build for {goos}/{goarch}")
            return False
        size_mb = binary_path.stat().st_size / (1024 * 1024)
        self.print_success(f"Built: {binary_path} ({size_mb:.1f} MB)")
        return True

    def build_all_platforms(self) -> bool:
        """Build binaries for all supported platforms."""
        self.print_header("Building for all supported platforms")
        all_ok = True
        for goos, goarch in self.SUPPORTED_PLATFORMS:
            ok = self.build_for_platform(goos, goarch)
            all_ok = all_ok and ok
        return all_ok
    """Main build automation runner."""

    def __init__(self):
        self.root_dir = Path(__file__).parent
        self.build_dir = self.root_dir / "build"
        self.binary_name = "shoal.exe" if platform.system() == "Windows" else "shoal"
        self.start_time = time.time()

    def print_header(self, title: str) -> None:
        """Print a formatted header."""
        print(f"\n{Colors.BOLD}{Colors.BLUE}{'='*60}{Colors.RESET}")
        print(f"{Colors.BOLD}{Colors.BLUE} {title}{Colors.RESET}")
        print(f"{Colors.BOLD}{Colors.BLUE}{'='*60}{Colors.RESET}\n")

    def print_step(self, step: str) -> None:
        """Print a build step."""
        print(f"{Colors.BOLD}{Colors.CYAN}→{Colors.RESET} {step}")

    def print_success(self, message: str) -> None:
        """Print a success message."""
        print(f"{Colors.BOLD}{Colors.GREEN}✓{Colors.RESET} {message}")

    def print_error(self, message: str) -> None:
        """Print an error message."""
        print(f"{Colors.BOLD}{Colors.RED}✗{Colors.RESET} {message}")

    def print_warning(self, message: str) -> None:
        """Print a warning message."""
        print(f"{Colors.BOLD}{Colors.YELLOW}⚠{Colors.RESET} {message}")

    def run_command(self, cmd: List[str], cwd: Optional[Path] = None, check: bool = True) -> Tuple[int, str, str]:
        """Run a command and return (returncode, stdout, stderr)."""
        try:
            result = subprocess.run(
                cmd,
                cwd=cwd or self.root_dir,
                capture_output=True,
                text=True,
                check=False
            )

            if check and result.returncode != 0:
                self.print_error(f"Command failed: {' '.join(cmd)}")
                if result.stdout:
                    print(f"STDOUT:\n{result.stdout}")
                if result.stderr:
                    print(f"STDERR:\n{result.stderr}")

            return result.returncode, result.stdout, result.stderr

        except FileNotFoundError:
            self.print_error(f"Command not found: {cmd[0]}")
            return 1, "", f"Command not found: {cmd[0]}"

    def check_prerequisites(self) -> bool:
        """Check if required tools are available."""
        self.print_step("Checking prerequisites")

        # Check Go installation
        ret_code, stdout, _ = self.run_command(["go", "version"], check=False)
        if ret_code != 0:
            self.print_error("Go is not installed or not in PATH")
            return False

        go_version = stdout.strip()
        self.print_success(f"Found {go_version}")

        # Check if we're in a Go module
        if not (self.root_dir / "go.mod").exists():
            self.print_error("go.mod not found - not in a Go module directory")
            return False

        self.print_success("All prerequisites met")
        return True

    def clean(self) -> bool:
        """Clean build artifacts."""
        self.print_step("Cleaning build artifacts")

        # Remove build directory
        if self.build_dir.exists():
            shutil.rmtree(self.build_dir)
            self.print_success("Removed build directory")

        # Remove binary from root
        binary_path = self.root_dir / self.binary_name
        if binary_path.exists():
            binary_path.unlink()
            self.print_success(f"Removed {self.binary_name}")

        # Remove test artifacts
        test_artifacts = [
            "coverage.out",
            "coverage.html",
            "coverage.txt",
            "*.test",
            "*.db",
            "*.sqlite",
            "*.sqlite3"
        ]

        for pattern in test_artifacts:
            for file_path in self.root_dir.rglob(pattern):
                if file_path.is_file():
                    file_path.unlink()

        self.print_success("Cleaned test artifacts")
        return True

    def download_dependencies(self) -> bool:
        """Download Go module dependencies."""
        self.print_step("Downloading dependencies")

        ret_code, _, _ = self.run_command(["go", "mod", "download"])
        if ret_code != 0:
            return False

        # Verify dependencies
        ret_code, _, _ = self.run_command(["go", "mod", "verify"])
        if ret_code != 0:
            self.print_error("Dependency verification failed")
            return False

        self.print_success("Dependencies downloaded and verified")
        return True

    def format_code(self) -> bool:
        """Format Go code."""
        self.print_step("Formatting Go code")

        ret_code, _, _ = self.run_command(["go", "fmt", "./..."])
        if ret_code != 0:
            return False

        self.print_success("Code formatted")
        return True

    def lint_code(self) -> bool:
        """Lint Go code (if linter is available)."""
        self.print_step("Linting code")

        # Try golangci-lint first
        ret_code, _, _ = self.run_command(["golangci-lint", "--version"], check=False)
        if ret_code == 0:
            ret_code, _, _ = self.run_command(["golangci-lint", "run"])
            if ret_code != 0:
                return False
            self.print_success("Linting passed (golangci-lint)")
            return True

        # Try go vet as fallback
        ret_code, _, _ = self.run_command(["go", "vet", "./..."])
        if ret_code != 0:
            return False

        self.print_success("Static analysis passed (go vet)")
        return True

    def run_tests(self, with_coverage: bool = False) -> bool:
        """Run Go tests."""
        self.print_step("Running tests")

        cmd = ["go", "test"]
        if with_coverage:
            cmd.extend(["-coverprofile=coverage.out"])
        cmd.extend(["-v", "./..."])

        ret_code, stdout, stderr = self.run_command(cmd)
        if ret_code != 0:
            return False

        # Parse test results
        self.print_success("All tests passed")

        # Generate coverage report if requested
        if with_coverage and (self.root_dir / "coverage.out").exists():
            ret_code, coverage_output, _ = self.run_command(
                ["go", "tool", "cover", "-func=coverage.out"], check=False
            )
            if ret_code == 0:
                # Extract total coverage
                for line in coverage_output.strip().split('\n'):
                    if 'total:' in line:
                        coverage = line.split()[-1]
                        self.print_success(f"Test coverage: {coverage}")
                        break

                # Generate HTML coverage report
                self.run_command(
                    ["go", "tool", "cover", "-html=coverage.out", "-o", "coverage.html"],
                    check=False
                )
                if (self.root_dir / "coverage.html").exists():
                    self.print_success("Coverage report generated: coverage.html")

        return True

    def build_binary(self) -> bool:
        """Build the application binary."""
        self.print_step("Building application")

        # Create build directory
        self.build_dir.mkdir(exist_ok=True)

        # Build optimized binary with embedded assets
        binary_path = self.build_dir / self.binary_name
        cmd = [
            "go", "build",
            "-ldflags", "-s -w -extldflags=-static",  # Strip debug info and static linking for single binary
            "-tags", "netgo,osusergo",  # Use pure Go implementations for networking and user lookups
            "-o", str(binary_path),
            "./cmd/shoal"
        ]

        ret_code, _, _ = self.run_command(cmd)
        if ret_code != 0:
            return False

        # Verify binary was created
        if not binary_path.exists():
            self.print_error("Binary was not created")
            return False

        # Get binary size
        size_mb = binary_path.stat().st_size / (1024 * 1024)
        self.print_success(f"Binary built: {binary_path} ({size_mb:.1f} MB)")

        # Test binary execution
        ret_code, _, _ = self.run_command([str(binary_path), "-h"], check=False)
        if ret_code == 0:
            self.print_success("Binary execution test passed")
        else:
            self.print_warning("Binary execution test failed (may be normal)")

        return True

    def run_security_checks(self) -> bool:
        """Run security checks (if tools are available)."""
        self.print_step("Running security checks")

        # Try gosec if available
        ret_code, _, _ = self.run_command(["gosec", "-version"], check=False)
        if ret_code == 0:
            ret_code, _, _ = self.run_command(["gosec", "./..."])
            if ret_code != 0:
                self.print_warning("Security scan found issues")
                return False
            self.print_success("Security scan passed")
        else:
            self.print_warning("gosec not available - skipping security scan")

        return True

    def generate_build_info(self) -> Dict:
        """Generate build information."""
        # Get Git information if available
        git_commit = "unknown"
        git_branch = "unknown"
        git_dirty = False

        ret_code, stdout, _ = self.run_command(["git", "rev-parse", "HEAD"], check=False)
        if ret_code == 0:
            git_commit = stdout.strip()[:8]

        ret_code, stdout, _ = self.run_command(["git", "branch", "--show-current"], check=False)
        if ret_code == 0:
            git_branch = stdout.strip()

        ret_code, stdout, _ = self.run_command(["git", "status", "--porcelain"], check=False)
        if ret_code == 0:
            git_dirty = bool(stdout.strip())

        # Get Go version
        ret_code, go_version, _ = self.run_command(["go", "version"], check=False)
        go_version = go_version.strip() if ret_code == 0 else "unknown"

        build_info = {
            "timestamp": time.strftime("%Y-%m-%d %H:%M:%S UTC", time.gmtime()),
            "go_version": go_version,
            "git_commit": git_commit,
            "git_branch": git_branch,
            "git_dirty": git_dirty,
            "platform": platform.system(),
            "architecture": platform.machine()
        }

        # Write build info to file
        build_info_path = self.build_dir / "build-info.json"
        build_info_path.write_text(json.dumps(build_info, indent=2))

        return build_info

    def print_summary(self, success: bool, elapsed_time: float) -> None:
        """Print build summary."""
        self.print_header("Build Summary")

        status = "SUCCESS" if success else "FAILED"
        color = Colors.GREEN if success else Colors.RED

        print(f"Status: {Colors.BOLD}{color}{status}{Colors.RESET}")
        print(f"Time: {elapsed_time:.1f}s")

        if success and self.build_dir.exists():
            binary_path = self.build_dir / self.binary_name
            if binary_path.exists():
                print(f"Binary: {binary_path}")

    def validate(self) -> bool:
        """Run full validation pipeline."""
        self.print_header("Shoal Build & Test Validation")

        steps = [
            ("Prerequisites", self.check_prerequisites),
            ("Dependencies", self.download_dependencies),
            ("Format", self.format_code),
            ("Lint", self.lint_code),
            ("Tests", lambda: self.run_tests(with_coverage=True)),
            ("Security", self.run_security_checks),
            ("Build", self.build_binary),
        ]

        for step_name, step_func in steps:
            try:
                if not step_func():
                    self.print_error(f"Step '{step_name}' failed")
                    return False
            except Exception as e:
                self.print_error(f"Step '{step_name}' failed with exception: {e}")
                return False

        # Generate build info
        build_info = self.generate_build_info()
        self.print_success("Build info generated")

        return True


def main():
    """Main entry point."""
    parser = argparse.ArgumentParser(description="Shoal Build Automation")
    parser.add_argument(
        "command",
        nargs="?",
        default="validate",
        choices=["build", "test", "clean", "fmt", "lint", "coverage", "deps", "validate", "build-all"],
        help="Build command to run"
    )
    parser.add_argument(
        "--platform",
        type=str,
        default=None,
        help="Target platform in the form os/arch (e.g., linux/amd64, windows/amd64, darwin/arm64)"
    )

    args = parser.parse_args()

    runner = BuildRunner()
    success = False

    try:
        if args.command == "clean":
            success = runner.clean()
        elif args.command == "deps":
            success = runner.check_prerequisites() and runner.download_dependencies()
        elif args.command == "fmt":
            success = runner.check_prerequisites() and runner.format_code()
        elif args.command == "lint":
            success = runner.check_prerequisites() and runner.lint_code()
        elif args.command == "test":
            success = (runner.check_prerequisites() and
                      runner.download_dependencies() and
                      runner.run_tests())
        elif args.command == "coverage":
            success = (runner.check_prerequisites() and
                      runner.download_dependencies() and
                      runner.run_tests(with_coverage=True))
        elif args.command == "build":
            if args.platform:
                try:
                    goos, goarch = args.platform.split("/")
                except ValueError:
                    runner.print_error("--platform must be in the form os/arch, e.g., linux/amd64")
                    sys.exit(1)
                success = (runner.check_prerequisites() and
                           runner.download_dependencies() and
                           runner.build_for_platform(goos, goarch))
            else:
                success = (runner.check_prerequisites() and
                           runner.download_dependencies() and
                           runner.build_binary())
        elif args.command == "build-all":
            success = (runner.check_prerequisites() and
                       runner.download_dependencies() and
                       runner.build_all_platforms())
        elif args.command == "validate":
            success = runner.validate()

    except KeyboardInterrupt:
        runner.print_error("Build interrupted by user")
        success = False
    except Exception as e:
        runner.print_error(f"Build failed with exception: {e}")
        success = False

    elapsed_time = time.time() - runner.start_time
    runner.print_summary(success, elapsed_time)

    sys.exit(0 if success else 1)


if __name__ == "__main__":
    main()
