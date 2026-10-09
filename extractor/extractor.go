// Package extractor generates a Runtime Conditions Profile from Go source.
//
// The legacy binding path reads declarations a workload makes deliberately
// through an extension's Go API. SDK mappings may additionally infer conditions
// from ordinary calls when explicitly enabled. Generated structural bindings
// use a separate, fail-closed entry point.
//
// Nothing in the target workload is executed. The Go toolchain is used to
// type-check it, which is what makes a call attributable to the module that
// declares it.
package extractor

import (
	"fmt"
	"go/token"
	"path/filepath"
	"slices"

	"github.com/runtimeconditions/go-rc-profiler/extensioncheck"
	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/binding"
	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/gosource"
	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/sdkmap"
	"gopkg.in/yaml.v3"
)

// Options configures source extraction and the generated profile metadata.
type Options struct {
	Name            string
	WorkloadURI     string
	WorkloadVersion string

	// EnableSDKMappings opts into inferred conditions from installed SDK
	// mapping metadata. It requires semantic Go package loading.
	EnableSDKMappings bool

	// ExtensionRoots are the catalogs searched for extension definitions and
	// their language bindings.
	ExtensionRoots []string

	// SkipValidation emits the profile without checking it against the
	// vocabulary it claims to use.
	SkipValidation bool

	// DisableGoPackages forces the syntax-only path. Declarations still
	// resolve, but SDK mappings cannot be applied without type information.
	DisableGoPackages bool

	// RequireGoPackages turns a failed type-check into an error instead of
	// silently falling back to syntax alone and emitting a narrower profile.
	RequireGoPackages bool
}

// ExtractDir reads the earlier handwritten Go declaration and SDK mapping
// contracts. It remains available for existing integrations; generated
// structural bindings use ExtractGeneratedDir instead.
func ExtractDir(dir string, opts Options) (*RuntimeConditionsProfile, error) {
	if opts.EnableSDKMappings {
		if opts.DisableGoPackages {
			return nil, fmt.Errorf("SDK mapping extraction requires semantic Go package loading")
		}
		opts.RequireGoPackages = true
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	fset, files, scope, err := loadWorkload(absDir, opts)
	if err != nil {
		return nil, err
	}

	bindings, mappings, err := discoverVocabulary(absDir, files, opts)
	if err != nil {
		return nil, err
	}
	catalogRoots := binding.CatalogRoots(opts.ExtensionRoots, bindings)
	if !opts.SkipValidation {
		if err := extensioncheck.ValidateBindingManifests(binding.ManifestPaths(bindings), extensioncheck.Options{
			Language:     "go",
			CatalogRoots: catalogRoots,
		}); err != nil {
			return nil, err
		}
	}

	for _, file := range files {
		scope.Collect(file.Syntax)
	}

	// SDK conditions come first so a profile reads dependency-inward: what the
	// workload was observed doing, then what it declares about itself.
	var sdkConditions []Condition
	var sdkExtensions []ExtensionReference
	if opts.EnableSDKMappings {
		sdkConditions, sdkExtensions, err = sdkmap.ExtractConditions(files, scope.Semantic, mappings)
		if err != nil {
			return nil, err
		}
	}
	declaredConditions, declaredExtensions, err := binding.ExtractConditions(fset, scope, files, bindings)
	if err != nil {
		return nil, err
	}

	result := &RuntimeConditionsProfile{
		APIVersion: "runtimeconditions.io/v1alpha1",
		Kind:       "RuntimeConditionsProfile",
		Metadata:   Metadata{Name: opts.Name},
		Workload: Workload{
			URI:     opts.WorkloadURI,
			Version: opts.WorkloadVersion,
		},
		Extensions: sortedExtensions(sdkExtensions, declaredExtensions),
		Conditions: append(sdkConditions, declaredConditions...),
	}
	if opts.SkipValidation {
		return result, nil
	}
	if err := validate(result, catalogRoots); err != nil {
		return nil, err
	}
	return result, nil
}

// loadWorkload parses the workload and type-checks it when permitted.
//
// Parsing always happens, so a workload that cannot be type-checked still
// yields whatever it declares outright. Callers that need SDK mappings applied
// should set RequireGoPackages, since those need type information and would
// otherwise be skipped without explanation.
func loadWorkload(absDir string, opts Options) (*token.FileSet, []gosource.File, *gosource.PackageScope, error) {
	fset := token.NewFileSet()
	files, err := gosource.ParseDir(fset, absDir)
	if err != nil {
		return nil, nil, nil, err
	}

	var semantic *gosource.Semantic
	if !opts.DisableGoPackages {
		loadedFset, loadedFiles, loaded, err := gosource.Load(absDir)
		switch {
		case err != nil:
			if opts.RequireGoPackages {
				return nil, nil, nil, err
			}
		case len(loadedFiles) > 0:
			fset, files, semantic = loadedFset, loadedFiles, loaded
		}
	}
	return fset, files, gosource.NewPackageScope(semantic), nil
}

// discoverVocabulary collects the metadata that tells the profiler what the
// workload's dependencies mean: bindings from the configured catalogs and from
// the packages it imports, plus SDK mappings from the modules it resolves.
func discoverVocabulary(absDir string, files []gosource.File, opts Options) ([]*binding.Binding, []sdkmap.Mapping, error) {
	packageBindings, err := binding.DiscoverPackages(absDir, files)
	if err != nil {
		return nil, nil, err
	}
	bindings, err := binding.Discover(opts.ExtensionRoots)
	if err != nil {
		return nil, nil, err
	}
	bindings = append(bindings, packageBindings...)

	var mappings []sdkmap.Mapping
	if opts.EnableSDKMappings {
		mappings, err = sdkmap.Discover(absDir, files, opts.ExtensionRoots)
		if err != nil {
			return nil, nil, err
		}
	}
	return bindings, mappings, nil
}

// validate resolves the profile's extension list to its full dependency closure
// and then checks the document against the resolved vocabulary.
//
// The closure is written back because a profile has to name every extension it
// relies on, including those reached indirectly.
func validate(result *RuntimeConditionsProfile, catalogRoots []string) error {
	closure, err := extensioncheck.ResolveExtensionClosure(result.Extensions, extensioncheck.ProfileOptions{
		CatalogRoots: catalogRoots,
	})
	if err != nil {
		return err
	}
	result.Extensions = closure

	data, err := yaml.Marshal(result)
	if err != nil {
		return err
	}
	return extensioncheck.ValidateProfileYAML(data, extensioncheck.ProfileOptions{
		CatalogRoots: catalogRoots,
	})
}

// sortedExtensions returns the deduplicated union of the extension IDs each
// extraction path reported, in a stable order.
func sortedExtensions(lists ...[]ExtensionReference) []ExtensionReference {
	seen := make(map[ExtensionReference]bool)
	result := make([]ExtensionReference, 0)
	for _, list := range lists {
		for _, id := range list {
			if seen[id] {
				continue
			}
			seen[id] = true
			result = append(result, id)
		}
	}
	slices.SortFunc(result, ExtensionReference.Compare)
	return result
}
