package extensioncheck

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	goBindingsManifest       = "runtimeconditions.bindings.yaml"
	goPackageBindingManifest = "runtimeconditions.package.yaml"
	defaultExtensionFile     = "runtimeconditions.extension.yaml"
)

// Options configures static extension validation.
type Options struct {
	Language               string
	CatalogRoots           []string
	RequireLanguagePackage bool
}

// ProfileOptions configures validation for generated Runtime Conditions
// Profiles.
type ProfileOptions struct {
	CatalogRoots []string
}

// ValidateExtension validates the extension definition under root, plus any
// dependency extensions required to check its binding and declaration package.
func ValidateExtension(root string, opts Options) error {
	return validate(root, opts, true)
}

// ValidateExtensions validates every extension definition found under root.
func ValidateExtensions(root string, opts Options) error {
	return validate(root, opts, false)
}

// ValidateBindingManifest validates a Go binding or package manifest against
// its referenced extension definition and resolved dependency graph.
func ValidateBindingManifest(path string, opts Options) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	binding, err := readBindingDocument(absPath)
	if err != nil {
		return err
	}
	if opts.Language == "" {
		opts.Language = binding.Metadata.Language
		if opts.Language == "" {
			opts.Language = "go"
		}
	}
	extensionDefinition := binding.extensionDefinitionPath()
	if extensionDefinition == "" {
		extensionDefinition = filepath.Join(filepath.Dir(absPath), defaultExtensionFile)
	}
	if !filepath.IsAbs(extensionDefinition) {
		extensionDefinition = filepath.Join(filepath.Dir(absPath), extensionDefinition)
	}
	extensionDefinition, err = filepath.Abs(extensionDefinition)
	if err != nil {
		return err
	}
	definition, ok, err := readExtensionDefinition(extensionDefinition)
	if err != nil {
		return err
	}
	if !ok {
		return validationErrors{extensionDefinition + ": kind must be RuntimeConditionsExtensionDefinition"}
	}

	validator := &validator{opts: opts}
	if err := validator.loadCatalog(catalogRoots(filepath.Dir(extensionDefinition), opts.CatalogRoots, true)); err != nil {
		return err
	}
	id := definitionID(definition)
	node := validator.nodes[id]
	if node == nil {
		validator.addf(extensionDefinition, "extension definition %s was not loaded from catalog", id)
		return validator.err()
	}
	node.BindingPath = absPath
	node.Binding = binding
	node.BindingDir = filepath.Dir(absPath)
	if opts.Language == "go" {
		node.GoDir = node.BindingDir
	}
	validator.validateNode(id)
	return validator.err()
}

// ValidateBindingManifests validates multiple binding manifests and reports all
// manifest failures together.
func ValidateBindingManifests(paths []string, opts Options) error {
	var errs []string
	seen := make(map[string]bool)
	for _, path := range paths {
		absPath, err := filepath.Abs(path)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if seen[absPath] {
			continue
		}
		seen[absPath] = true
		if err := ValidateBindingManifest(absPath, opts); err != nil {
			errs = appendValidationError(errs, err)
		}
	}
	if len(errs) > 0 {
		return validationErrors(errs)
	}
	return nil
}

// ResolveExtensionClosure returns ids plus all transitive extension
// dependencies, sorted for stable profile output.
func ResolveExtensionClosure(ids []string, opts ProfileOptions) ([]string, error) {
	validator := &validator{}
	if err := validator.loadCatalog(opts.CatalogRoots); err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var visit func(string)
	visit = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		node := validator.nodes[id]
		if node == nil {
			validator.addf(id, "missing extension definition for %s", id)
			return
		}
		validator.validateNode(id)
		for _, dependency := range node.Definition.Spec.Dependencies {
			visit(dependency)
		}
	}
	for _, id := range ids {
		visit(id)
	}
	if err := validator.err(); err != nil {
		return nil, err
	}
	result := make([]string, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}
	slices.Sort(result)
	return result, nil
}

// ValidateProfileYAML validates a Runtime Conditions Profile against the
// resolved vocabulary provided by its declared extensions.
func ValidateProfileYAML(data []byte, opts ProfileOptions) error {
	var profile profileDocument
	if err := yaml.Unmarshal(data, &profile); err != nil {
		return err
	}
	return validateProfile(profile, opts)
}

func validateProfile(profile profileDocument, opts ProfileOptions) error {
	validator := &validator{}
	if err := validator.loadCatalog(opts.CatalogRoots); err != nil {
		return err
	}
	profileValidator := &profileValidator{
		catalog: validator,
	}
	profileValidator.validate(profile)
	if err := validator.err(); err != nil {
		profileValidator.errs = appendValidationError(profileValidator.errs, err)
	}
	if len(profileValidator.errs) > 0 {
		return profileValidationErrors(profileValidator.errs)
	}
	return nil
}

func validate(root string, opts Options, targetOnly bool) error {
	if opts.Language == "" {
		opts.Language = "go"
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	includeParentCatalog := false
	if targetOnly {
		var err error
		includeParentCatalog, err = containsDirectExtensionDefinition(absRoot)
		if err != nil {
			return err
		}
	}

	validator := &validator{opts: opts}
	if err := validator.loadCatalog(catalogRoots(absRoot, opts.CatalogRoots, includeParentCatalog)); err != nil {
		return err
	}

	targets := validator.targets(absRoot, targetOnly)
	if len(targets) == 0 {
		validator.addf(absRoot, "no RuntimeConditionsExtensionDefinition YAML files found")
		return validator.err()
	}
	for _, id := range targets {
		validator.validateNode(id)
	}
	return validator.err()
}

func containsDirectExtensionDefinition(root string) (bool, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if !isYAML(path) {
			continue
		}
		_, ok, err := readExtensionDefinition(path)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func catalogRoots(root string, explicit []string, includeParent bool) []string {
	roots := []string{root}
	parent := filepath.Dir(root)
	if includeParent && parent != root {
		roots = append(roots, parent)
	}
	for _, item := range explicit {
		if strings.TrimSpace(item) != "" {
			roots = append(roots, item)
		}
	}
	return roots
}

type validator struct {
	opts   Options
	nodes  map[string]*extensionNode
	states map[string]int
	errs   []string
}

type extensionNode struct {
	ID             string
	Dir            string
	DefinitionPath string
	Definition     extensionDefinition
	BindingPath    string
	Binding        *bindingDocument
	BindingDir     string
	GoDir          string
}

type extensionDefinition struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		ID string `yaml:"id"`
	} `yaml:"metadata"`
	Spec extensionSpec `yaml:"spec"`
}

type extensionSpec struct {
	Dependencies    []string                    `yaml:"dependencies"`
	Kinds           []extensionKind             `yaml:"kinds"`
	InterfaceTypes  []extensionInterfaceType    `yaml:"interfaceTypes"`
	ConditionFields []extensionConditionField   `yaml:"conditionFields"`
	InterfaceFields []extensionInterfaceField   `yaml:"interfaceFields"`
	FieldValues     []extensionFieldValue       `yaml:"fieldValues"`
	Schemas         []extensionValidationSchema `yaml:"schemas"`
}

type extensionKind struct {
	Name string `yaml:"name"`
}

type extensionInterfaceType struct {
	Name       string `yaml:"name"`
	TargetKind string `yaml:"targetKind"`
}

type extensionConditionField struct {
	Name                    string   `yaml:"name"`
	AppliesToKinds          []string `yaml:"appliesToKinds"`
	AppliesToInterfaceTypes []string `yaml:"appliesToInterfaceTypes"`
}

type extensionInterfaceField struct {
	Name       string `yaml:"name"`
	TargetKind string `yaml:"targetKind"`
	TargetType string `yaml:"targetType"`
}

type extensionFieldValue struct {
	Field      string   `yaml:"field"`
	TargetKind string   `yaml:"targetKind"`
	TargetType string   `yaml:"targetType"`
	Values     []string `yaml:"values"`
}

