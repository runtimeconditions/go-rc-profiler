package sdkmap

import (
	"encoding/json"
	"go/ast"
	"go/token"
	"slices"

	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/gosource"
	"github.com/runtimeconditions/go-rc-profiler/extractor/internal/profile"
)

// observation is one condition seen at a call site, kept with the dependency it
// belongs to until the merge groups them.
type observation struct {
	condition          profile.Condition
	extensionID        string
	dependencyIdentity string
}

// ExtractConditions replays the verified mappings against the workload and
// returns the conditions found, with the sorted extension IDs they draw on.
//
// Type information is required. Without it an SDK call cannot be distinguished
// from any other call, so no mapping may be applied at all.
func ExtractConditions(files []gosource.File, semantic *gosource.Semantic, mappings []Mapping) ([]profile.Condition, []string, error) {
	if len(mappings) == 0 || semantic == nil {
		return nil, nil, nil
	}

	var calls []Call
	for _, mapping := range mappings {
		for _, call := range mapping.Calls {
			call.ExtensionID = mapping.Extension.ID
			calls = append(calls, call)
		}
	}

	var observations []observation
	extensions := make(map[string]bool)
	for _, file := range files {
		// State is per-file: a variable in one file says nothing about a
		// same-named variable in another.
		states := newStateTable(semantic)
		ast.Inspect(file.Syntax, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.DeclStmt:
				states.recordValueDeclaration(typed)
			case *ast.AssignStmt:
				states.recordValueAssignments(typed)
				states.recordStateProduction(typed, calls)
			case *ast.UnaryExpr:
				if typed.Op == token.AND {
					states.invalidateAddressOf(typed.X)
				}
			case *ast.CallExpr:
				if found, ok := observeCall(typed, states, calls); ok {
					observations = append(observations, found)
					extensions[found.extensionID] = true
				}
			}
			return true
		})
	}

	ids := make([]string, 0, len(extensions))
	for id := range extensions {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return mergeObservations(observations), ids, nil
}

// observeCall reports the condition a single call contributes, if any.
//
// A call that requires state is skipped unless the receiver or argument
// actually carries it, which is what stops an operation from being attributed
// to a dependency the workload never established.
func observeCall(call *ast.CallExpr, states *stateTable, calls []Call) (observation, bool) {
	mapping, ok := states.callFor(call, calls)
	if !ok {
		return observation{}, false
	}

	state, hasState := states.receiverState(call)
	if mapping.ReceiverState != "" && (!hasState || state.stateType != mapping.ReceiverState) {
		return observation{}, false
	}
	if mapping.ArgumentState != nil {
		argumentState, ok := states.argumentState(call, mapping.ArgumentState.Argument)
		if !ok || argumentState.stateType != mapping.ArgumentState.StateType {
			return observation{}, false
		}
	}
	if mapping.ConditionTemplate.Kind == "" {
		return observation{}, false
	}

	condition, ok := states.resolveCondition(call, mapping, state)
	if !ok {
		return observation{}, false
	}

	// A call that both establishes a dependency and reports a condition, such as
	// connecting, belongs to the identity it just created rather than to
	// whatever its receiver held.
	dependencyIdentity := state.dependencyIdentity
	if assigned := states.callDependencies[call]; assigned != "" {
		dependencyIdentity = assigned
	}
	return observation{condition: condition, extensionID: mapping.ExtensionID, dependencyIdentity: dependencyIdentity}, true
}

// mergeObservations collapses observations that describe the same dependency
// into a single condition carrying all of its operations.
//
// Grouping is by extension, dependency identity, kind, and interface type, so
// two connections to different services stay separate while many calls on one
// connection become one condition. An observation with no identity cannot be
// proven to share a dependency with anything, so it stands alone.
func mergeObservations(observations []observation) []profile.Condition {
	conditions := make([]profile.Condition, 0, len(observations))
	groupIndexes := make(map[string]int)
	for _, item := range observations {
		if item.dependencyIdentity == "" {
			conditions = append(conditions, item.condition)
			continue
		}
		key := item.groupKey()
		index, exists := groupIndexes[key]
		if !exists {
			groupIndexes[key] = len(conditions)
			conditions = append(conditions, item.condition)
			continue
		}
		for _, operation := range item.condition.Interface.Operations {
			if !containsOperation(conditions[index].Interface.Operations, operation) {
				conditions[index].Interface.Operations = append(conditions[index].Interface.Operations, operation)
			}
		}
	}
	return conditions
}

// groupKey identifies the condition an observation merges into. The separator
// cannot appear in any component, so distinct tuples cannot collide.
func (o observation) groupKey() string {
	const separator = "\x00"
	return o.extensionID + separator + o.dependencyIdentity + separator + o.condition.Kind + separator + o.condition.Interface.Type
}

// containsOperation reports whether an equivalent operation was already
// recorded. Operations carry extension-defined fields in an untyped map, so
// they are compared by their serialized form rather than field by field.
func containsOperation(operations []profile.Operation, candidate profile.Operation) bool {
	candidateJSON, err := json.Marshal(candidate)
	if err != nil {
		return false
	}
	for _, operation := range operations {
		operationJSON, err := json.Marshal(operation)
		if err == nil && string(operationJSON) == string(candidateJSON) {
			return true
		}
	}
	return false
}
