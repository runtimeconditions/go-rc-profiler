// Package schema derives JSON Schema fragments from Go types.
//
// Declarations name request and response bodies with ordinary Go types, so the
// profile can only describe those bodies if the profiler can translate a type
// into a schema. Two paths exist for the same job: a type-checked path that
// follows named types across packages, and a syntax-only fallback limited to
// what the workload declares itself.
package schema

import (
	"fmt"
	"go/ast"
	"go/types"

	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/gosource"
)

// Deriver translates type expressions using one workload's scope.
type Deriver struct {
	scope *gosource.PackageScope
}

// NewDeriver returns a Deriver bound to a workload scope.
func NewDeriver(scope *gosource.PackageScope) *Deriver {
	return &Deriver{scope: scope}
}

// ForExpr derives the schema for a type expression, such as the type argument
// of a generic request or response option.
//
// Type information is preferred when available because it resolves types the
// workload merely imports. The syntax fallback below only sees what the
// workload declares in its own files.
func (d *Deriver) ForExpr(expr ast.Expr) (any, error) {
	if typ, ok := d.scope.Semantic.TypeOf(expr); ok {
		return d.forGoType(typ, make(map[types.Type]bool))
	}

	switch typed := gosource.Unparen(expr).(type) {
	case *ast.Ident:
		if basic, ok := builtinSchemaType(typed.Name); ok {
			return basic, nil
		}
		structType, ok := d.scope.Struct(typed.Name)
		if !ok {
			return nil, fmt.Errorf("unsupported schema type %q", typed.Name)
		}
		return d.forSyntaxStruct(structType)
	case *ast.StarExpr:
		return d.ForExpr(typed.X)
	case *ast.ArrayType:
		element, err := d.ForExpr(typed.Elt)
		if err != nil {
			return nil, err
		}
		return []any{element}, nil
	case *ast.SelectorExpr:
		if ident, ok := gosource.Unparen(typed.X).(*ast.Ident); ok && ident.Name == "time" && typed.Sel.Name == "Time" {
			return "string", nil
		}
		return nil, fmt.Errorf("unsupported external schema type %s", gosource.ExprString(typed))
	default:
		return nil, fmt.Errorf("unsupported schema expression %s", gosource.ExprString(typed))
	}
}

// forSyntaxStruct builds a schema from a struct declared in the workload,
// without type information. Embedded fields are flattened into the parent, and
// unexported fields are omitted, matching encoding/json.
func (d *Deriver) forSyntaxStruct(structType *ast.StructType) (map[string]any, error) {
	schema := make(map[string]any)
	for _, field := range structType.Fields.List {
		if len(field.Names) == 0 {
			if err := d.flattenSyntaxEmbedded(schema, field.Type); err != nil {
				return nil, err
			}
			continue
		}
		for _, name := range field.Names {
			if !name.IsExported() {
				continue
			}
			jsonName, skip := jsonFieldNameFromLiteral(name.Name, field.Tag)
			if skip {
				continue
			}
			fieldSchema, err := d.ForExpr(field.Type)
			if err != nil {
				return nil, err
			}
			schema[jsonName] = fieldSchema
		}
	}
	return schema, nil
}

func (d *Deriver) flattenSyntaxEmbedded(schema map[string]any, fieldType ast.Expr) error {
	embedded, err := d.ForExpr(fieldType)
	if err != nil {
		return err
	}
	nested, ok := embedded.(map[string]any)
	if !ok {
		return nil
	}
	for key, value := range nested {
		schema[key] = value
	}
	return nil
}

// builtinSchemaType maps a predeclared Go type name to its JSON Schema type.
func builtinSchemaType(name string) (string, bool) {
	switch name {
	case "string":
		return "string", true
	case "bool":
		return "boolean", true
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr":
		return "integer", true
	case "float32", "float64":
		return "number", true
	default:
		return "", false
	}
}
