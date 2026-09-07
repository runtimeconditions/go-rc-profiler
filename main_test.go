// The command-line surface is part of the profiler contract: rc-demos, the spec
// guides, and the NATS authorship harness all drive the profiler through these
// flags. These tests build the real binary and lock its subcommands, flag
// defaults, output destinations, diagnostics, and exit codes.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var profilerBinary string

func TestMain(m *testing.M) {
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

func TestGenerateWritesProfileToStdout(t *testing.T) {
	result := runProfiler(t,
		"-dir", regressionPath("workloads", "request-logger-http"),
		"-name", "request-logger-http",
		"-workload-uri", "github.com/runtimeconditions/rc-demos/apps/request-logger-http",
		"-workload-version", "dev",
	)
	requireExitCode(t, result, 0)
	if result.stdout != goldenProfile(t, "request-logger-http.golden.yaml") {
		t.Fatalf("stdout profile differs from the golden profile\n--- got ---\n%s", result.stdout)
	}
}

func TestGenerateWritesProfileToOutFile(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "profile.yaml")
	result := runProfiler(t,
		"-dir", regressionPath("workloads", "request-logger-http"),
		"-name", "request-logger-http",
		"-workload-uri", "github.com/runtimeconditions/rc-demos/apps/request-logger-http",
		"-workload-version", "dev",
		"-out", outPath,
	)
	requireExitCode(t, result, 0)
	if result.stdout != "" {
		t.Fatalf("-out must keep stdout empty, got: %s", result.stdout)
	}
	written, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != goldenProfile(t, "request-logger-http.golden.yaml") {
		t.Fatalf("written profile differs from the golden profile\n--- got ---\n%s", written)
	}
}

// TestGenerateDerivesMetadataDefaults locks the documented defaults: the profile
// name falls back to the source directory, the workload URI to the enclosing Go
// module path, and the workload version to dev.
func TestGenerateDerivesMetadataDefaults(t *testing.T) {
	result := runProfiler(t, "-dir", regressionPath("workloads", "request-logger-http"))
	requireExitCode(t, result, 0)

	var profile struct {
		Metadata struct {
			Name string `yaml:"name"`
		} `yaml:"metadata"`
		Workload struct {
			URI     string `yaml:"uri"`
			Version string `yaml:"version"`
		} `yaml:"workload"`
	}
	if err := yaml.Unmarshal([]byte(result.stdout), &profile); err != nil {
		t.Fatal(err)
	}
	if profile.Metadata.Name != "request-logger-http" {
		t.Fatalf("unexpected default name: %q", profile.Metadata.Name)
	}
	if profile.Workload.URI != "github.com/runtimeconditions/rc-demos/apps/request-logger-http" {
		t.Fatalf("unexpected default workload URI: %q", profile.Workload.URI)
	}
	if profile.Workload.Version != "dev" {
		t.Fatalf("unexpected default workload version: %q", profile.Workload.Version)
	}
}

func TestValidateExtensionsAcceptsFirstPartyCatalog(t *testing.T) {
	result := runProfiler(t, "validate-extensions", "-root", regressionPath("extensions"))
	requireExitCode(t, result, 0)
	if strings.TrimSpace(result.stderr) != "runtimeconditions: extensions validation passed" {
		t.Fatalf("unexpected stderr: %q", result.stderr)
	}
}

func TestValidateExtensionAcceptsSingleExtension(t *testing.T) {
	result := runProfiler(t,
		"validate-extension",
		"-root", regressionPath("extensions", "common-integrations"),
		"-catalog-root", regressionPath("extensions"),
	)
	requireExitCode(t, result, 0)
	if strings.TrimSpace(result.stderr) != "runtimeconditions: extension validation passed" {
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
		{name: "validate-extension", args: []string{"validate-extension", "-root", missing}},
		{name: "validate-extensions", args: []string{"validate-extensions", "-root", missing}},
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

func goldenProfile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(regressionPath("golden", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
