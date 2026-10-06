package extensioncheck

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

// These are checked-in snapshots of the extension-binding contract schemas.
// go:embed includes the local copies in an installed profiler binary.
//
//go:embed schema/*.yaml
var bindingSchemas embed.FS

func parseDocument(data []byte) (map[string]any, error) {
	if len(data) > 64<<20 {
		return nil, fmt.Errorf("YAML exceeds 64 MiB")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document, trailing yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("YAML must contain exactly one document")
	}
	if len(document.Content) != 1 {
		return nil, fmt.Errorf("YAML must contain exactly one document")
	}
	count, aliases := 0, 0
	if err := checkYAMLNode(document.Content[0], 1, &count, &aliases); err != nil {
		return nil, err
	}
	budget := 0
	value, err := yamlValue(document.Content[0], map[*yaml.Node]bool{}, &budget)
	if err != nil {
		return nil, err
	}
	result, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("YAML root must be a mapping")
	}
	return result, nil
}

func checkYAMLNode(node *yaml.Node, depth int, count, aliases *int) error {
	*count++
	if *count > 1_000_000 || depth > 256 {
		return fmt.Errorf("YAML size or nesting limit exceeded")
	}
	if node.Kind == yaml.AliasNode {
		*aliases++
		if *aliases > 100 {
			return fmt.Errorf("YAML alias limit exceeded")
		}
	}
	if node.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
				return fmt.Errorf("YAML mapping keys must be strings")
			}
			if seen[key.Value] {
				return fmt.Errorf("duplicate YAML key %q", key.Value)
			}
			seen[key.Value] = true
		}
	}
	for _, child := range node.Content {
		if err := checkYAMLNode(child, depth+1, count, aliases); err != nil {
			return err
		}
	}
	return nil
}

func yamlValue(node *yaml.Node, active map[*yaml.Node]bool, budget *int) (any, error) {
	*budget++
	if *budget > 1_000_000 {
		return nil, fmt.Errorf("expanded YAML node limit exceeded")
	}
	if node.Kind == yaml.AliasNode {
		if node.Alias == nil || active[node.Alias] {
			return nil, fmt.Errorf("cyclic YAML alias")
		}
		active[node.Alias] = true
		value, err := yamlValue(node.Alias, active, budget)
		delete(active, node.Alias)
		return value, err
	}
	switch node.Kind {
	case yaml.MappingNode:
		result := make(map[string]any, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			value, err := yamlValue(node.Content[i+1], active, budget)
			if err != nil {
				return nil, err
			}
			result[node.Content[i].Value] = value
		}
		return result, nil
	case yaml.SequenceNode:
		result := make([]any, len(node.Content))
		for i, child := range node.Content {
			value, err := yamlValue(child, active, budget)
			if err != nil {
				return nil, err
			}
			result[i] = value
		}
		return result, nil
	case yaml.ScalarNode:
		switch node.Tag {
		case "!!str":
			return node.Value, nil
		case "!!bool":
			return strconv.ParseBool(node.Value)
		case "!!null":
			return nil, nil
		case "!!int":
			if n, err := strconv.ParseInt(node.Value, 0, 64); err == nil {
				return n, nil
			}
			return strconv.ParseUint(node.Value, 0, 64)
		case "!!float":
			n, err := strconv.ParseFloat(node.Value, 64)
			if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
				return nil, fmt.Errorf("unsupported number %q", node.Value)
			}
			return n, nil
		default:
			return nil, fmt.Errorf("unsupported YAML tag %q", node.Tag)
		}
	}
	return nil, fmt.Errorf("unsupported YAML node")
}

func validateSchema(name string, value map[string]any) error {
	data, err := bindingSchemas.ReadFile("schema/" + name)
	if err != nil {
		return err
	}
	schemaData, err := parseDocument(data)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(schemaData)
	if err != nil {
		return err
	}
	resource, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	const uri = "urn:runtimeconditions:embedded-schema"
	if err := compiler.AddResource(uri, resource); err != nil {
		return err
	}
	schema, err := compiler.Compile(uri)
	if err != nil {
		return err
	}
	return schema.Validate(value)
}

