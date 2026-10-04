package extractor

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"github.com/runtimeconditions/go-rc-profiler/extensioncheck"
	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/gosource"
)

type generatedReader struct {
	fset         *token.FileSet
	semantic     *gosource.Semantic
	packages     map[string]*extensioncheck.VerifiedGoPackage
	types        map[string]map[string]map[string]any
	declarations map[string]map[string]any
}

func newGeneratedReader(fset *token.FileSet, semantic *gosource.Semantic, packages []*extensioncheck.VerifiedGoPackage) *generatedReader {
	r := &generatedReader{fset: fset, semantic: semantic,
		packages:     map[string]*extensioncheck.VerifiedGoPackage{},
		types:        map[string]map[string]map[string]any{},
		declarations: map[string]map[string]any{}}
	for _, pkg := range packages {
		r.packages[pkg.ImportPath] = pkg
		r.types[pkg.ImportPath] = map[string]map[string]any{}
		for _, raw := range manifestArray(pkg.Manifest, "types") {
			item := raw.(map[string]any)
			r.types[pkg.ImportPath][manifestString(item, "nativeName")] = item
		}
		for _, raw := range manifestArray(pkg.Manifest, "declarations") {
			item := raw.(map[string]any)
			r.declarations[pkg.ImportPath+"\x00"+manifestString(item, "function")] = item
		}
	}
	return r
}

// A declaration function passed around as a value could be called without a
// resolvable package symbol. Reject that use instead of silently omitting it.
func (r *generatedReader) rejectDeclarationAliases(file *ast.File) error {
	direct := map[ast.Node]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		function, ok := r.semantic.ObjectForExpr(call.Fun).(*types.Func)
		if !ok || function.Pkg() == nil || r.declarations[function.Pkg().Path()+"\x00"+function.Name()] == nil {
			return true
		}
		callee := gosource.Unparen(call.Fun)
		direct[callee] = true
		if selector, ok := callee.(*ast.SelectorExpr); ok {
			direct[selector.Sel] = true
		}
		return true
	})
	var failure error
	ast.Inspect(file, func(node ast.Node) bool {
		if failure != nil {
			return false
		}
		expr, ok := node.(ast.Expr)
		if !ok {
			return true
		}
		if _, ok := expr.(*ast.Ident); !ok {
			if _, ok := expr.(*ast.SelectorExpr); !ok {
				return true
			}
		}
		function, ok := r.semantic.ObjectForExpr(expr).(*types.Func)
		if !ok || function.Pkg() == nil || r.declarations[function.Pkg().Path()+"\x00"+function.Name()] == nil || direct[expr] {
			return true
		}
		failure = gosource.NodeError(r.fset, expr, fmt.Errorf("generated declaration %s must be called directly", function.Name()))
		return false
	})
	return failure
}

