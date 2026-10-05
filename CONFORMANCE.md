# Go profiler Phase 4 conformance report

The Phase 4 evidence below records the pre-migration contract and remains
historical evidence. On 2026-10-05 the active contract moved to HTTPS
`<uri>:<version>` identifiers, `metadata.uri`/`metadata.version`, catalog URL
derivation with default provider `rc`, and core schema `0.2.0`. Active
conformance inputs and models were regenerated; the original v4 evidence was
preserved. This identity migration does not yet add remote retrieval to the
installed profiler CLI. See the
[identity migration result](conformance/identity-contract-2026-10-05/result.yaml)
for its separate installed CLI checks.

**Result (2026-10-04): the Go profiler's Phase 4 obligations pass in the tested
local environment.** The separately installed CLI passed all **19 positive**
and **six negative** prepared consumer workloads against the official v4
assembly. The consolidated
[Phase 4 review](../phase4-dependency-schema-review-20261004/consolidated-review.yaml)
accepts every applicable Section 13 gate (**1–14 and 16**) across the 14 Go
modules and 14 Python distributions. Its
[shared gate evidence](../phase4-dependency-schema-review-20261004/evidence-final/gates.yaml)
and the [Phase 4 summary](../extensions/tooling/extension-bindings/PHASE-4.md)
record the package-generation checks. This report explains the Go profiler's
installed CLI evidence and how it fits that review.

The evidence spans the original 18-positive/five-negative run, the added
dependency-only schema case, the resulting resolver repair, and the v4 rerun
with matching release provenance. Acceptance uses test-only assembled packages
and a local Go module proxy. The result establishes installed profiler
behavior, separately from package publication and binary distribution.

## Contract and installed boundary

| Piece | Role |
| --- | --- |
| [Implementation contract](../extensions/tooling/extension-bindings/IMPLEMENTATION.md) | Section 11 specifies the four fixed package resources and structural extraction. Section 13 defines verification gates and the Phase 4 exit criteria. |
| [Fifth-draft specification](../spec/docs/fifth-draft.md) | Section 5.1 defines extension URI IDs; Sections 5.2 and 9.5 define resolved dependencies and Adapter lookup. This report distinguishes that profile-facing contract from Go package retrieval. |
| [Core profile schema](../spec/schema/runtimeconditions.profile.v0.1.0.schema.yaml) | Linked into the profiler executable by [the build script](scripts/build-with-core-schema.sh); the installed CLI validates the complete profile without a schema checkout. |
| [Reviewed conformance catalog](../extensions/tooling/extension-bindings/fixtures/conformance.yaml) | Defines complete Conditions, direct contributors, positive declarations, and negative constraints independently of profiler output. |
| [Fixture assembler](../extensions/tooling/extension-bindings/fixtures/assemble.py) and [consumer preparer](../extensions/tooling/extension-bindings/fixtures/prepare_results.py) | Build test-only modules, then materialize separate workloads, expected profiles, exact diagnostics, commands, and hashes. Preparation never invokes the profiler. |
| Go package graph and [extensioncheck](extensioncheck/) | Resolve imports through Go's module graph, read only the downloaded package's fixed resources, verify release identities and digests, and compute the full extension closure without executing package code. |
| Go extractor and CLI | Statically reconstruct generated declarations, emit only directly contributing extension IDs, validate the complete profile against core and every applicable extension schema in the closure, and write only after validation succeeds. |

The profiler receives a workload module and dependencies installed through
Go tooling. It does not receive the `extensions` checkout, fixture assembly
directory, or local extension-definition paths. Each resolved binding package
supplies its declaration source (`bindings.go` in these fixtures) and
`runtimeconditions.bindings.yaml`,
`runtimeconditions.binding-model.yaml`, `runtimeconditions.extension.yaml`, and
`runtimeconditions.binding-release.yaml` at the package location. The runner
checks the four resource bytes against its reviewed inventory and the files in
Go's isolated module cache.

For generated packages, `extensioncheck` validates the strict v1alpha2
structural manifest, normalized model, root extension, and v1alpha1 release
metadata, then checks their linked identities, digests, package coordinates,
and dependency packages. The `generate` CLI uses typed static extraction and
requires this validation before writing YAML. There is no `generate-legacy`
entry point. SDK mapping extraction remains an opt-in library path; it is not
part of the Phase 4 installed CLI acceptance asserted here.
The contract schemas under `extensioncheck/schema/` are checked-in copies
embedded in the binary, not a runtime dependency on the `extensions`
repository. Their longer-term versioned distribution and drift detection are
tracked in [binding-tooling future work](../extensions/tooling/extension-bindings/FUTURE_WORK.md).

