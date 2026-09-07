package check

import (
	"fmt"
	"slices"
	"strings"

	"github.com/runtimeconditions/go-rc-profiler/extensioncheck/internal/catalog"
	"github.com/runtimeconditions/go-rc-profiler/extensioncheck/internal/diag"
)

// ProfileChecker validates a generated profile against the vocabulary that its
// declared extensions resolve to.
type ProfileChecker struct {
	checker *Checker
	report  *diag.Report
}

// NewProfileChecker returns a profile checker that resolves vocabulary through
// checker and reports to report.
func NewProfileChecker(checker *Checker, report *diag.Report) *ProfileChecker {
	return &ProfileChecker{checker: checker, report: report}
}

// Validate checks a profile's metadata, its declared extension closure, and each
// of its conditions. rawConditions holds the same conditions as decoded YAML,
// which is what extension JSON Schemas are applied to.
func (p *ProfileChecker) Validate(profile catalog.ProfileDocument, rawConditions []any) {
	if profile.APIVersion != "runtimeconditions.io/v1alpha1" {
		p.report.Addf("apiVersion must be runtimeconditions.io/v1alpha1")
	}
	if profile.Kind != "RuntimeConditionsProfile" {
		p.report.Addf("kind must be RuntimeConditionsProfile")
	}
	if len(profile.Extensions) == 0 && len(profile.Conditions) > 0 {
		p.report.Addf("extensions must declare the extension dependency closure used by conditions")
	}

	declared := make(map[string]bool)
	for _, id := range profile.Extensions {
		if declared[id] {
			p.report.Addf("duplicate extension id %s", id)
			continue
		}
		declared[id] = true
		if p.checker.catalog.Nodes[id] == nil {
			p.report.Addf("missing extension definition for %s", id)
			continue
		}
		p.checker.ValidateNode(id)
	}

	// A profile must name every extension it depends on, not just the ones it
	// uses directly, so that its vocabulary is reproducible from the list alone.
	closure := p.extensionClosure(profile.Extensions)
	for id := range closure {
		if !declared[id] {
			p.report.Addf("extensions missing dependency %s", id)
		}
	}

	resolved := p.vocabulary(closure)
	p.checker.checkResolvedConflicts("profile", resolved)
	for index, condition := range profile.Conditions {
		var raw any
		if index < len(rawConditions) {
			raw = rawConditions[index]
		}
		p.validateCondition(index, resolved, condition, raw)
	}
}

func (p *ProfileChecker) extensionClosure(ids []string) map[string]bool {
	seen := make(map[string]bool)
	var visit func(string)
	visit = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		node := p.checker.catalog.Nodes[id]
		if node == nil {
			return
		}
		for _, dependency := range node.Definition.Spec.Dependencies {
			visit(dependency)
		}
	}
	for _, id := range ids {
		visit(id)
	}
	return seen
}

func (p *ProfileChecker) vocabulary(ids map[string]bool) catalog.Vocabulary {
	var nodes []*catalog.Node
	for id := range ids {
		if node := p.checker.catalog.Nodes[id]; node != nil {
			nodes = append(nodes, node)
		}
	}
	slices.SortFunc(nodes, func(left *catalog.Node, right *catalog.Node) int {
		return strings.Compare(left.ID, right.ID)
	})
	return catalog.NewVocabulary(nodes)
}

func (p *ProfileChecker) validateCondition(index int, resolved catalog.Vocabulary, condition catalog.ProfileCondition, raw any) {
	prefix := fmt.Sprintf("conditions[%d]", index)
	p.report.ExpectExactlyOne(resolved.KindCount(condition.Kind), "%s.kind %s", prefix, condition.Kind)
	p.report.ExpectExactlyOne(resolved.InterfaceTypeCount(condition.Kind, condition.Interface.Type), "%s.interface.type %s/%s", prefix, condition.Kind, condition.Interface.Type)
	p.validateConditionInterface(prefix, resolved, condition)
	p.validateConditionConfiguration(prefix, resolved, condition)
	p.validateConditionSchemas(prefix, resolved, condition, raw)
}