type extensionValidationSchema struct {
	ID                     string `yaml:"id"`
	Description            string `yaml:"description"`
	AppliesToKind          string `yaml:"appliesToKind"`
	AppliesToInterfaceType string `yaml:"appliesToInterfaceType"`
	Schema                 any    `yaml:"schema"`
}

type bindingDocument struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Extension           string `yaml:"extension"`
		ExtensionDefinition string `yaml:"extensionDefinition"`
		Package             string `yaml:"package"`
		Language            string `yaml:"language"`
	} `yaml:"metadata"`
	Extension struct {
		ID         string `yaml:"id"`
		Definition string `yaml:"definition"`
	} `yaml:"extension"`
	Go   bindingGo   `yaml:"go"`
	Java bindingJava `yaml:"java"`
}

type profileDocument struct {
	APIVersion string             `yaml:"apiVersion"`
	Kind       string             `yaml:"kind"`
	Extensions []string           `yaml:"extensions"`
	Conditions []profileCondition `yaml:"conditions"`
}

type profileCondition struct {
	Name          string                `yaml:"name"`
	Kind          string                `yaml:"kind"`
	Interface     profileInterface      `yaml:"interface"`
	Optional      bool                  `yaml:"optional"`
	Configuration *profileConfiguration `yaml:"configuration"`
}

type profileInterface struct {
	Type        string             `yaml:"type"`
	Engine      string             `yaml:"engine"`
	BucketClass string             `yaml:"bucketClass"`
	Spec        *profileAPISpec    `yaml:"spec"`
	Operations  []profileOperation `yaml:"operations"`
	Subjects    []profileSubject   `yaml:"subjects"`
}

type profileAPISpec struct {
	Format  string `yaml:"format"`
	URI     string `yaml:"uri"`
	Version string `yaml:"version"`
}

type profileOperation struct {
	Method            string `yaml:"method"`
	Path              string `yaml:"path"`
	RequestBodySchema any    `yaml:"requestBodySchema"`
	ResponseSchema    any    `yaml:"responseSchema"`
}

type profileSubject struct {
	Name          string `yaml:"name"`
	Direction     string `yaml:"direction"`
	PayloadSchema any    `yaml:"payloadSchema"`
}

type profileConfiguration struct {
	Env          []profileEnvInput                 `yaml:"env"`
	Alternatives []profileConfigurationAlternative `yaml:"alternatives"`
}

type profileConfigurationAlternative struct {
	Env []profileEnvInput `yaml:"env"`
}

type profileEnvInput struct {
	Property string `yaml:"property"`
	Name     string `yaml:"name"`
}

type bindingGo struct {
	ImportPath   string               `yaml:"importPath"`
	Package      string               `yaml:"package"`
	Constants    map[string]string    `yaml:"constants"`
	Constructors []bindingConstructor `yaml:"constructors"`
	Declarations []bindingDeclaration `yaml:"declarations"`
	Options      []bindingOption      `yaml:"options"`
}

type bindingJava struct {
	Package      string               `yaml:"package"`
	Class        string               `yaml:"class"`
	Constants    map[string]string    `yaml:"constants"`
	Declarations []bindingDeclaration `yaml:"declarations"`
	Options      []bindingOption      `yaml:"options"`
}

type bindingConstructor struct {
	Function string `yaml:"function"`
	Receiver string `yaml:"receiver"`
}

type bindingDeclaration struct {
	Class         string         `yaml:"class"`
	Function      string         `yaml:"function"`
	Receiver      string         `yaml:"receiver"`
	Method        string         `yaml:"method"`
	Name          string         `yaml:"name"`
	Kind          string         `yaml:"kind"`
	InterfaceType string         `yaml:"interfaceType"`
	NameArg       *int           `yaml:"nameArg"`
	Values        []bindingValue `yaml:"values"`
	Options       []bindingOption
}

type bindingValue struct {
	Target string `yaml:"target"`
	Value  string `yaml:"value"`
}

type bindingOption struct {
	Class                   string            `yaml:"class"`
	Function                string            `yaml:"function"`
	Target                  string            `yaml:"target"`
	Value                   string            `yaml:"value"`
	ValueArg                *int              `yaml:"valueArg"`
	TypeArg                 *int              `yaml:"typeArg"`
	EngineArg               *int              `yaml:"engineArg"`
	EnumArg                 *int              `yaml:"enumArg"`
	ClassArg                *int              `yaml:"classArg"`
	Method                  string            `yaml:"method"`
	AppliesToKinds          []string          `yaml:"appliesToKinds"`
	AppliesToInterfaceTypes []string          `yaml:"appliesToInterfaceTypes"`
	StringArgs              map[string]int    `yaml:"stringArgs"`
	Options                 []bindingOption   `yaml:"options"`
	Configuration           any               `yaml:"configuration"`
	Values                  []bindingValue    `yaml:"values"`
	Declarations            []bindingDocument `yaml:"declarations"`
}

type goPackage struct {
	name      string
	funcs     map[string]goFunc
	methods   map[string]goFunc
	constants map[string]string
}

type goFunc struct {
	name           string
	params         []goParam
	typeParamCount int
}

type javaFunc struct {
	name   string
	params []javaParam
}

type javaParam struct {
	name     string
	typ      string
	variadic bool
}

type javaClass struct {
	name      string
	packageID string
	methods   map[string][]javaFunc
	constants map[string]string
}

type goParam struct {
	name     string
	typ      string
	variadic bool
}

type scope struct {
	kind          string
	interfaceType string
}

func (v *validator) loadCatalog(roots []string) error {
	v.nodes = make(map[string]*extensionNode)
	v.states = make(map[string]int)
	seenRoots := make(map[string]bool)
	seenFiles := make(map[string]bool)
	for _, root := range roots {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return err
		}
		if seenRoots[absRoot] {
			continue
		}
		seenRoots[absRoot] = true
		err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				switch d.Name() {
				case ".git", "vendor", "node_modules":
					return filepath.SkipDir
				}
				return nil
			}
			if seenFiles[path] || !isYAML(path) {
				return nil
			}
			seenFiles[path] = true
			definition, ok, err := readExtensionDefinition(path)
			if err != nil {
				v.addf(path, "%v", err)
				return nil
			}
			if !ok {
				return nil
			}
			id := definitionID(definition)
			if id == "" {
				v.addf(path, "metadata.id is required")
				return nil
			}
			if existing := v.nodes[id]; existing != nil {
				v.addf(path, "duplicate extension id %s already defined by %s", id, existing.DefinitionPath)
				return nil
			}
			node := &extensionNode{
				ID:             id,
				Dir:            filepath.Dir(path),
				DefinitionPath: path,
				Definition:     definition,
			}
			v.discoverLanguageBinding(node)
			v.nodes[id] = node
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (v *validator) discoverLanguageBinding(node *extensionNode) {
	switch v.opts.Language {
	case "go":
		v.discoverBinding(node, "go")
	case "java":
		v.discoverBinding(node, "java")
	}
}

func (v *validator) discoverGoBinding(node *extensionNode) {
	v.discoverBinding(node, "go")
}

func (v *validator) discoverBinding(node *extensionNode, language string) {
	dir := filepath.Join(node.Dir, language)
	info, err := os.Stat(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			v.addf(dir, "%v", err)
		}
		return
	}
	if !info.IsDir() {
		v.addf(dir, "expected language package path to be a directory")
		return
	}
	node.BindingDir = dir
	if language == "go" {
		node.GoDir = dir
	}
	manifest, ok, err := findBindingManifest(dir)
	if err != nil {
		v.addf(dir, "%v", err)
		return
	}
	if !ok {
		v.addf(dir, "missing %s", goBindingsManifest)
		return
	}
	binding, err := readBindingDocument(manifest)
	if err != nil {
		v.addf(manifest, "%v", err)
		return
	}
	node.BindingPath = manifest
	node.Binding = binding
}

