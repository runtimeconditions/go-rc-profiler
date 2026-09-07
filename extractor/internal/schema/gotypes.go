package schema

import (
	"fmt"
	"go/types"
)

// forGoType derives a schema from resolved type information.
//
// seen breaks recursive types: a type already being derived higher in the stack
// yields an empty object rather than recursing forever.
func (d *Deriver) forGoType(typ types.Type, seen map[types.Type]bool) (any, error) {
	typ = types.Unalias(typ)
	if seen[typ] {
		return map[string]any{}, nil
	}
	seen[typ] = true
	defer delete(seen, typ)

	switch typed := typ.(type) {
	case *types.Basic:
		basic, ok := basicSchemaType(typed)
		if !ok {
			return nil, fmt.Errorf("unsupported schema type %q", typed.Name())
		}
		return basic, nil
	case *types.Pointer:
		return d.forGoType(typed.Elem(), seen)
	case *types.Slice:
		return d.forGoSequence(typed.Elem(), seen)
	case *types.Array:
		return d.forGoSequence(typed.Elem(), seen)
	case *types.Named:
		if isStdTime(typed) {
			return "string", nil
		}
		return d.forGoType(typed.Underlying(), seen)
	case *types.Struct:
		return d.forGoStruct(typed, seen)
	case *types.Map:
		return d.forGoMap(typed, seen)
	default:
		return nil, fmt.Errorf("unsupported schema type %s", typ.String())
	}
}

func (d *Deriver) forGoSequence(element types.Type, seen map[types.Type]bool) (any, error) {
	elementSchema, err := d.forGoType(element, seen)
	if err != nil {
		return nil, err
	}
	return []any{elementSchema}, nil
}

// forGoMap only accepts string-keyed maps, because a JSON object cannot express
// any other key type.
func (d *Deriver) forGoMap(mapType *types.Map, seen map[types.Type]bool) (any, error) {
	if key, ok := types.Unalias(mapType.Key()).(*types.Basic); !ok || key.Kind() != types.String {
		return nil, fmt.Errorf("unsupported schema map key type %s", mapType.Key().String())
	}
	value, err := d.forGoType(mapType.Elem(), seen)
	if err != nil {
		return nil, err
	}
	return map[string]any{"additionalProperties": value}, nil
}

// forGoStruct mirrors encoding/json: unexported fields are omitted, embedded
// fields are flattened into the parent, and json tags rename or drop fields.
func (d *Deriver) forGoStruct(structType *types.Struct, seen map[types.Type]bool) (map[string]any, error) {
	schema := make(map[string]any)
	for i := range structType.NumFields() {
		field := structType.Field(i)
		if !field.Anonymous() && !field.Exported() {
			continue
		}
		fieldSchema, err := d.forGoType(field.Type(), seen)
		if err != nil {
			return nil, err
		}
		if field.Anonymous() {
			nested, ok := fieldSchema.(map[string]any)
			if !ok {
				continue
			}
			for key, value := range nested {
				schema[key] = value
			}
			continue
		}
		jsonName, skip := jsonFieldNameFromTag(field.Name(), structType.Tag(i))
		if skip {
			continue
		}
		schema[jsonName] = fieldSchema
	}
	return schema, nil
}

// isStdTime reports whether a named type is time.Time, which serializes as an
// RFC 3339 string rather than as its struct fields.
func isStdTime(named *types.Named) bool {
	object := named.Obj()
	return object != nil && object.Pkg() != nil && object.Pkg().Path() == "time" && object.Name() == "Time"
}

// basicSchemaType maps a resolved basic type to its JSON Schema type.
func basicSchemaType(typ *types.Basic) (string, bool) {
	switch typ.Kind() {
	case types.String:
		return "string", true
	case types.Bool:
		return "boolean", true
	case types.Int, types.Int8, types.Int16, types.Int32, types.Int64, types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64, types.Uintptr:
		return "integer", true
	case types.Float32, types.Float64:
		return "number", true
	default:
		return "", false
	}
}
