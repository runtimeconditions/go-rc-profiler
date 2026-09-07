package binding

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/gosource"
	"gopkg.in/yaml.v3"
)

// Discover walks the configured extension catalogs and returns every Go binding
// they publish. Manifests for other languages are skipped rather than rejected,
// since one catalog serves every language.
func Discover(roots []string) ([]*Binding, error) {
	var bindings []*Binding
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		err = filepath.WalkDir(absRoot, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if gosource.SkipDir(entry.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Name() != BindingsManifest {
				return nil
			}
			binding, ok, err := readIfGo(path)
			if err != nil {
				return err
			}
			if ok {
				bindings = append(bindings, binding)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return bindings, nil
}

// readIfGo reads a manifest only when it targets Go, reporting false for any
// other language.
func readIfGo(path string) (*Binding, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	var probe struct {
		Metadata struct {
			Language string `yaml:"language"`
		} `yaml:"metadata"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return nil, false, fmt.Errorf("%s: %w", path, err)
	}
	if probe.Metadata.Language != "go" {
		return nil, false, nil
	}
	binding, err := Read(path)
	return binding, err == nil, err
}

// DiscoverPackages finds manifests shipped by the packages the workload
// directly imports, resolved through the workload's own module.
//
// This is what lets a package be understood without configuring a catalog: the
// vocabulary travels with the code that uses it.
func DiscoverPackages(sourceDir string, files []gosource.File) ([]*Binding, error) {
	module, err := gosource.ReadModule(sourceDir)
	if err != nil || module == nil {
		return nil, err
	}

	var bindings []*Binding
	seen := make(map[string]bool)
	for _, importPath := range gosource.DirectImportPaths(files) {
		packageDir := module.ResolveImport(importPath)
		if packageDir == "" {
			continue
		}
		manifestPath, ok, err := FindPackageManifest(packageDir)
		if err != nil {
			return nil, err
		}
		if !ok || seen[manifestPath] {
			continue
		}
		seen[manifestPath] = true
		binding, err := Read(manifestPath)
		if err != nil {
			return nil, err
		}
		bindings = append(bindings, binding)
	}
	return bindings, nil
}

// FindPackageManifest reports the binding manifest a package ships, if any.
func FindPackageManifest(dir string) (string, bool, error) {
	for _, name := range []string{BindingsManifest, PackageManifest} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", false, err
		}
		return path, true, nil
	}
	return "", false, nil
}

// ManifestPaths returns the sorted, deduplicated manifest paths behind a set of
// bindings, for validation against their extension definitions.
func ManifestPaths(bindings []*Binding) []string {
	seen := make(map[string]bool)
	var paths []string
	for _, binding := range bindings {
		if binding.ManifestPath == "" || seen[binding.ManifestPath] {
			continue
		}
		seen[binding.ManifestPath] = true
		paths = append(paths, binding.ManifestPath)
	}
	slices.Sort(paths)
	return paths
}

// CatalogRoots returns the directories to search when resolving extension
// definitions, in priority order.
//
// Alongside the configured roots it adds the directory holding each discovered
// binding's definition and that directory's parent, so a package that ships its
// own extension is resolvable without the caller knowing where it lives.
func CatalogRoots(extensionRoots []string, bindings []*Binding) []string {
	seen := make(map[string]bool)
	var roots []string
	add := func(path string) {
		if strings.TrimSpace(path) == "" {
			return
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			return
		}
		absPath = filepath.Clean(absPath)
		if seen[absPath] {
			return
		}
		seen[absPath] = true
		roots = append(roots, absPath)
	}

	for _, root := range extensionRoots {
		add(root)
	}
	for _, binding := range bindings {
		if binding.ExtensionDefinitionPath == "" {
			continue
		}
		dir := filepath.Dir(binding.ExtensionDefinitionPath)
		add(dir)
		if parent := filepath.Dir(dir); parent != dir {
			add(parent)
		}
	}
	return roots
}
