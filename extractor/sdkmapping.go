package extractor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const goSDKMappingIndexPath = "runtimeconditions/index.yaml"

type goSDKMapping struct {
	Path      string
	Extension sdkExtensionReference
	Calls     []goSDKCall
}

type sdkExtensionReference struct {
	ID             string `yaml:"id"`
	Version        string `yaml:"version"`
	SemanticSHA256 string `yaml:"semanticSha256"`
}

type goSDKCall struct {
	ExtensionID       string                      `yaml:"-"`
	ID                string                      `yaml:"id"`
	Symbol            goSDKSymbol                 `yaml:"symbol"`
	ReceiverState     string                      `yaml:"receiverState"`
	ArgumentState     *goSDKArgumentState         `yaml:"argumentState"`
	ConditionTemplate goSDKConditionTemplate      `yaml:"conditionTemplate"`
	OperationBindings map[string]goSDKValueSource `yaml:"operationBindings"`
	Produces          *goSDKStateProduction       `yaml:"produces"`
}

type goSDKSymbol struct {
	Package  string `yaml:"package"`
	Function string `yaml:"function"`
	Receiver string `yaml:"receiver"`
	Method   string `yaml:"method"`
}

type goSDKConditionTemplate struct {
	Kind          string         `yaml:"kind"`
	InterfaceType string         `yaml:"interfaceType"`
	Operation     map[string]any `yaml:"operation"`
}

type goSDKValueSource struct {
	Argument *goSDKArgumentSource `yaml:"argument"`
	State    string               `yaml:"state"`
	Optional bool                 `yaml:"optional"`
}

type goSDKArgumentSource struct {
	Parameter string `yaml:"parameter"`
	Position  *int   `yaml:"position"`
	Field     string `yaml:"field"`
}

type goSDKArgumentState struct {
	StateType string              `yaml:"stateType"`
	Argument  goSDKArgumentSource `yaml:"argument"`
}

type goSDKStateProduction struct {
	StateType          string                      `yaml:"stateType"`
	DependencyIdentity string                      `yaml:"dependencyIdentity"`
	Bindings           map[string]goSDKValueSource `yaml:"bindings"`
}

type goSDKMappingDocument struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name           string `yaml:"name"`
		Module         string `yaml:"module"`
		ModuleVersion  string `yaml:"moduleVersion"`
		Language       string `yaml:"language"`
		SemanticSHA256 string `yaml:"semanticSha256"`
	} `yaml:"metadata"`
	Extension sdkExtensionReference `yaml:"extension"`
	Go        struct {
		Calls []goSDKCall `yaml:"calls"`
	} `yaml:"go"`
}

type goSDKMappingIndex struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Module        string `yaml:"module"`
		ModuleVersion string `yaml:"moduleVersion"`
		Language      string `yaml:"language"`
	} `yaml:"metadata"`
	Mappings []struct {
		Name   string `yaml:"name"`
		Path   string `yaml:"path"`
		SHA256 string `yaml:"sha256"`
	} `yaml:"mappings"`
}

type listedGoModule struct {
	Path    string
	Version string
	Dir     string
	Replace *listedGoModule
}

func (m listedGoModule) sourceDir() string {
	if m.Replace != nil && m.Replace.Dir != "" {
		return m.Replace.Dir
	}
	return m.Dir
}

func discoverGoSDKMappings(sourceDir string, files []parsedFile, extensionRoots []string) ([]goSDKMapping, error) {
	modules, err := listGoModules(sourceDir)
	if err != nil {
		return nil, err
	}
	imports := directImportPaths(files)
	var mappings []goSDKMapping
	for _, module := range modules {
		if module.sourceDir() == "" || !moduleImported(module.Path, imports) {
			continue
		}
		indexPath := filepath.Join(module.sourceDir(), goSDKMappingIndexPath)
		if _, err := os.Stat(indexPath); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		loaded, err := readGoSDKMappingIndex(indexPath, module, extensionRoots)
		if err != nil {
			return nil, err
		}
		mappings = append(mappings, loaded...)
	}
	slices.SortFunc(mappings, func(left goSDKMapping, right goSDKMapping) int { return strings.Compare(left.Path, right.Path) })
	return mappings, nil
}

func moduleImported(modulePath string, imports []string) bool {
	for _, importPath := range imports {
		if importPath == modulePath || strings.HasPrefix(importPath, modulePath+"/") {
			return true
		}
	}
	return false
}

