package extensioncheck

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
)

// FinalizeGeneratedProfile computes direct contributors from the reconstructed
// Conditions, validates them against the installed binding graph and external
// core schema, and returns a document only after every check succeeds.
// conditionPackages names the Go packages whose binding symbols contributed to
// each Condition, in the same order as profile.conditions.
func FinalizeGeneratedProfile(ctx context.Context, workloadDir string, profile map[string]any, conditionPackages [][]string) (map[string]any, error) {
	conditions, ok := profile["conditions"].([]any)
	if !ok || len(conditionPackages) != len(conditions) {
		return nil, fmt.Errorf("conditions and their resolved binding packages must correspond exactly")
	}
	paths := map[string]bool{}
	for _, group := range conditionPackages {
		if len(group) == 0 {
			return nil, fmt.Errorf("every generated Condition must identify a binding package")
		}
		for _, path := range group {
			paths[path] = true
		}
	}
	var packages []*VerifiedGoPackage
	if len(paths) != 0 {
		selected := make([]string, 0, len(paths))
		for path := range paths {
			selected = append(selected, path)
		}
		slices.Sort(selected)
		var err error
		packages, err = ResolveImportedGoPackages(ctx, workloadDir, selected)
		if err != nil {
			return nil, err
		}
	}
	byPath := map[string]*VerifiedGoPackage{}
	byID := map[ExtensionReference]*VerifiedGoPackage{}
	for _, pkg := range packages {
		byPath[pkg.ImportPath], byID[pkg.ExtensionReference()] = pkg, pkg
	}
	if err := verifyVocabularyOwnership(packages); err != nil {
		return nil, err
	}
	direct, err := validateGeneratedVocabulary(conditions, conditionPackages, byPath)
	if err != nil {
		return nil, err
	}
	result := make(map[string]any, len(profile))
	for key, value := range profile {
		result[key] = value
	}
	listed := make([]any, len(direct))
	for i, id := range direct {
		listed[i] = id.ID
		if id.Version != "" {
			listed[i] = id.ID + ":" + id.Version
		}
	}
	result["extensions"] = listed
	coreSchema, err := loadCoreProfileSchema(packages)
	if err != nil {
		return nil, err
	}
	if err := coreSchema.Validate(result); err != nil {
		return nil, fmt.Errorf("core profile schema: %w", err)
	}
	if err := validateConditionNames(conditions); err != nil {
		return nil, err
	}
	closure, err := installedClosure(direct, byID)
	if err != nil {
		return nil, err
	}
	if err := validateGeneratedSchemas(conditions, closure, byID); err != nil {
		return nil, err
	}
	return result, nil
}

func verifyVocabularyOwnership(packages []*VerifiedGoPackage) error {
	byCoordinate := map[string][]byte{}
	byMeaning := map[string]string{}
	installed := map[ExtensionReference]bool{}
	for _, pkg := range packages {
		installed[pkg.ExtensionReference()] = true
	}
	for _, pkg := range packages {
		for _, entry := range array(object(pkg.Model, "vocabulary"), "owners") {
			item := entry.(map[string]any)
			coordinate, owner := stringValue(item, "coordinate"), stringValue(item, "owner")
			reference, err := vocabularyReference(pkg.Model, item)
			if err != nil {
				return err
			}
			if !installed[reference] {
				return fmt.Errorf("vocabulary owner %s has no installed extension package", owner)
			}
			encoded, err := canonicalJSON(item)
			if err != nil {
				return err
			}
			if previous := byCoordinate[coordinate]; previous != nil && !bytes.Equal(previous, encoded) {
				return fmt.Errorf("vocabulary coordinate %s has conflicting definitions", coordinate)
			}
			byCoordinate[coordinate] = encoded
			value, err := canonicalJSON(item["value"])
			if err != nil {
				return err
			}
			meaning := stringValue(item, "category") + "\x00" + stringValue(item, "kind") + "\x00" + stringValue(item, "interfaceType") + "\x00" + stringValue(item, "path") + "\x00" + string(value)
			if previous := byMeaning[meaning]; previous != "" && previous != owner {
				return fmt.Errorf("vocabulary %s has conflicting owners %s and %s", coordinate, previous, owner)
			}
			byMeaning[meaning] = owner
		}
	}
	return nil
}

