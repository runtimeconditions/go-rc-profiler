package gosource

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Module is the workload's own go.mod, reduced to what import resolution needs:
// the module path, the directory it lives in, and its local replacements.
//
// This deliberately reads go.mod directly instead of shelling out. It runs
// before the toolchain is consulted, and it only has to resolve imports that
// point at directories already on disk.
type Module struct {
	path     string
	dir      string
	replaces map[string]string
}

// ReadModule finds the nearest go.mod at or above sourceDir. It returns a nil
// Module, and no error, when the workload is not inside a module.
func ReadModule(sourceDir string) (*Module, error) {
	for current := sourceDir; ; current = filepath.Dir(current) {
		module, err := parseModFile(filepath.Join(current, "go.mod"))
		if err == nil {
			module.dir = current
			return module, nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
		if parent := filepath.Dir(current); parent == current {
			return nil, nil
		}
	}
}

func parseModFile(path string) (*Module, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	module := &Module{replaces: make(map[string]string)}
	inReplaceBlock := false
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(stripModComment(rawLine))
		switch {
		case line == "":
		case inReplaceBlock:
			if line == ")" {
				inReplaceBlock = false
				continue
			}
			parseReplaceLine(line, module.replaces)
		case strings.HasPrefix(line, "module "):
			module.path = strings.TrimSpace(strings.TrimPrefix(line, "module "))
		case line == "replace (":
			inReplaceBlock = true
		case strings.HasPrefix(line, "replace "):
			parseReplaceLine(strings.TrimSpace(strings.TrimPrefix(line, "replace ")), module.replaces)
		}
	}
	if module.path == "" {
		return nil, fmt.Errorf("%s: module path is required", path)
	}
	return module, nil
}

func stripModComment(line string) string {
	if index := strings.Index(line, "//"); index >= 0 {
		return line[:index]
	}
	return line
}

func parseReplaceLine(line string, replaces map[string]string) {
	fields := strings.Fields(line)
	for i, field := range fields {
		if field != "=>" || i == 0 || i+1 >= len(fields) {
			continue
		}
		replaces[fields[0]] = fields[i+1]
		return
	}
}

// ResolveImport maps an import path to a directory on disk, or returns empty
// when the import resolves outside the module and its local replacements.
//
// The longest matching module path wins, so a replacement for a nested module
// takes precedence over the enclosing one.
func (m *Module) ResolveImport(importPath string) string {
	type candidate struct {
		modulePath string
		dir        string
	}

	candidates := []candidate{{modulePath: m.path, dir: m.dir}}
	for modulePath, replacement := range m.replaces {
		if !isLocalReplacement(replacement) {
			continue
		}
		dir := replacement
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(m.dir, dir)
		}
		candidates = append(candidates, candidate{modulePath: modulePath, dir: filepath.Clean(dir)})
	}

	var best candidate
	for _, item := range candidates {
		if importPath != item.modulePath && !strings.HasPrefix(importPath, item.modulePath+"/") {
			continue
		}
		if len(item.modulePath) > len(best.modulePath) {
			best = item
		}
	}
	if best.modulePath == "" {
		return ""
	}
	suffix := strings.TrimPrefix(strings.TrimPrefix(importPath, best.modulePath), "/")
	return filepath.Join(best.dir, filepath.FromSlash(suffix))
}

// isLocalReplacement reports whether a replace directive points at a directory
// rather than another module version.
func isLocalReplacement(path string) bool {
	return filepath.IsAbs(path) || strings.HasPrefix(path, ".")
}
