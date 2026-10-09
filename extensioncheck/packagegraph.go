package extensioncheck

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

func resolvePackageClosure(ctx context.Context, workloadDir string, listed map[string]goListPackage, modules map[string]goListModule, roots []string) ([]*VerifiedGoPackage, error) {
	byPath := map[string]*VerifiedGoPackage{}
	byID := map[ExtensionReference]*VerifiedGoPackage{}
	claimedIDs := map[ExtensionReference]string{}
	visiting := map[string]bool{}
	var visit func(string) (*VerifiedGoPackage, error)
	visit = func(path string) (*VerifiedGoPackage, error) {
		if visiting[path] {
			return nil, fmt.Errorf("binding package dependency cycle at %s", path)
		}
		if found := byPath[path]; found != nil {
			return found, nil
		}
		pkg, found := listed[path]
		if !found {
			var err error
			module, present := modules[path]
			if !present || module.Version == "" || module.Dir == "" || module.GoMod == "" {
				return nil, fmt.Errorf("%s is not a resolved versioned Go module package", path)
			}
			if module.Replace != nil {
				return nil, fmt.Errorf("%s uses a go.mod replace; generated bindings must resolve to a published module version", path)
			}
			pkg, err = listExactPackage(ctx, path, module)
			if err != nil {
				return nil, err
			}
			listed[path] = pkg
		}
		if pkg.Dir == "" || pkg.Module == nil || pkg.Module.Version == "" || pkg.Module.GoMod == "" {
			return nil, fmt.Errorf("%s is not a resolved versioned Go module package", path)
		}
		if pkg.Module.Replace != nil {
			return nil, fmt.Errorf("%s uses a go.mod replace; generated bindings must resolve to a published module version", path)
		}
		validated, err := validateImportedGoPackage(pkg)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if other := claimedIDs[validated.ExtensionReference()]; other != "" && other != path {
			return nil, fmt.Errorf("extension %s is supplied by both %s and %s", validated.ExtensionID, other, path)
		}
		claimedIDs[validated.ExtensionReference()] = path
		visiting[path] = true
		deps := map[string]*VerifiedGoPackage{}
		for _, entry := range array(validated.Release, "packageDependencies") {
			item := entry.(map[string]any)
			coordinate := stringValue(item, "coordinate")
			if deps[coordinate] != nil {
				return nil, fmt.Errorf("duplicate package dependency %s", coordinate)
			}
			dependency, err := visit(coordinate)
			if err != nil {
				return nil, err
			}
			if err := verifyGoPackageDependency(ctx, workloadDir, validated, item, dependency); err != nil {
				return nil, fmt.Errorf("%s dependency %s: %w", path, coordinate, err)
			}
			deps[coordinate] = dependency
		}
		visiting[path] = false
		byPath[path] = validated
		byID[validated.ExtensionReference()] = validated
		if err := verifyDirectDependencySet(validated, deps); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return validated, nil
	}
	for _, path := range roots {
		if _, err := visit(path); err != nil {
			return nil, err
		}
	}
	for _, pkg := range byPath {
		if err := verifyInstalledExtensionClosure(pkg, byPath, byID); err != nil {
			return nil, fmt.Errorf("%s: %w", pkg.ImportPath, err)
		}
	}
	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	result := make([]*VerifiedGoPackage, 0, len(paths))
	for _, path := range paths {
		result = append(result, byPath[path])
	}
	return result, nil
}