// validateConditionInterface checks that every interface field a condition sets
// is declared for its kind and interface type, and that closed fields carry a
// value the vocabulary enumerates.
func (p *ProfileChecker) validateConditionInterface(prefix string, resolved catalog.Vocabulary, condition catalog.ProfileCondition) {
	kind := condition.Kind
	interfaceType := condition.Interface.Type
	if condition.Interface.Spec != nil {
		p.report.ExpectExactlyOne(resolved.InterfaceFieldCount(kind, interfaceType, "spec"), "%s.interface.spec for %s/%s", prefix, kind, interfaceType)
		p.report.ExpectExactlyOne(resolved.FieldValueCount("interface.spec.format", kind, interfaceType, condition.Interface.Spec.Format), "%s.interface.spec.format %s for %s/%s", prefix, condition.Interface.Spec.Format, kind, interfaceType)
	}
	if len(condition.Interface.Operations) > 0 {
		p.report.ExpectExactlyOne(resolved.InterfaceFieldCount(kind, interfaceType, "operations"), "%s.interface.operations for %s/%s", prefix, kind, interfaceType)
		for operationIndex, operation := range condition.Interface.Operations {
			if operation.Method != "" {
				p.report.ExpectExactlyOne(resolved.FieldValueCount("interface.operations[].method", kind, interfaceType, operation.Method), "%s.interface.operations[%d].method %s for %s/%s", prefix, operationIndex, operation.Method, kind, interfaceType)
			}
		}
	}
	if len(condition.Interface.Subjects) > 0 {
		p.report.ExpectExactlyOne(resolved.InterfaceFieldCount(kind, interfaceType, "subjects"), "%s.interface.subjects for %s/%s", prefix, kind, interfaceType)
	}
	if condition.Interface.Engine != "" {
		p.report.ExpectExactlyOne(resolved.InterfaceFieldCount(kind, interfaceType, "engine"), "%s.interface.engine for %s/%s", prefix, kind, interfaceType)
		p.report.ExpectExactlyOne(resolved.FieldValueCount("interface.engine", kind, interfaceType, condition.Interface.Engine), "%s.interface.engine %s for %s/%s", prefix, condition.Interface.Engine, kind, interfaceType)
	}
	if condition.Interface.BucketClass != "" {
		p.report.ExpectExactlyOne(resolved.InterfaceFieldCount(kind, interfaceType, "bucketClass"), "%s.interface.bucketClass for %s/%s", prefix, kind, interfaceType)
		p.report.ExpectExactlyOne(resolved.FieldValueCount("interface.bucketClass", kind, interfaceType, condition.Interface.BucketClass), "%s.interface.bucketClass %s for %s/%s", prefix, condition.Interface.BucketClass, kind, interfaceType)
	}
}

func (p *ProfileChecker) validateConditionConfiguration(prefix string, resolved catalog.Vocabulary, condition catalog.ProfileCondition) {
	if condition.Configuration == nil {
		return
	}
	kind := condition.Kind
	interfaceType := condition.Interface.Type
	p.report.ExpectExactlyOne(resolved.ConditionFieldCount(kind, interfaceType, "configuration"), "%s.configuration for %s/%s", prefix, kind, interfaceType)
	for envIndex, env := range condition.Configuration.Env {
		p.report.ExpectExactlyOne(resolved.FieldValueCount("configuration.env[].property", kind, interfaceType, env.Property), "%s.configuration.env[%d].property %s for %s/%s", prefix, envIndex, env.Property, kind, interfaceType)
	}
	for alternativeIndex, alternative := range condition.Configuration.Alternatives {
		for envIndex, env := range alternative.Env {
			p.report.ExpectExactlyOne(resolved.FieldValueCount("configuration.alternatives[].env[].property", kind, interfaceType, env.Property), "%s.configuration.alternatives[%d].env[%d].property %s for %s/%s", prefix, alternativeIndex, envIndex, env.Property, kind, interfaceType)
		}
	}
}

// validateConditionSchemas applies every extension schema whose scope matches the
// condition. A schema with no declared scope applies to all conditions.
func (p *ProfileChecker) validateConditionSchemas(prefix string, resolved catalog.Vocabulary, condition catalog.ProfileCondition, raw any) {
	for _, node := range resolved.Nodes() {
		for _, item := range node.Definition.Spec.Schemas {
			if item.AppliesToKind != "" && item.AppliesToKind != condition.Kind {
				continue
			}
			if item.AppliesToInterfaceType != "" && item.AppliesToInterfaceType != condition.Interface.Type {
				continue
			}
			schema, err := compileConditionSchema(item.Schema)
			if err != nil {
				p.report.Addf("%s schema %s could not be compiled: %v", prefix, item.ID, err)
				continue
			}
			instance, err := jsonSchemaValue(raw)
			if err != nil {
				p.report.Addf("%s could not be converted for schema validation: %v", prefix, err)
				continue
			}
			if err := schema.Validate(instance); err != nil {
				p.report.Addf("%s does not satisfy extension schema %s: %v", prefix, item.ID, err)
			}
		}
	}
}
