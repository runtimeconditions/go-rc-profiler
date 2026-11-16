package gosource

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const plainModule = "module example.com/plain\n\ngo 1.25.0\n"

func TestLoadTypeChecksTheWorkloadAndSortsItsFiles(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":   plainModule,
		"b.go":     "package main\n\nconst Name = \"orders\"\n\nfunc main() { _ = Name }\n",
		"a.go":     "package main\n\ntype Server struct{ Port int }\n",
		"sub/s.go": "package sub\n\nfunc Hello() string { return \"hi\" }\n",
	})
	fset, files, semantic, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fset == nil || semantic == nil || len(files) != 3 {
		t.Fatalf("expected 3 files and type information, got %d files, semantic %v", len(files), semantic)
	}
	for index := 1; index < len(files); index++ {
		if files[index-1].Path >= files[index].Path {
			t.Fatalf("files are not sorted by path: %s then %s", files[index-1].Path, files[index].Path)
		}
	}
}

func TestLoadReturnsFilesOfAllPackagesSortedByPathNotByPackage(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":   plainModule,
		"z.go":     "package main\n\nfunc main() {}\n",
		"aaa/x.go": "package aaa\n\nfunc X() {}\n",
		"mmm/m.go": "package mmm\n\nfunc M() {}\n",
	})
	_, files, _, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, file := range files {
		names = append(names, filepath.Base(file.Path))
	}
	if strings.Join(names, ",") != "x.go,m.go,z.go" {
		t.Fatalf("files must be ordered by full path (aaa/x.go, mmm/m.go, z.go), got %v", names)
	}
}

func TestLoadFailsWhenAnyPackageDoesNotTypeCheck(t *testing.T) {
	for name, source := range map[string]string{
		"a syntax error":    "package main\n\nfunc main( {\n",
		"an undefined name": "package main\n\nfunc main() { _ = missing }\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := writeModule(t, map[string]string{"go.mod": plainModule, "main.go": source})
			_, _, _, err := Load(dir)
			if err == nil || !strings.Contains(err.Error(), "go/packages load failed") {
				t.Fatalf("a package that does not type-check must fail the load, got %v", err)
			}
		})
	}
}

func TestLoadOfADirectoryWithoutGoSourceReturnsNoFiles(t *testing.T) {
	dir := writeModule(t, map[string]string{"go.mod": plainModule})
	_, files, _, err := Load(dir)
	if err == nil && len(files) != 0 {
		t.Fatalf("expected no files, got %d", len(files))
	}
}

// find returns the first expression of the wanted source text in the loaded files.
func find(t *testing.T, files []File, want string) ast.Expr {
	t.Helper()
	var found ast.Expr
	for _, file := range files {
		ast.Inspect(file.Syntax, func(node ast.Node) bool {
			expr, ok := node.(ast.Expr)
			if !ok || found != nil {
				return found == nil
			}
			if ident, ok := expr.(*ast.Ident); ok && ident.Name == want {
				found = expr
			}
			if literal, ok := expr.(*ast.BasicLit); ok && literal.Value == want {
				found = expr
			}
			return found == nil
		})
	}
	if found == nil {
		t.Fatalf("no expression %q in the loaded files", want)
	}
	return found
}

func TestSemanticResolvesConstantsTypesAndObjects(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": plainModule,
		"main.go": `package main

import "time"

const Name = "orders"
const Limit = 2 * 21

type Server struct{ Port int }

var Timeout = time.Second

func main() {
	server := &Server{Port: 8080}
	_ = server
	_ = (Name)
	_ = Limit
}
`,
	})
	_, files, semantic, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	// A constant, through parentheses, and one computed by the type checker.
	name := find(t, files, "Name")
	wrapped := &ast.ParenExpr{X: name}
	if value, ok := semantic.ConstantValue(wrapped); !ok || constant.StringVal(value) != "orders" {
		t.Fatalf("expected the string constant, got %v, %v", value, ok)
	}
	if value, ok := semantic.ConstantValue(find(t, files, "Limit")); !ok || value.ExactString() != "42" {
		t.Fatalf("expected 2 * 21 to be 42, got %v, %v", value, ok)
	}
	// A variable is not a constant.
	if _, ok := semantic.ConstantValue(find(t, files, "Timeout")); ok {
		t.Fatal("a variable must not resolve to a constant")
	}

	// The type of an expression, and the named type underneath a pointer to it.
	typ, ok := semantic.TypeOf(find(t, files, "server"))
	if !ok {
		t.Fatal("no type for server")
	}
	if path, name, ok := NamedTypeIdentity(typ); !ok || path != "example.com/plain" || name != "Server" {
		t.Fatalf("expected example.com/plain.Server through the pointer, got %q %q %v", path, name, ok)
	}
	// An object for a use of an identifier and for a package-qualified selector.
	if object := semantic.ObjectForExpr(find(t, files, "Limit")); object == nil || object.Name() != "Limit" {
		t.Fatalf("expected the object Limit, got %v", object)
	}
}

func TestSemanticMethodsAreSafeOnANilSemantic(t *testing.T) {
	var semantic *Semantic
	expr := &ast.Ident{Name: "x"}
	if _, ok := semantic.ConstantValue(expr); ok {
		t.Fatal("nil semantic resolved a constant")
	}
	if _, ok := semantic.TypeOf(expr); ok {
		t.Fatal("nil semantic resolved a type")
	}
	if semantic.ObjectForExpr(expr) != nil {
		t.Fatal("nil semantic resolved an object")
	}
}

