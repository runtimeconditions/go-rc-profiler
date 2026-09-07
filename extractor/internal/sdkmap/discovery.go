package sdkmap

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/gosource"
	"gopkg.in/yaml.v3"
)

// Discover returns the verified mappings published by modules the workload
// actually imports.
//
// A module that ships mappings but is never imported is skipped entirely, so
// unused metadata can neither contribute conditions nor fail a build.
func Discover(sourceDir string, files []gosource.File, extensionRoots []string) ([]Mapping, error) {
	modules, err := gosource.ListModules(sourceDir)
	if err != nil {
		return nil, err
	}
	imports := gosource.DirectImportPaths(files)

	var mappings []Mapping
	for _, module := range modules {
		if module.SourceDir() == "" || !gosource.ModuleImported(module.Path, imports) {
			continue
		}
		path := filepath.Join(module.SourceDir(), indexPath)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		loaded, err := readIndex(path, module, extensionRoots)
		if err != nil {
			return nil, err
		}
		mappings = append(mappings, loaded...)
	}
	slices.SortFunc(mappings, func(left Mapping, right Mapping) int {
		return strings.Compare(left.Path, right.Path)
	})
	return mappings, nil
}

// readIndex verifies a module's mapping index and loads everything it lists.
//
// The index must name the module and version the build actually resolved,
// pin each mapping by digest, and stay inside the module.
func readIndex(path string, module gosource.ListedModule, extensionRoots []string) ([]Mapping, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var parsed index
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if parsed.APIVersion != "runtimeconditions.io/sdk-mapping/v1alpha1" || parsed.Kind != "RuntimeConditionsSDKMappingIndex" {
		return nil, fmt.Errorf("%s: unsupported SDK mapping index", path)
	}
	if parsed.Metadata.Module != module.Path || parsed.Metadata.ModuleVersion != module.Version || parsed.Metadata.Language != "go" {
		return nil, fmt.Errorf("%s: index identity does not match resolved module %s %s", path, module.Path, module.Version)
	}

	seen := make(map[string]bool)
	var mappings []Mapping
	for _, item := range parsed.Mappings {
		if item.Name == "" || item.Path == "" || item.SHA256 == "" || seen[item.Name] {
			return nil, fmt.Errorf("%s: mapping entries require unique names, paths, and SHA-256 digests", path)
		}
		seen[item.Name] = true

		mappingPath := filepath.Clean(filepath.Join(module.SourceDir(), filepath.FromSlash(item.Path)))
		if !pathWithin(module.SourceDir(), mappingPath) {
			return nil, fmt.Errorf("%s: mapping path %q escapes module root", path, item.Path)
		}
		mappingData, err := os.ReadFile(mappingPath)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(mappingData)
		if hex.EncodeToString(digest[:]) != item.SHA256 {
			return nil, fmt.Errorf("%s: mapping SHA-256 does not match index", mappingPath)
		}

		mapping, err := readMapping(mappingPath, mappingData, module, item.Name, extensionRoots)
		if err != nil {
			return nil, err
		}
		mappings = append(mappings, mapping)
	}
	return mappings, nil
}