func (v *validator) targets(root string, targetOnly bool) []string {
	var ids []string
	for id, node := range v.nodes {
		if !targetOnly || pathWithin(root, node.DefinitionPath) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

func (v *validator) validateNode(id string) {
	switch v.states[id] {
	case 1:
		v.addf(id, "extension dependency cycle includes %s", id)
		return
	case 2:
		return
	}
	node := v.nodes[id]
	if node == nil {
		v.addf(id, "missing extension definition for dependency %s", id)
		return
	}
	v.states[id] = 1
	for _, dep := range node.Definition.Spec.Dependencies {
		v.validateNode(dep)
	}
	v.validateDefinition(node)
	resolved := v.resolvedVocabulary(id)
	v.validateVocabulary(node, resolved)
	v.validateBinding(node, resolved)
	v.validateGoDeclarations(node)
	v.validateJavaDeclarations(node)
	v.states[id] = 2
}

func (v *validator) validateDefinition(node *extensionNode) {
	def := node.Definition
	if def.APIVersion != "runtimeconditions.io/v1alpha1" {
		v.addf(node.DefinitionPath, "apiVersion must be runtimeconditions.io/v1alpha1")
	}
	if def.Kind != "RuntimeConditionsExtensionDefinition" {
		v.addf(node.DefinitionPath, "kind must be RuntimeConditionsExtensionDefinition")
	}
	if def.Metadata.ID == "" {
		v.addf(node.DefinitionPath, "metadata.id is required")
	} else if !validExtensionID(def.Metadata.ID) {
		v.addf(node.DefinitionPath, "metadata.id must be an absolute HTTP or HTTPS URI")
	}
	if node.ID != definitionID(def) {
		v.addf(node.DefinitionPath, "extension id %s does not match metadata", node.ID)
	}
	if len(def.Spec.Dependencies) == 0 &&
		len(def.Spec.Kinds) == 0 &&
		len(def.Spec.InterfaceTypes) == 0 &&
		len(def.Spec.ConditionFields) == 0 &&
		len(def.Spec.InterfaceFields) == 0 &&
		len(def.Spec.FieldValues) == 0 &&
		len(def.Spec.Schemas) == 0 {
		v.addf(node.DefinitionPath, "spec must define at least one vocabulary item or schema")
	}
	for _, dep := range def.Spec.Dependencies {
		if !validExtensionID(dep) {
			v.addf(node.DefinitionPath, "invalid dependency extension id %q", dep)
		}
		if v.nodes[dep] == nil {
			v.addf(node.DefinitionPath, "dependency %s cannot be resolved", dep)
		}
	}
	v.checkSelfDuplicates(node)
}

func (v *validator) checkSelfDuplicates(node *extensionNode) {
	seen := make(map[string]bool)
	check := func(key string) {
		if seen[key] {
			v.addf(node.DefinitionPath, "duplicate vocabulary definition %s", key)
		}
		seen[key] = true
	}
	for _, kind := range node.Definition.Spec.Kinds {
		if kind.Name == "" {
			v.addf(node.DefinitionPath, "kind name is required")
			continue
		}
		check("kind:" + kind.Name)
	}
	for _, item := range node.Definition.Spec.InterfaceTypes {
		if item.Name == "" || item.TargetKind == "" {
			v.addf(node.DefinitionPath, "interfaceTypes entries require name and targetKind")
			continue
		}
		check("interfaceType:" + item.TargetKind + ":" + item.Name)
	}
	for _, item := range node.Definition.Spec.ConditionFields {
		if item.Name == "" {
			v.addf(node.DefinitionPath, "conditionFields entries require name")
			continue
		}
		if len(item.AppliesToKinds) == 0 {
			v.addf(node.DefinitionPath, "condition field %s must declare appliesToKinds", item.Name)
		}
		check("conditionField:" + item.Name + ":" + strings.Join(item.AppliesToKinds, ",") + ":" + strings.Join(item.AppliesToInterfaceTypes, ","))
	}
	for _, item := range node.Definition.Spec.InterfaceFields {
		if item.Name == "" || item.TargetKind == "" || item.TargetType == "" {
			v.addf(node.DefinitionPath, "interfaceFields entries require name, targetKind, and targetType")
			continue
		}
		check("interfaceField:" + item.TargetKind + ":" + item.TargetType + ":" + item.Name)
	}
	for _, item := range node.Definition.Spec.FieldValues {
		if item.Field == "" || item.TargetKind == "" || len(item.Values) == 0 {
			v.addf(node.DefinitionPath, "fieldValues entries require field, targetKind, and values")
			continue
		}
		check("fieldValue:" + item.TargetKind + ":" + item.TargetType + ":" + item.Field)
		valueSeen := make(map[string]bool)
		for _, value := range item.Values {
			if valueSeen[value] {
				v.addf(node.DefinitionPath, "duplicate field value %q for %s", value, item.Field)
			}
			valueSeen[value] = true
		}
	}
	for _, schema := range node.Definition.Spec.Schemas {
		if schema.ID == "" {
			v.addf(node.DefinitionPath, "schema id is required")
		}
		if schema.Description == "" {
			v.addf(node.DefinitionPath, "schema %s description is required", schema.ID)
		}
		if schema.Schema == nil {
			v.addf(node.DefinitionPath, "schema %s schema object is required", schema.ID)
		}
	}
}

func (v *validator) validateVocabulary(node *extensionNode, resolved vocabulary) {
	for _, item := range node.Definition.Spec.InterfaceTypes {
		v.expectExactlyOne(node.DefinitionPath, resolved.kindCount(item.TargetKind), "interface type %s targetKind %s", item.Name, item.TargetKind)
	}
	for _, item := range node.Definition.Spec.InterfaceFields {
		v.expectExactlyOne(node.DefinitionPath, resolved.kindCount(item.TargetKind), "interface field %s targetKind %s", item.Name, item.TargetKind)
		v.expectExactlyOne(node.DefinitionPath, resolved.interfaceTypeCount(item.TargetKind, item.TargetType), "interface field %s targetType %s/%s", item.Name, item.TargetKind, item.TargetType)
	}
	for _, item := range node.Definition.Spec.ConditionFields {
		for _, kind := range item.AppliesToKinds {
			v.expectExactlyOne(node.DefinitionPath, resolved.kindCount(kind), "condition field %s appliesToKind %s", item.Name, kind)
			for _, targetType := range item.AppliesToInterfaceTypes {
				v.expectExactlyOne(node.DefinitionPath, resolved.interfaceTypeCount(kind, targetType), "condition field %s appliesToInterfaceType %s/%s", item.Name, kind, targetType)
			}
		}
	}
	for _, item := range node.Definition.Spec.FieldValues {
		v.expectExactlyOne(node.DefinitionPath, resolved.kindCount(item.TargetKind), "field values %s targetKind %s", item.Field, item.TargetKind)
		if item.TargetType != "" {
			v.expectExactlyOne(node.DefinitionPath, resolved.interfaceTypeCount(item.TargetKind, item.TargetType), "field values %s targetType %s/%s", item.Field, item.TargetKind, item.TargetType)
		}
		v.validateFieldValuePath(node.DefinitionPath, resolved, item)
	}
	for _, schema := range node.Definition.Spec.Schemas {
		if schema.AppliesToKind != "" {
			v.expectExactlyOne(node.DefinitionPath, resolved.kindCount(schema.AppliesToKind), "schema %s appliesToKind %s", schema.ID, schema.AppliesToKind)
		}
		if schema.AppliesToKind != "" && schema.AppliesToInterfaceType != "" {
			v.expectExactlyOne(node.DefinitionPath, resolved.interfaceTypeCount(schema.AppliesToKind, schema.AppliesToInterfaceType), "schema %s appliesToInterfaceType %s/%s", schema.ID, schema.AppliesToKind, schema.AppliesToInterfaceType)
		}
	}
	v.checkResolvedConflicts(node.DefinitionPath, resolved)
}

func (v *validator) validateFieldValuePath(path string, resolved vocabulary, item extensionFieldValue) {
	switch {
	case strings.HasPrefix(item.Field, "interface."):
		field := firstPathSegment(strings.TrimPrefix(item.Field, "interface."))
		if field == "type" {
			return
		}
		v.expectExactlyOne(path, resolved.interfaceFieldCount(item.TargetKind, item.TargetType, field), "fieldValues field %s interface field %s/%s/%s", item.Field, item.TargetKind, item.TargetType, field)
	case strings.HasPrefix(item.Field, "configuration."):
		v.expectExactlyOne(path, resolved.conditionFieldCount(item.TargetKind, item.TargetType, "configuration"), "fieldValues field %s condition field configuration for %s/%s", item.Field, item.TargetKind, item.TargetType)
	default:
		field := firstPathSegment(item.Field)
		v.expectExactlyOne(path, resolved.conditionFieldCount(item.TargetKind, item.TargetType, field), "fieldValues field %s condition field %s for %s/%s", item.Field, field, item.TargetKind, item.TargetType)
	}
}

func (v *validator) checkResolvedConflicts(path string, resolved vocabulary) {
	for key, count := range resolved.counts() {
		if count > 1 {
			v.addf(path, "resolved extension set contains vocabulary conflict for %s", key)
		}
	}
	for _, conflict := range resolved.conditionFieldConflicts() {
		v.addf(path, "resolved extension set contains vocabulary conflict for %s", conflict)
	}
}

func (v *validator) validateBinding(node *extensionNode, resolved vocabulary) {
	if node.Binding == nil {
		if node.BindingDir != "" || v.opts.RequireLanguagePackage {
			v.addf(node.Dir, "missing %s binding manifest", languageDisplayName(v.opts.Language))
		}
		return
	}
	binding := node.Binding
	if binding.APIVersion != "runtimeconditions.io/v1alpha1" {
		v.addf(node.BindingPath, "apiVersion must be runtimeconditions.io/v1alpha1")
	}
	if binding.Kind != "RuntimeConditionsBinding" && binding.Kind != "RuntimeConditionsPackage" {
		v.addf(node.BindingPath, "kind must be RuntimeConditionsBinding or RuntimeConditionsPackage")
	}
	if binding.Kind == "RuntimeConditionsPackage" && filepath.Base(node.BindingPath) == goBindingsManifest {
		v.addf(node.BindingPath, "kind RuntimeConditionsBinding is required for %s", goBindingsManifest)
	}
	if binding.Kind == "RuntimeConditionsBinding" && filepath.Base(node.BindingPath) == goPackageBindingManifest {
		v.addf(node.BindingPath, "kind RuntimeConditionsPackage is required for %s", goPackageBindingManifest)
	}
	if binding.Metadata.Language == "" {
		v.addf(node.BindingPath, "metadata.language is required")
	} else if binding.Metadata.Language != v.opts.Language {
		v.addf(node.BindingPath, "metadata.language must be %s", v.opts.Language)
	}
	bindingID := binding.extensionID()
	if bindingID == "" {
		v.addf(node.BindingPath, "extension id is required")
	} else if bindingID != node.ID {
		v.addf(node.BindingPath, "binding extension id %s does not match extension definition %s", bindingID, node.ID)
	}
	if definition := binding.extensionDefinitionPath(); definition != "" {
		resolvedPath := definition
		if !filepath.IsAbs(resolvedPath) {
			resolvedPath = filepath.Join(filepath.Dir(node.BindingPath), resolvedPath)
		}
		if absDefinition, err := filepath.Abs(resolvedPath); err != nil {
			v.addf(node.BindingPath, "%v", err)
		} else if absNodeDefinition, err := filepath.Abs(node.DefinitionPath); err != nil {
			v.addf(node.DefinitionPath, "%v", err)
		} else if filepath.Clean(absDefinition) != filepath.Clean(absNodeDefinition) {
			v.addf(node.BindingPath, "binding extension definition %s does not match %s", absDefinition, absNodeDefinition)
		}
	}

	switch v.opts.Language {
	case "go":
		v.validateGoBinding(node, resolved)
	case "java":
		v.validateJavaBinding(node, resolved)
	default:
		v.addf(node.BindingPath, "unsupported binding language %s", v.opts.Language)
	}
}

func (v *validator) validateGoBinding(node *extensionNode, resolved vocabulary) {
	binding := node.Binding
	if binding.Go.ImportPath == "" {
		v.addf(node.BindingPath, "go.importPath is required")
	}
	if binding.Go.Package == "" {
		v.addf(node.BindingPath, "go.package is required")
	}
	if len(binding.Go.Declarations) == 0 && len(binding.Go.Options) == 0 {
		v.addf(node.BindingPath, "go.declarations or go.options must not be empty")
	}
	for name, value := range binding.Go.Constants {
		if resolved.fieldValueValueCount(value) == 0 {
			v.addf(node.BindingPath, "constant %s value %q is not defined by resolved field values", name, value)
		}
	}
	for _, declaration := range binding.Go.Declarations {
		v.validateBindingDeclaration(node.BindingPath, resolved, declaration)
	}
	for _, option := range binding.Go.Options {
		v.validateBindingOption(node.BindingPath, resolved, option, scopesFromOption(option, resolved))
	}
}

func (v *validator) validateJavaBinding(node *extensionNode, resolved vocabulary) {
	binding := node.Binding
	if binding.Java.Package == "" {
		v.addf(node.BindingPath, "java.package is required")
	}
	if binding.Java.Class != "" {
		v.addf(node.BindingPath, "top-level java.class must not be used; Java bindings use per-entry class")
	}
	if len(binding.Java.Declarations) == 0 && len(binding.Java.Options) == 0 {
		v.addf(node.BindingPath, "java.declarations or java.options must not be empty")
	}
	for name, value := range binding.Java.Constants {
		if resolved.fieldValueValueCount(value) == 0 {
			v.addf(node.BindingPath, "constant %s value %q is not defined by resolved field values", name, value)
		}
	}
	for _, declaration := range binding.Java.Declarations {
		v.validateBindingDeclaration(node.BindingPath, resolved, declaration)
		v.validateJavaDeclarationShape(node.BindingPath, declaration)
	}
	for _, option := range binding.Java.Options {
		v.validateBindingOption(node.BindingPath, resolved, option, scopesFromOption(option, resolved))
		v.validateJavaOptionShape(node.BindingPath, option)
	}
}

func (v *validator) validateBindingDeclaration(path string, resolved vocabulary, declaration bindingDeclaration) {
	if declaration.Function == "" && declaration.Method == "" {
		v.addf(path, "declaration must specify function or method")
	}
	v.expectExactlyOne(path, resolved.kindCount(declaration.Kind), "declaration kind %s", declaration.Kind)
	if declaration.InterfaceType != "" {
		v.expectExactlyOne(path, resolved.interfaceTypeCount(declaration.Kind, declaration.InterfaceType), "declaration interfaceType %s/%s", declaration.Kind, declaration.InterfaceType)
	}
	for _, value := range declaration.Values {
		v.validateBindingValue(path, resolved, declaration.Kind, declaration.InterfaceType, value)
	}
	for _, option := range declaration.Options {
		v.validateBindingOption(path, resolved, option, []scope{{kind: declaration.Kind, interfaceType: declaration.InterfaceType}})
	}
}

func (v *validator) validateBindingValue(path string, resolved vocabulary, kind string, interfaceType string, value bindingValue) {
	switch value.Target {
	case "interface.bucketClass":
		v.expectExactlyOne(path, resolved.interfaceFieldCount(kind, interfaceType, "bucketClass"), "binding value target %s for %s/%s", value.Target, kind, interfaceType)
		v.expectExactlyOne(path, resolved.fieldValueCount("interface.bucketClass", kind, interfaceType, value.Value), "binding value %s=%s for %s/%s", value.Target, value.Value, kind, interfaceType)
	default:
		v.addf(path, "unsupported binding value target %s", value.Target)
	}
}

func (v *validator) validateBindingOption(path string, resolved vocabulary, option bindingOption, optionScopes []scope) {
	switch option.Target {
	case "interface.spec":
		for _, scope := range optionScopes {
			v.expectExactlyOne(path, resolved.interfaceFieldCount(scope.kind, scope.interfaceType, "spec"), "binding option %s for %s/%s", option.Target, scope.kind, scope.interfaceType)
		}
	case "interface.operations[]":
		for _, scope := range optionScopes {
			v.expectExactlyOne(path, resolved.interfaceFieldCount(scope.kind, scope.interfaceType, "operations"), "binding option %s for %s/%s", option.Target, scope.kind, scope.interfaceType)
			v.expectExactlyOne(path, resolved.fieldValueCount("interface.operations[].method", scope.kind, scope.interfaceType, option.Method), "binding option method %s for %s/%s", option.Method, scope.kind, scope.interfaceType)
		}
	case "interface.type":
		for i, scope := range optionScopes {
			v.expectExactlyOne(path, resolved.interfaceTypeCount(scope.kind, option.Value), "binding option interface.type %s/%s", scope.kind, option.Value)
			optionScopes[i].interfaceType = option.Value
			if option.EngineArg != nil {
				v.expectExactlyOne(path, resolved.interfaceFieldCount(scope.kind, option.Value, "engine"), "binding option engine for %s/%s", scope.kind, option.Value)
			}
		}
	case "configuration.env[]":
		v.validateConfigurationBindingOption(path, resolved, optionScopes, "configuration.env[].property")
	case "configuration.alternatives[]":
		v.validateConfigurationBindingOption(path, resolved, optionScopes, "configuration.alternatives[].env[].property")
	case "requestBodySchema", "responseSchema", "env.sensitive", "env.required":
	case "":
		v.addf(path, "binding option %s is missing target", option.Function)
	default:
		v.addf(path, "unsupported binding option target %s", option.Target)
	}
	for _, nested := range option.Options {
		v.validateBindingOption(path, resolved, nested, optionScopes)
	}
}

func (v *validator) validateJavaDeclarationShape(path string, declaration bindingDeclaration) {
	if declaration.Class == "" {
		v.addf(path, "Java declaration function %s is missing class", declaration.Function)
	}
	if declaration.Function == "" {
		v.addf(path, "Java declaration for class %s is missing function", declaration.Class)
	}
	if declaration.Method != "" {
		v.addf(path, "Java declaration %s.%s must use function, not method", declaration.Class, declaration.Method)
	}
	for _, option := range declaration.Options {
		v.validateJavaOptionShape(path, option)
	}
}

func (v *validator) validateJavaOptionShape(path string, option bindingOption) {
	if option.Class == "" {
		v.addf(path, "Java option function %s is missing class", option.Function)
	}
	if option.Function == "" {
		v.addf(path, "Java option for class %s is missing function", option.Class)
	}
	for _, nested := range option.Options {
		v.validateJavaOptionShape(path, nested)
	}
}

func (v *validator) validateConfigurationBindingOption(path string, resolved vocabulary, optionScopes []scope, propertyField string) {
	if len(optionScopes) == 0 {
		v.addf(path, "configuration binding option requires appliesToKinds/appliesToInterfaceTypes or a declaration scope")
		return
	}
	for _, scope := range optionScopes {
		v.expectExactlyOne(path, resolved.conditionFieldCount(scope.kind, scope.interfaceType, "configuration"), "binding option configuration for %s/%s", scope.kind, scope.interfaceType)
		v.expectExactlyOne(path, resolved.fieldValueDefinitionCount(propertyField, scope.kind, scope.interfaceType), "binding option property field %s for %s/%s", propertyField, scope.kind, scope.interfaceType)
	}
}

func (v *validator) validateGoDeclarations(node *extensionNode) {
	if v.opts.Language != "go" || node.Binding == nil {
		return
	}
	pkg, err := readGoPackage(node.GoDir)
	if err != nil {
		v.addf(node.GoDir, "%v", err)
		return
	}
	if node.Binding.Go.Package != "" && pkg.name != node.Binding.Go.Package {
		v.addf(node.GoDir, "go package name %s does not match binding package %s", pkg.name, node.Binding.Go.Package)
	}
	for name, value := range node.Binding.Go.Constants {
		actual, ok := pkg.constants[name]
		if !ok {
			v.addf(node.GoDir, "binding constant %s is not declared in Go package", name)
			continue
		}
		if actual != value {
			v.addf(node.GoDir, "binding constant %s value %q does not match Go value %q", name, value, actual)
		}
	}
	for _, declaration := range node.Binding.Go.Declarations {
		if declaration.Function != "" {
			fn, ok := pkg.funcs[declaration.Function]
			if !ok {
				v.addf(node.GoDir, "binding declaration function %s is not declared in Go package", declaration.Function)
			} else {
				v.validateFunctionIndexes(node.GoDir, fn, declaration.NameArg, nil, nil, nil)
			}
		}
		if declaration.Method != "" {
			key := declaration.Receiver + "." + declaration.Method
			if _, ok := pkg.methods[key]; !ok {
				v.addf(node.GoDir, "binding declaration method %s is not declared in Go package", key)
			}
		}
		for _, option := range declaration.Options {
			v.validateGoOption(node.GoDir, pkg, option)
		}
	}
	for _, option := range node.Binding.Go.Options {
		v.validateGoOption(node.GoDir, pkg, option)
	}
	for _, constructor := range node.Binding.Go.Constructors {
		if constructor.Function == "" || constructor.Receiver == "" {
			v.addf(node.GoDir, "constructors require function and receiver")
			continue
		}
		if _, ok := pkg.funcs[constructor.Function]; !ok {
			v.addf(node.GoDir, "binding constructor function %s is not declared in Go package", constructor.Function)
		}
	}
}

func (v *validator) validateGoOption(path string, pkg goPackage, option bindingOption) {
	fn, ok := pkg.funcs[option.Function]
	if !ok {
		v.addf(path, "binding option function %s is not declared in Go package", option.Function)
		return
	}
	v.validateFunctionIndexes(path, fn, nil, option.StringArgs, option.ValueArg, option.EngineArg)
	if option.TypeArg != nil && *option.TypeArg >= fn.typeParamCount {
		v.addf(path, "binding option %s typeArg %d is out of range", option.Function, *option.TypeArg)
	}
	for _, nested := range option.Options {
		v.validateGoOption(path, pkg, nested)
	}
}

func (v *validator) validateFunctionIndexes(path string, fn goFunc, nameArg *int, stringArgs map[string]int, valueArg *int, engineArg *int) {
	if nameArg != nil {
		v.validateParamIndex(path, fn, *nameArg, "nameArg", true)
	}
	for name, index := range stringArgs {
		v.validateParamIndex(path, fn, index, "stringArgs."+name, true)
	}
	if valueArg != nil {
		v.validateParamIndex(path, fn, *valueArg, "valueArg", false)
	}
	if engineArg != nil {
		v.validateParamIndex(path, fn, *engineArg, "engineArg", false)
	}
}

func (v *validator) validateParamIndex(path string, fn goFunc, index int, field string, requireString bool) {
	if index < 0 || index >= len(fn.params) {
		v.addf(path, "binding %s for function %s index %d is out of range", field, fn.name, index)
		return
	}
	param := fn.params[index]
	if requireString && param.typ != "string" {
		v.addf(path, "binding %s for function %s points at non-string parameter %s %s", field, fn.name, param.name, param.typ)
	}
}

func (v *validator) validateJavaDeclarations(node *extensionNode) {
	if v.opts.Language != "java" || node.Binding == nil {
		return
	}
	binding := node.Binding
	if node.BindingDir == "" {
		return
	}
	for name, value := range binding.Java.Constants {
		className, constantName, ok := javaConstantParts(name)
		if !ok {
			v.addf(node.BindingDir, "Java constant %s must include class and constant name", name)
			continue
		}
		class, ok := v.readJavaBindingClass(node, className)
		if !ok {
			continue
		}
		actual, ok := class.constants[constantName]
		if !ok {
			v.addf(node.BindingDir, "binding constant %s is not declared in Java class %s", name, className)
			continue
		}
		if actual != value {
			v.addf(node.BindingDir, "binding constant %s value %q does not match Java value %q", name, value, actual)
		}
	}
	for _, declaration := range binding.Java.Declarations {
		v.validateJavaDeclarationSource(node, declaration)
	}
	for _, option := range binding.Java.Options {
		v.validateJavaOptionSource(node, option)
	}
}

func (v *validator) validateJavaDeclarationSource(node *extensionNode, declaration bindingDeclaration) {
	if declaration.Class == "" || declaration.Function == "" {
		return
	}
	class, ok := v.readJavaBindingClass(node, declaration.Class)
	if !ok {
		return
	}
	methods := class.methods[declaration.Function]
	if len(methods) == 0 {
		v.addf(node.BindingDir, "binding declaration function %s.%s is not declared in Java package", declaration.Class, declaration.Function)
		return
	}
	v.validateJavaFunctionIndexes(node.BindingDir, methods[0], declaration.NameArg, nil, nil, nil, nil)
	for _, option := range declaration.Options {
		v.validateJavaOptionSource(node, option)
	}
}

func (v *validator) validateJavaOptionSource(node *extensionNode, option bindingOption) {
	if option.Class == "" || option.Function == "" {
		return
	}
	class, ok := v.readJavaBindingClass(node, option.Class)
	if !ok {
		return
	}
	methods := class.methods[option.Function]
	if len(methods) == 0 {
		v.addf(node.BindingDir, "binding option function %s.%s is not declared in Java package", option.Class, option.Function)
		return
	}
	v.validateJavaFunctionIndexes(node.BindingDir, methods[0], nil, option.StringArgs, option.ValueArg, option.EnumArg, option.ClassArg)
	for _, nested := range option.Options {
		v.validateJavaOptionSource(node, nested)
	}
}

func (v *validator) readJavaBindingClass(node *extensionNode, className string) (javaClass, bool) {
	class, err := readJavaClass(node.BindingDir, node.Binding.Java.Package, className)
	if err != nil {
		v.addf(node.BindingDir, "%v", err)
		return javaClass{}, false
	}
	if class.packageID != node.Binding.Java.Package {
		v.addf(node.BindingDir, "Java package %s does not match binding package %s", class.packageID, node.Binding.Java.Package)
		return javaClass{}, false
	}
	return class, true
}

func (v *validator) validateJavaFunctionIndexes(path string, fn javaFunc, nameArg *int, stringArgs map[string]int, valueArg *int, enumArg *int, classArg *int) {
	if nameArg != nil {
		v.validateJavaParamIndex(path, fn, *nameArg, "nameArg", "String")
	}
	for name, index := range stringArgs {
		v.validateJavaParamIndex(path, fn, index, "stringArgs."+name, "String")
	}
	if valueArg != nil {
		v.validateJavaParamIndex(path, fn, *valueArg, "valueArg", "")
	}
	if enumArg != nil {
		v.validateJavaParamIndex(path, fn, *enumArg, "enumArg", "")
	}
	if classArg != nil {
		v.validateJavaParamIndex(path, fn, *classArg, "classArg", "Class")
	}
}

func (v *validator) validateJavaParamIndex(path string, fn javaFunc, index int, field string, expectedType string) {
	if index < 0 || index >= len(fn.params) {
		v.addf(path, "binding %s for Java function %s index %d is out of range", field, fn.name, index)
		return
	}
	if expectedType == "" {
		return
	}
	param := fn.params[index]
	if !javaTypeMatches(param.typ, expectedType) {
		v.addf(path, "binding %s for Java function %s points at non-%s parameter %s %s", field, fn.name, expectedType, param.name, param.typ)
	}
}

func readExtensionDefinition(path string) (extensionDefinition, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return extensionDefinition{}, false, err
	}
	var probe struct {
		Kind string `yaml:"kind"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return extensionDefinition{}, false, err
	}
	if probe.Kind != "RuntimeConditionsExtensionDefinition" {
		return extensionDefinition{}, false, nil
	}
	var definition extensionDefinition
	if err := yaml.Unmarshal(data, &definition); err != nil {
		return extensionDefinition{}, false, err
	}
	return definition, true, nil
}

func readBindingDocument(path string) (*bindingDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var document bindingDocument
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	return &document, nil
}

func readJavaClass(dir string, packageID string, className string) (javaClass, error) {
	if className == "" {
		return javaClass{}, fmt.Errorf("Java class name is required")
	}
	if strings.ContainsAny(className, ".$/") {
		return javaClass{}, fmt.Errorf("Java binding class %s must be a top-level class name", className)
	}
	sourceRoot := filepath.Join(dir, "src", "main", "java")
	if packageID != "" {
		sourceRoot = filepath.Join(sourceRoot, filepath.Join(strings.Split(packageID, ".")...))
	}
	path := filepath.Join(sourceRoot, className+".java")
	data, err := os.ReadFile(path)
	if err != nil {
		return javaClass{}, err
	}
	return parseJavaClassSource(className, string(data)), nil
}

func parseJavaClassSource(className string, source string) javaClass {
	class := javaClass{
		name:      className,
		methods:   make(map[string][]javaFunc),
		constants: make(map[string]string),
	}
	if match := regexp.MustCompile(`(?m)^\s*package\s+([A-Za-z_][A-Za-z0-9_.]*)\s*;`).FindStringSubmatch(source); len(match) == 2 {
		class.packageID = match[1]
	}
	methodPattern := regexp.MustCompile(`(?m)\bpublic\s+static\s+(?:<[^>]+>\s+)?[A-Za-z0-9_.$<>\[\]?]+\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(([^)]*)\)`)
	for _, match := range methodPattern.FindAllStringSubmatch(source, -1) {
		if len(match) != 3 {
			continue
		}
		fn := javaFunc{
			name:   match[1],
			params: parseJavaParams(match[2]),
		}
		class.methods[fn.name] = append(class.methods[fn.name], fn)
	}
	enumPattern := regexp.MustCompile(`(?m)\b([A-Z][A-Z0-9_]*)\s*\(\s*"([^"]*)"\s*\)`)
	for _, match := range enumPattern.FindAllStringSubmatch(source, -1) {
		if len(match) == 3 {
			class.constants[match[1]] = match[2]
		}
	}
	stringConstantPattern := regexp.MustCompile(`(?m)\b([A-Z][A-Z0-9_]*)\s*=\s*"([^"]*)"`)
	for _, match := range stringConstantPattern.FindAllStringSubmatch(source, -1) {
		if len(match) == 3 {
			class.constants[match[1]] = match[2]
		}
	}
	return class
}

