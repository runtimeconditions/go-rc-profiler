// Package check validates extension definitions, their binding manifests, and
// generated profiles against the vocabulary a resolved extension set provides.
package check

import (
	"github.com/runtimeconditions/go-rc-profiler/extensioncheck/internal/catalog"
	"github.com/runtimeconditions/go-rc-profiler/extensioncheck/internal/diag"
)

// Options selects the language binding to validate and how strictly to require it.
type Options struct {
	Language               string
	RequireLanguagePackage bool
}

// visit records how far a node has progressed, so that dependencies are
// validated once and cycles are reported rather than followed.
type visit int

const (
	unvisited visit = iota
	inProgress
	done
)

// Checker validates nodes drawn from a catalog, reporting to a shared collector.
type Checker struct {
	catalog   *catalog.Catalog
	opts      Options
	states    map[string]visit
	collector *diag.Collector
}

// New returns a checker over cat that reports to collector.
func New(cat *catalog.Catalog, opts Options, collector *diag.Collector) *Checker {
	return &Checker{
		catalog:   cat,
		opts:      opts,
		states:    make(map[string]visit),
		collector: collector,
	}
}

// ValidateNode validates the extension id and every extension it depends on.
// Dependencies are validated first, so that a definition is only checked against
// vocabulary that has itself been checked.
func (c *Checker) ValidateNode(id string) {
	switch c.states[id] {
	case inProgress:
		c.collector.Addf(id, "extension dependency cycle includes %s", id)
		return
	case done:
		return
	}
	node := c.catalog.Nodes[id]
	if node == nil {
		c.collector.Addf(id, "missing extension definition for dependency %s", id)
		return
	}
	c.states[id] = inProgress
	for _, dependency := range node.Definition.Spec.Dependencies {
		c.ValidateNode(dependency)
	}
	c.validateDefinition(node)
	resolved := c.catalog.Resolve(id)
	c.validateVocabulary(node, resolved)
	c.validateBinding(node, resolved)
	c.validateGoDeclarations(node)
	c.states[id] = done
}
