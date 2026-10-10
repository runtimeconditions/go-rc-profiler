# Go Runtime Conditions Profiler

`go-rc-profiler` reads a Go project's source and generates a validated Runtime
Conditions Profile. It recognizes declarations made through generated Go
extension binding packages. It analyzes source and package resources without
running the project or its dependencies.

Runtime Conditions is seeking adoption by an established parent project. The
repositories in this organization support implementation, review, and demos;
they are not intended to present Runtime Conditions as a standalone foundation
or competing project. Start at https://runtimeconditions.github.io/.

## What you need

- A Go project with a `go.mod` and a working Go toolchain.
- An installed `go-rc-profiler` executable built with the approved, versioned
  core profile schema. Download the archive for your platform from
  [GitHub Releases](https://github.com/runtimeconditions/go-rc-profiler/releases).
- A published generated Go binding module available through your normal Go
  package management configuration, such as `runtimeconditions.io/x/rc/common`
  or `runtimeconditions.io/x/rc/env`.

The developer does not need an `extensions`, `spec`, or profiler source
checkout. The Go module download supplies each binding package. Its generated
declarations and four resources live together in the resolved package
directory: `runtimeconditions.bindings.yaml`,
`runtimeconditions.binding-model.yaml`, `runtimeconditions.extension.yaml`, and
`runtimeconditions.binding-release.yaml`.

## Install the executable

Release builds support Linux and macOS on `amd64` and `arm64`, and Windows on
`amd64`. Download the matching archive and `checksums.txt` from the same release,
verify the archive's SHA-256, and extract it. On Linux and macOS, place
`go-rc-profiler` in a directory on `PATH`. On Windows, place `go-rc-profiler.exe`
in a directory on `PATH`. Run `go-rc-profiler --version` to check the release
version and source commit. Go must also be installed for workload analysis.

The [release workflow](.github/workflows/release.yaml) runs on `vMAJOR.MINOR.PATCH`
tags, including prereleases. It runs the test workflow, builds and checks each
executable on its native platform, and publishes archives plus `checksums.txt`
only after all jobs pass. The core schema is downloaded from the pinned `spec`
release `v0.4.0`, checked against the digest in
[the build script](scripts/build-with-core-schema.sh), and embedded in each
binary. A manual workflow run builds artifacts without publishing a release.

## Generate a profile

Set `PROJECT_DIR` to the absolute path of the Go project you want to profile.
Install the published binding modules needed by your project:

```sh
PROJECT_DIR="$HOME/work/my-service"
go -C "$PROJECT_DIR" get runtimeconditions.io/x/rc/common@v0.1.0 runtimeconditions.io/x/rc/env@v0.1.0
```

Import the binding packages in the project's Go source and use their generated
declaration APIs. For example:

```go
package main

import (
    "runtimeconditions.io/x/rc/common"
    "runtimeconditions.io/x/rc/env"
)

func init() {
    _ = common.Cache(
        common.KeyValue{Engine: &[]common.KeyValueEngine{common.KeyValueEngineRedis}[0]},
        env.KeyValueConfiguration{
            Env: env.KeyValueConfigurationEnv{
                {Name: "REDIS_URL", Property: env.KeyValueConfigurationEnvPropertyUrl},
            },
        },
    )
}

func main() {}
```

Use the real declarations and types supplied by your binding package. The
profiler must be able to determine the declaration's values statically, for
example from literals, named constants, typed composite literals, and supported
optional pointer expressions. A declaration with a dynamic or ambiguous value
fails rather than producing a partial Condition.

Download dependencies with Go tooling, then run the installed profiler against
the absolute project directory:

```sh
go -C "$PROJECT_DIR" mod download
go-rc-profiler generate \
  -dir "$PROJECT_DIR" \
  -name my-service \
  -workload-uri https://example.com/my-service \
  -workload-version 1.2.3 \
  -out "$PROJECT_DIR/runtimeconditions.profile.yaml"
```

The command writes the file only after the project type-checks, declarations
are resolved, packaged resources and the extension dependency closure pass
integrity checks, and the complete profile passes core and applicable extension
schemas. On failure it exits nonzero and does not replace the output file.
Omit `-out` to print YAML to stdout. If you omit the identity flags, the
defaults are the project directory name for `-name`, its Go module path for
`-workload-uri`, and `dev` for `-workload-version`.

The Go import path identifies the **binding package**. Current bindings identify
extensions by their complete definition URI. Profile `extensions` entries use
that URI directly:

```yaml
extensions:
  - https://runtimeconditions.io/extensions/common-integrations/v1alpha1/runtimeconditions.extension.yaml
  - https://runtimeconditions.io/extensions/env-configuration/v1alpha1/runtimeconditions.extension.yaml
```

`metadata.id` is required and `metadata.version` is an optional annotation.
Legacy binding manifests that explicitly identify an extension by both ID and
version remain supported and produce `id:version` profile entries. The profiler
checks the full dependency closure, including validation-only dependencies,
using the installed binding resources. Profiles use core schema `0.4.0`.
The exact approved `0.2.0` core input recorded in the published `v0.1.0`
bindings is also supported; output is validated with core `0.4.0`, which accepts
complete definition URIs. Other core identities or digests must match the
installed schema. Profile consumers resolve the declared references through
their configured extension resolver.

Dependency archive checks accept the publisher's exact ZIP bytes or reproduce
its deterministic ZIP from Go's downloaded module files before comparing the
recorded SHA-256. This permits proxy ZIP metadata differences while retaining
the file inventory and content check. Go's module checksum verification also
runs before package inspection.

To inspect installed binding packages before generation, use their Go import
paths:

```sh
go-rc-profiler validate-extensions -dir "$PROJECT_DIR"
go-rc-profiler validate-extension \
  -dir "$PROJECT_DIR" \
  -package runtimeconditions.io/x/rc/common
```

`generate` already performs the checks needed for its output. These separate
commands are useful when diagnosing a package or dependency problem. Go module
resolution may contact the configured module proxy while downloading packages;
after the modules are installed, the profiler can run with dependency retrieval
disabled.
