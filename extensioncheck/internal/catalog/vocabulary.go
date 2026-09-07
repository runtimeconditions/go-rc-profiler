package catalog

import (
	"fmt"
	"slices"
)

// Vocabulary is the merged vocabulary of an extension and its dependencies.
// Every lookup counts matches rather than returning them, because a resolved
// extension set must define each name exactly once.
type Vocabulary struct {
	nodes []*Node
}

// NewVocabulary returns the vocabulary contributed by nodes.
func NewVocabulary(nodes []*Node) Vocabulary {
	return Vocabulary{nodes: nodes}
}

// Nodes returns the nodes contributing to the vocabulary.
func (v Vocabulary) Nodes() []*Node {
	return v.nodes
}

// Resolve returns the vocabulary of id together with its transitive
// dependencies, ordered dependencies first.
func (c *Catalog) Resolve(id string) Vocabulary {
	seen := make(map[string]bool)
	var nodes []*Node
	var visit func(string)
	visit = func(current string) {
		if seen[current] {
			return
		}
		seen[current] = true
		node := c.Nodes[current]
		if node == nil {
			return
		}
		for _, dependency := range node.Definition.Spec.Dependencies {
			visit(dependency)
		}
		nodes = append(nodes, node)
	}
	visit(id)
	return Vocabulary{nodes: nodes}
}

// KindCount returns how many resolved extensions declare the named kind.
func (v Vocabulary) KindCount(name string) int {
	count := 0
	for _, node := range v.nodes {
		for _, item := range node.Definition.Spec.Kinds {
			if item.Name == name {
				count++
			}
		}
	}
	return count
}

// InterfaceTypeCount returns how many resolved extensions declare the interface
// type on kind.
func (v Vocabulary) InterfaceTypeCount(kind string, name string) int {
	count := 0
	for _, node := range v.nodes {
		for _, item := range node.Definition.Spec.InterfaceTypes {
			if item.TargetKind == kind && item.Name == name {
				count++
			}
		}
	}
	return count
}

// InterfaceFieldCount returns how many resolved extensions declare the named
// field on the given kind and interface type.
func (v Vocabulary) InterfaceFieldCount(kind string, interfaceType string, name string) int {
	count := 0
	for _, node := range v.nodes {
		for _, item := range node.Definition.Spec.InterfaceFields {
			if item.TargetKind == kind && item.TargetType == interfaceType && item.Name == name {
				count++
			}
		}
	}
	return count
}

// ConditionFieldCount returns how many resolved extensions declare the named
// condition field for the given kind and interface type.
func (v Vocabulary) ConditionFieldCount(kind string, interfaceType string, name string) int {
	count := 0
	for _, node := range v.nodes {
		for _, item := range node.Definition.Spec.ConditionFields {
			if item.Name == name && conditionFieldApplies(item, kind, interfaceType) {
				count++
			}
		}
	}
	return count
}

// FieldValueCount returns how many resolved extensions permit value for field on
// the given kind and interface type.
func (v Vocabulary) FieldValueCount(field string, kind string, interfaceType string, value string) int {
	count := 0
	for _, node := range v.nodes {
		for _, item := range node.Definition.Spec.FieldValues {
			if item.Field == field && item.TargetKind == kind && item.TargetType == interfaceType && slices.Contains(item.Values, value) {
				count++
			}
		}
	}
	return count
}

// FieldValueDefinitionCount returns how many resolved extensions enumerate
// values for field, regardless of what those values are.
func (v Vocabulary) FieldValueDefinitionCount(field string, kind string, interfaceType string) int {
	count := 0
	for _, node := range v.nodes {
		for _, item := range node.Definition.Spec.FieldValues {
			if item.Field == field && item.TargetKind == kind && item.TargetType == interfaceType {
				count++
			}
		}
	}
	return count
}

// FieldValueValueCount returns how many resolved extensions permit value for any
// field, which is how binding constants are checked.
func (v Vocabulary) FieldValueValueCount(value string) int {
	count := 0
	for _, node := range v.nodes {
		for _, item := range node.Definition.Spec.FieldValues {
			if slices.Contains(item.Values, value) {
				count++
			}
		}
	}
	return count
}

// Counts returns how many times each vocabulary name is declared across the
// resolved set. Anything above one is a conflict.
func (v Vocabulary) Counts() map[string]int {
	counts := make(map[string]int)
	for _, node := range v.nodes {
		for _, item := range node.Definition.Spec.Kinds {
			counts["kind:"+item.Name]++
		}
		for _, item := range node.Definition.Spec.InterfaceTypes {
			counts["interfaceType:"+item.TargetKind+":"+item.Name]++
		}
		for _, item := range node.Definition.Spec.InterfaceFields {
			counts["interfaceField:"+item.TargetKind+":"+item.TargetType+":"+item.Name]++
		}
		for _, item := range node.Definition.Spec.FieldValues {
			counts["fieldValues:"+item.TargetKind+":"+item.TargetType+":"+item.Field]++
		}
	}
	return counts
}

// ConditionFieldConflicts returns the condition fields that two resolved
// extensions both define over overlapping scopes. Condition fields cannot be
// counted by name alone, since two definitions collide only when the kinds and
// interface types they apply to intersect.
func (v Vocabulary) ConditionFieldConflicts() []string {
	var conflicts []string
	definitions := v.conditionFieldDefinitions()
	for i := range definitions {
		for j := i + 1; j < len(definitions); j++ {
			left := definitions[i]
			right := definitions[j]
			if left.field.Name == right.field.Name && conditionFieldScopesOverlap(left.field, right.field) {
				conflicts = append(conflicts, fmt.Sprintf("conditionField:%s between %s and %s", left.field.Name, left.node.ID, right.node.ID))
			}
		}
	}
	return conflicts
}

type conditionFieldDefinition struct {
	node  *Node
	field ConditionField
}

func (v Vocabulary) conditionFieldDefinitions() []conditionFieldDefinition {
	var definitions []conditionFieldDefinition
	for _, node := range v.nodes {
		for _, field := range node.Definition.Spec.ConditionFields {
			definitions = append(definitions, conditionFieldDefinition{
				node:  node,
				field: field,
			})
		}
	}
	return definitions
}

func conditionFieldScopesOverlap(left ConditionField, right ConditionField) bool {
	for _, kind := range left.AppliesToKinds {
		if !slices.Contains(right.AppliesToKinds, kind) {
			continue
		}
		return stringSetsOverlapOrEitherEmpty(left.AppliesToInterfaceTypes, right.AppliesToInterfaceTypes)
	}
	return false
}

// stringSetsOverlapOrEitherEmpty treats an empty set as unrestricted, so a field
// scoped to every interface type overlaps one scoped to a specific type.
func stringSetsOverlapOrEitherEmpty(left []string, right []string) bool {
	if len(left) == 0 || len(right) == 0 {
		return true
	}
	for _, item := range left {
		if slices.Contains(right, item) {
			return true
		}
	}
	return false
}

func conditionFieldApplies(field ConditionField, kind string, interfaceType string) bool {
	if !slices.Contains(field.AppliesToKinds, kind) {
		return false
	}
	return len(field.AppliesToInterfaceTypes) == 0 || slices.Contains(field.AppliesToInterfaceTypes, interfaceType)
}
