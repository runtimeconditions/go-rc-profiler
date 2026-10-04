package extractor

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"math"

	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/gosource"
)

// decode follows only edges in the verified structural manifest. The accepted
// syntax is intentionally static: no workload or binding code is executed.
func (r *generatedReader) decode(expr ast.Expr, ref map[string]any, packagePath string, depth int) (any, error) {
	if depth > 128 {
		return nil, fmt.Errorf("binding value exceeds maximum structural depth")
	}
	expr = gosource.Unparen(expr)
	if identifier, ok := expr.(*ast.Ident); ok && identifier.Name == "nil" {
		if ref["pointer"] == true || manifestString(ref, "builtin") == "any" {
			return nil, nil
		}
		return nil, fmt.Errorf("nil is not allowed by this binding reference")
	}
	if ref["pointer"] == true {
		if call, ok := expr.(*ast.CallExpr); ok {
			if builtin, ok := r.semantic.ObjectForExpr(call.Fun).(*types.Builtin); ok && builtin.Name() == "new" && len(call.Args) == 1 {
				return r.zeroPointerValue(call, ref, packagePath)
			}
		}
	}
	if unary, ok := expr.(*ast.UnaryExpr); ok && unary.Op == token.AND {
		if ref["pointer"] != true && manifestString(ref, "type") == "" {
			return nil, fmt.Errorf("address-of value does not match binding reference")
		}
		if ref["pointer"] == true {
			if index, ok := gosource.Unparen(unary.X).(*ast.IndexExpr); ok {
				literal, ok := gosource.Unparen(index.X).(*ast.CompositeLit)
				if !ok {
					return nil, fmt.Errorf("pointer index must select a composite literal")
				}
				indexValue, ok := r.semantic.ConstantValue(index.Index)
				if !ok {
					return nil, fmt.Errorf("pointer index is not constant")
				}
				number, exact := constant.Int64Val(indexValue)
				if !exact || number < 0 || number >= int64(len(literal.Elts)) {
					return nil, fmt.Errorf("pointer index is outside literal")
				}
				copyRef := make(map[string]any, len(ref))
				for key, value := range ref {
					copyRef[key] = value
				}
				delete(copyRef, "pointer")
				return r.decode(literal.Elts[number], copyRef, packagePath, depth+1)
			}
		}
		expr = gosource.Unparen(unary.X)
	}
	if name := manifestString(ref, "type"); name != "" {
		entry := r.types[packagePath][name]
		if entry == nil {
			return nil, fmt.Errorf("native type %s is absent from verified manifest", name)
		}
		typ, ok := r.semantic.TypeOf(expr)
		if !ok {
			return nil, fmt.Errorf("value type is unavailable")
		}
		actualPath, actualName, ok := gosource.NamedTypeIdentity(typ)
		if !ok || actualPath != packagePath || (actualName != name && !r.isUnionVariant(packagePath, name, actualName)) {
			return nil, fmt.Errorf("expected generated type %s.%s, got %s.%s", packagePath, name, actualPath, actualName)
		}
		switch manifestString(entry, "construct") {
		case "scalar":
			return r.staticScalar(expr, manifestString(entry, "underlying"))
		case "object":
			return r.staticObject(expr, entry, packagePath, depth+1)
		case "collection":
			return r.staticCollection(expr, entry, packagePath, depth+1)
		case "map":
			return r.staticMap(expr, entry, packagePath, depth+1)
		case "union":
			for _, raw := range manifestArray(entry, "variants") {
				variant := raw.(map[string]any)
				if manifestString(manifestObject(variant, "value"), "type") == actualName {
					return r.decode(expr, manifestObject(variant, "value"), packagePath, depth+1)
				}
			}
			return nil, fmt.Errorf("union variant %s is absent from manifest", actualName)
		case "any":
			return r.staticJSON(expr, depth+1)
		default:
			return nil, fmt.Errorf("unsupported generated construct %q", manifestString(entry, "construct"))
		}
	}
	if manifestString(ref, "builtin") == "any" {
		return r.staticJSON(expr, depth+1)
	}
	return r.staticScalar(expr, manifestString(ref, "builtin"))
}