// readMapping verifies one mapping document and returns it.
//
// The file digest checked by the index covers the bytes; the semantic digest
// checked here covers the mapping behavior itself, so the go body cannot be
// altered even by someone who can rewrite the index.
func readMapping(path string, data []byte, module gosource.ListedModule, expectedName string, extensionRoots []string) (Mapping, error) {
	var parsed document
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return Mapping{}, fmt.Errorf("%s: %w", path, err)
	}
	if parsed.APIVersion != "runtimeconditions.io/sdk-mapping/v1alpha1" || parsed.Kind != "RuntimeConditionsSDKMapping" {
		return Mapping{}, fmt.Errorf("%s: unsupported SDK mapping document", path)
	}
	if parsed.Metadata.Name != expectedName || parsed.Metadata.Module != module.Path || parsed.Metadata.ModuleVersion != module.Version || parsed.Metadata.Language != "go" {
		return Mapping{}, fmt.Errorf("%s: mapping identity does not match index and resolved module", path)
	}
	if err := verifySemanticDigest(path, data, parsed.Metadata.SemanticSHA256); err != nil {
		return Mapping{}, err
	}

	if parsed.Extension.ID == "" || parsed.Extension.Version == "" || parsed.Extension.SemanticSHA256 == "" {
		return Mapping{}, fmt.Errorf("%s: exact extension id, version, and semantic SHA-256 are required", path)
	}
	if err := verifyExtensionReference(parsed.Extension, extensionRoots); err != nil {
		return Mapping{}, fmt.Errorf("%s: %w", path, err)
	}
	if len(parsed.Go.Calls) == 0 {
		return Mapping{}, fmt.Errorf("%s: go.calls must not be empty", path)
	}
	for _, call := range parsed.Go.Calls {
		if err := validateCall(path, call); err != nil {
			return Mapping{}, err
		}
	}
	return Mapping{Path: path, Extension: parsed.Extension, Calls: parsed.Go.Calls}, nil
}

// verifySemanticDigest re-derives the digest over the canonicalized go body and
// compares it to the one the document declares.
func verifySemanticDigest(path string, data []byte, declared string) error {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return err
	}
	semantic, err := json.Marshal(raw["go"])
	if err != nil {
		return err
	}
	digest := sha256.Sum256(semantic)
	if hex.EncodeToString(digest[:]) != declared {
		return fmt.Errorf("%s: semantic SHA-256 does not match go mapping body", path)
	}
	return nil
}

// validateCall rejects a call that could not do anything useful: no resolvable
// symbol, or neither a condition nor produced state.
func validateCall(path string, call Call) error {
	hasCondition := call.ConditionTemplate.Kind != "" || call.ConditionTemplate.InterfaceType != "" || len(call.ConditionTemplate.Operation) != 0
	symbolIsAmbiguous := (call.Symbol.Function == "") == (call.Symbol.Method == "")
	if call.ID == "" || call.Symbol.Package == "" || symbolIsAmbiguous || (!hasCondition && call.Produces == nil) {
		return fmt.Errorf("%s: call %q has an incomplete symbol or condition template", path, call.ID)
	}
	if hasCondition && (call.ConditionTemplate.Kind == "" || call.ConditionTemplate.InterfaceType == "" || len(call.ConditionTemplate.Operation) == 0) {
		return fmt.Errorf("%s: call %q has a partial condition template", path, call.ID)
	}
	if call.Produces != nil && call.Produces.DependencyIdentity != "" && call.Produces.DependencyIdentity != "new" && call.Produces.DependencyIdentity != "inherit" {
		return fmt.Errorf("%s: call %q has unsupported produces.dependencyIdentity %q", path, call.ID, call.Produces.DependencyIdentity)
	}
	return nil
}

// verifyExtensionReference requires that the exact extension release named by a
// mapping be installed.
//
// Finding the extension at a different version or digest is an error rather than
// a miss: the mapping's vocabulary was authored against one release, and a
// silently different one would produce a profile that does not validate.
func verifyExtensionReference(reference ExtensionReference, roots []string) error {
	for _, root := range roots {
		var matched bool
		var mismatch error
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || (filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml") {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			var probe struct {
				Kind     string             `yaml:"kind"`
				Metadata ExtensionReference `yaml:"metadata"`
			}
			if err := yaml.Unmarshal(data, &probe); err != nil {
				return err
			}
			if probe.Kind == "RuntimeConditionsExtensionDefinition" && probe.Metadata.ID == reference.ID {
				matched = true
				if probe.Metadata.Version != reference.Version || probe.Metadata.SemanticSHA256 != reference.SemanticSHA256 {
					mismatch = fmt.Errorf("extension %s version or semantic SHA-256 does not match installed definition", reference.ID)
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		if matched {
			return mismatch
		}
	}
	return fmt.Errorf("extension %s is not available in the configured extension roots", reference.ID)
}

// pathWithin reports whether path stays inside root, so an index cannot point at
// a mapping outside the module it describes.
func pathWithin(root string, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