func semanticDigest(value map[string]any) (string, error) {
	encoded, err := canonicalJSON(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// Matches the normalizer's canonical JSON for JSON-compatible model data.
func canonicalJSON(value any) ([]byte, error) {
	var out bytes.Buffer
	if err := writeCanonical(&out, value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func writeCanonical(out *bytes.Buffer, value any) error {
	switch v := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		out.WriteString(strconv.FormatBool(v))
	case string:
		if !utf8.ValidString(v) {
			return fmt.Errorf("invalid UTF-8")
		}
		out.WriteByte('"')
		for _, c := range v {
			switch c {
			case '"', '\\':
				out.WriteByte('\\')
				out.WriteRune(c)
			case '\b':
				out.WriteString(`\b`)
			case '\t':
				out.WriteString(`\t`)
			case '\n':
				out.WriteString(`\n`)
			case '\f':
				out.WriteString(`\f`)
			case '\r':
				out.WriteString(`\r`)
			default:
				if c < 0x20 {
					fmt.Fprintf(out, `\u%04x`, c)
				} else {
					out.WriteRune(c)
				}
			}
		}
		out.WriteByte('"')
	case int64:
		out.WriteString(strconv.FormatInt(v, 10))
	case uint64:
		out.WriteString(strconv.FormatUint(v, 10))
	case float64:
		if math.IsInf(v, 0) || math.IsNaN(v) {
			return fmt.Errorf("non-finite number")
		}
		if v == 0 {
			out.WriteByte('0')
		} else {
			encoded, _ := json.Marshal(v)
			out.Write(encoded)
		}
	case []any:
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := writeCanonical(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			a, b := utf16.Encode([]rune(keys[i])), utf16.Encode([]rune(keys[j]))
			for n := 0; n < len(a) && n < len(b); n++ {
				if a[n] != b[n] {
					return a[n] < b[n]
				}
			}
			return len(a) < len(b)
		})
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := writeCanonical(out, key); err != nil {
				return err
			}
			out.WriteByte(':')
			if err := writeCanonical(out, v[key]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	default:
		return fmt.Errorf("unsupported canonical value %T", value)
	}
	return nil
}

func normalizeExtension(value any, schema, root map[string]any) (any, error) {
	selected, err := selectSemanticSchema(value, schema, root)
	if err != nil {
		return nil, err
	}
	switch v := value.(type) {
	case map[string]any:
		properties, _ := selected["properties"].(map[string]any)
		additional, _ := selected["additionalProperties"].(map[string]any)
		result := make(map[string]any, len(v))
		for key, child := range v {
			childSchema, _ := properties[key].(map[string]any)
			if childSchema == nil {
				childSchema = additional
			}
			if childSchema == nil {
				return nil, fmt.Errorf("semantic schema has no rule for %q", key)
			}
			item, err := normalizeExtension(child, childSchema, root)
			if err != nil {
				return nil, err
			}
			result[key] = item
		}
		return result, nil
	case []any:
		ordering, _ := selected["x-runtimeconditions-ordering"].(string)
		if ordering != "set" && ordering != "source" {
			return nil, fmt.Errorf("semantic array has no ordering")
		}
		itemSchema, _ := selected["items"].(map[string]any)
		if itemSchema == nil {
			return nil, fmt.Errorf("semantic array has no item schema")
		}
		result := make([]any, len(v))
		for i, item := range v {
			normalized, err := normalizeExtension(item, itemSchema, root)
			if err != nil {
				return nil, err
			}
			result[i] = normalized
		}
		if ordering == "set" {
			sort.SliceStable(result, func(i, j int) bool {
				a, _ := canonicalJSON(result[i])
				b, _ := canonicalJSON(result[j])
				return bytes.Compare(a, b) < 0
			})
		}
		return result, nil
	default:
		return value, nil
	}
}

func selectSemanticSchema(value any, schema, root map[string]any) (map[string]any, error) {
	resolved, err := dereferenceSchema(schema, root)
	if err != nil {
		return nil, err
	}
	branches, ok := resolved["oneOf"].([]any)
	if !ok {
		return resolved, nil
	}
	var matches []map[string]any
	for _, item := range branches {
		branch, ok := item.(map[string]any)
		if !ok {
			continue
		}
		branch, err = dereferenceSchema(branch, root)
		if err != nil {
			return nil, err
		}
		if schemaMatches(value, branch, root) {
			matches = append(matches, branch)
		}
	}
	if len(matches) != 1 {
		return nil, fmt.Errorf("semantic schema matched %d branches", len(matches))
	}
	return matches[0], nil
}

func dereferenceSchema(schema, root map[string]any) (map[string]any, error) {
	seen := map[string]bool{}
	for {
		ref, ok := schema["$ref"].(string)
		if !ok {
			return schema, nil
		}
		if seen[ref] || !strings.HasPrefix(ref, "#/") {
			return nil, fmt.Errorf("invalid semantic schema reference %q", ref)
		}
		seen[ref] = true
		var value any = root
		for _, segment := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			mapping, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("unresolved semantic schema reference %q", ref)
			}
			value = mapping[strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")]
		}
		var okMap bool
		schema, okMap = value.(map[string]any)
		if !okMap {
			return nil, fmt.Errorf("unresolved semantic schema reference %q", ref)
		}
	}
}

func schemaMatches(value any, schema, root map[string]any) bool {
	resolved, err := dereferenceSchema(schema, root)
	if err != nil {
		return false
	}
	if branches, ok := resolved["oneOf"].([]any); ok {
		matches := 0
		for _, item := range branches {
			if branch, ok := item.(map[string]any); ok && schemaMatches(value, branch, root) {
				matches++
			}
		}
		return matches == 1
	}
	if constant, ok := resolved["const"]; ok {
		a, _ := canonicalJSON(constant)
		b, _ := canonicalJSON(value)
		if !bytes.Equal(a, b) {
			return false
		}
	}
	var actual string
	switch value.(type) {
	case map[string]any:
		actual = "object"
	case []any:
		actual = "array"
	case string:
		actual = "string"
	case bool:
		actual = "boolean"
	case nil:
		actual = "null"
	case int64, uint64:
		actual = "integer"
	case float64:
		actual = "number"
	}
	if expected, ok := resolved["type"].(string); ok {
		return expected == actual || expected == "number" && actual == "integer"
	}
	if expected, ok := resolved["type"].([]any); ok {
		for _, item := range expected {
			if item == actual || item == "number" && actual == "integer" {
				return true
			}
		}
		return false
	}
	return true
}
