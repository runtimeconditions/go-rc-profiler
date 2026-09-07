package sdkmap

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"

	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/gosource"
	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/profile"
)

// callFor matches a call expression against the mapped SDK symbols.
//
// The match is made on the resolved function object, not on source text, so a
// local function that merely shares a name with an SDK symbol cannot be
// mistaken for it.
func (t *stateTable) callFor(call *ast.CallExpr, calls []Call) (Call, bool) {
	function, ok := t.semantic.ObjectForExpr(call.Fun).(*types.Func)
	if !ok {
		return Call{}, false
	}
	pkg := ""
	if function.Pkg() != nil {
		pkg = function.Pkg().Path()
	}
	receiver := ""
	if signature, ok := function.Type().(*types.Signature); ok && signature.Recv() != nil {
		_, receiver, _ = gosource.NamedTypeIdentity(signature.Recv().Type())
	}

	for _, candidate := range calls {
		if candidate.Symbol.Package != pkg {
			continue
		}
		if candidate.Symbol.Function != "" && receiver == "" && candidate.Symbol.Function == function.Name() {
			return candidate, true
		}
		if candidate.Symbol.Method != "" && candidate.Symbol.Receiver == receiver && candidate.Symbol.Method == function.Name() {
			return candidate, true
		}
	}
	return Call{}, false
}

// receiverState reports the state held by the value a method is called on.
func (t *stateTable) receiverState(call *ast.CallExpr) (resolvedState, bool) {
	selector, ok := gosource.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return resolvedState{}, false
	}
	ident, ok := gosource.Unparen(selector.X).(*ast.Ident)
	if !ok {
		return resolvedState{}, false
	}
	return t.stateFor(ident)
}

// argumentState reports the state held by a value passed as an argument, for
// SDKs that take their dependency as a parameter rather than a receiver.
func (t *stateTable) argumentState(call *ast.CallExpr, source ArgumentSource) (resolvedState, bool) {
	position, ok := t.argumentPosition(call, source)
	if !ok || position >= len(call.Args) {
		return resolvedState{}, false
	}
	ident, ok := gosource.Unparen(call.Args[position]).(*ast.Ident)
	if !ok {
		return resolvedState{}, false
	}
	return t.stateFor(ident)
}

// resolveCondition fills the mapping's template with values read from the call
// site. A required binding that cannot be resolved abandons the condition: a
// half-filled operation would misdescribe what the workload does.
func (t *stateTable) resolveCondition(call *ast.CallExpr, mapping Call, state resolvedState) (profile.Condition, bool) {
	operation := make(map[string]any, len(mapping.ConditionTemplate.Operation)+len(mapping.OperationBindings))
	for name, value := range mapping.ConditionTemplate.Operation {
		operation[name] = value
	}
	for name, source := range mapping.OperationBindings {
		value, ok := t.resolveValue(call, source, state)
		if !ok {
			if source.Optional {
				continue
			}
			return profile.Condition{}, false
		}
		operation[name] = value
	}
	return profile.Condition{
		Kind: mapping.ConditionTemplate.Kind,
		Interface: profile.Interface{
			Type:       mapping.ConditionTemplate.InterfaceType,
			Operations: []profile.Operation{{Fields: operation}},
		},
	}, true
}

// resolveValue reads one value, either from state carried by the receiver or
// from an argument at the call site.
//
// Only compile-time values count. A subject built at runtime cannot be named in
// the profile, so it is reported unresolved rather than guessed at.
func (t *stateTable) resolveValue(call *ast.CallExpr, source ValueSource, state resolvedState) (any, bool) {
	if source.State != "" {
		value, ok := state.values[source.State]
		return value, ok
	}
	if source.Argument == nil {
		return nil, false
	}
	position, ok := t.argumentPosition(call, *source.Argument)
	if !ok || position >= len(call.Args) {
		return nil, false
	}

	expr := t.resolveExpression(call.Args[position])
	if source.Argument.Field != "" {
		expr, ok = structLiteralField(expr, source.Argument.Field)
		if !ok {
			return nil, false
		}
		expr = t.resolveExpression(expr)
	}

	if value, ok := t.semantic.StringValue(expr); ok {
		return value, true
	}
	if list, ok := expr.(*ast.CompositeLit); ok {
		values := make([]string, 0, len(list.Elts))
		for _, element := range list.Elts {
			value, ok := t.stringValue(gosource.Unparen(element))
			if !ok {
				return nil, false
			}
			values = append(values, value)
		}
		return values, true
	}
	if literal, ok := expr.(*ast.BasicLit); ok && literal.Kind == token.STRING {
		value, err := strconv.Unquote(literal.Value)
		return value, err == nil
	}
	return nil, false
}

// structLiteralField reads a named field out of a struct literal, seeing through
// a leading address-of so both a value and a pointer literal work.
func structLiteralField(expr ast.Expr, field string) (ast.Expr, bool) {
	if address, ok := expr.(*ast.UnaryExpr); ok && address.Op == token.AND {
		expr = gosource.Unparen(address.X)
	}
	composite, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil, false
	}
	for _, element := range composite.Elts {
		keyValue, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, ok := keyValue.Key.(*ast.Ident); ok && key.Name == field {
			return gosource.Unparen(keyValue.Value), true
		}
	}
	return nil, false
}

// resolveExpression follows an identifier to the expression it was assigned,
// letting a value passed indirectly still be read.
//
// A variable that was reassigned or had its address taken resolves to itself,
// since its value is no longer knowable. An identifier with a resolved object
// but no tracked value falls back to the name-keyed table, which is the only
// record available for variables the type checker could not resolve.
func (t *stateTable) resolveExpression(expr ast.Expr) ast.Expr {
	unparenthesized := gosource.Unparen(expr)
	ident, ok := unparenthesized.(*ast.Ident)
	if !ok {
		return unparenthesized
	}
	if object := t.semantic.ObjectForExpr(ident); object != nil {
		key := varKey{object: object}
		if t.invalid[key] {
			return unparenthesized
		}
		if resolved := t.values[key]; resolved != nil {
			return gosource.Unparen(resolved)
		}
	}
	key := varKey{name: ident.Name}
	if t.invalid[key] {
		return unparenthesized
	}
	if resolved := t.values[key]; resolved != nil {
		return gosource.Unparen(resolved)
	}
	return unparenthesized
}

// argumentPosition locates an argument by parameter name where the mapping gives
// one, falling back to a fixed position. Resolving by name survives an SDK
// reordering its parameters.
func (t *stateTable) argumentPosition(call *ast.CallExpr, source ArgumentSource) (int, bool) {
	if source.Parameter != "" {
		function, ok := t.semantic.ObjectForExpr(call.Fun).(*types.Func)
		if !ok {
			return 0, false
		}
		signature, ok := function.Type().(*types.Signature)
		if !ok {
			return 0, false
		}
		for position := range signature.Params().Len() {
			if signature.Params().At(position).Name() == source.Parameter {
				return position, true
			}
		}
		return 0, false
	}
	if source.Position == nil || *source.Position < 0 {
		return 0, false
	}
	return *source.Position, true
}

// stringValue resolves an expression to a string via type information, falling
// back to a literal.
func (t *stateTable) stringValue(expr ast.Expr) (string, bool) {
	if value, ok := t.semantic.StringValue(expr); ok {
		return value, true
	}
	return gosource.StringLiteral(expr)
}
