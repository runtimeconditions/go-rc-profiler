# Go SDK mapping consumer contract

## Application experience

An application developer does not configure an SDK mapping. The profiler resolves the application's ordinary Go module graph, discovers static Runtime Conditions metadata shipped inside imported modules, verifies it, and analyzes the application's existing calls without importing or executing the SDK or application.

If an imported SDK has no mapping, profiling continues with the declarative extension bindings and other mapped SDKs that are available. An unresolved application value does not produce a widened condition. Invalid installed metadata is different: an identity, version, digest, path, or extension mismatch stops profiling because silently trusting corrupt metadata would make every emitted condition suspect.

## Package layout

An SDK module that participates ships an index at `runtimeconditions/index.yaml`. Each indexed mapping stays within the module directory and is protected by a SHA-256 digest. Go modules already package ordinary non-Go files, so this layout requires no runtime dependency, initialization code, public API change, or special package-data declaration.

The index identifies the exact Go module path and selected module version. A mapping identifies the same owner and version, records its own semantic digest, and targets one exact extension ID, semantic version, and semantic digest. Normal `go.mod` selection remains the application compatibility mechanism; Runtime Conditions does not add a second compatibility lock.

## Static analysis

Mapped calls use exact Go package, function, declared receiver, and method identities resolved by `go/types`. Argument positions exclude the receiver. A mapping can bind compile-time strings and string slices from ordinary arguments or keyed struct literals into an extension-defined operation.

A call can also produce named static state. Later calls on that result may bind fields from the producer state into a fixed condition template. The NATS proof uses this generic mechanism to carry a key/value or object-store bucket from `KeyValue`, `CreateKeyValue`, `ObjectStore`, or `CreateObjectStore` into later read, write, and watch calls. It carries stream and consumer names from `Consumer` into `Consume`. This is not NATS vocabulary in the profiler; state names and condition fields come from the mapping and extension.

The current state analysis intentionally handles local assignment from one mapped producer call. It fails closed for unresolved values, state stored through unsupported containers, return propagation across application functions, and dynamic composite construction. Those cases require either a future generic data-flow expansion or an explicit no-op extension declaration.

## Validation and failure boundary

The profiler verifies the module index, mapping byte digest, mapping semantic digest, exact module version, mapping path containment, mapping identity, and exact extension coordinates before using a call record. Generated profiles are then validated against the extension's vocabulary and its complete Draft 2020-12 JSON Schema. The schema step is important because operation fields are extension-owned and must not be hard-coded into the Go profiler.

SDK metadata is discovered only for modules actually imported by the application. A malformed mapping in an unrelated module therefore cannot break a profile, while a malformed mapping for an imported SDK fails visibly.

## Maintainer validation

The profiler validates consumption, not whether unused mapping symbols still exist in a new SDK release. SDK repositories should run a deterministic source-surface check during release automation. The NATS authorship proof provides that check and verifies all reviewed symbols, argument positions, struct fields, produced states, and state consumers without executing the SDK.