The Go module coordinate retrieves the binding package. The extension ID in a
profile identifies the extension definition and is independent of that module
coordinate. The profiler uses the downloaded package resources to validate
generation; it does not fetch definitions over HTTP by extension ID. A later
consumer or Adapter must locate the declared extension IDs and their
transitive dependencies before interpreting the profile. Under fifth-draft
§5.1, those IDs have the form `<absolute HTTP(S) URI>:<version>`; they are not
file paths into the `extensions` source checkout.

The v4 conformance fixtures use `urn:runtimeconditions:conformance:...`
extension IDs. They exercise exact ID propagation, ownership, and closure but
do **not** demonstrate the fifth-draft §5.1 HTTP(S) identifier rule or remote
Adapter lookup. This distinction limits the conformance claim below; the
fixture proxy is a Go module source for testing, not an extension-definition
service for end users.

## Complete consumer corpus

The final [v4 assembly](../phase4-dependency-schema-fixtures-v4-20261004/)
contains **14 Go modules** from ten positive source cases. The
[prepared inventory](../phase4-dependency-schema-results-v4-20261004/inventory.yaml)
contains **19 positive** and **six negative** Go workloads. They cover all
**11 emitted Go calls** plus **one completed deferred declaration**. Every
positive has an independently reviewed, complete YAML profile; every negative
has an exact diagnostic, failure stage, and absent-output expectation.

| Source case | Positive workload IDs | Negative workload IDs | Main check |
| --- | --- | --- | --- |
| `01-owned-kind-interface` | `owned` | — | Owned kind, interface, and field |
| `02-additive-field` | `additive-owner`, `additive-consumer` | — | Direct additive ownership |
| `03-transitive-closure` | `transitive-leaf-consumer`, `transitive-middle-consumer`, `transitive-root-consumer` | — | Complete consumer declarations and transitive closure |
| `06-recursive-reference` | `recursive` | — | Recursive object and required values |
| `07-object-alternatives` | `alternatives-command`, `alternatives-image` | `alternatives-missing`, `alternatives-both` | Branch-dependent `oneOf` |
| `08-heterogeneous-union` | `union-object`, `union-string` | `union-missing-id` | Object and scalar union variants |
| `09-collections-and-maps` | `collections`, `collection-second-value` | `collection-invalid-value` | Arrays, maps, and allowed numbers |
| `10-scoped-domains-collisions` | `scoped-grpc`, `scoped-http`, `scoped-http-proxy`, `scoped-optional-omitted` | `scoped-invalid-value` | Scoped domains, collisions, and optional omission |
| `11-source-name-preservation` | `source-names` | — | Exact serialized names, including Unicode |
| `13-dependency-schema-only` | `dependency-schema-only` | `dependency-schema-invalid` | Validation-only dependency |

The three normalizer negatives, `04-dependency-cycle`,
`05-vocabulary-conflict`, and `12-unsupported-structural-keyword`, have exact
expected diagnostics and produce no installable module. Their sources and
checkpoints are under the [shared conformance cases](../extensions/tooling/extension-bindings/model/conformance/cases/).

The reviewed oracles assert exact serialized source names, including Unicode
in case 11. The complete YAML comparisons also assert each profile's direct
`extensions` list. The inventory independently records the full dependency
closure for every workload; the runner compares it with the installed graph.
The six profiler negatives fail at these reviewed stages:

| Stage | Workload IDs |
| --- | --- |
| `structure` | `union-missing-id` |
| `vocabulary` | `scoped-invalid-value` |
| `schema` | `alternatives-missing`, `alternatives-both`, `collection-invalid-value`, `dependency-schema-invalid` |

Case 13 proves the distinction between emitted IDs and validation closure.
The root package owns `job`, `process`, and `command`. Its dependency owns **no
vocabulary** but provides a `command-limit` schema with `maxLength: 6`. The
positive profile emits only the root extension ID, while the installed
verifier finds both modules in the closure and applies both schemas. The
`dependency-schema-invalid` declaration uses `too-long`; the dependency
schema rejects it at the recorded schema stage with its exact diagnostic and
no profile file. No root schema duplicates that limit.

