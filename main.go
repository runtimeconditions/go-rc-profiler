package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/runtimeconditions/go-rc-profiler/extensioncheck"
	"github.com/runtimeconditions/go-rc-profiler/extractor"
	"gopkg.in/yaml.v3"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "validate-extension":
			runValidateExtension(os.Args[2:])
			return
		case "validate-extensions":
			runValidateExtensions(os.Args[2:])
			return
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
	extensionsRoot := flags.String("extensions-root", "", "development root containing extension definitions; package manifests are discovered from resolved Go imports")
	skipValidation := flags.Bool("skip-validation", false, "skip extension manifest and generated profile validation")
	disableGoPackages := flags.Bool("disable-go-packages", false, "disable semantic Go package loading and use syntax-only extraction")
	requireGoPackages := flags.Bool("require-go-packages", false, "fail extraction when semantic Go package loading fails")
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

	profile, err := extractor.ExtractDir(absDir, extractor.Options{
		Name:              profileName,
		WorkloadURI:       uri,
		WorkloadVersion:   *workloadVersion,
		ExtensionRoots:    splitList(*extensionsRoot),
		SkipValidation:    *skipValidation,
		DisableGoPackages: *disableGoPackages,
		RequireGoPackages: *requireGoPackages,
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
		err = os.WriteFile(*out, data, 0o644)
	}
	if err != nil {
		exitErr(err)
	}
}

func runValidateExtension(args []string) {
	flags := flag.NewFlagSet("validate-extension", flag.ExitOnError)
	root := flags.String("root", ".", "extension directory to validate")
	language := flags.String("language", "go", "language binding package to validate")
	catalogRoot := flags.String("catalog-root", "", "additional comma-separated directories containing dependency extension definitions")
	requireLanguagePackage := flags.Bool("require-language-package", false, "require the selected language package and bindings")
	flags.Parse(args)

	err := extensioncheck.ValidateExtension(*root, extensioncheck.Options{
		Language:               *language,
		CatalogRoots:           splitList(*catalogRoot),
		RequireLanguagePackage: *requireLanguagePackage,
	})
	if err != nil {
		exitErr(err)
	}
	fmt.Fprintln(os.Stderr, "runtimeconditions: extension validation passed")
}

func runValidateExtensions(args []string) {
	flags := flag.NewFlagSet("validate-extensions", flag.ExitOnError)
	root := flags.String("root", ".", "directory containing extension definitions to validate")
	language := flags.String("language", "go", "language binding package to validate")
	catalogRoot := flags.String("catalog-root", "", "additional comma-separated directories containing dependency extension definitions")
	requireLanguagePackage := flags.Bool("require-language-package", false, "require every extension to provide the selected language package and bindings")
	flags.Parse(args)

	err := extensioncheck.ValidateExtensions(*root, extensioncheck.Options{
		Language:               *language,
		CatalogRoots:           splitList(*catalogRoot),
		RequireLanguagePackage: *requireLanguagePackage,
	})
	if err != nil {
		exitErr(err)
	}
	fmt.Fprintln(os.Stderr, "runtimeconditions: extensions validation passed")
}

func splitList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
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