func listExactPackage(ctx context.Context, path string, module goListModule) (goListPackage, error) {
	// Running go list for an unimported package from the workload can demand a
	// go.mod edit under module pruning. Query the exact downloaded module root
	// instead, then attach the selected version from the workload build list.
	command := exec.CommandContext(ctx, "go", "list", "-mod=readonly", "-json", ".")
	command.Dir = module.Dir
	command.Env = append(os.Environ(), "GOWORK=off")
	output, err := command.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return goListPackage{}, fmt.Errorf("go list %s: %s", path, strings.TrimSpace(string(exit.Stderr)))
		}
		return goListPackage{}, err
	}
	var pkg goListPackage
	if err := json.Unmarshal(output, &pkg); err != nil {
		return goListPackage{}, err
	}
	if pkg.ImportPath != path || pkg.Error != nil || len(pkg.DepsErrors) != 0 || pkg.Module == nil ||
		pkg.Module.Path != module.Path {
		return goListPackage{}, fmt.Errorf("go list did not resolve dependency package %s exactly", path)
	}
	localDir, err := os.Stat(pkg.Dir)
	if err != nil {
		return goListPackage{}, err
	}
	resolvedDir, err := os.Stat(module.Dir)
	if err != nil {
		return goListPackage{}, err
	}
	if !os.SameFile(localDir, resolvedDir) {
		return goListPackage{}, fmt.Errorf("dependency package %s is outside its resolved module directory", path)
	}
	localGoMod, err := os.ReadFile(pkg.Module.GoMod)
	if err != nil {
		return goListPackage{}, err
	}
	resolvedGoMod, err := os.ReadFile(module.GoMod)
	if err != nil {
		return goListPackage{}, err
	}
	if !bytes.Equal(localGoMod, resolvedGoMod) {
		return goListPackage{}, fmt.Errorf("dependency package %s go.mod differs from resolved module", path)
	}
	pkg.Module = &module
	return pkg, nil
}

func verifyDirectDependencySet(pkg *VerifiedGoPackage, deps map[string]*VerifiedGoPackage) error {
	direct := map[ExtensionReference]bool{}
	for _, entry := range array(pkg.Model, "extensions") {
		item := entry.(map[string]any)
		if extensionReference(item) != pkg.ExtensionReference() {
			continue
		}
		for _, value := range array(item, "dependencies") {
			direct[extensionReference(value.(map[string]any))] = true
		}
	}
	defined := map[ExtensionReference]bool{}
	for _, value := range array(object(pkg.Extension, "spec"), "dependencies") {
		defined[extensionReference(value.(map[string]any))] = true
	}
	if len(direct) != len(defined) {
		return fmt.Errorf("normalized model root dependencies differ from packaged extension definition")
	}
	for id := range direct {
		if !defined[id] {
			return fmt.Errorf("normalized model root dependency %s is absent from packaged extension definition", id)
		}
	}
	if len(direct) != len(deps) {
		return fmt.Errorf("packageDependencies do not match root extension direct dependencies")
	}
	for _, dep := range deps {
		if !direct[dep.ExtensionReference()] {
			return fmt.Errorf("package dependency %s does not match a direct extension dependency", dep.ImportPath)
		}
	}
	return nil
}