func listGoModules(sourceDir string) ([]listedGoModule, error) {
	command := exec.Command("go", "list", "-m", "-json", "all")
	command.Dir = sourceDir
	output, err := command.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("go list -m failed: %s", strings.TrimSpace(string(exit.Stderr)))
		}
		return nil, fmt.Errorf("go list -m failed: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	var modules []listedGoModule
	for {
		var module listedGoModule
		if err := decoder.Decode(&module); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		modules = append(modules, module)
	}
	return modules, nil
}

func readGoSDKMappingIndex(path string, module listedGoModule, extensionRoots []string) ([]goSDKMapping, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var index goSDKMappingIndex
	if err := yaml.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if index.APIVersion != "runtimeconditions.io/sdk-mapping/v1alpha1" || index.Kind != "RuntimeConditionsSDKMappingIndex" {
		return nil, fmt.Errorf("%s: unsupported SDK mapping index", path)
	}
	if index.Metadata.Module != module.Path || index.Metadata.ModuleVersion != module.Version || index.Metadata.Language != "go" {
		return nil, fmt.Errorf("%s: index identity does not match resolved module %s %s", path, module.Path, module.Version)
	}
	seen := make(map[string]bool)
	var mappings []goSDKMapping
	for _, item := range index.Mappings {
		if item.Name == "" || item.Path == "" || item.SHA256 == "" || seen[item.Name] {
			return nil, fmt.Errorf("%s: mapping entries require unique names, paths, and SHA-256 digests", path)
		}
		seen[item.Name] = true
		mappingPath := filepath.Clean(filepath.Join(module.sourceDir(), filepath.FromSlash(item.Path)))
		if !sdkPathWithin(module.sourceDir(), mappingPath) {
			return nil, fmt.Errorf("%s: mapping path %q escapes module root", path, item.Path)
		}
		mappingData, err := os.ReadFile(mappingPath)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(mappingData)
		if hex.EncodeToString(digest[:]) != item.SHA256 {
			return nil, fmt.Errorf("%s: mapping SHA-256 does not match index", mappingPath)
		}
		mapping, err := readGoSDKMapping(mappingPath, mappingData, module, item.Name, extensionRoots)
		if err != nil {
			return nil, err
		}
		mappings = append(mappings, mapping)
	}
	return mappings, nil
}

func readGoSDKMapping(path string, data []byte, module listedGoModule, expectedName string, extensionRoots []string) (goSDKMapping, error) {
	var document goSDKMappingDocument
	if err := yaml.Unmarshal(data, &document); err != nil {
		return goSDKMapping{}, fmt.Errorf("%s: %w", path, err)
	}
	if document.APIVersion != "runtimeconditions.io/sdk-mapping/v1alpha1" || document.Kind != "RuntimeConditionsSDKMapping" {
		return goSDKMapping{}, fmt.Errorf("%s: unsupported SDK mapping document", path)
	}
	if document.Metadata.Name != expectedName || document.Metadata.Module != module.Path || document.Metadata.ModuleVersion != module.Version || document.Metadata.Language != "go" {
		return goSDKMapping{}, fmt.Errorf("%s: mapping identity does not match index and resolved module", path)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return goSDKMapping{}, err
	}
	semantic, err := json.Marshal(raw["go"])
	if err != nil {
		return goSDKMapping{}, err
	}
	digest := sha256.Sum256(semantic)
	if hex.EncodeToString(digest[:]) != document.Metadata.SemanticSHA256 {
		return goSDKMapping{}, fmt.Errorf("%s: semantic SHA-256 does not match go mapping body", path)
	}
	if document.Extension.ID == "" || document.Extension.Version == "" || document.Extension.SemanticSHA256 == "" {
		return goSDKMapping{}, fmt.Errorf("%s: exact extension id, version, and semantic SHA-256 are required", path)
	}
	if err := verifySDKExtensionReference(document.Extension, extensionRoots); err != nil {
		return goSDKMapping{}, fmt.Errorf("%s: %w", path, err)
	}
	if len(document.Go.Calls) == 0 {
		return goSDKMapping{}, fmt.Errorf("%s: go.calls must not be empty", path)
	}
	for _, call := range document.Go.Calls {
		hasCondition := call.ConditionTemplate.Kind != "" || call.ConditionTemplate.InterfaceType != "" || len(call.ConditionTemplate.Operation) != 0
		if call.ID == "" || call.Symbol.Package == "" || (call.Symbol.Function == "") == (call.Symbol.Method == "") || (!hasCondition && call.Produces == nil) {
			return goSDKMapping{}, fmt.Errorf("%s: call %q has an incomplete symbol or condition template", path, call.ID)
		}
		if hasCondition && (call.ConditionTemplate.Kind == "" || call.ConditionTemplate.InterfaceType == "" || len(call.ConditionTemplate.Operation) == 0) {
			return goSDKMapping{}, fmt.Errorf("%s: call %q has a partial condition template", path, call.ID)
		}
		if call.Produces != nil && call.Produces.DependencyIdentity != "" && call.Produces.DependencyIdentity != "new" && call.Produces.DependencyIdentity != "inherit" {
			return goSDKMapping{}, fmt.Errorf("%s: call %q has unsupported produces.dependencyIdentity %q", path, call.ID, call.Produces.DependencyIdentity)
		}
	}
	return goSDKMapping{Path: path, Extension: document.Extension, Calls: document.Go.Calls}, nil
}

