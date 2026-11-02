// Package cli is the go-rc-profiler command line as a library.
//
// Callers need the go toolchain on PATH: the profiler runs go list and loads packages through
// golang.org/x/tools/go/packages. The core profile schema is injected at link time, so an
// executable that imports this package must be linked with
// -X github.com/runtimeconditions/go-rc-profiler/extensioncheck.installedCoreSchemaBase64=<base64 of the pinned schema>.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/runtimeconditions/go-rc-profiler/extensioncheck"
	"github.com/runtimeconditions/go-rc-profiler/extractor"
)

var (
	version = "dev"
	commit  = "unknown"
)

// CoreSchemas lists the core profile schema versions this profiler emits.
var CoreSchemas = []string{"0.4.0"}

// ExitError carries the exit code of a failed command. Reported is set when the message was already written.
type ExitError struct {
	Code     int
	Err      error
	Reported bool
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

// Exit reports err on stderr unless it was already reported and returns the process exit code.
func Exit(stderr io.Writer, err error) int {
	if err == nil {
		return 0
	}
	var exit *ExitError
	if errors.As(err, &exit) {
		if !exit.Reported {
			fmt.Fprintf(stderr, "runtimeconditions: %v\n", err)
		}
		return exit.Code
	}
	fmt.Fprintf(stderr, "runtimeconditions: %v\n", err)
	return 1
}

// Run executes one profiler command. Arguments exclude the program name.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "--version", "version":
			if len(args) > 1 && args[1] == "--json" {
				return json.NewEncoder(stdout).Encode(struct {
					Name        string   `json:"name"`
					Version     string   `json:"version"`
					CoreSchemas []string `json:"coreSchemas"`
				}{"go-rc-profiler", version, CoreSchemas})
			}
			fmt.Fprintf(stdout, "go-rc-profiler %s (%s)\n", version, commit)
			return nil
		case "generate":
			return runGenerate(args[1:], stdout, stderr)
		case "validate-extension":
			return runValidateExtension(ctx, args[1:], stderr)
		case "validate-extensions":
			return runValidateExtensions(ctx, args[1:], stderr)
		}
		if !strings.HasPrefix(args[0], "-") {
			return fmt.Errorf("unknown command %q", args[0])
		}
	}
	return runGenerate(args, stdout, stderr)
}

func parse(flags *flag.FlagSet, args []string, stderr io.Writer) (bool, error) {
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return true, nil
		}
		return false, &ExitError{Code: 2, Err: err, Reported: true}
	}
	return false, nil
}

func runGenerate(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("generate", flag.ContinueOnError)
	dir := flags.String("dir", ".", "directory containing Go source declarations")
	name := flags.String("name", "", "profile metadata.name")
	workloadURI := flags.String("workload-uri", "", "workload.uri")
	workloadVersion := flags.String("workload-version", "dev", "workload.version")
	out := flags.String("out", "", "output file path; defaults to stdout")
	if help, err := parse(flags, args, stderr); help || err != nil {
		return err
	}

	absDir, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}

	profileName := *name
	if profileName == "" {
		profileName = filepath.Base(absDir)
	}

	uri := *workloadURI
	if uri == "" {
		uri = modulePath(absDir)
	}

	profile, err := extractor.ExtractGeneratedDir(absDir, extractor.GeneratedOptions{
		Name:            profileName,
		WorkloadURI:     uri,
		WorkloadVersion: *workloadVersion,
	})
	if err != nil {
		return err
	}

	data, err := extractor.MarshalProfileYAML(profile)
	if err != nil {
		return err
	}

	if *out == "" {
		_, err = stdout.Write(data)
		return err
	}
	return writeProfileAtomically(*out, data)
}

func writeProfileAtomically(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".runtimeconditions-profile-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0o644); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func runValidateExtension(ctx context.Context, args []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("validate-extension", flag.ContinueOnError)
	dir := flags.String("dir", ".", "Go workload module directory")
	packagePath := flags.String("package", "", "import path of a generated extension binding package")
	if help, err := parse(flags, args, stderr); help || err != nil {
		return err
	}
	if *packagePath == "" {
		return fmt.Errorf("-package is required")
	}
	if _, err := extensioncheck.ValidateImportedGoPackages(ctx, *dir, []string{*packagePath}); err != nil {
		return err
	}
	fmt.Fprintln(stderr, "runtimeconditions: generated binding structure validated")
	return nil
}

func runValidateExtensions(ctx context.Context, args []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("validate-extensions", flag.ContinueOnError)
	dir := flags.String("dir", ".", "Go workload module directory")
	if help, err := parse(flags, args, stderr); help || err != nil {
		return err
	}
	if _, err := extensioncheck.ValidateImportedGoPackages(ctx, *dir, nil); err != nil {
		return err
	}
	fmt.Fprintln(stderr, "runtimeconditions: generated binding structures validated")
	return nil
}

func modulePath(dir string) string {
	for current := dir; ; current = filepath.Dir(current) {
		modPath := filepath.Join(current, "go.mod")
		if module := readModulePath(modPath); module != "" {
			return module
		}
		parent := filepath.Dir(current)
		if parent == current {
			return dir
		}
	}
}

func readModulePath(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}