func verifyInstalledExtensionClosure(pkg *VerifiedGoPackage, byPath map[string]*VerifiedGoPackage, byID map[ExtensionReference]*VerifiedGoPackage) error {
	reachable := map[ExtensionReference]bool{}
	var visit func(*VerifiedGoPackage) error
	visit = func(current *VerifiedGoPackage) error {
		if reachable[current.ExtensionReference()] {
			return nil
		}
		reachable[current.ExtensionReference()] = true
		for _, entry := range array(current.Release, "packageDependencies") {
			coordinate := stringValue(entry.(map[string]any), "coordinate")
			dependency := byPath[coordinate]
			if dependency == nil {
				return fmt.Errorf("dependency package %s is not installed", coordinate)
			}
			if err := visit(dependency); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(pkg); err != nil {
		return err
	}
	modelExtensions := map[ExtensionReference]map[string]any{}
	for _, entry := range array(pkg.Model, "extensions") {
		item := entry.(map[string]any)
		modelExtensions[extensionReference(item)] = item
	}
	locked := map[ExtensionReference]bool{}
	for _, entry := range array(object(pkg.Release, "dependencyLock"), "extensions") {
		item := entry.(map[string]any)
		id := extensionReference(item)
		installed := byID[id]
		if installed == nil || !reachable[id] {
			return fmt.Errorf("dependency extension %s is absent from Go-resolved binding packages", id)
		}
		if locked[id] || modelExtensions[id] == nil {
			return fmt.Errorf("duplicate or unknown dependency lock extension %s", id)
		}
		locked[id] = true
		data, err := os.ReadFile(filepath.Join(installed.Dir, extensionFile))
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if stringValue(item, "sourceSha256") != hex.EncodeToString(sum[:]) || stringValue(item, "semanticSha256") != stringValue(object(installed.Model, "rootExtension"), "semanticSha256") {
			return fmt.Errorf("dependency extension %s source or semantic digest differs from installed package", id)
		}
	}
	if len(locked) != len(modelExtensions) || len(locked) != len(reachable) {
		return fmt.Errorf("dependency lock does not cover exact model closure")
	}
	return nil
}

func verifyGoPackageDependency(ctx context.Context, workloadDir string, parent *VerifiedGoPackage, entry map[string]any, dep *VerifiedGoPackage) error {
	if extensionReference(object(entry, "extension")) != dep.ExtensionReference() || stringValue(entry, "coordinate") != dep.ImportPath || stringValue(entry, "name") != dep.name {
		return fmt.Errorf("package identity differs from installed package")
	}
	tested := stringValue(entry, "testedVersion")
	if tested != dep.Version {
		return fmt.Errorf("testedVersion %s differs from resolved module version %s", tested, dep.Version)
	}
	rangeInfo := object(entry, "compatibleVersionRange")
	min, max := stringValue(rangeInfo, "minimumInclusive"), stringValue(rangeInfo, "nextBreakingExclusive")
	if !semver.IsValid(tested) || !semver.IsValid(min) || !semver.IsValid(max) || semver.Compare(tested, min) < 0 || semver.Compare(tested, max) >= 0 {
		return fmt.Errorf("testedVersion is outside compatibleVersionRange")
	}
	goMod, err := os.ReadFile(parent.module.GoMod)
	if err != nil {
		return err
	}
	parsed, err := modfile.Parse(parent.module.GoMod, goMod, nil)
	if err != nil {
		return err
	}
	var required string
	for _, requirement := range parsed.Require {
		if requirement.Mod.Path == dep.module.Path {
			required = requirement.Mod.Version
		}
	}
	if required == "" || !semver.IsValid(required) || semver.Compare(required, min) < 0 || semver.Compare(required, max) >= 0 || semver.Compare(required, tested) > 0 {
		return fmt.Errorf("go.mod requirement for %s is absent or outside compatibleVersionRange", dep.module.Path)
	}
	command := exec.CommandContext(ctx, "go", "mod", "download", "-json", dep.module.Path+"@"+tested)
	command.Dir = workloadDir
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("go mod download %s@%s: %w", dep.module.Path, tested, err)
	}
	var artifact struct {
		Path, Version, Zip, Error string
	}
	if err := json.Unmarshal(output, &artifact); err != nil {
		return err
	}
	if artifact.Error != "" || artifact.Path != dep.module.Path || artifact.Version != tested || artifact.Zip == "" {
		return fmt.Errorf("Go did not supply the exact dependency module archive: %s", artifact.Error)
	}
	file, err := os.Open(artifact.Zip)
	if err != nil {
		return err
	}
	defer file.Close()
	checksum := sha256.New()
	if _, err := io.Copy(checksum, file); err != nil {
		return err
	}
	if stringValue(object(entry, "artifact"), "kind") != "go-module-zip" || hex.EncodeToString(checksum.Sum(nil)) != stringValue(object(entry, "artifact"), "sha256") {
		return fmt.Errorf("dependency Go module archive SHA-256 mismatch")
	}
	return nil
}
