#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 || "$2" != /* ]]; then
  echo 'usage: build-with-core-schema.sh /absolute/path/to/core-schema.yaml /absolute/path/to/output-binary' >&2
  exit 2
fi

schema_file=$1
output_binary=$2
expected_source_sha256=96d430c7936fcf7334aa9613f63bb306592e07cf56fe4154af57933d2c1480e6
if command -v sha256sum >/dev/null 2>&1; then
  actual_source_sha256=$(sha256sum "$schema_file" | awk '{print $1}')
else
  actual_source_sha256=$(shasum -a 256 "$schema_file" | awk '{print $1}')
fi
if [[ "$actual_source_sha256" != "$expected_source_sha256" ]]; then
  echo "core profile schema source SHA-256 mismatch: $actual_source_sha256" >&2
  exit 1
fi

schema_base64=$(base64 < "$schema_file" | tr -d '\r\n')
build_version=${RC_PROFILER_VERSION:-dev}
build_commit=${RC_PROFILER_COMMIT:-unknown}
if [[ ! "$build_version" =~ ^[A-Za-z0-9.+-]+$ || ! "$build_commit" =~ ^[A-Za-z0-9]+$ ]]; then
  echo 'invalid profiler build version or commit' >&2
  exit 2
fi
script_dir=$(cd -- "$(dirname -- "$0")" && pwd)
cd "$script_dir/.."
linker_flags="-X github.com/runtimeconditions/go-rc-profiler/extensioncheck.installedCoreSchemaBase64=$schema_base64 -X main.version=$build_version -X main.commit=$build_commit"
go test -ldflags "$linker_flags" ./extensioncheck
CGO_ENABLED=0 go build -trimpath -ldflags "$linker_flags" -o "$output_binary" .
