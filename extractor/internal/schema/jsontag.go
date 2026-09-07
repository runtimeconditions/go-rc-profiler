package schema

import (
	"go/ast"
	"reflect"
	"strconv"
	"strings"
)

// jsonFieldNameFromLiteral reads a json tag straight off the AST, for the
// syntax-only path where no resolved struct tag is available.
func jsonFieldNameFromLiteral(defaultName string, tag *ast.BasicLit) (string, bool) {
	if tag == nil {
		return defaultName, false
	}
	tagValue, err := strconv.Unquote(tag.Value)
	if err != nil {
		return defaultName, false
	}
	return jsonFieldNameFromTag(defaultName, tagValue)
}

// jsonFieldNameFromTag reports the name a field serializes under, and whether
// encoding/json would omit it entirely.
func jsonFieldNameFromTag(defaultName string, tagValue string) (name string, skip bool) {
	jsonTag := reflect.StructTag(tagValue).Get("json")
	if jsonTag == "" {
		return defaultName, false
	}
	switch tagged := strings.Split(jsonTag, ",")[0]; tagged {
	case "-":
		return "", true
	case "":
		return defaultName, false
	default:
		return tagged, false
	}
}
