package gosource

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// ListedModule is one entry from the resolved module graph.
//
// Version is the version the build actually selected, which is what an SDK
// mapping has to agree with before it may be trusted.
type ListedModule struct {
	Path    string
	Version string
	Dir     string
	Replace *ListedModule
}

// SourceDir is the directory holding the module's extracted source, honoring a
// replacement. It is empty when the module has not been downloaded.
func (m ListedModule) SourceDir() string {
	if m.Replace != nil && m.Replace.Dir != "" {
		return m.Replace.Dir
	}
	return m.Dir
}

// ListModules asks the Go toolchain for the module graph as the build resolves
// it. Unlike ReadModule this reflects version selection across the whole graph,
// which is why it costs a subprocess.
func ListModules(sourceDir string) ([]ListedModule, error) {
	command := exec.Command("go", "list", "-m", "-json", "all")
	command.Dir = sourceDir
	output, err := command.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, fmt.Errorf("go list -m failed: %s", strings.TrimSpace(string(exit.Stderr)))
		}
		return nil, fmt.Errorf("go list -m failed: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	var modules []ListedModule
	for {
		var module ListedModule
		err := decoder.Decode(&module)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		modules = append(modules, module)
	}
	return modules, nil
}

// ModuleImported reports whether any of the workload's imports come from the
// module. Metadata installed in a module the workload never imports must not
// participate in extraction.
func ModuleImported(modulePath string, imports []string) bool {
	for _, importPath := range imports {
		if importPath == modulePath || strings.HasPrefix(importPath, modulePath+"/") {
			return true
		}
	}
	return false
}