func verifySDKExtensionReference(reference sdkExtensionReference, roots []string) error {
	for _, root := range roots {
		var matched bool
		var matchErr error
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || (filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml") {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			var probe struct {
				Kind     string                `yaml:"kind"`
				Metadata sdkExtensionReference `yaml:"metadata"`
			}
			if err := yaml.Unmarshal(data, &probe); err != nil {
				return err
			}
			if probe.Kind == "RuntimeConditionsExtensionDefinition" && probe.Metadata.ID == reference.ID {
				matched = true
				if probe.Metadata.Version != reference.Version || probe.Metadata.SemanticSHA256 != reference.SemanticSHA256 {
					matchErr = fmt.Errorf("extension %s version or semantic SHA-256 does not match installed definition", reference.ID)
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		if matched {
			return matchErr
		}
	}
	return fmt.Errorf("extension %s is not available in the configured extension roots", reference.ID)
}

type sdkResolvedState struct {
	stateType          string
	values             map[string]any
	dependencyIdentity string
}

type sdkStateTable struct {
	semantic         map[types.Object]sdkResolvedState
	syntax           map[string]sdkResolvedState
	callDependencies map[*ast.CallExpr]string
}

type sdkConditionObservation struct {
	condition          Condition
	extensionID        string
	dependencyIdentity string
}

func extractGoSDKConditions(files []parsedFile, semantic *semanticScope, mappings []goSDKMapping) ([]Condition, []string, error) {
	if len(mappings) == 0 || semantic == nil {
		return nil, nil, nil
	}
	var calls []goSDKCall
	for _, mapping := range mappings {
		for _, call := range mapping.Calls {
			call.ExtensionID = mapping.Extension.ID
			calls = append(calls, call)
		}
	}
	var observations []sdkConditionObservation
	usedExtensions := make(map[string]bool)
	for _, parsed := range files {
		states := sdkStateTable{semantic: make(map[types.Object]sdkResolvedState), syntax: make(map[string]sdkResolvedState), callDependencies: make(map[*ast.CallExpr]string)}
		ast.Inspect(parsed.file, func(node ast.Node) bool {
			switch item := node.(type) {
			case *ast.AssignStmt:
				recordSDKAssignment(item, semantic, calls, &states)
			case *ast.CallExpr:
				call, ok := goSDKCallForExpression(item, semantic, calls)
				if !ok {
					return true
				}
				state, hasState := receiverSDKState(item, semantic, states)
				if call.ReceiverState != "" && (!hasState || state.stateType != call.ReceiverState) {
					return true
				}
				if call.ArgumentState != nil {
					argumentState, ok := argumentSDKState(item, semantic, states, call.ArgumentState.Argument)
					if !ok || argumentState.stateType != call.ArgumentState.StateType {
						return true
					}
				}
				if call.ConditionTemplate.Kind == "" {
					return true
				}
				condition, ok := resolveSDKCondition(item, semantic, call, state)
				if ok {
					dependencyIdentity := state.dependencyIdentity
					if assignedIdentity := states.callDependencies[item]; assignedIdentity != "" {
						dependencyIdentity = assignedIdentity
					}
					observations = append(observations, sdkConditionObservation{condition: condition, extensionID: call.ExtensionID, dependencyIdentity: dependencyIdentity})
					usedExtensions[call.ExtensionID] = true
				}
			}
			return true
		})
	}
	ids := make([]string, 0, len(usedExtensions))
	for id := range usedExtensions {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return mergeSDKConditionObservations(observations), ids, nil
}

func recordSDKAssignment(assign *ast.AssignStmt, semantic *semanticScope, calls []goSDKCall, states *sdkStateTable) {
	if len(assign.Rhs) != 1 || len(assign.Lhs) == 0 {
		return
	}
	callExpr, ok := unparen(assign.Rhs[0]).(*ast.CallExpr)
	if !ok {
		return
	}
	call, ok := goSDKCallForExpression(callExpr, semantic, calls)
	if !ok || call.Produces == nil {
		return
	}
	receiverState, _ := receiverSDKState(callExpr, semantic, *states)
	argumentState := sdkResolvedState{}
	if call.ArgumentState != nil {
		var argumentOK bool
		argumentState, argumentOK = argumentSDKState(callExpr, semantic, *states, call.ArgumentState.Argument)
		if !argumentOK || argumentState.stateType != call.ArgumentState.StateType {
			return
		}
	}
	values := make(map[string]any)
	for name, source := range call.Produces.Bindings {
		value, ok := resolveSDKValue(callExpr, semantic, source, receiverState)
		if !ok {
			return
		}
		values[name] = value
	}
	ident, ok := unparen(assign.Lhs[0]).(*ast.Ident)
	if !ok || ident.Name == "_" {
		return
	}
	dependencyIdentity := ""
	switch call.Produces.DependencyIdentity {
	case "new":
		dependencyIdentity = sdkAssignmentIdentity(assign, semantic)
	case "inherit":
		dependencyIdentity = receiverState.dependencyIdentity
		if dependencyIdentity == "" {
			dependencyIdentity = argumentState.dependencyIdentity
		}
	}
	state := sdkResolvedState{stateType: call.Produces.StateType, values: values, dependencyIdentity: dependencyIdentity}
	if dependencyIdentity != "" {
		states.callDependencies[callExpr] = dependencyIdentity
	}
	if object := semantic.objectForExpr(ident); object != nil {
		states.semantic[object] = state
	} else {
		states.syntax[ident.Name] = state
	}
}

func sdkAssignmentIdentity(assign *ast.AssignStmt, semantic *semanticScope) string {
	if len(assign.Lhs) == 0 {
		return ""
	}
	ident, ok := unparen(assign.Lhs[0]).(*ast.Ident)
	if !ok || ident.Name == "_" {
		return ""
	}
	if object := semantic.objectForExpr(ident); object != nil {
		return fmt.Sprintf("object:%p", object)
	}
	return ""
}

func goSDKCallForExpression(call *ast.CallExpr, semantic *semanticScope, calls []goSDKCall) (goSDKCall, bool) {
	object := semantic.objectForExpr(call.Fun)
	function, ok := object.(*types.Func)
	if !ok {
		return goSDKCall{}, false
	}
	pkg := ""
	if function.Pkg() != nil {
		pkg = function.Pkg().Path()
	}
	receiver := ""
	if signature, ok := function.Type().(*types.Signature); ok && signature.Recv() != nil {
		_, receiver, _ = namedTypeIdentity(signature.Recv().Type())
	}
	for _, candidate := range calls {
		if candidate.Symbol.Package != pkg {
			continue
		}
		if candidate.Symbol.Function != "" && receiver == "" && candidate.Symbol.Function == function.Name() {
			return candidate, true
		}
		if candidate.Symbol.Method != "" && candidate.Symbol.Receiver == receiver && candidate.Symbol.Method == function.Name() {
			return candidate, true
		}
	}
	return goSDKCall{}, false
}

func receiverSDKState(call *ast.CallExpr, semantic *semanticScope, states sdkStateTable) (sdkResolvedState, bool) {
	selector, ok := unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return sdkResolvedState{}, false
	}
	ident, ok := unparen(selector.X).(*ast.Ident)
	if !ok {
		return sdkResolvedState{}, false
	}
	if object := semantic.objectForExpr(ident); object != nil {
		state, ok := states.semantic[object]
		return state, ok
	}
	state, ok := states.syntax[ident.Name]
	return state, ok
}

func argumentSDKState(call *ast.CallExpr, semantic *semanticScope, states sdkStateTable, source goSDKArgumentSource) (sdkResolvedState, bool) {
	position, ok := sdkArgumentPosition(call, semantic, source)
	if !ok || position >= len(call.Args) {
		return sdkResolvedState{}, false
	}
	ident, ok := unparen(call.Args[position]).(*ast.Ident)
	if !ok {
		return sdkResolvedState{}, false
	}
	if object := semantic.objectForExpr(ident); object != nil {
		state, ok := states.semantic[object]
		return state, ok
	}
	state, ok := states.syntax[ident.Name]
	return state, ok
}

func resolveSDKCondition(call *ast.CallExpr, semantic *semanticScope, mapping goSDKCall, state sdkResolvedState) (Condition, bool) {
	operation := make(map[string]any, len(mapping.ConditionTemplate.Operation)+len(mapping.OperationBindings))
	for name, value := range mapping.ConditionTemplate.Operation {
		operation[name] = value
	}
	for name, source := range mapping.OperationBindings {
		value, ok := resolveSDKValue(call, semantic, source, state)
		if !ok {
			if source.Optional {
				continue
			}
			return Condition{}, false
		}
		operation[name] = value
	}
	return Condition{Kind: mapping.ConditionTemplate.Kind, Interface: Interface{Type: mapping.ConditionTemplate.InterfaceType, Operations: []Operation{{Fields: operation}}}}, true
}

func resolveSDKValue(call *ast.CallExpr, semantic *semanticScope, source goSDKValueSource, state sdkResolvedState) (any, bool) {
	if source.State != "" {
		value, ok := state.values[source.State]
		return value, ok
	}
	if source.Argument == nil {
		return nil, false
	}
	position, ok := sdkArgumentPosition(call, semantic, *source.Argument)
	if !ok || position >= len(call.Args) {
		return nil, false
	}
	expression := unparen(call.Args[position])
	if source.Argument.Field != "" {
		if address, ok := expression.(*ast.UnaryExpr); ok && address.Op.String() == "&" {
			expression = unparen(address.X)
		}
		composite, ok := expression.(*ast.CompositeLit)
		if !ok {
			return nil, false
		}
		var found ast.Expr
		for _, element := range composite.Elts {
			keyValue, ok := element.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if key, keyOK := keyValue.Key.(*ast.Ident); keyOK && key.Name == source.Argument.Field {
				found = keyValue.Value
				break
			}
		}
		if found == nil {
			return nil, false
		}
		expression = unparen(found)
	}
	if value, ok := semantic.stringValue(expression); ok {
		return value, true
	}
	if list, ok := expression.(*ast.CompositeLit); ok {
		values := make([]string, 0, len(list.Elts))
		for _, element := range list.Elts {
			value, ok := sdkStringValue(semantic, unparen(element))
			if !ok {
				return nil, false
			}
			values = append(values, value)
		}
		return values, true
	}
	if literal, ok := expression.(*ast.BasicLit); ok && literal.Kind.String() == "STRING" {
		value, err := strconv.Unquote(literal.Value)
		return value, err == nil
	}
	return nil, false
}

func sdkArgumentPosition(call *ast.CallExpr, semantic *semanticScope, source goSDKArgumentSource) (int, bool) {
	if source.Parameter != "" {
		function, ok := semantic.objectForExpr(call.Fun).(*types.Func)
		if !ok {
			return 0, false
		}
		signature, ok := function.Type().(*types.Signature)
		if !ok {
			return 0, false
		}
		for position := range signature.Params().Len() {
			if signature.Params().At(position).Name() == source.Parameter {
				return position, true
			}
		}
		return 0, false
	}
	if source.Position == nil || *source.Position < 0 {
		return 0, false
	}
	return *source.Position, true
}

func mergeSDKConditionObservations(observations []sdkConditionObservation) []Condition {
	conditions := make([]Condition, 0, len(observations))
	groupIndexes := make(map[string]int)
	for _, observation := range observations {
		if observation.dependencyIdentity == "" {
			conditions = append(conditions, observation.condition)
			continue
		}
		key := observation.extensionID + "\x00" + observation.dependencyIdentity + "\x00" + observation.condition.Kind + "\x00" + observation.condition.Interface.Type
		index, exists := groupIndexes[key]
		if !exists {
			groupIndexes[key] = len(conditions)
			conditions = append(conditions, observation.condition)
			continue
		}
		for _, operation := range observation.condition.Interface.Operations {
			if !containsSDKOperation(conditions[index].Interface.Operations, operation) {
				conditions[index].Interface.Operations = append(conditions[index].Interface.Operations, operation)
			}
		}
	}
	return conditions
}

func containsSDKOperation(operations []Operation, candidate Operation) bool {
	candidateJSON, err := json.Marshal(candidate)
	if err != nil {
		return false
	}
	for _, operation := range operations {
		operationJSON, err := json.Marshal(operation)
		if err == nil && string(operationJSON) == string(candidateJSON) {
			return true
		}
	}
	return false
}

func sdkStringValue(semantic *semanticScope, expression ast.Expr) (string, bool) {
	if value, ok := semantic.stringValue(expression); ok {
		return value, true
	}
	return stringLiteral(expression)
}

func sdkPathWithin(root string, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
