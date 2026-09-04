# Go SDK mapping consumer contract

## Application experience

An application developer does not configure an SDK mapping. The profiler resolves the application's ordinary Go module graph, discovers static Runtime Conditions metadata shipped inside imported modules, verifies it, and analyzes existing application calls without importing or executing the SDK or application.

If an imported SDK has no mapping, profiling continues with the declarative extension bindings and other mapped SDKs that are available. An unresolved application value does not produce a widened condition. Invalid installed metadata is different: an identity, version, digest, path, or extension mismatch stops profiling because silently trusting corrupt metadata would make every emitted condition suspect.

## Package layout

An SDK module that participates ships an index at `runtimeconditions/index.yaml`. Each indexed mapping stays within the module directory and is protected by a SHA-256 digest. Go modules already package ordinary non-Go files, so this layout requires no runtime dependency, initialization code, public API change, or special package-data declaration.

The index identifies the exact Go module path and selected module version. A mapping identifies the same owner and version, records its own semantic digest, and targets one exact extension ID, semantic version, and semantic digest. Normal `go.mod` selection remains the application compatibility mechanism; Runtime Conditions does not add a second compatibility lock.

## Division of responsibility

The extension owns condition kinds, interfaces, operation forms, validation rules, and adapter-actionable semantic distinctions. An SDK mapping selects exact Go symbols and identifies which named parameters, typed fields, or prior producer-state fields populate the extension operation. The SDK's real declarations validate those selections. The application supplies concrete values through ordinary source. The profiler implements generic Go resolution and does not contain SDK or extension field vocabulary.

## Static analysis

Mapped calls use exact Go package, function, declared receiver, and method identities resolved by `go/types`. Mappings can select a parameter by its declared name; a release-time source validator ensures that name still exists. The legacy numeric-position form remains readable, with positions excluding the receiver, but named parameters are the preferred authoring form because a signature change fails with a useful name rather than silently shifting a binding.

A value binding can select a compile-time string or string slice from an ordinary argument or from a keyed field in a typed struct literal. The struct literal may be inline, assigned directly with `:=`, or introduced by an initialized local `var` declaration before the mapped call. The mapping supplies the parameter and field names; the profiler contains no special cases for names such as `Name`, `Bucket`, or `Subject`.

Remembered local values are deliberately conservative. An ordinary reassignment, field assignment, index assignment, or explicit address-taking operation invalidates the local. Unresolved expressions emit no condition when a required field is missing and omit only that field when the mapping marks it optional. Aliases, helper-function returns, values stored in structs or containers, arbitrary control-flow joins, and implicit or indirect escape paths are outside the current proof boundary.

A mapped call can produce named static state without emitting a condition. Later mapped calls may require state on their receiver or on one named argument, and they may bind fields from that state into a fixed operation template. A producer can start a new dependency identity or inherit one from its receiver or state-bearing argument. This supports generic delegation and wrapper APIs without assuming that every public call directly reaches a distinct external service.

Compatible observations merge only when their extension identity, condition kind, interface type, and source-proven dependency identity match. Duplicate operations within that group are removed. Calls without a proven dependency identity remain separate. This prevents two independently constructed clients or connections from being collapsed into one requirement merely because they use the same extension.

## Validation and failure boundary

The profiler verifies the module index, mapping byte digest, mapping semantic digest, exact module version, mapping path containment, mapping identity, and exact extension coordinates before using a call record. Generated profiles are then validated against the extension's vocabulary and complete Draft 2020-12 JSON Schema. The schema step is essential because operation fields are extension-owned and must not be hard-coded into the Go profiler.

SDK metadata is discovered only for modules actually imported by the application. A malformed mapping in an unrelated module therefore cannot break a profile, while malformed metadata for an imported SDK fails visibly.

## Maintainer validation

The profiler validates mapping consumption, not whether unused mapping symbols still exist in a new SDK release. SDK integrations should run a deterministic source-surface check during release automation. That check should validate exact symbols, named parameters, typed fields, producer and consumer state, and dependency identity, and it should present newly introduced public behavior for explicit semantic classification. Coverage classifications belong to maintenance tooling; they are not SDK mapping data and are not emitted in application profiles.

The NATS authorship experiment provides the current reference implementation and runs ordinary application fixtures through the real profiler as an acceptance gate. Its project-specific scripts are prototypes for shared tooling, not code that every SDK maintainer should be expected to create.
