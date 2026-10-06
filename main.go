package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/runtimeconditions/go-rc-profiler/extensioncheck"
	"github.com/runtimeconditions/go-rc-profiler/extractor"
	"gopkg.in/yaml.v3"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "version":
			fmt.Printf("go-rc-profiler %s (%s)\n", version, commit)
			return
		case "generate":
			runGenerate(os.Args[2:])
			return
		case "validate-extension":
			runValidateExtension(os.Args[2:])
			return
		case "validate-extensions":
			runValidateExtensions(os.Args[2:])
			return
		}
		if !strings.HasPrefix(os.Args[1], "-") {
			exitErr(fmt.Errorf("unknown command %q", os.Args[1]))
		}
	}
	runGenerate(os.Args[1:])
}

func runGenerate(args []string) {
	flags := flag.NewFlagSet("generate", flag.ExitOnError)
	dir := flags.String("dir", ".", "directory containing Go source declarations")
	name := flags.String("name", "", "profile metadata.name")
	workloadURI := flags.String("workload-uri", "", "workload.uri")
	workloadVersion := flags.String("workload-version", "dev", "workload.version")
	out := flags.String("out", "", "output file path; defaults to stdout")
	flags.Parse(args)

	absDir, err := filepath.Abs(*dir)
	if err != nil {
		exitErr(err)
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
		exitErr(err)
	}

	data, err := yaml.Marshal(profile)
	if err != nil {
		exitErr(err)
	}

	if *out == "" {
		_, err = os.Stdout.Write(data)
	} else {
		err = writeProfileAtomically(*out, data)
	}
	if err != nil {
		exitErr(err)
	}
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

func runValidateExtension(args []string) {
	flags := flag.NewFlagSet("validate-extension", flag.ExitOnError)
	dir := flags.String("dir", ".", "Go workload module directory")
	packagePath := flags.String("package", "", "import path of a generated extension binding package")
	flags.Parse(args)
	if *packagePath == "" {
		exitErr(fmt.Errorf("-package is required"))
	}
	_, err := extensioncheck.ValidateImportedGoPackages(context.Background(), *dir, []string{*packagePath})
	if err != nil {
		exitErr(err)
	}
	fmt.Fprintln(os.Stderr, "runtimeconditions: generated binding structure validated")
}

func runValidateExtensions(args []string) {
	flags := flag.NewFlagSet("validate-extensions", flag.ExitOnError)
	dir := flags.String("dir", ".", "Go workload module directory")
	flags.Parse(args)
	_, err := extensioncheck.ValidateImportedGoPackages(context.Background(), *dir, nil)
	if err != nil {
		exitErr(err)
	}
	fmt.Fprintln(os.Stderr, "runtimeconditions: generated binding structures validated")
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

func exitErr(err error) {
	fmt.Fprintf(os.Stderr, "runtimeconditions: %v\n", err)
	os.Exit(1)
}
