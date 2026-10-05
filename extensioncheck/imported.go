package extensioncheck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/runtimeconditions/go-rc-profiler/extensionidentity"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	bindingsFile  = "runtimeconditions.bindings.yaml"
	modelFile     = "runtimeconditions.binding-model.yaml"
	extensionFile = "runtimeconditions.extension.yaml"
	releaseFile   = "runtimeconditions.binding-release.yaml"
)

// ImportedGoPackage is a generated declaration package found in a workload's
// native Go dependency graph. Dir is the exact directory reported by go list.
type ImportedGoPackage struct {
	ImportPath  string
	Dir         string
	Version     string
	ExtensionID string
	ModelSHA256 string
}

// VerifiedGoPackage retains the exact installed resources after their schema,
// identity, and source checks. Callers must use this verified graph rather than
// reopening an unrelated checkout or catalog during profile generation.
type VerifiedGoPackage struct {
	ImportedGoPackage
	Manifest  map[string]any
	Model     map[string]any
	Extension map[string]any
	Release   map[string]any
	module    *goListModule
	name      string
}

type goListModule struct {
	Path      string
	Version   string
	GoVersion string
	Dir       string
	GoMod     string
	Replace   *goListModule
}

type goListPackage struct {
	ImportPath string
	Name       string
	Dir        string
	Module     *goListModule
	Standard   bool
	Error      *struct{ Err string }
	DepsErrors []struct{ Err string }
}

// ValidateImportedGoPackages resolves the workload's imported packages with
// Go modules, then validates generated packages at their go-list directories.
// If importPaths is empty, every dependency package with a binding manifest is
// validated. Explicit paths must be imports in the workload's dependency graph.
// No extension catalog or sibling source repository is consulted.
func ValidateImportedGoPackages(ctx context.Context, workloadDir string, importPaths []string) ([]ImportedGoPackage, error) {
	packages, err := ResolveImportedGoPackages(ctx, workloadDir, importPaths)
	if err != nil {
		return nil, err
	}
	result := make([]ImportedGoPackage, 0, len(packages))
	for _, pkg := range packages {
		result = append(result, pkg.ImportedGoPackage)
	}
	return result, nil
}

// ResolveImportedGoPackages validates the requested bindings and their exact
// package dependency closure using only Go-resolved module artifacts.
func ResolveImportedGoPackages(ctx context.Context, workloadDir string, importPaths []string) ([]*VerifiedGoPackage, error) {
	absDir, err := filepath.Abs(workloadDir)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(filepath.Join(absDir, "go.mod")); err != nil || info.IsDir() {
		return nil, fmt.Errorf("%s: workload must have a go.mod: %v", absDir, err)
	}
	for _, args := range [][]string{{"mod", "download", "all"}, {"mod", "verify"}} {
		command := exec.CommandContext(ctx, "go", args...)
		command.Dir = absDir
		output, err := command.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("go %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
		}
	}
	command := exec.CommandContext(ctx, "go", "list", "-mod=readonly", "-deps", "-json", "./...")
	command.Dir = absDir
	output, err := command.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("go list -mod=readonly -deps -json ./...: %s", strings.TrimSpace(string(exit.Stderr)))
		}
		return nil, fmt.Errorf("go list -mod=readonly -deps -json ./...: %w", err)
	}
	listed := map[string]goListPackage{}
	decoder := json.NewDecoder(bytes.NewReader(output))
	for decoder.More() {
		var pkg goListPackage
		if err := decoder.Decode(&pkg); err != nil {
			return nil, fmt.Errorf("decode go list: %w", err)
		}
		if pkg.Error != nil {
			return nil, fmt.Errorf("go list %s: %s", pkg.ImportPath, pkg.Error.Err)
		}
		if len(pkg.DepsErrors) > 0 {
			return nil, fmt.Errorf("go list %s: %s", pkg.ImportPath, pkg.DepsErrors[0].Err)
		}
		listed[pkg.ImportPath] = pkg
	}
	selected := map[string]bool{}
	if len(importPaths) > 0 {
		for _, path := range importPaths {
			pkg, found := listed[path]
			if !found || pkg.Dir == "" {
				return nil, fmt.Errorf("%s is not in the workload's resolved Go dependency graph", path)
			}
			selected[path] = true
		}
	} else {
		for path, pkg := range listed {
			if pkg.Module == nil || pkg.Module.Version == "" || pkg.Dir == "" {
				continue
			}
			_, err := os.Stat(filepath.Join(pkg.Dir, bindingsFile))
			if err == nil {
				selected[path] = true
			} else if !os.IsNotExist(err) {
				return nil, err
			}
		}
	}
	paths := make([]string, 0, len(selected))
	for path := range selected {
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no imported generated Go binding packages found in the workload dependency graph")
	}
	sort.Strings(paths)
	modules, err := listResolvedModules(ctx, absDir)
	if err != nil {
		return nil, err
	}
	return resolvePackageClosure(ctx, absDir, listed, modules, paths)
}