// Vocabulary provenance supplies the owner ID and semantic digest; the model
// closure supplies the corresponding exact release.
func vocabularyReference(model, item map[string]any) (ExtensionReference, error) {
	reference := ExtensionReference{}
	for _, entry := range array(model, "extensions") {
		identity := entry.(map[string]any)
		if stringValue(identity, "id") != stringValue(item, "owner") || stringValue(identity, "semanticSha256") != stringValue(item, "extensionSha256") {
			continue
		}
		if reference.Valid() {
			return ExtensionReference{}, fmt.Errorf("ambiguous vocabulary owner %s", item["owner"])
		}
		reference = extensionReference(identity)
	}
	if !reference.Valid() {
		return reference, fmt.Errorf("unresolved vocabulary owner %s", item["owner"])
	}
	return reference, nil
}

func validateGeneratedVocabulary(conditions []any, sources [][]string, packages map[string]*VerifiedGoPackage) ([]ExtensionReference, error) {
	direct := map[ExtensionReference]bool{}
	for index, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("conditions[%d] must be an object", index)
		}
		iface, ok := condition["interface"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("conditions[%d].interface must be an object", index)
		}
		kind, _ := condition["kind"].(string)
		interfaceType, _ := iface["type"].(string)
		if kind == "" || interfaceType == "" {
			return nil, fmt.Errorf("conditions[%d] needs kind and interface.type", index)
		}
		items := map[string]map[string]any{}
		for _, path := range sources[index] {
			pkg := packages[path]
			if pkg == nil {
				return nil, fmt.Errorf("conditions[%d] references unverified binding package %s", index, path)
			}
			for _, group := range []string{"owners", "conditionFields", "interfaceFields", "valueDomains"} {
				for _, entry := range array(object(pkg.Model, "vocabulary"), group) {
					item := maps.Clone(entry.(map[string]any))
					reference, err := vocabularyReference(pkg.Model, item)
					if err != nil {
						return nil, err
					}
					item["_release"] = reference
					key := group + "\x00" + stringValue(item, "coordinate")
					if previous := items[key]; previous != nil {
						left, _ := canonicalJSON(previous)
						right, _ := canonicalJSON(item)
						if !bytes.Equal(left, right) {
							return nil, fmt.Errorf("conditions[%d] has conflicting definition for %s", index, key)
						}
					} else {
						items[key] = item
					}
				}
			}
			for _, entry := range array(pkg.Manifest, "rootBindings") {
				root := entry.(map[string]any)
				if stringValue(root, "role") != "schema-field" {
					continue
				}
				ref := object(root, "modelRef")
				key := "schemaFields\x00" + stringValue(ref, "coordinate") + "\x00" + stringValue(ref, "jsonPointer")
				scope := object(root, "scope")
				item := map[string]any{
					"owner": pkg.ExtensionID, "_release": pkg.ExtensionReference(), "kind": stringValue(scope, "kind"),
					"interfaceType": stringValue(scope, "interfaceType"), "segments": array(root, "path"),
				}
				if previous := items[key]; previous != nil && previous["owner"] != item["owner"] {
					return nil, fmt.Errorf("conditions[%d] has conflicting schema-field owner for %s", index, key)
				}
				items[key] = item
			}
		}
		if err := claimExactOwner(items, "kind", kind, "", "", direct); err != nil {
			return nil, fmt.Errorf("conditions[%d]: %w", index, err)
		}
		if err := claimExactOwner(items, "interface", kind, interfaceType, "", direct); err != nil {
			return nil, fmt.Errorf("conditions[%d]: %w", index, err)
		}
		for _, name := range sortedDocumentKeys(condition) {
			if name == "name" || name == "optional" || name == "kind" || name == "interface" || name == "extension" {
				continue
			}
			if err := claimField(items, "conditionFields", kind, interfaceType, name, direct); err != nil {
				return nil, fmt.Errorf("conditions[%d].%s: %w", index, name, err)
			}
		}
		for _, name := range sortedDocumentKeys(iface) {
			if name == "type" {
				continue
			}
			if err := claimField(items, "interfaceFields", kind, interfaceType, name, direct); err != nil {
				return nil, fmt.Errorf("conditions[%d].interface.%s: %w", index, name, err)
			}
		}
		keys := make([]string, 0, len(items))
		for key := range items {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			domain := items[key]
			if len(key) < len("valueDomains") || key[:len("valueDomains")] != "valueDomains" || !matchesScope(domain, kind, interfaceType) {
				continue
			}
			for _, value := range valuesAtSegments(condition, array(domain, "segments")) {
				if !domainContains(domain, value) {
					return nil, fmt.Errorf("conditions[%d] value %v is outside domain %s", index, value, stringValue(domain, "coordinate"))
				}
				direct[domain["_release"].(ExtensionReference)] = true
			}
		}
	}
	result := make([]ExtensionReference, 0, len(direct))
	for id := range direct {
		result = append(result, id)
	}
	slices.SortFunc(result, ExtensionReference.Compare)
	return result, nil
}