func (r *generatedReader) zeroPointerValue(expr ast.Expr, ref map[string]any, packagePath string) (any, error) {
	typ, ok := r.semantic.TypeOf(expr)
	if !ok {
		return nil, fmt.Errorf("new expression type is unavailable")
	}
	pointer, ok := types.Unalias(typ).(*types.Pointer)
	if !ok {
		return nil, fmt.Errorf("new expression is not a pointer")
	}
	if name := manifestString(ref, "type"); name != "" {
		path, actual, ok := gosource.NamedTypeIdentity(pointer.Elem())
		if !ok || path != packagePath || actual != name {
			return nil, fmt.Errorf("new expression does not match generated type %s.%s", packagePath, name)
		}
		entry := r.types[packagePath][name]
		if manifestString(entry, "construct") == "object" {
			for _, raw := range manifestArray(entry, "fields") {
				if raw.(map[string]any)["required"] == true {
					return nil, fmt.Errorf("new object omits required fields")
				}
			}
			return map[string]any{}, nil
		}
		return zeroScalar(manifestString(entry, "underlying"))
	}
	builtin := manifestString(ref, "builtin")
	if pointer.Elem().String() != builtin {
		return nil, fmt.Errorf("new expression does not match %s", builtin)
	}
	return zeroScalar(builtin)
}

func zeroScalar(kind string) (any, error) {
	switch kind {
	case "string":
		return "", nil
	case "bool":
		return false, nil
	case "int64":
		return int64(0), nil
	case "float64":
		return float64(0), nil
	case "struct{}":
		return map[string]any{}, nil
	}
	return nil, fmt.Errorf("unsupported zero value for %s", kind)
}

func (r *generatedReader) staticScalar(expr ast.Expr, underlying string) (any, error) {
	if underlying == "struct{}" {
		literal, ok := gosource.Unparen(expr).(*ast.CompositeLit)
		if ok && len(literal.Elts) == 0 {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("struct{} value must be an empty literal")
	}
	value, ok := r.semantic.ConstantValue(expr)
	if !ok {
		return nil, fmt.Errorf("value is not a compile-time constant")
	}
	switch underlying {
	case "string":
		if value.Kind() == constant.String {
			return constant.StringVal(value), nil
		}
	case "bool":
		if value.Kind() == constant.Bool {
			return constant.BoolVal(value), nil
		}
	case "int64":
		if value.Kind() == constant.Int {
			if number, exact := constant.Int64Val(value); exact {
				return number, nil
			}
		}
	case "float64":
		if value.Kind() == constant.Float || value.Kind() == constant.Int {
			if number, _ := constant.Float64Val(value); !math.IsInf(number, 0) && !math.IsNaN(number) {
				return number, nil
			}
		}
	}
	return nil, fmt.Errorf("constant does not match %s", underlying)
}

func (r *generatedReader) staticObject(expr ast.Expr, entry map[string]any, packagePath string, depth int) (any, error) {
	literal, ok := gosource.Unparen(expr).(*ast.CompositeLit)
	if !ok {
		return nil, fmt.Errorf("object must be a composite literal")
	}
	fields := manifestArray(entry, "fields")
	byNative := map[string]map[string]any{}
	for _, raw := range fields {
		field := raw.(map[string]any)
		byNative[manifestString(field, "nativeName")] = field
	}
	result := map[string]any{}
	seen := map[string]bool{}
	for index, raw := range literal.Elts {
		value := raw
		var field map[string]any
		if keyed, ok := raw.(*ast.KeyValueExpr); ok {
			name, ok := keyed.Key.(*ast.Ident)
			if !ok {
				return nil, fmt.Errorf("object field key is not a native identifier")
			}
			field = byNative[name.Name]
			value = keyed.Value
		} else if index < len(fields) {
			field = fields[index].(map[string]any)
		}
		if field == nil {
			return nil, fmt.Errorf("object literal field is absent from manifest")
		}
		name := manifestString(field, "sourceName")
		if seen[name] {
			return nil, fmt.Errorf("duplicate object field %s", name)
		}
		seen[name] = true
		decoded, err := r.decode(value, manifestObject(field, "value"), packagePath, depth+1)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", name, err)
		}
		if decoded == nil && manifestObject(field, "value")["pointer"] == true && field["required"] != true {
			continue
		}
		result[name] = decoded
	}
	for _, raw := range fields {
		field := raw.(map[string]any)
		if field["required"] == true && !seen[manifestString(field, "sourceName")] {
			return nil, fmt.Errorf("required field %s is absent", manifestString(field, "sourceName"))
		}
	}
	return result, nil
}