// listResolvedModules includes versioned modules in the workload build list,
// including binding dependencies whose packages are not imported by Go source.
// The module graph, rather than a sibling checkout, is the authority for those
// dependency directories and selected versions.
func listResolvedModules(ctx context.Context, workloadDir string) (map[string]goListModule, error) {
	command := exec.CommandContext(ctx, "go", "list", "-m", "-mod=readonly", "-json", "all")
	command.Dir = workloadDir
	output, err := command.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("go list -m -mod=readonly -json all: %s", strings.TrimSpace(string(exit.Stderr)))
		}
		return nil, fmt.Errorf("go list -m -mod=readonly -json all: %w", err)
	}
	modules := map[string]goListModule{}
	decoder := json.NewDecoder(bytes.NewReader(output))
	for decoder.More() {
		var module goListModule
		if err := decoder.Decode(&module); err != nil {
			return nil, fmt.Errorf("decode go module graph: %w", err)
		}
		if module.Path == "" || modules[module.Path].Path != "" {
			return nil, fmt.Errorf("empty or duplicate module path %q in Go build list", module.Path)
		}
		modules[module.Path] = module
	}
	return modules, nil
}

func validateImportedGoPackage(pkg goListPackage) (*VerifiedGoPackage, error) {
	files := map[string]map[string]any{}
	for _, name := range []string{bindingsFile, modelFile, extensionFile, releaseFile} {
		data, err := os.ReadFile(filepath.Join(pkg.Dir, name))
		if err != nil {
			return nil, fmt.Errorf("required package resource %s: %w", name, err)
		}
		files[name], err = parseDocument(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	manifest, model, extension := files[bindingsFile], files[modelFile], files[extensionFile]
	for _, item := range []struct {
		name  string
		value map[string]any
	}{
		{"runtimeconditions.binding-manifest.schema.yaml", manifest},
		{"runtimeconditions.binding-model.schema.yaml", model},
		{"runtimeconditions.extension-semantic.schema.yaml", extension},
		{"runtimeconditions.binding-release.schema.yaml", files[releaseFile]},
	} {
		if err := validateSchema(item.name, item.value); err != nil {
			return nil, fmt.Errorf("%s does not satisfy %s: %w", item.name, item.name, err)
		}
	}
	packageInfo := object(manifest, "package")
	if stringValue(packageInfo, "language") != "go" || stringValue(packageInfo, "coordinate") != pkg.ImportPath || stringValue(packageInfo, "name") != pkg.Name || stringValue(packageInfo, "version") != pkg.Module.Version {
		return nil, fmt.Errorf("binding manifest package identity does not match go list (%s, %s, %s)", pkg.ImportPath, pkg.Name, pkg.Module.Version)
	}
	moduleGoVersion := pkg.Module.GoVersion
	if dot := strings.LastIndex(moduleGoVersion, "."); dot >= 0 && strings.Count(moduleGoVersion, ".") > 1 {
		moduleGoVersion = moduleGoVersion[:dot]
	}
	if stringValue(packageInfo, "minimumGoVersion") != moduleGoVersion {
		return nil, fmt.Errorf("binding manifest minimumGoVersion does not match resolved module go directive (%s)", pkg.Module.GoVersion)
	}
	modelInfo := object(manifest, "model")
	modelMetadata := object(model, "metadata")
	claimedModelDigest := stringValue(modelMetadata, "semanticSha256")
	if stringValue(modelInfo, "apiVersion") != stringValue(model, "apiVersion") || stringValue(modelInfo, "semanticSha256") != claimedModelDigest {
		return nil, fmt.Errorf("binding manifest model identity does not match normalized model")
	}
	delete(modelMetadata, "semanticSha256")
	actualModelDigest, err := semanticDigest(model)
	if err != nil {
		return nil, err
	}
	modelMetadata["semanticSha256"] = claimedModelDigest
	if actualModelDigest != claimedModelDigest {
		return nil, fmt.Errorf("normalized model semanticSha256 mismatch")
	}
	rootInfo := object(model, "rootExtension")
	manifestExtension := object(manifest, "extension")
	extensionMetadata := object(extension, "metadata")
	rootID := stringValue(rootInfo, "id")
	rootDigest := stringValue(rootInfo, "semanticSha256")
	identity, err := extensionidentity.ParseExtensionIdentity(stringValue(extensionMetadata, "uri"), stringValue(extensionMetadata, "version"))
	if err != nil {
		return nil, fmt.Errorf("extension identity: %w", err)
	}
	for _, entry := range array(model, "extensions") {
		item := entry.(map[string]any)
		parsed, err := extensionidentity.ParseExtensionIdentifier(stringValue(item, "id"))
		if err != nil || parsed.Version != stringValue(item, "version") {
			return nil, fmt.Errorf("model extension identity or version is invalid: %s", stringValue(item, "id"))
		}
		for _, dep := range array(item, "dependencies") {
			if _, err := extensionidentity.ParseExtensionIdentifier(dep.(string)); err != nil {
				return nil, err
			}
		}
	}
	if stringValue(manifestExtension, "id") != rootID || stringValue(manifestExtension, "semanticSha256") != rootDigest || identity.Identifier() != rootID || identity.Version != stringValue(rootInfo, "version") {
		return nil, fmt.Errorf("root extension identity mismatch between package resources")
	}
	schemaBytes, err := bindingSchemas.ReadFile("schema/runtimeconditions.extension-semantic.schema.yaml")
	if err != nil {
		return nil, err
	}
	semanticSchema, err := parseDocument(schemaBytes)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeExtension(extension, semanticSchema, semanticSchema)
	if err != nil {
		return nil, fmt.Errorf("normalize root extension: %w", err)
	}
	actualRootDigest, err := semanticDigest(normalized.(map[string]any))
	if err != nil {
		return nil, err
	}
	if actualRootDigest != rootDigest {
		return nil, fmt.Errorf("root extension semanticSha256 mismatch")
	}
	if err := checkReleaseIdentity(files[releaseFile], manifest, model, extension, pkg); err != nil {
		return nil, err
	}
	if err := validateManifestGraph(manifest, model, pkg.Dir); err != nil {
		return nil, err
	}
	return &VerifiedGoPackage{
		ImportedGoPackage: ImportedGoPackage{ImportPath: pkg.ImportPath, Dir: pkg.Dir, Version: pkg.Module.Version, ExtensionID: rootID, ModelSHA256: claimedModelDigest},
		Manifest:          manifest, Model: model, Extension: extension, Release: files[releaseFile], module: pkg.Module, name: pkg.Name,
	}, nil
}

func object(value map[string]any, key string) map[string]any {
	child, _ := value[key].(map[string]any)
	return child
}
func stringValue(value map[string]any, key string) string {
	result, _ := value[key].(string)
	return result
}

// All model references must point to an identity present in the normalized
// model. Repeated projections may contain the same identity; that is one
// semantic coordinate and pointer, not multiple different targets.
func validateManifestGraph(manifest, model map[string]any, dir string) error {
	identities := map[string]bool{}
	addIdentity := func(value map[string]any) {
		if coordinate := stringValue(value, "coordinate"); coordinate != "" {
			identities[coordinate+"\x00"+stringValue(value, "jsonPointer")] = true
		}
	}
	var gatherProvenance func(any)
	gatherProvenance = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if provenance, ok := v["provenance"].(map[string]any); ok {
				addIdentity(provenance)
			}
			for _, child := range v {
				gatherProvenance(child)
			}
		case []any:
			for _, child := range v {
				gatherProvenance(child)
			}
		}
	}
	for _, group := range object(model, "vocabulary") {
		if entries, ok := group.([]any); ok {
			for _, entry := range entries {
				if item, ok := entry.(map[string]any); ok {
					addIdentity(item)
					gatherProvenance(item)
				}
			}
		}
	}
	for _, entry := range array(model, "scopes") {
		if item, ok := entry.(map[string]any); ok {
			addIdentity(item)
			gatherProvenance(item["projection"])
		}
	}
	for _, entry := range array(model, "schemas") {
		if item, ok := entry.(map[string]any); ok {
			addIdentity(item)
			gatherProvenance(item["projection"])
		}
	}
	declarations := map[string]map[string]any{}
	types := map[string]map[string]any{}
	for _, entry := range array(manifest, "declarations") {
		declaration := entry.(map[string]any)
		if err := checkModelRef(declaration, identities); err != nil {
			return err
		}
		coordinate := stringValue(object(declaration, "modelRef"), "coordinate")
		if declarations[coordinate] != nil {
			return fmt.Errorf("duplicate declaration coordinate %s", coordinate)
		}
		declarations[coordinate] = declaration
	}
	for _, entry := range array(manifest, "types") {
		item := entry.(map[string]any)
		if err := checkModelRef(item, identities); err != nil {
			return err
		}
		name := stringValue(item, "nativeName")
		if types[name] != nil {
			return fmt.Errorf("duplicate native type %s", name)
		}
		types[name] = item
	}
	for _, entry := range array(manifest, "importedMarkerContracts") {
		contract := entry.(map[string]any)
		if err := checkModelRef(contract, identities); err != nil {
			return err
		}
		coordinate := stringValue(object(contract, "modelRef"), "coordinate")
		if declarations[coordinate] != nil {
			return fmt.Errorf("duplicate declaration coordinate %s", coordinate)
		}
		declarations[coordinate] = contract
	}
	for _, entry := range array(manifest, "rootBindings") {
		binding := entry.(map[string]any)
		if err := checkModelRef(binding, identities); err != nil {
			return err
		}
		if declarations[stringValue(binding, "declarationCoordinate")] == nil {
			return fmt.Errorf("root binding references unknown declaration %q", binding["declarationCoordinate"])
		}
		if err := checkNativeRef(object(binding, "value"), types); err != nil {
			return err
		}
	}
	for _, item := range types {
		for _, edge := range []string{"fields", "variants", "members"} {
			for _, entry := range array(item, edge) {
				child := entry.(map[string]any)
				if err := checkModelRef(child, identities); err != nil {
					return err
				}
				if edge != "members" {
					if err := checkNativeRef(object(child, "value"), types); err != nil {
						return err
					}
				}
			}
		}
		if element, ok := item["element"].(map[string]any); ok {
			if err := checkModelRef(element, identities); err != nil {
				return err
			}
			if err := checkNativeRef(object(element, "value"), types); err != nil {
				return err
			}
		}
		for _, entry := range array(item, "implements") {
			implementation := entry.(map[string]any)
			if declarations[stringValue(implementation, "declarationCoordinate")] == nil {
				return fmt.Errorf("type %s implements unknown declaration", item["nativeName"])
			}
		}
	}
	return checkGoSymbols(manifest, dir)
}

