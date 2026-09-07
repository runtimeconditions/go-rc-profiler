// Package extensioncheck validates Runtime Conditions extension definitions,
// the Go binding manifests that accompany them, and the profiles they produce.
package extensioncheck

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/runtimeconditions/go-rc-profiler/extensioncheck/internal/catalog"
	"github.com/runtimeconditions/go-rc-profiler/extensioncheck/internal/check"
	"github.com/runtimeconditions/go-rc-profiler/extensioncheck/internal/diag"
)

// Options configures static extension validation.
type Options struct {
	Language               string
	CatalogRoots           []string
	RequireLanguagePackage bool
}

// ProfileOptions configures validation for generated Runtime Conditions
// Profiles.
type ProfileOptions struct {
	CatalogRoots []string
}

// ValidateExtension validates the extension definition under root, plus any
// dependency extensions required to check its binding and declaration package.
func ValidateExtension(root string, opts Options) error {
	return validate(root, opts, true)
}

// ValidateExtensions validates every extension definition found under root.
func ValidateExtensions(root string, opts Options) error {
	return validate(root, opts, false)
}

// ValidateBindingManifest validates a Go binding or package manifest against
// its referenced extension definition and resolved dependency graph.
func ValidateBindingManifest(path string, opts Options) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	binding, err := catalog.ReadBindingDocument(absPath)
	if err != nil {
		return err
	}
	if opts.Language == "" {
		opts.Language = binding.Metadata.Language
		if opts.Language == "" {
			opts.Language = "go"
		}
	}
	if err := checkLanguage(opts.Language); err != nil {
		return err
	}
	definitionPath, err := bindingDefinitionPath(absPath, binding)
	if err != nil {
		return err
	}
	definition, ok, err := catalog.ReadExtensionDefinition(definitionPath)
	if err != nil {
		return err
	}
	if !ok {
		return diag.ValidationErrors{definitionPath + ": kind must be RuntimeConditionsExtensionDefinition"}
	}

	collector := &diag.Collector{}
	loaded, err := catalog.Load(catalogRoots(filepath.Dir(definitionPath), opts.CatalogRoots, true), opts.Language, collector)
	if err != nil {
		return err
	}
	id := catalog.DefinitionID(definition)
	node := loaded.Nodes[id]
	if node == nil {
		collector.Addf(definitionPath, "extension definition %s was not loaded from catalog", id)
		return collector.Err()
	}
	// The manifest under test replaces whatever the catalog discovered for this
	// extension, so that a manifest outside the conventional layout is checked.
	node.BindingPath = absPath
	node.Binding = binding
	node.BindingDir = filepath.Dir(absPath)
	if opts.Language == "go" {
		node.GoDir = node.BindingDir
	}
	check.New(loaded, checkOptions(opts), collector).ValidateNode(id)
	return collector.Err()
}

// ValidateBindingManifests validates multiple binding manifests and reports all
// manifest failures together.
func ValidateBindingManifests(paths []string, opts Options) error {
	var errs []string
	seen := make(map[string]bool)
	for _, path := range paths {
		absPath, err := filepath.Abs(path)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if seen[absPath] {
			continue
		}
		seen[absPath] = true
		if err := ValidateBindingManifest(absPath, opts); err != nil {
			errs = diag.Append(errs, err)
		}
	}
	if len(errs) > 0 {
		return diag.ValidationErrors(errs)
	}
	return nil
}

// ResolveExtensionClosure returns ids plus all transitive extension
// dependencies, sorted for stable profile output.
func ResolveExtensionClosure(ids []string, opts ProfileOptions) ([]string, error) {
	collector := &diag.Collector{}
	loaded, err := catalog.Load(opts.CatalogRoots, "", collector)
	if err != nil {
		return nil, err
	}
	checker := check.New(loaded, check.Options{}, collector)
	seen := make(map[string]bool)
	var visit func(string)
	visit = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		node := loaded.Nodes[id]
		if node == nil {
			collector.Addf(id, "missing extension definition for %s", id)
			return
		}
		checker.ValidateNode(id)
		for _, dependency := range node.Definition.Spec.Dependencies {
			visit(dependency)
		}
	}
	for _, id := range ids {
		visit(id)
	}
	if err := collector.Err(); err != nil {
		return nil, err
	}
	result := make([]string, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}
	slices.Sort(result)
	return result, nil
}