func parseJavaParams(params string) []javaParam {
	params = strings.TrimSpace(params)
	if params == "" {
		return nil
	}
	parts := splitJavaParams(params)
	result := make([]javaParam, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		part = strings.TrimPrefix(part, "final ")
		if part == "" {
			continue
		}
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		name := fields[len(fields)-1]
		typ := strings.TrimSpace(strings.TrimSuffix(part[:len(part)-len(name)], " "))
		variadic := strings.Contains(typ, "...")
		typ = strings.ReplaceAll(typ, "...", "")
		result = append(result, javaParam{name: name, typ: strings.TrimSpace(typ), variadic: variadic})
	}
	return result
}

func splitJavaParams(params string) []string {
	var parts []string
	start := 0
	depth := 0
	for index, r := range params {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, params[start:index])
				start = index + len(string(r))
			}
		}
	}
	parts = append(parts, params[start:])
	return parts
}

func readGoPackage(dir string) (goPackage, error) {
	fset := token.NewFileSet()
	pkg := goPackage{
		funcs:     make(map[string]goFunc),
		methods:   make(map[string]goFunc),
		constants: make(map[string]string),
	}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		if pkg.name == "" {
			pkg.name = file.Name.Name
		} else if pkg.name != file.Name.Name {
			return fmt.Errorf("mixed package names %s and %s", pkg.name, file.Name.Name)
		}
		collectGoFile(file, pkg)
		return nil
	})
	return pkg, err
}

