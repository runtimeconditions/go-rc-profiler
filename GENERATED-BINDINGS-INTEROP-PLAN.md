# Generated Extension Bindings Interoperability Plan: Go Profiler

## Purpose and acceptance boundary

The Go profiler must generate a complete Runtime Conditions Profile from Go
source that uses automatically generated extension bindings. The supported
end-user environment contains an installed profiler CLI, a workload Go module
whose binding dependencies are resolved by Go, and exact extension definitions
available from installed binding packages, a verified local cache, or supported
URI resolution. Profiling must succeed without an `extensions` repository
checkout, sibling catalog directory, local `replace` directive, or `go run`
from the profiler source tree.

This plan covers the profiler's interoperability work. The extension binding
generator owns the structural manifest format. Its Phase 2 Go API uses inert
declaration functions, marker interfaces, named scalar types, structs, slices,
maps, unions, and typed constants. Phase 4 will complete the manifest's
structural relationships. The profiler must consume that versioned contract
without interpreting extension-specific names or preserving the earlier
handwritten manifest behavior as the generated-binding path.

**Priority:** Correct extraction and complete validation of generated binding
declarations take precedence over the previous SDK mapping feature. SDK mapping
extraction may be disabled, separated, or removed if its automatic execution,
discovery, fallback, or data model conflicts with this goal. It is not a
compatibility requirement for the generated-binding path.

## Current strengths and functional gaps

| Area | Current implementation and gap | Required remediation |
| --- | --- | --- |
| Installed CLI | `main.go` is a CLI, but the documented path is `go run .` with sibling checkouts. | Build and install a versioned CLI artifact. Exercise it from an unrelated workload directory with the profiler source and `extensions` checkout unavailable. |
| Binding discovery | `extractor/internal/binding/discovery.go` discovers resolved imports through `gosource.Module.ResolveImport`, which handles the workload module and local replacements. Downloaded binding modules are not covered by this lookup. Catalog walking is also available. | Resolve each imported binding package through the workload's native Go dependency graph (`go list -deps -json`, with the required module download and verification steps). Read resources only from the returned package directory. Do not search module caches or catalogs recursively during normal profiling. |
| Binding contract | `extractor/internal/binding/manifest.go` accepts the earlier `RuntimeConditionsBinding` / `RuntimeConditionsPackage` mapping of declaration and option functions. Phase 2 emits `RuntimeConditionsBindingManifest` with a provisional symbol inventory. | Parse the completed structural manifest version strictly. Check its package identity, model digest, root extension identity and digest, native symbols, and relationships against the packaged model and resolved source. Reject unknown or mismatched contract versions before extraction. |
| Source reconstruction | `extractor/internal/binding/condition.go` starts from legacy option calls and fixed `profile.Condition` fields; `option.go` handles known targets. It does not reconstruct arbitrary extension-defined object fields from generated Go types. | Match the generated declaration function by resolved package identity and Go type information. Walk typed composite literals, field names, named scalars and constants, slices, maps, union variants, recursive values, optional pointers, and dependency-owned marker contracts. Build a generic Condition data tree using manifest-to-model coordinates. Preserve exact serialized field names and distinguish omission, `nil`, empty collections, and explicit values. |
| Static-value boundary | Legacy extraction accepts a limited set of option arguments and can ignore unmatched subcalls. | Specify the supported Go expression grammar, including safe constant references, conversions, address-of expressions, and pointer construction needed by optional fields. Never execute application or package code. A recognized declaration containing an unsupported or ambiguous value must fail with file, line, column, symbol, and model coordinate; it must not yield a partial Condition. |
| Validation and identity | `extensioncheck.ValidateProfileYAML` applies extension schemas, but the generated-binding trust chain does not yet exist. `extractor.validate` also replaces the emitted `extensions` list with its transitive closure. | Verify packaged manifest, model, release metadata, and root extension bytes as required by their contract. Resolve exact dependency extension definitions and validate core structure, vocabulary ownership, and every applicable JSON Schema. Emit only extension IDs that directly contributed vocabulary; use the full transitive closure for validation. Reject missing or altered artifacts. |
| Failure behavior | `loadWorkload` can silently fall back from `go/packages` to syntax-only extraction unless `RequireGoPackages` is set. `--skip-validation` can write an unchecked profile. | Make the generated-binding production path fail closed when package/type resolution or validation is unavailable. Any diagnostic must prevent writing an accepted profile. Development-only bypasses, if retained, must be clearly separate from the normal CLI success path. |
| SDK mapping interaction | `extractor.ExtractDir` runs SDK mapping extraction alongside binding extraction. | Make generated-binding behavior independently testable. Remove automatic SDK extraction from this path if it changes failure behavior, extension selection, deterministic output, or validation guarantees. |

## Remediation sequence

1. **Freeze the installed-artifact contract.** Record the CLI inputs, package
   resource locations, supported cache/URI resolution, and exact manifest/model
   identity checks. Audit which existing source and validation components can
   be reused without carrying over legacy mapping semantics. The published
   binding package, not a repository path, supplies its binding resources.
2. **Prove native package discovery.** From an isolated workload module, resolve
   an installed or downloaded generated binding and its direct and transitive
   dependencies. Verify the fixed resource location returned by `go list` and
   reject wrong package coordinates, versions, digests, and extension IDs.
3. **Implement structural extraction.** Start with an owned declaration and an
   interface object, then additive fields from another module, nested objects,
   optional pointers, named collections and maps, enums/constants, recursive
   shapes, and unions. Resolve symbols using Go type information, including
   aliases. Apply one generic model-coordinate algorithm to every extension.
4. **Make profile validation mandatory.** Construct a complete YAML profile
   with caller-provided workload identity. Derive direct contributors from
   used generated symbols, resolve the full extension closure, apply all
   matching schemas, and reject an invalid result before writing output.
5. **Stabilize user-facing failures.** Report deterministic diagnostics for
   unsupported expressions, missing package resources, digest mismatches,
   duplicate or conflicting fields, schema failures, and dependency problems.
   Output ordering and bytes must be stable for the same inputs.
6. **Decide the SDK path last.** Keep SDK mapping behavior only if it can meet
   the same installed-artifact, fail-closed, and semantic-validation guarantees
   without complicating the generated-binding path.

## Evidence required before declaring interoperability

- Build the profiler binary and generated binding Go modules as installable
  artifacts. Run the binary against a fresh workload module with no repository
  checkout mounted or accessible. The successful invocation supplies no
  `--extensions-root` and has no local `replace` directive.
- Compare complete generated profile YAML with reviewed fixtures for every
  profile-capable generated construct, including direct and transitive additive
  packages. Type-coverage fixtures that cannot form valid Conditions must have
  explicit expected rejection rather than a weakened validator.
- Show exact failures for malformed manifests, missing resources, altered
  digests, unresolved extension definitions, unsupported dynamic expressions,
  and every branch-dependent schema constraint in the profile fixtures.
- Verify that importing an unused binding does not add its extension to the
  profile, that transitive dependencies are used for validation, and that no
  application or binding code is executed.
- Run the installed-artifact workflow in the available execution environment.
  Check native package resolution, bounded resource use on a representative
  large model, deterministic diagnostics and output, and absence of output
  after failure. Cross-OS and architecture coverage belongs to distribution
  packaging work, not this Phase 4 acceptance gate. Windows is unsupported
  for this phase.

Phase 4 integration should use this evidence as the profiler readiness gate;
source-tree demos and legacy-manifest golden tests alone do not satisfy it.
