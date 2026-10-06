// These tests build the real binary and lock its commands, diagnostics, and
// failure behavior.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var profilerBinary string

func TestMain(m *testing.M) {
	if installed := os.Getenv("RC_GO_PROFILER_BIN"); installed != "" {
		profilerBinary = installed
		os.Exit(m.Run())
	}
	buildRoot, err := os.MkdirTemp("", "runtimeconditions-cli-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot create build directory: %v\n", err)
		os.Exit(1)
	}
	profilerBinary = filepath.Join(buildRoot, "go-rc-profiler")
	build := exec.Command("go", "build", "-o", profilerBinary, ".")
	build.Env = append(os.Environ(), "GOWORK=off")
	if output, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "cannot build profiler: %v\n%s", err, output)
		os.RemoveAll(buildRoot)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(buildRoot)
	os.Exit(code)
}

func TestGeneratedEntryPointRequiresInstalledBindingsWithoutWritingProfile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/workload\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(t.TempDir(), "profile.yaml")
	result := runProfiler(t, "generate", "-dir", dir, "-out", outPath)
	requireExitCode(t, result, 1)
	if result.stdout != "" || !strings.Contains(result.stderr, "no imported generated Go binding packages") {
		t.Fatalf("expected missing installed binding failure without profile output, got stdout %q, stderr %q", result.stdout, result.stderr)
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("failed generation created output file: %v", err)
	}
}

func TestGeneratedEntryPointRejectsLegacyBypassFlags(t *testing.T) {
	result := runProfiler(t, "generate", "-skip-validation")
	requireExitCode(t, result, 2)
	if result.stdout != "" || !strings.Contains(result.stderr, "flag provided but not defined") {
		t.Fatalf("unexpected bypass result: stdout %q, stderr %q", result.stdout, result.stderr)
	}
}

func TestLegacyCommandIsUnavailable(t *testing.T) {
	result := runProfiler(t, "generate-legacy")
	requireExitCode(t, result, 1)
	if result.stdout != "" || !strings.Contains(result.stderr, `unknown command "generate-legacy"`) {
		t.Fatalf("unexpected command result: stdout %q, stderr %q", result.stdout, result.stderr)
	}
}

func TestGeneratedEntryPointRequiresTypeCheckedSource(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/workload\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nvar _ = missingSymbol\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := runProfiler(t, "generate", "-dir", dir)
	requireExitCode(t, result, 1)
	if result.stdout != "" || !strings.Contains(result.stderr, "missingSymbol") || strings.Contains(result.stderr, "not yet implemented") {
		t.Fatalf("expected Go type-checking error before extraction, got stdout %q, stderr %q", result.stdout, result.stderr)
	}
}

func TestValidateExtensionsRejectsWorkloadWithoutBindings(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/workload\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := runProfiler(t, "validate-extensions", "-dir", dir)
	requireExitCode(t, result, 1)
	if !strings.Contains(result.stderr, "no imported generated Go binding packages") {
		t.Fatalf("unexpected stderr: %q", result.stderr)
	}
}

// TestFailuresReportDiagnosticsAndExitNonZero locks the failure contract every
// caller depends on: a runtimeconditions-prefixed diagnostic on stderr, nothing
// on stdout, and a non-zero exit status.
func TestFailuresReportDiagnosticsAndExitNonZero(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")

	tests := []struct {
		name string
		args []string
	}{
		{name: "generate", args: []string{"-dir", missing}},
		{name: "validate-extension", args: []string{"validate-extension", "-dir", missing, "-package", "example.com/missing"}},
		{name: "validate-extensions", args: []string{"validate-extensions", "-dir", missing}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := runProfiler(t, test.args...)
			requireExitCode(t, result, 1)
			if result.stdout != "" {
				t.Fatalf("a failing run must not write a profile to stdout, got: %s", result.stdout)
			}
			if !strings.HasPrefix(result.stderr, "runtimeconditions: ") {
				t.Fatalf("unexpected diagnostic: %q", result.stderr)
			}
		})
	}
}

type profilerResult struct {
	stdout   string
	stderr   string
	exitCode int
}

func runProfiler(t *testing.T, args ...string) profilerResult {
	t.Helper()
	var stdout, stderr strings.Builder
	command := exec.Command(profilerBinary, args...)
	command.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()
	result := profilerResult{stdout: stdout.String(), stderr: stderr.String()}
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		result.exitCode = exit.ExitCode()
	default:
		t.Fatal(err)
	}
	return result
}

func requireExitCode(t *testing.T, result profilerResult, want int) {
	t.Helper()
	if result.exitCode != want {
		t.Fatalf("expected exit code %d, got %d\nstdout: %s\nstderr: %s", want, result.exitCode, result.stdout, result.stderr)
	}
}

func regressionPath(parts ...string) string {
	return filepath.Join(append([]string{"extractor", "testdata", "regression"}, parts...)...)
}