The [original acceptance bundle](conformance/phase4-go-2026-10-04/) passed
18 positives and five negatives, covering ten emitted calls and one deferred
declaration. It did not distinguish direct contributors from the full closure:
those sets were equal in every case. Adding case 13 exposed a profiler defect
in the [v3 run](conformance/phase4-go-dependency-schema-2026-10-04/): an
unimported, validation-only dependency prompted `go list` from the workload
and a request to edit `go.mod`. The [recorded failure](conformance/phase4-go-dependency-schema-2026-10-04/resolver-defect.yaml)
led to a resolver repair: it selects the dependency from Go's module build
list and inspects the exact downloaded package without requiring an import or
source checkout. `TestInstalledValidationOnlyDependency` guards that path.
The v3 run then passed 19 positives and six negatives, but its immutable
release manifests still recorded the earlier binary hash. The official v4
assembly corrected that provenance and supplied the final matched rerun.
The [v3-to-v4 comparison](conformance/phase4-go-matched-provenance-2026-10-04/assembly-provenance-review.yaml)
shows that Go release metadata and dependent archive locks changed while
generated Go source remained identical. Separately, the 14 Python wheels,
their 56 fixed resources, 50 Python workload files, and 25 Python expectations
were byte-identical across v3 and v4, as recorded in the
[Python equivalence report](../phase4-dependency-schema-review-20261004/python-v4-equivalence.yaml).
The approved case 13 additions to the
[normalizer suite](../extensions/tooling/extension-bindings/normalizer/conformance_test.go),
[Go emitter suite](../extensions/tooling/extension-bindings/emitters/go/conformance_test.go),
and [reviewed model checkpoint](../extensions/tooling/extension-bindings/model/conformance/expected/13-dependency-schema-only/runtimeconditions.binding-model.yaml)
changed no production source or assembled package bytes.

## Installed CLI result

The definitive [v4 Go result](conformance/phase4-go-matched-provenance-2026-10-04/result.yaml)
preserves the exact commands, version and SHA-256 of the tested executable,
package archive and resource hashes, resolved module-cache directories,
closure checks, outputs, diagnostics, and canary results. Its
[reviewed inventory](conformance/phase4-go-matched-provenance-2026-10-04/inventory.yaml),
[oracles](conformance/phase4-go-matched-provenance-2026-10-04/oracles/),
[actual profiles](conformance/phase4-go-matched-provenance-2026-10-04/actual/profiles/),
and [actual diagnostics](conformance/phase4-go-matched-provenance-2026-10-04/actual/diagnostics/)
are retained together. The tested executable SHA-256 is
`b2e2aee5e10af47a0e75998d3ac591ee17f2887aaaddeae46405296763bc1444`;
**all 14 release manifests record that same digest**. The approved core
schema source SHA-256 is
`ad101336b676b468ec975aff45c21749df22fddf422371156c586ed62abf3223`.
Its `$id` is
`https://runtimeconditions.io/schemas/profile/0.1.0/runtimeconditions.profile.schema.yaml`,
its version is `0.1.0`, and its semantic SHA-256 is
`49890a0f3e7276d1e480d654176672d977df9c63094f3a24983b0a8102e1a3e3`.
The run used `go version go1.26.5 darwin/arm64`. The result file also records
the reviewed catalog digest, prepared inventory digest, fixture assembler and
preparer digests, and every package archive and resource digest.

| Check | Final result |
| --- | --- |
| Positive profiles | 19/19 successful; complete YAML matches the reviewed bytes; a second run produces identical bytes. |
| Negative declarations | 6/6 fail at the reviewed `structure`, `vocabulary`, or `schema` stage with exact normalized diagnostics, nonzero exit, and no profile output. |
| Normalizer negatives | 3/3 produce exact diagnostics and no model. |
| Ownership and closure | Direct contributors match the emitted `extensions`; the full installed closure matches the inventory for all 25 workloads, including case 13's validation-only dependency. |
| Installed packages | 14/14 module archives and four resources per module match recorded hashes; `go list` resolves each binding in an isolated module cache. |
| Execution boundary | The CLI runs with `GOPROXY=off`; effective source-checkout read denials, workload `init` panics, and a downloaded binding `init` panic canary do not interfere with static profiling. |
| Go regressions | Schema-linked `go test ./... -count=1`, including `TestInstalledValidationOnlyDependency`, and `go vet ./...` pass; [checks.yaml](conformance/phase4-go-matched-provenance-2026-10-04/checks.yaml) links the logs. |

The runner first downloads each workload's dependencies through the test-only
Go proxy into a separate module cache. It then sets `GOPROXY=off` for the
installed profiler. A native macOS `sandbox-exec` policy denies reads of the
`extensions`, `spec`, and profiler source checkouts; three denial controls
confirm those paths are blocked before the profiling checks. Workload source
contains a panicking `init`. A separate derived binding archive also contains
a panicking `init` while retaining the four official resource bytes. The
expected profile still appears, demonstrating that the profiler analyzes
source and package data without executing either package or application code.

The final acceptance invocation, also stored in `result.yaml`, was:

