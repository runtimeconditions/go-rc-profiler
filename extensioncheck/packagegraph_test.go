package extensioncheck

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstalledExtensionClosureUsesDependencyPackages(t *testing.T) {
	makePackage := func(id, path, source string) *VerifiedGoPackage {
		t.Helper()
		dir := filepath.Join(t.TempDir(), path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, extensionFile), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
		return &VerifiedGoPackage{ImportedGoPackage: ImportedGoPackage{ImportPath: path, Dir: dir, ExtensionID: id}, Model: map[string]any{"rootExtension": map[string]any{"semanticSha256": id}}}
	}
	root := makePackage("root", "root-package", "root source")
	leaf := makePackage("leaf", "leaf-package", "leaf source")
	lock := func(pkg *VerifiedGoPackage, source string) map[string]any {
		sum := sha256.Sum256([]byte(source))
		return map[string]any{"id": pkg.ExtensionID, "sourceSha256": hex.EncodeToString(sum[:]), "semanticSha256": pkg.ExtensionID}
	}
	root.Model["extensions"] = []any{map[string]any{"id": "root"}, map[string]any{"id": "leaf"}}
	root.Release = map[string]any{
		"packageDependencies": []any{map[string]any{"coordinate": leaf.ImportPath}},
		"dependencyLock":      map[string]any{"extensions": []any{lock(root, "root source"), lock(leaf, "leaf source")}},
	}
	byPath := map[string]*VerifiedGoPackage{root.ImportPath: root, leaf.ImportPath: leaf}
	byID := map[string]*VerifiedGoPackage{root.ExtensionID: root, leaf.ExtensionID: leaf}
	if err := verifyInstalledExtensionClosure(root, byPath, byID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(leaf.Dir, extensionFile), []byte("altered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyInstalledExtensionClosure(root, byPath, byID); err == nil || !strings.Contains(err.Error(), "digest differs") {
		t.Fatalf("expected altered dependency bytes to fail, got %v", err)
	}
}
