package sdkmap

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/gosource"
)

// varKey identifies a variable within one file.
//
// Type information gives a variable a unique object, which distinguishes
// same-named variables in different scopes. When an identifier cannot be
// resolved to an object the name is the only handle available, so the two forms
// share one key space and never collide.
type varKey struct {
	object types.Object
	name   string
}

// resolvedState is what a variable is known to hold.
//
// dependencyIdentity groups everything reached through the same underlying
// dependency, so operations performed via one connection collapse into one
// condition rather than appearing as many.
type resolvedState struct {
	stateType          string
	values             map[string]any
	dependencyIdentity string
}

// stateTable tracks, for one file, what each variable holds.
//
// values records compile-time expressions assigned to variables so an argument
// passed indirectly can still be resolved. invalid marks variables that were
// reassigned or had their address taken, after which no assumption about their
// value is safe.
type stateTable struct {
	semantic *gosource.Semantic

	states           map[varKey]resolvedState
	values           map[varKey]ast.Expr
	invalid          map[varKey]bool
	callDependencies map[*ast.CallExpr]string
}

func newStateTable(semantic *gosource.Semantic) *stateTable {
	return &stateTable{
		semantic:         semantic,
		states:           make(map[varKey]resolvedState),
		values:           make(map[varKey]ast.Expr),
		invalid:          make(map[varKey]bool),
		callDependencies: make(map[*ast.CallExpr]string),
	}
}

// keyFor prefers the resolved object and falls back to the identifier's name.
func (t *stateTable) keyFor(ident *ast.Ident) varKey {
	if object := t.semantic.ObjectForExpr(ident); object != nil {
		return varKey{object: object}
	}
	return varKey{name: ident.Name}
}

// stateFor reports the state held by the variable an identifier names.
func (t *stateTable) stateFor(ident *ast.Ident) (resolvedState, bool) {
	state, ok := t.states[t.keyFor(ident)]
	return state, ok
}

// recordValueDeclaration tracks the initializers of a var declaration.
func (t *stateTable) recordValueDeclaration(declaration *ast.DeclStmt) {
	general, ok := declaration.Decl.(*ast.GenDecl)
	if !ok || general.Tok != token.VAR {
		return
	}
	for _, spec := range general.Specs {
		values, ok := spec.(*ast.ValueSpec)
		if !ok || len(values.Names) != len(values.Values) {
			continue
		}
		for index, ident := range values.Names {
			if ident.Name == "_" {
				continue
			}
			t.values[t.keyFor(ident)] = values.Values[index]
		}
	}
}

// recordValueAssignments tracks assignments that define a variable and
// invalidates anything else.
//
// Only a direct one-to-one definition is trustworthy. Reassignment, or writing
// through a field or index, means the value can no longer be read off the
// original expression.
func (t *stateTable) recordValueAssignments(assign *ast.AssignStmt) {
	for index, left := range assign.Lhs {
		ident := assignmentRoot(left)
		if ident == nil || ident.Name == "_" {
			continue
		}
		isDirectDefinition := gosource.Unparen(left) == ident &&
			t.semantic.IsDefinition(ident) &&
			len(assign.Lhs) == len(assign.Rhs)
		if isDirectDefinition {
			t.values[t.keyFor(ident)] = assign.Rhs[index]
			continue
		}
		t.invalidate(ident)
	}
}

// invalidateAddressOf drops any tracked value for a variable whose address is
// taken, since it may be written through the pointer.
func (t *stateTable) invalidateAddressOf(expr ast.Expr) {
	ident := assignmentRoot(expr)
	if ident == nil || ident.Name == "_" {
		return
	}
	t.invalidate(ident)
}

func (t *stateTable) invalidate(ident *ast.Ident) {
	key := t.keyFor(ident)
	delete(t.values, key)
	t.invalid[key] = true
}

// assignmentRoot finds the variable at the base of an assignment target, so
// writing to a field or element invalidates the whole variable.
func assignmentRoot(expr ast.Expr) *ast.Ident {
	switch typed := gosource.Unparen(expr).(type) {
	case *ast.Ident:
		return typed
	case *ast.SelectorExpr:
		return assignmentRoot(typed.X)
	case *ast.IndexExpr:
		return assignmentRoot(typed.X)
	default:
		return nil
	}
}

// recordStateProduction records the state established by a call assigned to a
// variable, such as a connection or a derived handle.
//
// The call is ignored unless every required binding resolves, because state
// carrying unknown values would attach operations to a dependency the profile
// cannot describe.
func (t *stateTable) recordStateProduction(assign *ast.AssignStmt, calls []Call) {
	if len(assign.Rhs) != 1 || len(assign.Lhs) == 0 {
		return
	}
	callExpr, ok := gosource.Unparen(assign.Rhs[0]).(*ast.CallExpr)
	if !ok {
		return
	}
	call, ok := t.callFor(callExpr, calls)
	if !ok || call.Produces == nil {
		return
	}

	receiverState, _ := t.receiverState(callExpr)
	argumentState := resolvedState{}
	if call.ArgumentState != nil {
		var ok bool
		argumentState, ok = t.argumentState(callExpr, call.ArgumentState.Argument)
		if !ok || argumentState.stateType != call.ArgumentState.StateType {
			return
		}
	}

	values := make(map[string]any)
	for name, source := range call.Produces.Bindings {
		value, ok := t.resolveValue(callExpr, source, receiverState)
		if !ok {
			if source.Optional {
				continue
			}
			return
		}
		values[name] = value
	}

	ident, ok := gosource.Unparen(assign.Lhs[0]).(*ast.Ident)
	if !ok || ident.Name == "_" {
		return
	}

	dependencyIdentity := ""
	switch call.Produces.DependencyIdentity {
	case "new":
		dependencyIdentity = t.newDependencyIdentity(ident)
	case "inherit":
		dependencyIdentity = receiverState.dependencyIdentity
		if dependencyIdentity == "" {
			dependencyIdentity = argumentState.dependencyIdentity
		}
	}
	if dependencyIdentity != "" {
		t.callDependencies[callExpr] = dependencyIdentity
	}
	t.states[t.keyFor(ident)] = resolvedState{
		stateType:          call.Produces.StateType,
		values:             values,
		dependencyIdentity: dependencyIdentity,
	}
}

// newDependencyIdentity mints an identity for a freshly established dependency,
// derived from the variable that receives it. An identifier with no resolved
// object yields no identity, so its operations stay ungrouped rather than
// merging with an unrelated dependency.
func (t *stateTable) newDependencyIdentity(ident *ast.Ident) string {
	object := t.semantic.ObjectForExpr(ident)
	if object == nil {
		return ""
	}
	return fmt.Sprintf("object:%p", object)
}
