# Go Runtime Conditions Profiler

Runtime Conditions is currently seeking adoption by an established parent
project. The repositories in this organization are split for hands-on usability,
review, demos, and implementation feedback. They are not intended to present
Runtime Conditions as a standalone foundation or competing project.

Start here: https://runtimeconditions.github.io/

## Purpose

This repository contains the Go Runtime Conditions profile generator and
extension validation tooling. The generator reads Go source using native package
resolution and emits Runtime Conditions Profile YAML from declaration packages
and package manifests.

## Run

Generate the Go request logger demo profile from a sibling `rc-demos` checkout:

```sh
go run . \
  -dir ../rc-demos/apps/request-logger-http \
  -name request-logger-http \
  -workload-uri github.com/runtimeconditions/rc-demos/apps/request-logger-http \
  -workload-version dev
```

Validate first-party extensions from a sibling `extensions` checkout:

```sh
go run . validate-extensions -root ../extensions
```

## Test

```sh
go test ./...
```