func (r *generatedReader) staticCollection(expr ast.Expr, entry map[string]any, packagePath string, depth int) (any, error) {
	literal, ok := gosource.Unparen(expr).(*ast.CompositeLit)
	if !ok {
		return nil, fmt.Errorf("collection must be a composite literal")
	}
	result := make([]any, 0, len(literal.Elts))
	for _, element := range literal.Elts {
		if _, keyed := element.(*ast.KeyValueExpr); keyed {
			return nil, fmt.Errorf("indexed collection elements are unsupported")
		}
		value, err := r.decode(element, manifestObject(manifestObject(entry, "element"), "value"), packagePath, depth+1)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func (r *generatedReader) staticMap(expr ast.Expr, entry map[string]any, packagePath string, depth int) (any, error) {
	literal, ok := gosource.Unparen(expr).(*ast.CompositeLit)
	if !ok {
		return nil, fmt.Errorf("map must be a composite literal")
	}
	result := map[string]any{}
	for _, raw := range literal.Elts {
		pair, ok := raw.(*ast.KeyValueExpr)
		if !ok {
			return nil, fmt.Errorf("map entry must have a key")
		}
		key, err := r.staticScalar(pair.Key, "string")
		if err != nil {
			return nil, fmt.Errorf("map key: %w", err)
		}
		name := key.(string)
		if _, exists := result[name]; exists {
			return nil, fmt.Errorf("duplicate map key %q", name)
		}
		value, err := r.decode(pair.Value, manifestObject(manifestObject(entry, "element"), "value"), packagePath, depth+1)
		if err != nil {
			return nil, fmt.Errorf("map key %q: %w", name, err)
		}
		result[name] = value
	}
	return result, nil
}

func (r *generatedReader) staticJSON(expr ast.Expr, depth int) (any, error) {
	if depth > 128 {
		return nil, fmt.Errorf("JSON value exceeds maximum structural depth")
	}
	expr = gosource.Unparen(expr)
	if ident, ok := expr.(*ast.Ident); ok && ident.Name == "nil" {
		return nil, nil
	}
	if value, ok := r.semantic.ConstantValue(expr); ok {
		switch value.Kind() {
		case constant.String:
			return constant.StringVal(value), nil
		case constant.Bool:
			return constant.BoolVal(value), nil
		case constant.Int:
			if n, exact := constant.Int64Val(value); exact {
				return n, nil
			}
		case constant.Float:
			if n, _ := constant.Float64Val(value); !math.IsInf(n, 0) && !math.IsNaN(n) {
				return n, nil
			}
		}
	}
	literal, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil, fmt.Errorf("JSON value is not statically reconstructible")
	}
	typ, ok := r.semantic.TypeOf(expr)
	if !ok {
		return nil, fmt.Errorf("JSON composite type is unavailable")
	}
	switch types.Unalias(typ).Underlying().(type) {
	case *types.Map:
		result := map[string]any{}
		for _, raw := range literal.Elts {
			pair, ok := raw.(*ast.KeyValueExpr)
			if !ok {
				return nil, fmt.Errorf("mixed JSON composite entries")
			}
			key, err := r.staticScalar(pair.Key, "string")
			if err != nil {
				return nil, err
			}
			name := key.(string)
			if _, exists := result[name]; exists {
				return nil, fmt.Errorf("duplicate JSON key %q", name)
			}
			result[name], err = r.staticJSON(pair.Value, depth+1)
			if err != nil {
				return nil, err
			}
		}
		return result, nil
	case *types.Slice, *types.Array:
		result := make([]any, 0, len(literal.Elts))
		for _, raw := range literal.Elts {
			if _, keyed := raw.(*ast.KeyValueExpr); keyed {
				return nil, fmt.Errorf("indexed JSON array elements are unsupported")
			}
			value, err := r.staticJSON(raw, depth+1)
			if err != nil {
				return nil, err
			}
			result = append(result, value)
		}
		return result, nil
	}
	return nil, fmt.Errorf("JSON composite must be a map or array")
}