func collectGoFile(file *ast.File, pkg goPackage) {
	for _, decl := range file.Decls {
		switch typed := decl.(type) {
		case *ast.FuncDecl:
			fn := goFunc{
				name:           typed.Name.Name,
				params:         functionParams(typed.Type),
				typeParamCount: typeParamCount(typed.Type),
			}
			if typed.Recv == nil || len(typed.Recv.List) == 0 {
				pkg.funcs[typed.Name.Name] = fn
			} else {
				receiver := receiverTypeName(typed.Recv.List[0].Type)
				pkg.methods[receiver+"."+typed.Name.Name] = fn
			}
		case *ast.GenDecl:
			if typed.Tok != token.CONST {
				continue
			}
			for _, spec := range typed.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range valueSpec.Names {
					if i >= len(valueSpec.Values) {
						continue
					}
					value, ok := stringLiteral(valueSpec.Values[i])
					if ok {
						pkg.constants[name.Name] = value
					}
				}
			}
		}
	}
}

func functionParams(fn *ast.FuncType) []goParam {
	if fn.Params == nil {
		return nil
	}
	var params []goParam
	for _, field := range fn.Params.List {
		typ := fieldTypeName(field.Type)
		variadic := false
		if ellipsis, ok := field.Type.(*ast.Ellipsis); ok {
			variadic = true
			typ = fieldTypeName(ellipsis.Elt)
		}
		if len(field.Names) == 0 {
			params = append(params, goParam{typ: typ, variadic: variadic})
			continue
		}
		for _, name := range field.Names {
			params = append(params, goParam{name: name.Name, typ: typ, variadic: variadic})
		}
	}
	return params
}

