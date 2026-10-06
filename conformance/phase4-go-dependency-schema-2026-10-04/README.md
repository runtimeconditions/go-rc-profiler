# Go Phase 4 dependency-schema acceptance (v3 assembly)

This bundle preserves the installed CLI run against the immutable assembled
fixture tree at
`/Users/colacy/code/github.com/runtimeconditions/phase4-dependency-schema-fixtures-v3-20261004`
and prepared results at
`/Users/colacy/code/github.com/runtimeconditions/phase4-dependency-schema-results-20261004`.
The fixture proxy and packages are test infrastructure; they are not part of
the profiler binary or a production distribution.

[`result.yaml`](result.yaml) records the exact runner invocation, Go version,
tested profiler and core-schema digests, fixture tool and binding archive and
resource hashes, every workload command and result, downloaded module
directories, extension closures, source-read denial controls, and execution
canary. [`inventory.yaml`](inventory.yaml),
[`reviewed-conformance.yaml`](reviewed-conformance.yaml),
[`oracles/`](oracles/), [`actual/profiles/`](actual/profiles/), and
[`actual/diagnostics/`](actual/diagnostics/) preserve the compared evidence.
[`checks.yaml`](checks.yaml) links the schema-linked Go test and vet logs.

Run [`run.py`](run.py) using its exact `result.yaml` invocation with a fresh
external `--cache-root`; the run downloads dependencies through Go into a
separate module cache for each workload and sets `GOPROXY=off` for profiling.
The profiler and child Go processes run with native macOS sandbox rules that
deny reads of the `extensions`, `spec`, and profiler source checkouts. The
runner first checks that each denial actually works. Run [`check-go.py`](check-go.py)
with the v3 proxy and tested binary to repeat schema-linked Go tests and vet.

The repaired profiler produced 19 exact positive profiles twice, rejected six
negative workloads with exact diagnostics and no profile, and passed three
normalizer negatives. `dependency-schema-only` emits only the root extension
while its installed closure contains both root and validation-only dependency.
`dependency-schema-invalid` fails at that dependency's `command-limit` JSON
Schema. [`resolver-defect.yaml`](resolver-defect.yaml) records the original
binary's failure and the narrowly scoped resolver fix.

**Provenance remains pending:** all 14 immutable v3 binding releases name the
original profiler digest `46af7aa5...`; the repaired binary tested here hashes
to `b2e2aee5...`. The behavioral evidence passes, but full Phase 4 acceptance
needs an official assembly whose release metadata names the repaired binary.
