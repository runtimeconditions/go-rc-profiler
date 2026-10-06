package extensioncheck

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// checkReleaseIdentity checks the equalities that the release schema cannot
// express across package resources and Go module metadata.
func checkReleaseIdentity(release, manifest, model, extension map[string]any, pkg goListPackage) error {
	releasePackage := object(release, "package")
	manifestPackage := object(manifest, "package")
	for _, key := range []string{"language", "coordinate", "name", "version", "minimumGoVersion"} {
		if stringValue(releasePackage, key) != stringValue(manifestPackage, key) {
			return fmt.Errorf("binding release package.%s does not match binding manifest", key)
		}
	}
	if stringValue(releasePackage, "coordinate") != pkg.ImportPath || stringValue(releasePackage, "version") != pkg.Module.Version {
		return fmt.Errorf("binding release package identity does not match go list")
	}
	for _, key := range []string{"apiVersion", "semanticSha256"} {
		if stringValue(object(release, "model"), key) != stringValue(object(manifest, "model"), key) {
			return fmt.Errorf("binding release model.%s does not match binding manifest", key)
		}
	}
	root := object(release, "rootExtension")
	modelRoot := object(model, "rootExtension")
	for _, key := range []string{"id", "version", "semanticSha256"} {
		if stringValue(root, key) != stringValue(modelRoot, key) {
			return fmt.Errorf("binding release rootExtension.%s does not match normalized model", key)
		}
	}
	if stringValue(root, "id") != stringValue(object(extension, "metadata"), "uri")+":"+stringValue(object(extension, "metadata"), "version") {
		return fmt.Errorf("binding release root extension does not match packaged definition")
	}
	provenance := object(release, "provenance")
	if stringValue(provenance, "mode") == "production" {
		key := stringValue(releasePackage, "packageKey")
		language := stringValue(releasePackage, "language")
		version := stringValue(releasePackage, "version")
		base := "bindings/" + key + "/" + language
		if stringValue(provenance, "sourceDirectory") != base || stringValue(provenance, "targetReleaseTag") != base+"/"+version {
			return fmt.Errorf("binding release production source directory or tag differs from package identity")
		}
	}
	if err := checkDependencyLock(release, model, pkg.Dir); err != nil {
		return err
	}
	return nil
}

func checkDependencyLock(release, model map[string]any, dir string) error {
	modelExtensions := map[string]map[string]any{}
	for _, entry := range array(model, "extensions") {
		extension := entry.(map[string]any)
		id := stringValue(extension, "id")
		if modelExtensions[id] != nil {
			return fmt.Errorf("duplicate normalized model extension %s", id)
		}
		modelExtensions[id] = extension
	}
	locked := map[string]bool{}
	rootID := stringValue(object(model, "rootExtension"), "id")
	for _, entry := range array(object(release, "dependencyLock"), "extensions") {
		item := entry.(map[string]any)
		id := stringValue(item, "id")
		if locked[id] {
			return fmt.Errorf("duplicate binding release dependency lock extension %s", id)
		}
		locked[id] = true
		modelExtension := modelExtensions[id]
		if modelExtension == nil {
			return fmt.Errorf("binding release dependency lock has unknown extension %s", id)
		}
		for _, key := range []string{"version", "semanticSha256"} {
			if stringValue(item, key) != stringValue(modelExtension, key) {
				return fmt.Errorf("binding release dependency lock %s %s differs from normalized model", id, key)
			}
		}
		if !equalStringSets(array(item, "dependencies"), array(modelExtension, "dependencies")) {
			return fmt.Errorf("binding release dependency lock %s dependencies differ from normalized model", id)
		}
		if id == rootID {
			data, err := os.ReadFile(filepath.Join(dir, extensionFile))
			if err != nil {
				return err
			}
			digest := sha256.Sum256(data)
			if stringValue(item, "sourceSha256") != hex.EncodeToString(digest[:]) {
				return fmt.Errorf("binding release root sourceSha256 differs from packaged extension bytes")
			}
		}
	}
	if len(locked) != len(modelExtensions) {
		return fmt.Errorf("binding release dependency lock does not cover normalized model closure")
	}
	return nil
}

func equalStringSets(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	left, right := make([]string, len(a)), make([]string, len(b))
	for i := range a {
		left[i], _ = a[i].(string)
	}
	for i := range b {
		right[i], _ = b[i].(string)
	}
	slices.Sort(left)
	slices.Sort(right)
	return slices.Equal(left, right)
}
