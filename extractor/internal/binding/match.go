package binding

import (
	"go/ast"
	"slices"

	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/profile"
)

// optionMatch is a resolved option call: which binding declared the option,
// which option it is, and the type arguments the call supplied.
type optionMatch struct {
	binding  *Binding
	option   Option
	typeArgs []ast.Expr
}

// declarationForCall reports whether a call declares a condition, trying a
// package function first and then a method on a constructed receiver.
//
// A method matches only when the receiver type agrees, unless the manifest
// leaves the receiver open.
func (i *fileImports) declarationForCall(call *ast.CallExpr) (*Binding, Declaration, bool) {
	if name, binding, ok := i.callName(call); ok {
		for _, declaration := range binding.Declarations {
			if declaration.Function == name {
				return binding, declaration, true
			}
		}
	}

	if name, receiver, ok := i.receiverMethod(call); ok {
		for _, declaration := range receiver.binding.Declarations {
			if declaration.Method == name && (declaration.Receiver == "" || declaration.Receiver == receiver.receiver) {
				return receiver.binding, declaration, true
			}
		}
	}

	return nil, Declaration{}, false
}

// nestedOption resolves a call against a specific list of nested options, such
// as the request and response options inside an operation. The call must come
// from the same binding that declared the enclosing option.
func (i *fileImports) nestedOption(call *ast.CallExpr, binding *Binding, options []Option) (optionMatch, bool) {
	name, typeArgs, callBinding, ok := i.callNameAndTypeArgs(call)
	if !ok || callBinding != binding {
		return optionMatch{}, false
	}
	for _, option := range options {
		if option.Function == name {
			return optionMatch{binding: callBinding, option: option, typeArgs: typeArgs}, true
		}
	}
	return optionMatch{}, false
}

// conditionOption resolves a call passed directly to a declaration.
//
// Options private to the declaration win. Failing that, any binding may
// contribute a standalone option, which is how one extension refines a
// condition declared by another. Those options say which kinds and interface
// types they apply to, and are ignored where they do not fit.
func (i *fileImports) conditionOption(call *ast.CallExpr, declarationBinding *Binding, declaration Declaration, condition profile.Condition) (optionMatch, bool) {
	name, typeArgs, optionBinding, ok := i.callNameAndTypeArgs(call)
	if !ok {
		return optionMatch{}, false
	}
	if optionBinding == declarationBinding {
		for _, option := range declaration.Options {
			if option.Function == name {
				return optionMatch{binding: optionBinding, option: option, typeArgs: typeArgs}, true
			}
		}
	}
	for _, option := range optionBinding.Options {
		if option.Function == name && option.appliesTo(condition) {
			return optionMatch{binding: optionBinding, option: option, typeArgs: typeArgs}, true
		}
	}
	return optionMatch{}, false
}

// appliesTo reports whether a standalone option may refine a condition. An
// empty restriction means the option is unrestricted on that axis.
func (o Option) appliesTo(condition profile.Condition) bool {
	if len(o.AppliesToKinds) > 0 && !slices.Contains(o.AppliesToKinds, condition.Kind) {
		return false
	}
	if len(o.AppliesToInterfaceTypes) > 0 && condition.Interface.Type != "" && !slices.Contains(o.AppliesToInterfaceTypes, condition.Interface.Type) {
		return false
	}
	return true
}
