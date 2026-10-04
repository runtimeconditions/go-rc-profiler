#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 || "$2" != /* ]]; then
  echo 'usage: build-with-core-schema.sh /absolute/path/to/core-schema.yaml /absolute/path/to/output-binary' >&2
  exit 2
fi

schema_file=$1
output_binary=$2
expected_source_sha256=ad101336b676b468ec975aff45c21749df22fddf422371156c586ed62abf3223
actual_source_sha256=$(shasum -a 256 "$schema_file" | awk '{print $1}')
if [[ "$actual_source_sha256" != "$expected_source_sha256" ]]; then
  echo "core profile schema source SHA-256 mismatch: $actual_source_sha256" >&2
  exit 1
fi

schema_base64=$(base64 < "$schema_file" | tr -d '\r\n')
script_dir=$(cd -- "$(dirname -- "$0")" && pwd)
cd "$script_dir/.."
linker_flags="-X github.com/runtimeconditions/go-rc-profiler/extensioncheck.installedCoreSchemaBase64=$schema_base64"
go test -ldflags "$linker_flags" ./extensioncheck
go build -trimpath -ldflags "$linker_flags" -o "$output_binary" .
