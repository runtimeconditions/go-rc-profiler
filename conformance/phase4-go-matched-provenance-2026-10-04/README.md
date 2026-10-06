# Go Phase 4 installed acceptance: matching v4 provenance

This bundle records the installed Go profiler run against the **official v4
assembly** at
`/Users/colacy/code/github.com/runtimeconditions/phase4-dependency-schema-fixtures-v4-20261004`
and prepared results at
`/Users/colacy/code/github.com/runtimeconditions/phase4-dependency-schema-results-v4-20261004`.
The fixture proxy and its packages are test infrastructure, outside the
profiler implementation and its runtime inputs.

[`result.yaml`](result.yaml) is the primary machine-readable result. It holds
the exact [`run.py`](run.py) invocation, Go version, tested profiler and core
schema identities and hashes, fixture-builder hashes, all 14 module archive
and four-resource hashes, resolved module-cache directories, direct
contributors and closure, each command and exit, byte comparisons, negative
failure stages, source-read denial controls, and execution canary. The
[`inventory.yaml`](inventory.yaml) and
[`reviewed-conformance.yaml`](reviewed-conformance.yaml) are copied from the
tested preparation and catalog. The [`oracles/`](oracles/),
[`actual/profiles/`](actual/profiles/), and
[`actual/diagnostics/`](actual/diagnostics/) directories preserve expected
and actual bytes. [`assembly-provenance-review.yaml`](assembly-provenance-review.yaml)
is the assembler's separate v3-to-v4 comparison.

To replay, run the `result.yaml` invocation with a **new external**
`--cache-root`. The runner uses `go mod download all` through the fixture Go
proxy into an isolated cache per workload, then runs the installed profiler
with `GOPROXY=off`. A native macOS sandbox denies reads of the `extensions`,
`spec`, and profiler source checkouts; three denial controls confirm the rule
works. Both workload and downloaded-binding execution canaries remain inert.

The 19 positives exactly matched their YAML oracles and repeated byte for
byte. Six negatives produced exact recorded diagnostics and no profile;
three normalizer negatives produced exact diagnostics and no model. The new
positive emits only the root extension while its installed dependency closure
also contains a validation-only dependency. The new negative fails at that
dependency's `command-limit` schema. All **14** binding releases record the
executed profiler SHA-256
`b2e2aee5e10af47a0e75998d3ac591ee17f2887aaaddeae46405296763bc1444`.

[`check-go.py`](check-go.py) ran the schema-linked Go suite (including the
unimported-dependency regression) and `go vet`; [`checks.yaml`](checks.yaml)
links their passing logs. The original 18+5 bundle and the v3 supplemental
bundle remain separate historical evidence. This test does not establish
production module publication or profiler binary distribution.