func sortedDocumentKeys(document map[string]any) []string {
	keys := make([]string, 0, len(document))
	for key := range document {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func claimExactOwner(items map[string]map[string]any, category, kind, interfaceType, path string, direct map[ExtensionReference]bool) error {
	match := ExtensionReference{}
	for key, item := range items {
		if len(key) < len("owners") || key[:len("owners")] != "owners" || stringValue(item, "category") != category || stringValue(item, "kind") != kind {
			continue
		}
		if category == "interface" && stringValue(item, "interfaceType") != interfaceType {
			continue
		}
		if path != "" && stringValue(item, "path") != path {
			continue
		}
		if match.Valid() {
			return fmt.Errorf("%s %s/%s has multiple owners", category, kind, interfaceType)
		}
		match = item["_release"].(ExtensionReference)
	}
	if !match.Valid() {
		return fmt.Errorf("%s %s/%s has no owner", category, kind, interfaceType)
	}
	direct[match] = true
	return nil
}

func claimField(items map[string]map[string]any, group, kind, interfaceType, name string, direct map[ExtensionReference]bool) error {
	match := ExtensionReference{}
	for key, item := range items {
		if len(key) < len(group) || key[:len(group)] != group || !matchesScope(item, kind, interfaceType) {
			continue
		}
		segments := array(item, "segments")
		index := 0
		if group == "interfaceFields" {
			index = 1
			if len(segments) == 0 || stringValue(segments[0].(map[string]any), "name") != "interface" {
				continue
			}
		}
		if len(segments) <= index || stringValue(segments[index].(map[string]any), "name") != name {
			continue
		}
		owner := item["_release"].(ExtensionReference)
		if match.Valid() && match != owner {
			return fmt.Errorf("field has multiple owners")
		}
		match = owner
	}
	if !match.Valid() {
		if group == "conditionFields" {
			return claimField(items, "schemaFields", kind, interfaceType, name, direct)
		}
		return fmt.Errorf("field has no vocabulary owner")
	}
	direct[match] = true
	return nil
}

func matchesScope(item map[string]any, kind, interfaceType string) bool {
	return stringValue(item, "kind") == kind && (stringValue(item, "interfaceType") == "" || stringValue(item, "interfaceType") == interfaceType)
}

func valuesAtSegments(value any, segments []any) []any {
	if len(segments) == 0 {
		return []any{value}
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	segment := segments[0].(map[string]any)
	child, found := object[stringValue(segment, "name")]
	if !found {
		return nil
	}
	if segment["array"] == true {
		array, ok := child.([]any)
		if !ok {
			return nil
		}
		var result []any
		for _, item := range array {
			result = append(result, valuesAtSegments(item, segments[1:])...)
		}
		return result
	}
	return valuesAtSegments(child, segments[1:])
}

func domainContains(domain map[string]any, value any) bool {
	actual, err := canonicalJSON(value)
	if err != nil {
		return false
	}
	for _, entry := range array(domain, "values") {
		expected, err := canonicalJSON(entry.(map[string]any)["value"])
		if err == nil && bytes.Equal(actual, expected) {
			return true
		}
	}
	return false
}

func validateConditionNames(conditions []any) error {
	seen := map[string]bool{}
	for index, raw := range conditions {
		if name, ok := raw.(map[string]any)["name"].(string); ok {
			if seen[name] {
				return fmt.Errorf("duplicate Condition name %q at conditions[%d]", name, index)
			}
			seen[name] = true
		}
	}
	return nil
}

func installedClosure(direct []ExtensionReference, byID map[ExtensionReference]*VerifiedGoPackage) (map[ExtensionReference]bool, error) {
	closure := map[ExtensionReference]bool{}
	var visit func(ExtensionReference) error
	visit = func(id ExtensionReference) error {
		if closure[id] {
			return nil
		}
		pkg := byID[id]
		if pkg == nil {
			return fmt.Errorf("direct extension %s has no installed package", id)
		}
		closure[id] = true
		for _, entry := range array(pkg.Release, "packageDependencies") {
			if err := visit(extensionReference(object(entry.(map[string]any), "extension"))); err != nil {
				return err
			}
		}
		return nil
	}
	for _, id := range direct {
		if err := visit(id); err != nil {
			return nil, err
		}
	}
	return closure, nil
}

func validateGeneratedSchemas(conditions []any, closure map[ExtensionReference]bool, byID map[ExtensionReference]*VerifiedGoPackage) error {
	schemas := map[string]map[string]any{}
	for id := range closure {
		for _, entry := range array(byID[id].Model, "schemas") {
			item := entry.(map[string]any)
			owner, err := vocabularyReference(byID[id].Model, item)
			if err != nil {
				return err
			}
			if !closure[owner] {
				continue
			}
			coordinate := stringValue(item, "coordinate")
			if previous := schemas[coordinate]; previous != nil {
				left, _ := canonicalJSON(previous)
				right, _ := canonicalJSON(item)
				if !bytes.Equal(left, right) {
					return fmt.Errorf("conflicting installed schema %s", coordinate)
				}
			} else {
				schemas[coordinate] = item
			}
		}
	}
	coordinates := make([]string, 0, len(schemas))
	for coordinate := range schemas {
		coordinates = append(coordinates, coordinate)
	}
	slices.Sort(coordinates)
	for index, raw := range conditions {
		condition := raw.(map[string]any)
		kind := stringValue(condition, "kind")
		interfaceType := stringValue(object(condition, "interface"), "type")
		for _, coordinate := range coordinates {
			item := schemas[coordinate]
			if !schemaApplies(item, kind, interfaceType) {
				continue
			}
			exact := object(item, "exact")
			hash := sha256.Sum256([]byte(coordinate))
			uri := "urn:runtimeconditions:installed-schema:" + hex.EncodeToString(hash[:])
			compiled, err := compileDraft2020(exact, uri)
			if err != nil {
				return fmt.Errorf("extension schema %s: %w", coordinate, err)
			}
			if err := compiled.Validate(condition); err != nil {
				return fmt.Errorf("conditions[%d] fails extension schema %s: %w", index, coordinate, err)
			}
		}
	}
	return nil
}

func schemaApplies(item map[string]any, kind, interfaceType string) bool {
	schemaKind := stringValue(item, "kind")
	schemaInterface := stringValue(item, "interfaceType")
	return (schemaKind == "" || schemaKind == kind) && (schemaInterface == "" || schemaInterface == interfaceType)
}
