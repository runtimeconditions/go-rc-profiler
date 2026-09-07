// Package catalog indexes the extension definitions found beneath a set of
// roots, along with the binding manifests and vocabulary they resolve to.
package catalog

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/runtimeconditions/go-rc-profiler/extensioncheck/internal/diag"
)

// Node is one extension definition together with the language binding that
// accompanies it. Validation fills in the binding fields, which stay empty when
// no language package was requested.
type Node struct {
	ID             string
	Dir            string
	DefinitionPath string
	Definition     ExtensionDefinition
	BindingPath    string
	Binding        *BindingDocument
	BindingDir     string
	GoDir          string
}

// Catalog holds every extension definition discovered beneath a set of roots,
// indexed by extension id.
type Catalog struct {
	Nodes map[string]*Node
}

// Load walks roots and indexes every extension definition it finds. Problems
// with individual documents are recorded on collector rather than returned, so
// that one unreadable file does not hide the rest of the catalog; only a failure
// to walk a root is returned.
//
// Bindings are discovered only for language "go". Any other value, including the
// empty string, leaves every node unbound, which is how profile validation reads
// a catalog for its vocabulary alone.
func Load(roots []string, language string, collector *diag.Collector) (*Catalog, error) {
	catalog := &Catalog{Nodes: make(map[string]*Node)}
	seenRoots := make(map[string]bool)
	seenFiles := make(map[string]bool)
	for _, root := range roots {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		if seenRoots[absRoot] {
			continue
		}
		seenRoots[absRoot] = true
		err = filepath.WalkDir(absRoot, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				switch entry.Name() {
				case ".git", "vendor", "node_modules":
					return filepath.SkipDir
				}
				return nil
			}
			if seenFiles[path] || !IsYAML(path) {
				return nil
			}
			seenFiles[path] = true
			catalog.add(path, language, collector)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return catalog, nil
}

func (c *Catalog) add(path string, language string, collector *diag.Collector) {
	definition, ok, err := ReadExtensionDefinition(path)
	if err != nil {
		collector.Addf(path, "%v", err)
		return
	}
	if !ok {
		return
	}
	id := DefinitionID(definition)
	if id == "" {
		collector.Addf(path, "metadata.id is required")
		return
	}
	if existing := c.Nodes[id]; existing != nil {
		collector.Addf(path, "duplicate extension id %s already defined by %s", id, existing.DefinitionPath)
		return
	}
	node := &Node{
		ID:             id,
		Dir:            filepath.Dir(path),
		DefinitionPath: path,
		Definition:     definition,
	}
	if language == "go" {
		discoverBinding(node, language, collector)
	}
	c.Nodes[id] = node
}

// discoverBinding locates the manifest an extension publishes for language and
// attaches it to node.
func discoverBinding(node *Node, language string, collector *diag.Collector) {
	dir := filepath.Join(node.Dir, language)
	info, err := os.Stat(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			collector.Addf(dir, "%v", err)
		}
		return
	}
	if !info.IsDir() {
		collector.Addf(dir, "expected language package path to be a directory")
		return
	}
	node.BindingDir = dir
	if language == "go" {
		node.GoDir = dir
	}
	manifest, ok, err := FindBindingManifest(dir)
	if err != nil {
		collector.Addf(dir, "%v", err)
		return
	}
	if !ok {
		collector.Addf(dir, "missing %s", GoBindingsManifest)
		return
	}
	binding, err := ReadBindingDocument(manifest)
	if err != nil {
		collector.Addf(manifest, "%v", err)
		return
	}
	node.BindingPath = manifest
	node.Binding = binding
}

// Targets returns the ids to validate: those defined beneath root when
// targetOnly is set, and otherwise every id in the catalog.
func (c *Catalog) Targets(root string, targetOnly bool) []string {
	var ids []string
	for id, node := range c.Nodes {
		if !targetOnly || pathWithin(root, node.DefinitionPath) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

func pathWithin(root string, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}