func typeParamCount(fn *ast.FuncType) int {
	if fn.TypeParams == nil {
		return 0
	}
	count := 0
	for _, field := range fn.TypeParams.List {
		if len(field.Names) == 0 {
			count++
			continue
		}
		count += len(field.Names)
	}
	return count
}

func receiverTypeName(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.StarExpr:
		return receiverTypeName(typed.X)
	default:
		return fieldTypeName(expr)
	}
}

func fieldTypeName(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return fieldTypeName(typed.X) + "." + typed.Sel.Name
	case *ast.StarExpr:
		return "*" + fieldTypeName(typed.X)
	case *ast.ArrayType:
		return "[]" + fieldTypeName(typed.Elt)
	case *ast.Ellipsis:
		return fieldTypeName(typed.Elt)
	case *ast.InterfaceType:
		return "interface"
	default:
		return fmt.Sprintf("%T", expr)
	}
}

func scopesFromOption(option bindingOption, resolved vocabulary) []scope {
	if len(option.AppliesToKinds) == 0 {
		return nil
	}
	var scopes []scope
	if len(option.AppliesToInterfaceTypes) == 0 {
		for _, kind := range option.AppliesToKinds {
			scopes = append(scopes, scope{kind: kind})
		}
		return scopes
	}
	for _, kind := range option.AppliesToKinds {
		for _, interfaceType := range option.AppliesToInterfaceTypes {
			if resolved.interfaceTypeCount(kind, interfaceType) == 1 {
				scopes = append(scopes, scope{kind: kind, interfaceType: interfaceType})
			}
		}
	}
	return scopes
}