```sh
cd /Users/colacy/code/github.com/runtimeconditions/go-rc-profiler
/private/tmp/rc-profiler-step3-venv/bin/python conformance/phase4-go-matched-provenance-2026-10-04/run.py \
  --fixtures /Users/colacy/code/github.com/runtimeconditions/phase4-dependency-schema-fixtures-v4-20261004 \
  --results /Users/colacy/code/github.com/runtimeconditions/phase4-dependency-schema-results-v4-20261004 \
  --profiler /private/tmp/rc-phase4-profiler-dependency-fix-v2-20261004 \
  --schema /Users/colacy/code/github.com/runtimeconditions/spec/schema/runtimeconditions.profile.v0.1.0.schema.yaml \
  --catalog /Users/colacy/code/github.com/runtimeconditions/extensions/tooling/extension-bindings/fixtures/conformance.yaml \
  --source-root /Users/colacy/code/github.com/runtimeconditions \
  --cache-root /private/tmp/rc-phase4-go-v4-acceptance-cache-20261004 \
  --output /Users/colacy/code/github.com/runtimeconditions/go-rc-profiler/conformance/phase4-go-matched-provenance-2026-10-04
```

For a repeat run, choose a new external cache and output directory. The
[regression runner](conformance/phase4-go-matched-provenance-2026-10-04/check-go.py)
and its `checks.yaml` record the separate schema-linked test and vet commands.
The regression runner can be replayed with:

```sh
/private/tmp/rc-profiler-step3-venv/bin/python \
  /Users/colacy/code/github.com/runtimeconditions/go-rc-profiler/conformance/phase4-go-matched-provenance-2026-10-04/check-go.py \
  --profiler-source /Users/colacy/code/github.com/runtimeconditions/go-rc-profiler \
  --core-schema /Users/colacy/code/github.com/runtimeconditions/spec/schema/runtimeconditions.profile.v0.1.0.schema.yaml \
  --fixture-proxy /Users/colacy/code/github.com/runtimeconditions/phase4-dependency-schema-fixtures-v4-20261004/go/proxy \
  --installed-profiler /private/tmp/rc-phase4-profiler-dependency-fix-v2-20261004 \
  --output /private/tmp/rc-phase4-go-regression-recheck
```

It runs `go test ./... -count=1` with the approved schema linked through Go
linker flags and `go vet ./...`; the exact expanded command and passing logs
are preserved in [checks.yaml](conformance/phase4-go-matched-provenance-2026-10-04/checks.yaml).
The [build script](scripts/build-with-core-schema.sh) links the approved core
schema into the installed binary; schema validation at runtime does not read
the schema checkout.

## Consolidated Phase 4 decision and limits

The [consolidated review](../phase4-dependency-schema-review-20261004/consolidated-review.yaml)
combines this Go result, the [Python installed CLI result](../python-profiler-dependency-schema-acceptance-final-20261004/summary.yaml)
(19 exact positives, six exact negatives, and 29 supplemental checks: **54/54**),
and shared verification for all **28 packages**. The normalizer covers 13
cases; Go native AST checks cover all 14 models; the Python emitter suite
passes 98 tests. Every applicable Phase 4 gate (1–14 and 16) passes,
and the review records no remaining Phase 4 acceptance blocker in the tested
local scope. Gate 15 is assigned to Phase 5 production orchestration.

Minimum-version Go 1.22.12 binding and conformance sources compiled and
passed static analysis; those test binaries were not executed. Native Go
suites ran with the current `go1.26.5 darwin/arm64` toolchain, and Python
minimum-version checks used Python 3.11.16. The fixture assembler and local
proxy are test-only. The acceptance requires the profiler to work in the
environment in which it runs; qualification on other operating systems and
architectures belongs to distribution work.

| Status | Conclusion |
| --- | --- |
| **Passed** | The Go profiler's Phase 4 installed-package, exact-output, failure, closure, integrity, source-isolation, and nonexecution checks; schema-linked Go regressions and vet; all applicable shared Phase 4 gates. |
| **Failed** | No check in the final v4 Go acceptance or consolidated Phase 4 review. The earlier resolver and release-provenance defects were repaired and rerun. |
| **Pending outside this Phase 4 result** | Published binding modules and profiler binaries; Phase 5 production orchestration; automated distribution and drift checks for embedded contract schemas; a separate fifth-draft §5.1 HTTP(S) ID fixture and evidence of consumer or Adapter resolution by URI ID. The URN-based v4 fixtures cannot establish that last claim. |

Accordingly, the Go profiler's **Phase 4 obligations are satisfied in the
tested local scope**. This result does not assert that the current fixture
profiles meet the fifth-draft HTTP(S) ID rule or that remote resolution,
production publication, and distribution have been demonstrated.
