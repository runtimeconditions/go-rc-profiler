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
  package management configuration. Public binding modules are not yet
  published by this project; the modules in the conformance suite are test
  infrastructure.

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
release `v0.1.0`, checked against the digest in
[the build script](scripts/build-with-core-schema.sh), and embedded in each
binary. A manual workflow run builds artifacts without publishing a release.

## Generate a profile

Set `PROJECT_DIR` to the absolute path of the Go project you want to profile.
Use the *published Go module path and version* supplied by the binding
publisher; the following module name is illustrative.

```sh
PROJECT_DIR="$HOME/work/my-service"
go -C "$PROJECT_DIR" get github.com/acme/rc-service-binding@v1.2.3
```

Import that binding package in the project's Go source and use its generated
declaration API. For example, if the published package provides these names:

```go
package service

import binding "github.com/acme/rc-service-binding"

var _ = binding.Service(
    binding.Http{Endpoint: "https://api.example.com"},
    binding.Region("eu"),
)
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

The Go import path identifies the **binding package**. The profile's
`extensions` entries identify **extension definitions**. Under
[specification §5.1](https://github.com/runtimeconditions/spec/blob/main/docs/fifth-draft.md#51-extension-identifiers),
an extension ID has the form `<https-uri>:<version>`, for
example `https://extensions.example.com/provider/service:v1alpha1`.
Extension definitions carry `metadata.uri` and `metadata.version`, and their
combined identifier is `<uri>:<version>`. A URI of `https://example.com/aws/aws-s3`
at version `0.2.0` resolves to
`https://example.com/extensions/aws/aws-s3/0.2.0/runtimeconditions.extension.yaml`.
A providerless URI such as `https://example.com/env-configuration` resolves under
`/extensions/rc/env-configuration/<version>/runtimeconditions.extension.yaml`.
A missing definition at that URL is not found; alternate providers or paths are
not tried. Only HTTPS is supported; `file:` and `oci:` are deferred.

The profiler emits the exact IDs from the validated binding resources for
extensions that directly contribute vocabulary. It checks their full
dependency closure, including dependencies that contribute only a validation
schema. In the intended end-user flow, a consumer or Adapter resolves those
IDs and their transitive dependencies remotely when using the profile. The
developer supplies no local path to an `extensions` checkout. The identity
migration uses core schema `0.2.0`. Integrating this HTTPS
retrieval contract into the installed profiler is the next production-readiness
step. The current Go profiler obtains definitions for *generation-time
validation* from downloaded
Go binding modules; it does not fetch definitions over HTTP by URI ID.

To inspect installed binding packages before generation, use their Go import
paths:

```sh
go-rc-profiler validate-extensions -dir "$PROJECT_DIR"
go-rc-profiler validate-extension \
  -dir "$PROJECT_DIR" \
  -package github.com/acme/rc-service-binding
```

`generate` already performs the checks needed for its output. These separate
commands are useful when diagnosing a package or dependency problem. Go module
resolution may contact the configured module proxy while downloading packages;
after the modules are installed, the profiler can run with dependency retrieval
disabled.