type vocabulary struct {
	nodes []*extensionNode
}

type conditionFieldDefinition struct {
	node  *extensionNode
	field extensionConditionField
}

func (v *validator) resolvedVocabulary(id string) vocabulary {
	seen := make(map[string]bool)
	var nodes []*extensionNode
	var visit func(string)
	visit = func(current string) {
		if seen[current] {
			return
		}
		seen[current] = true
		node := v.nodes[current]
		if node == nil {
			return
		}
		for _, dep := range node.Definition.Spec.Dependencies {
			visit(dep)
		}
		nodes = append(nodes, node)
	}
	visit(id)
	return vocabulary{nodes: nodes}
}

func (v vocabulary) kindCount(name string) int {
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

func (v vocabulary) interfaceTypeCount(kind string, name string) int {
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

func (v vocabulary) interfaceFieldCount(kind string, interfaceType string, name string) int {
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

func (v vocabulary) conditionFieldCount(kind string, interfaceType string, name string) int {
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

func (v vocabulary) fieldValueCount(field string, kind string, interfaceType string, value string) int {
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

func (v vocabulary) fieldValueDefinitionCount(field string, kind string, interfaceType string) int {
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

func (v vocabulary) fieldValueValueCount(value string) int {
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

func (v vocabulary) counts() map[string]int {
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

func (v vocabulary) conditionFieldDefinitions() []conditionFieldDefinition {
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

func (v vocabulary) conditionFieldConflicts() []string {
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

func conditionFieldScopesOverlap(left extensionConditionField, right extensionConditionField) bool {
	for _, kind := range left.AppliesToKinds {
		if !slices.Contains(right.AppliesToKinds, kind) {
			continue
		}
		return stringSetsOverlapOrEitherEmpty(left.AppliesToInterfaceTypes, right.AppliesToInterfaceTypes)
	}
	return false
}

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

func conditionFieldApplies(field extensionConditionField, kind string, interfaceType string) bool {
	if !slices.Contains(field.AppliesToKinds, kind) {
		return false
	}
	return len(field.AppliesToInterfaceTypes) == 0 || slices.Contains(field.AppliesToInterfaceTypes, interfaceType)
}

type profileValidator struct {
	catalog *validator
	errs    []string
}

func (v *profileValidator) validate(profile profileDocument) {
	if profile.APIVersion != "runtimeconditions.io/v1alpha1" {
		v.addf("apiVersion must be runtimeconditions.io/v1alpha1")
	}
	if profile.Kind != "RuntimeConditionsProfile" {
		v.addf("kind must be RuntimeConditionsProfile")
	}
	if len(profile.Extensions) == 0 && len(profile.Conditions) > 0 {
		v.addf("extensions must declare the extension dependency closure used by conditions")
	}

	declared := make(map[string]bool)
	for _, id := range profile.Extensions {
		if declared[id] {
			v.addf("duplicate extension id %s", id)
			continue
		}
		declared[id] = true
		if v.catalog.nodes[id] == nil {
			v.addf("missing extension definition for %s", id)
			continue
		}
		v.catalog.validateNode(id)
	}

	closure := v.extensionClosure(profile.Extensions)
	for id := range closure {
		if !declared[id] {
			v.addf("extensions missing dependency %s", id)
		}
	}

	resolved := v.vocabulary(closure)
	v.catalog.checkResolvedConflicts("profile", resolved)
	for index, condition := range profile.Conditions {
		v.validateCondition(index, resolved, condition)
	}
}

func (v *profileValidator) extensionClosure(ids []string) map[string]bool {
	seen := make(map[string]bool)
	var visit func(string)
	visit = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		node := v.catalog.nodes[id]
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

func (v *profileValidator) vocabulary(ids map[string]bool) vocabulary {
	var nodes []*extensionNode
	for id := range ids {
		node := v.catalog.nodes[id]
		if node != nil {
			nodes = append(nodes, node)
		}
	}
	slices.SortFunc(nodes, func(left *extensionNode, right *extensionNode) int {
		return strings.Compare(left.ID, right.ID)
	})
	return vocabulary{nodes: nodes}
}

func (v *profileValidator) validateCondition(index int, resolved vocabulary, condition profileCondition) {
	prefix := fmt.Sprintf("conditions[%d]", index)
	v.expectExactlyOne(resolved.kindCount(condition.Kind), "%s.kind %s", prefix, condition.Kind)
	v.expectExactlyOne(resolved.interfaceTypeCount(condition.Kind, condition.Interface.Type), "%s.interface.type %s/%s", prefix, condition.Kind, condition.Interface.Type)

	if condition.Interface.Spec != nil {
		v.expectExactlyOne(resolved.interfaceFieldCount(condition.Kind, condition.Interface.Type, "spec"), "%s.interface.spec for %s/%s", prefix, condition.Kind, condition.Interface.Type)
		v.expectExactlyOne(resolved.fieldValueCount("interface.spec.format", condition.Kind, condition.Interface.Type, condition.Interface.Spec.Format), "%s.interface.spec.format %s for %s/%s", prefix, condition.Interface.Spec.Format, condition.Kind, condition.Interface.Type)
	}
	if len(condition.Interface.Operations) > 0 {
		v.expectExactlyOne(resolved.interfaceFieldCount(condition.Kind, condition.Interface.Type, "operations"), "%s.interface.operations for %s/%s", prefix, condition.Kind, condition.Interface.Type)
		for operationIndex, operation := range condition.Interface.Operations {
			v.expectExactlyOne(resolved.fieldValueCount("interface.operations[].method", condition.Kind, condition.Interface.Type, operation.Method), "%s.interface.operations[%d].method %s for %s/%s", prefix, operationIndex, operation.Method, condition.Kind, condition.Interface.Type)
		}
	}
	if len(condition.Interface.Subjects) > 0 {
		v.expectExactlyOne(resolved.interfaceFieldCount(condition.Kind, condition.Interface.Type, "subjects"), "%s.interface.subjects for %s/%s", prefix, condition.Kind, condition.Interface.Type)
	}
	if condition.Interface.Engine != "" {
		v.expectExactlyOne(resolved.interfaceFieldCount(condition.Kind, condition.Interface.Type, "engine"), "%s.interface.engine for %s/%s", prefix, condition.Kind, condition.Interface.Type)
		v.expectExactlyOne(resolved.fieldValueCount("interface.engine", condition.Kind, condition.Interface.Type, condition.Interface.Engine), "%s.interface.engine %s for %s/%s", prefix, condition.Interface.Engine, condition.Kind, condition.Interface.Type)
	}
	if condition.Interface.BucketClass != "" {
		v.expectExactlyOne(resolved.interfaceFieldCount(condition.Kind, condition.Interface.Type, "bucketClass"), "%s.interface.bucketClass for %s/%s", prefix, condition.Kind, condition.Interface.Type)
		v.expectExactlyOne(resolved.fieldValueCount("interface.bucketClass", condition.Kind, condition.Interface.Type, condition.Interface.BucketClass), "%s.interface.bucketClass %s for %s/%s", prefix, condition.Interface.BucketClass, condition.Kind, condition.Interface.Type)
	}
	if condition.Configuration != nil {
		v.expectExactlyOne(resolved.conditionFieldCount(condition.Kind, condition.Interface.Type, "configuration"), "%s.configuration for %s/%s", prefix, condition.Kind, condition.Interface.Type)
		for envIndex, env := range condition.Configuration.Env {
			v.expectExactlyOne(resolved.fieldValueCount("configuration.env[].property", condition.Kind, condition.Interface.Type, env.Property), "%s.configuration.env[%d].property %s for %s/%s", prefix, envIndex, env.Property, condition.Kind, condition.Interface.Type)
		}
		for alternativeIndex, alternative := range condition.Configuration.Alternatives {
			for envIndex, env := range alternative.Env {
				v.expectExactlyOne(resolved.fieldValueCount("configuration.alternatives[].env[].property", condition.Kind, condition.Interface.Type, env.Property), "%s.configuration.alternatives[%d].env[%d].property %s for %s/%s", prefix, alternativeIndex, envIndex, env.Property, condition.Kind, condition.Interface.Type)
			}
		}
	}
}

func (v *profileValidator) expectExactlyOne(count int, format string, args ...any) {
	if count == 1 {
		return
	}
	v.addf(format+": expected exactly one definition, got %d", append(args, count)...)
}

func (v *profileValidator) addf(format string, args ...any) {
	v.errs = append(v.errs, fmt.Sprintf(format, args...))
}

func (b *bindingDocument) extensionID() string {
	if b.Kind == "RuntimeConditionsPackage" {
		return b.Extension.ID
	}
	return b.Metadata.Extension
}

func (b *bindingDocument) extensionDefinitionPath() string {
	if b.Kind == "RuntimeConditionsPackage" {
		return b.Extension.Definition
	}
	return b.Metadata.ExtensionDefinition
}

func findBindingManifest(dir string) (string, bool, error) {
	for _, name := range []string{goBindingsManifest, goPackageBindingManifest} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", false, err
		}
		return path, true, nil
	}
	return "", false, nil
}

func languageDisplayName(language string) string {
	switch language {
	case "go":
		return "Go"
	case "java":
		return "Java"
	default:
		return language
	}
}

func javaConstantParts(name string) (string, string, bool) {
	parts := strings.Split(name, ".")
	if len(parts) < 2 || parts[0] == "" || parts[len(parts)-1] == "" {
		return "", "", false
	}
	return parts[0], parts[len(parts)-1], true
}

func javaTypeMatches(actual string, expected string) bool {
	actual = strings.TrimSpace(actual)
	switch expected {
	case "String":
		return actual == "String" || actual == "java.lang.String"
	case "Class":
		return actual == "Class" || strings.HasPrefix(actual, "Class<") || actual == "java.lang.Class" || strings.HasPrefix(actual, "java.lang.Class<")
	default:
		return actual == expected
	}
}

func definitionID(def extensionDefinition) string {
	return def.Metadata.ID
}

func validExtensionID(id string) bool {
	parsed, err := url.Parse(id)
	if err != nil {
		return false
	}
	return parsed.IsAbs() && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func isYAML(path string) bool {
	ext := filepath.Ext(path)
	return ext == ".yaml" || ext == ".yml"
}

func pathWithin(root string, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}

func firstPathSegment(path string) string {
	path = strings.TrimSuffix(path, "[]")
	if index := strings.Index(path, "."); index >= 0 {
		return strings.TrimSuffix(path[:index], "[]")
	}
	return strings.TrimSuffix(path, "[]")
}

func stringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	return value, err == nil
}

func (v *validator) expectExactlyOne(path string, count int, format string, args ...any) {
	if count == 1 {
		return
	}
	v.addf(path, format+": expected exactly one definition, got %d", append(args, count)...)
}

func (v *validator) addf(path string, format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if path == "" {
		v.errs = append(v.errs, message)
		return
	}
	v.errs = append(v.errs, path+": "+message)
}

func (v *validator) err() error {
	if len(v.errs) == 0 {
		return nil
	}
	return validationErrors(v.errs)
}

type validationErrors []string

func (e validationErrors) Error() string {
	if len(e) == 1 {
		return e[0]
	}
	var builder strings.Builder
	builder.WriteString("extension validation failed:")
	for _, item := range e {
		builder.WriteString("\n- ")
		builder.WriteString(item)
	}
	return builder.String()
}

type profileValidationErrors []string

func (e profileValidationErrors) Error() string {
	if len(e) == 1 {
		return e[0]
	}
	var builder strings.Builder
	builder.WriteString("profile validation failed:")
	for _, item := range e {
		builder.WriteString("\n- ")
		builder.WriteString(item)
	}
	return builder.String()
}

func appendValidationError(errs []string, err error) []string {
	switch typed := err.(type) {
	case validationErrors:
		return append(errs, typed...)
	case profileValidationErrors:
		return append(errs, typed...)
	default:
		return append(errs, err.Error())
	}
}