func TestUnparenStripsEveryLevelOfParentheses(t *testing.T) {
	inner := &ast.Ident{Name: "x"}
	if got := Unparen(&ast.ParenExpr{X: &ast.ParenExpr{X: &ast.ParenExpr{X: inner}}}); got != inner {
		t.Fatalf("expected the identifier, got %#v", got)
	}
	if got := Unparen(inner); got != inner {
		t.Fatal("an expression without parentheses must come back unchanged")
	}
}

func TestNodeErrorNamesThePositionOfTheNode(t *testing.T) {
	fset := token.NewFileSet()
	file := fset.AddFile("workload.go", -1, 100)
	file.SetLines([]int{0, 10, 20})
	err := NodeError(fset, &ast.Ident{Name: "x", NamePos: file.Pos(12)}, os.ErrInvalid)
	if !strings.Contains(err.Error(), "workload.go:2") || !strings.Contains(err.Error(), os.ErrInvalid.Error()) {
		t.Fatalf("expected the file, the line and the cause, got %q", err)
	}
}

func TestNamedTypeIdentityOnlyNamesNamedTypes(t *testing.T) {
	pkg := types.NewPackage("example.com/p", "p")
	name := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Thing", nil), types.NewStruct(nil, nil), nil)
	for label, typ := range map[string]types.Type{
		"a named type":                 name,
		"a pointer to it":              types.NewPointer(name),
		"a pointer to a pointer to it": types.NewPointer(types.NewPointer(name)),
	} {
		if path, got, ok := NamedTypeIdentity(typ); !ok || path != "example.com/p" || got != "Thing" {
			t.Errorf("%s: got %q %q %v", label, path, got, ok)
		}
	}
	for label, typ := range map[string]types.Type{
		"a basic type":              types.Typ[types.Int],
		"a slice":                   types.NewSlice(types.Typ[types.String]),
		"a pointer to a basic type": types.NewPointer(types.Typ[types.Int]),
	} {
		if _, _, ok := NamedTypeIdentity(typ); ok {
			t.Errorf("%s has no named type but one was reported", label)
		}
	}
}

// selector returns the first selector expression whose selected name is wanted.
func selector(t *testing.T, files []File, want string) *ast.SelectorExpr {
	t.Helper()
	var found *ast.SelectorExpr
	for _, file := range files {
		ast.Inspect(file.Syntax, func(node ast.Node) bool {
			if expr, ok := node.(*ast.SelectorExpr); ok && found == nil && expr.Sel.Name == want {
				found = expr
			}
			return found == nil
		})
	}
	if found == nil {
		t.Fatalf("no selector .%s in the loaded files", want)
	}
	return found
}

func TestObjectForExprResolvesPackageQualifiedAndFieldSelectors(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": plainModule,
		"main.go": `package main

import "strings"

type Server struct{ Port int }

func main() {
	server := Server{Port: 80}
	_ = strings.ToUpper("x")
	_ = server.Port
}
`,
	})
	_, files, semantic, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	function := semantic.ObjectForExpr(selector(t, files, "ToUpper"))
	if function == nil || function.Name() != "ToUpper" || function.Pkg() == nil || function.Pkg().Path() != "strings" {
		t.Fatalf("expected strings.ToUpper, got %v", function)
	}
	field := semantic.ObjectForExpr(selector(t, files, "Port"))
	if field == nil || field.Name() != "Port" {
		t.Fatalf("expected the field Port, got %v", field)
	}
	// Anything that is not an identifier or a selector names no object.
	if semantic.ObjectForExpr(&ast.BasicLit{Kind: token.INT, Value: "1"}) != nil {
		t.Fatal("a literal must not resolve to an object")
	}
}

// nodes returns every node of the loaded files that match.
func nodes[T ast.Node](files []File, match func(T) bool) []T {
	var found []T
	for _, file := range files {
		ast.Inspect(file.Syntax, func(node ast.Node) bool {
			if typed, ok := node.(T); ok && match(typed) {
				found = append(found, typed)
			}
			return true
		})
	}
	return found
}

func TestSemanticAnswersForExpressionsThatAreNotNames(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": plainModule,
		"main.go": `package main

import "strings"

const Limit = 7

func main() {
	_ = 40 + 2
	_ = strings.ToUpper("x")
	_ = Limit
}
`,
	})
	_, files, semantic, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	// A constant expression has a value because the type checker computed it, not because it names a constant.
	sum := nodes(files, func(*ast.BinaryExpr) bool { return true })
	if len(sum) != 1 {
		t.Fatalf("expected one binary expression, got %d", len(sum))
	}
	if value, ok := semantic.ConstantValue(sum[0]); !ok || value.ExactString() != "42" {
		t.Fatalf("expected 40 + 2 to be 42, got %v, %v", value, ok)
	}
	// A call expression has a type but names no object.
	calls := nodes(files, func(call *ast.CallExpr) bool {
		selector, ok := call.Fun.(*ast.SelectorExpr)
		return ok && selector.Sel.Name == "ToUpper"
	})
	typ, ok := semantic.TypeOf(calls[0])
	if !ok || typ.String() != "string" {
		t.Fatalf("expected the call to have type string, got %v, %v", typ, ok)
	}
	// A use of a constant, as opposed to its declaration, resolves to the same object.
	idents := nodes(files, func(ident *ast.Ident) bool { return ident.Name == "Limit" })
	if len(idents) != 2 {
		t.Fatalf("expected the declaration and one use of Limit, got %d", len(idents))
	}
	declared, used := semantic.ObjectForExpr(idents[0]), semantic.ObjectForExpr(idents[1])
	if declared == nil || used == nil || declared != used {
		t.Fatalf("the declaration and the use of Limit must name one object, got %v and %v", declared, used)
	}
	if value, ok := semantic.ConstantValue(idents[1]); !ok || value.ExactString() != "7" {
		t.Fatalf("expected the use of Limit to be 7, got %v, %v", value, ok)
	}
}