// ValidateProfileYAML validates a Runtime Conditions Profile against the
// resolved vocabulary provided by its declared extensions.
func ValidateProfileYAML(data []byte, opts ProfileOptions) error {
	var profile catalog.ProfileDocument
	if err := yaml.Unmarshal(data, &profile); err != nil {
		return err
	}
	// Conditions are decoded a second time as free-form values, because the
	// extension JSON Schemas validate the document as written rather than the
	// subset the typed model captures.
	var raw struct {
		Conditions []any `yaml:"conditions"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return err
	}

	collector := &diag.Collector{}
	loaded, err := catalog.Load(opts.CatalogRoots, "", collector)
	if err != nil {
		return err
	}
	report := &diag.Report{}
	check.NewProfileChecker(check.New(loaded, check.Options{}, collector), report).Validate(profile, raw.Conditions)
	if err := collector.Err(); err != nil {
		report.AppendError(err)
	}
	return report.Err()
}

func validate(root string, opts Options, targetOnly bool) error {
	if opts.Language == "" {
		opts.Language = "go"
	}
	if err := checkLanguage(opts.Language); err != nil {
		return err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	// Validating a single extension still needs its siblings, since dependencies
	// are resolved from the surrounding catalog.
	includeParentCatalog := false
	if targetOnly {
		includeParentCatalog, err = containsDirectExtensionDefinition(absRoot)
		if err != nil {
			return err
		}
	}

	collector := &diag.Collector{}
	loaded, err := catalog.Load(catalogRoots(absRoot, opts.CatalogRoots, includeParentCatalog), opts.Language, collector)
	if err != nil {
		return err
	}
	targets := loaded.Targets(absRoot, targetOnly)
	if len(targets) == 0 {
		collector.Addf(absRoot, "no RuntimeConditionsExtensionDefinition YAML files found")
		return collector.Err()
	}
	checker := check.New(loaded, checkOptions(opts), collector)
	for _, id := range targets {
		checker.ValidateNode(id)
	}
	return collector.Err()
}

func checkOptions(opts Options) check.Options {
	return check.Options{
		Language:               opts.Language,
		RequireLanguagePackage: opts.RequireLanguagePackage,
	}
}

func checkLanguage(language string) error {
	if language != "go" {
		return diag.ValidationErrors{fmt.Sprintf("unsupported language %s: only go is supported", language)}
	}
	return nil
}

// bindingDefinitionPath resolves the extension definition a manifest refers to,
// falling back to the conventional file name beside the manifest.
func bindingDefinitionPath(manifestPath string, binding *catalog.BindingDocument) (string, error) {
	definition := binding.ExtensionDefinitionPath()
	if definition == "" {
		definition = filepath.Join(filepath.Dir(manifestPath), catalog.DefaultExtensionFile)
	}
	if !filepath.IsAbs(definition) {
		definition = filepath.Join(filepath.Dir(manifestPath), definition)
	}
	return filepath.Abs(definition)
}

// containsDirectExtensionDefinition reports whether root itself holds an
// extension definition, as opposed to being a directory of extensions.
func containsDirectExtensionDefinition(root string) (bool, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if !catalog.IsYAML(path) {
			continue
		}
		_, ok, err := catalog.ReadExtensionDefinition(path)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func catalogRoots(root string, explicit []string, includeParent bool) []string {
	roots := []string{root}
	parent := filepath.Dir(root)
	if includeParent && parent != root {
		roots = append(roots, parent)
	}
	for _, item := range explicit {
		if strings.TrimSpace(item) != "" {
			roots = append(roots, item)
		}
	}
	return roots
}