func array(value map[string]any, key string) []any { result, _ := value[key].([]any); return result }

func checkModelRef(item map[string]any, identities map[string]bool) error {
	ref := object(item, "modelRef")
	key := stringValue(ref, "coordinate") + "\x00" + stringValue(ref, "jsonPointer")
	if !identities[key] {
		return fmt.Errorf("modelRef %q %q does not resolve in normalized model", ref["coordinate"], ref["jsonPointer"])
	}
	return nil
}

func checkNativeRef(ref map[string]any, types map[string]map[string]any) error {
	if name := stringValue(ref, "type"); name != "" && types[name] == nil {
		return fmt.Errorf("native type %s does not resolve in manifest", name)
	}
	return nil
}

func checkGoSymbols(manifest map[string]any, dir string) error {
	parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, "bindings.go"), nil, parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("bindings.go: %w", err)
	}
	functions, interfaces, namedTypes := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, declaration := range parsed.Decls {
		switch d := declaration.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				functions[d.Name.Name] = true
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if t, ok := spec.(*ast.TypeSpec); ok {
					namedTypes[t.Name.Name] = true
					if _, ok := t.Type.(*ast.InterfaceType); ok {
						interfaces[t.Name.Name] = true
					}
				}
			}
		}
	}
	for _, entry := range array(manifest, "declarations") {
		item := entry.(map[string]any)
		if !functions[stringValue(item, "function")] {
			return fmt.Errorf("declaration function %s missing from bindings.go", item["function"])
		}
		if !interfaces[stringValue(item, "markerInterface")] {
			return fmt.Errorf("marker interface %s missing from bindings.go", item["markerInterface"])
		}
	}
	for _, entry := range array(manifest, "types") {
		item := entry.(map[string]any)
		if !namedTypes[stringValue(item, "nativeName")] {
			return fmt.Errorf("native type %s missing from bindings.go", item["nativeName"])
		}
	}
	return nil
}
