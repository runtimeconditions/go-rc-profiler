package binding

import (
	"go/ast"
	"strconv"

	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/gosource"
)

// fileImports is what one source file can see of the known bindings: which
// local names refer to which binding, and which local variables hold a
// constructed receiver.
//
// It is per-file because import aliases are per-file.
type fileImports struct {
	aliases      map[string]*Binding
	dot          []*Binding
	receiverVars map[string]receiverBinding
	bindings     []*Binding
	semantic     *gosource.Semantic
}

// receiverBinding is a value whose methods declare conditions, together with
// the binding and receiver type it came from.
type receiverBinding struct {
	binding  *Binding
	receiver string
}

// importsForFile maps a file's imports onto the known bindings and then scans
// it for variables holding constructed receivers.
func importsForFile(file *ast.File, bindings []*Binding, semantic *gosource.Semantic) *fileImports {
	imports := &fileImports{
		aliases:      make(map[string]*Binding),
		receiverVars: make(map[string]receiverBinding),
		bindings:     bindings,
		semantic:     semantic,
	}
	byImportPath := make(map[string]*Binding, len(bindings))
	for _, binding := range bindings {
		byImportPath[binding.ImportPath] = binding
	}

	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		binding := byImportPath[path]
		if binding == nil {
			continue
		}
		if spec.Name == nil {
			if binding.PackageName != "" {
				imports.aliases[binding.PackageName] = binding
			}
			continue
		}
		switch spec.Name.Name {
		case ".":
			imports.dot = append(imports.dot, binding)
		case "_":
			continue
		default:
			imports.aliases[spec.Name.Name] = binding
		}
	}
	imports.collectReceiverVars(file)
	return imports
}

// declaresAnything reports whether the file imports any binding at all, which
// is the cheap check that skips files with nothing to extract.
func (i *fileImports) declaresAnything() bool {
	return len(i.aliases) > 0 || len(i.dot) > 0
}

// collectReceiverVars records every variable assigned the result of a binding
// constructor, so later method calls on it can be attributed.
func (i *fileImports) collectReceiverVars(file *ast.File) {
	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.AssignStmt:
			for index, right := range typed.Rhs {
				if index >= len(typed.Lhs) {
					continue
				}
				name, ok := typed.Lhs[index].(*ast.Ident)
				if !ok {
					continue
				}
				i.collectReceiverVar(name.Name, right)
			}
		case *ast.ValueSpec:
			for index, value := range typed.Values {
				if index >= len(typed.Names) {
					continue
				}
				i.collectReceiverVar(typed.Names[index].Name, value)
			}
		}
		return true
	})
}

func (i *fileImports) collectReceiverVar(name string, expr ast.Expr) {
	call, ok := gosource.Unparen(expr).(*ast.CallExpr)
	if !ok {
		return
	}
	if binding, receiver, ok := i.constructorForCall(call); ok {
		i.receiverVars[name] = receiverBinding{binding: binding, receiver: receiver}
	}
}

// constructorForCall reports whether a call is a binding constructor and, if so,
// the receiver type it yields.
func (i *fileImports) constructorForCall(call *ast.CallExpr) (*Binding, string, bool) {
	name, binding, ok := i.callName(call)
	if !ok {
		return nil, "", false
	}
	for _, constructor := range binding.Constructors {
		if constructor.Function == name {
			return binding, constructor.Receiver, true
		}
	}
	return nil, "", false
}

// callName resolves a call to a binding and the name it invokes, discarding any
// type arguments.
func (i *fileImports) callName(call *ast.CallExpr) (string, *Binding, bool) {
	name, _, binding, ok := i.callNameAndTypeArgs(call)
	return name, binding, ok
}

// callNameAndTypeArgs resolves a call to the binding it belongs to, the name it
// invokes, and any explicit type arguments. Generic options carry their schema
// type there rather than as a value argument.
func (i *fileImports) callNameAndTypeArgs(call *ast.CallExpr) (string, []ast.Expr, *Binding, bool) {
	fun, typeArgs := unwrapTypeArgs(call.Fun)

	switch expr := fun.(type) {
	case *ast.SelectorExpr:
		ident, ok := gosource.Unparen(expr.X).(*ast.Ident)
		if !ok {
			return "", nil, nil, false
		}
		binding := i.aliases[ident.Name]
		if binding == nil {
			return "", nil, nil, false
		}
		return expr.Sel.Name, typeArgs, binding, true
	case *ast.Ident:
		// A dot import has no qualifier, so the name itself has to be
		// recognizable as belonging to exactly one binding.
		for _, binding := range i.dot {
			if binding.hasDeclaration(expr.Name) || binding.hasOption(expr.Name) || binding.hasConstant(expr.Name) {
				return expr.Name, typeArgs, binding, true
			}
		}
	}
	return "", nil, nil, false
}

// receiverMethod resolves a method call on a value produced by a binding
// constructor.
//
// Tracking the assignment locally handles the common case. When that fails,
// type information can still identify the receiver, which covers receivers
// obtained any other way, such as a struct field or a function result.
func (i *fileImports) receiverMethod(call *ast.CallExpr) (string, receiverBinding, bool) {
	fun, _ := unwrapTypeArgs(call.Fun)
	expr, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return "", receiverBinding{}, false
	}
	ident, ok := gosource.Unparen(expr.X).(*ast.Ident)
	if !ok {
		return "", receiverBinding{}, false
	}
	if receiver, ok := i.receiverVars[ident.Name]; ok && receiver.binding != nil {
		return expr.Sel.Name, receiver, true
	}

	pkgPath, typeName, ok := i.semantic.ReceiverTypeForSelector(expr)
	if !ok {
		return "", receiverBinding{}, false
	}
	for _, binding := range i.bindings {
		if binding.ImportPath == pkgPath {
			return expr.Sel.Name, receiverBinding{binding: binding, receiver: typeName}, true
		}
	}
	return "", receiverBinding{}, false
}

// unwrapTypeArgs peels explicit type arguments off a call target and returns the
// underlying function expression.
func unwrapTypeArgs(fun ast.Expr) (ast.Expr, []ast.Expr) {
	var typeArgs []ast.Expr
	fun = gosource.Unparen(fun)
	for {
		switch typed := fun.(type) {
		case *ast.IndexExpr:
			typeArgs = append(typeArgs, typed.Index)
			fun = gosource.Unparen(typed.X)
		case *ast.IndexListExpr:
			typeArgs = append(typeArgs, typed.Indices...)
			fun = gosource.Unparen(typed.X)
		default:
			return fun, typeArgs
		}
	}
}