func (r *generatedReader) readDeclaration(call *ast.CallExpr, function *types.Func, declaration map[string]any) (map[string]any, []string, error) {
	path := function.Pkg().Path()
	signature, ok := function.Type().(*types.Signature)
	if !ok || !signature.Variadic() || signature.Params().Len() != 1 {
		return nil, nil, fmt.Errorf("declaration signature does not match variadic marker contract")
	}
	parameter, ok := types.Unalias(signature.Params().At(0).Type()).(*types.Slice)
	if !ok {
		return nil, nil, fmt.Errorf("declaration parameter is not a marker slice")
	}
	marker, ok := types.Unalias(parameter.Elem()).(*types.Named)
	if !ok || marker.Obj() == nil || marker.Obj().Pkg() == nil || marker.Obj().Pkg().Path() != path || marker.Obj().Name() != manifestString(declaration, "markerInterface") {
		return nil, nil, fmt.Errorf("declaration marker interface differs from manifest")
	}
	if call.Ellipsis.IsValid() {
		return nil, nil, fmt.Errorf("variadic slice expansion cannot be reconstructed statically")
	}
	coordinate := manifestString(manifestObject(declaration, "modelRef"), "coordinate")
	kind := manifestString(declaration, "sourceName")
	condition := map[string]any{"kind": kind}
	sources := map[string]bool{path: true}
	interfaceType := ""
	for _, arg := range call.Args {
		root, packagePath, err := r.matchRoot(arg, coordinate, manifestString(declaration, "markerMethod"))
		if err != nil {
			return nil, nil, gosource.NodeError(r.fset, arg, err)
		}
		value, err := r.decode(arg, manifestObject(root, "value"), packagePath, 0)
		if err != nil {
			ref := manifestObject(root, "modelRef")
			return nil, nil, gosource.NodeError(r.fset, arg, fmt.Errorf("%s %s: %w", manifestString(ref, "coordinate"), manifestString(ref, "jsonPointer"), err))
		}
		scope := manifestObject(root, "scope")
		if manifestString(scope, "kind") != kind {
			return nil, nil, fmt.Errorf("root binding kind differs from declaration")
		}
		if rootInterface := manifestString(scope, "interfaceType"); rootInterface != "" {
			if interfaceType != "" && interfaceType != rootInterface {
				return nil, nil, fmt.Errorf("declaration combines different interface types")
			}
			interfaceType = rootInterface
		}
		if manifestString(root, "role") == "interface" {
			fixed := manifestString(root, "fixedInterfaceType")
			if fixed == "" || fixed != interfaceType {
				return nil, nil, fmt.Errorf("interface binding has inconsistent fixed type")
			}
			object, ok := value.(map[string]any)
			if !ok {
				return nil, nil, fmt.Errorf("interface binding must be an object")
			}
			if _, exists := condition["interface"]; exists {
				return nil, nil, fmt.Errorf("duplicate interface binding")
			}
			object["type"] = fixed
			condition["interface"] = object
		} else if err := insertRootValue(condition, manifestArray(root, "path"), value); err != nil {
			return nil, nil, err
		}
		sources[packagePath] = true
	}
	if _, ok := condition["interface"]; !ok {
		return nil, nil, fmt.Errorf("declaration has no interface binding")
	}
	paths := make([]string, 0, len(sources))
	for path := range sources {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return condition, paths, nil
}

func (r *generatedReader) matchRoot(expr ast.Expr, coordinate, markerMethod string) (map[string]any, string, error) {
	typ, ok := r.semantic.TypeOf(expr)
	if !ok {
		return nil, "", fmt.Errorf("argument type is unavailable")
	}
	path, name, ok := gosource.NamedTypeIdentity(typ)
	if !ok {
		return nil, "", fmt.Errorf("argument has no generated named type")
	}
	pkg := r.packages[path]
	if pkg == nil {
		return nil, "", fmt.Errorf("argument type %s.%s has no verified binding package", path, name)
	}
	var matches []map[string]any
	for _, raw := range manifestArray(pkg.Manifest, "rootBindings") {
		root := raw.(map[string]any)
		if manifestString(root, "declarationCoordinate") != coordinate {
			continue
		}
		rootType := manifestString(manifestObject(root, "value"), "type")
		if rootType != name && !r.isUnionVariant(path, rootType, name) {
			continue
		}
		if !implementsMarker(r.types[path][rootType], coordinate, markerMethod) {
			continue
		}
		matches = append(matches, root)
	}
	if len(matches) != 1 {
		return nil, "", fmt.Errorf("type %s.%s matches %d root bindings for %s", path, name, len(matches), coordinate)
	}
	return matches[0], path, nil
}

func implementsMarker(entry map[string]any, coordinate, method string) bool {
	for _, raw := range manifestArray(entry, "implements") {
		item := raw.(map[string]any)
		if manifestString(item, "declarationCoordinate") == coordinate && manifestString(item, "markerMethod") == method {
			return true
		}
	}
	return false
}

func (r *generatedReader) isUnionVariant(path, unionName, actualName string) bool {
	entry := r.types[path][unionName]
	if manifestString(entry, "construct") != "union" {
		return false
	}
	for _, raw := range manifestArray(entry, "variants") {
		if manifestString(manifestObject(raw.(map[string]any), "value"), "type") == actualName {
			return true
		}
	}
	return false
}

func insertRootValue(condition map[string]any, path []any, value any) error {
	if len(path) == 0 {
		return fmt.Errorf("root binding has no path")
	}
	cursor := condition
	for i, raw := range path {
		segment := raw.(map[string]any)
		name := manifestString(segment, "name")
		if segment["array"] == true {
			return fmt.Errorf("root path %q requires an array context", name)
		}
		if i == len(path)-1 {
			if _, exists := cursor[name]; exists {
				return fmt.Errorf("duplicate root field %q", name)
			}
			cursor[name] = value
			return nil
		}
		next, exists := cursor[name]
		if !exists {
			next = map[string]any{}
			cursor[name] = next
		}
		child, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("root path %q conflicts with a scalar", name)
		}
		cursor = child
	}
	return nil
}

func manifestObject(value map[string]any, key string) map[string]any {
	item, _ := value[key].(map[string]any)
	return item
}
func manifestString(value map[string]any, key string) string {
	item, _ := value[key].(string)
	return item
}
func manifestArray(value map[string]any, key string) []any {
	item, _ := value[key].([]any)
	return item
}
