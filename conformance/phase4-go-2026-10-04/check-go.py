#!/usr/bin/env python3
"""Run schema-linked Go regression tests against the official fixture proxy."""

import argparse
import base64
import json
import os
from pathlib import Path
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--profiler-source", required=True, type=Path)
    parser.add_argument("--core-schema", required=True, type=Path)
    parser.add_argument("--fixture-proxy", required=True, type=Path)
    parser.add_argument("--installed-profiler", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    source = args.profiler_source.resolve()
    output = args.output.resolve()
    logs = output / "checks"
    logs.mkdir(parents=True, exist_ok=True)
    schema = base64.b64encode(args.core_schema.read_bytes()).decode()
    flag = ("-X github.com/runtimeconditions/go-rc-profiler/"
            "extensioncheck.installedCoreSchemaBase64=" + schema)
    env = os.environ.copy()
    env.update(RC_GO_BINDING_FIXTURE_PROXY=str(args.fixture_proxy.resolve()),
               RC_GO_BINDING_CONFORMANCE_PROXY=str(args.fixture_proxy.resolve()),
               RC_GO_PROFILER_BIN=str(args.installed_profiler.resolve()))
    build_cache = Path("/private/tmp/rc-phase4-go-regression-cache-20261004")
    build_cache.mkdir(parents=True, exist_ok=True)
    env["GOCACHE"] = str(build_cache)
    checks = []
    commands = [
        ("schema-linked-go-test", ["go", "test", "-ldflags", flag, "./...", "-count=1"],
         "go test -ldflags '-X github.com/runtimeconditions/go-rc-profiler/"
         "extensioncheck.installedCoreSchemaBase64=<base64 core schema>' ./... -count=1"),
        ("go-vet", ["go", "vet", "./..."], "go vet ./..."),
    ]
    for name, command, display in commands:
        result = subprocess.run(command, cwd=source, env=env, capture_output=True, text=True)
        log = logs / f"{name}.log"
        log.write_text("command: " + display + "\n" + result.stdout + result.stderr)
        checks.append({"name": name, "command": display,
                       "environment": {key: env[key] for key in (
                           "RC_GO_BINDING_FIXTURE_PROXY", "RC_GO_BINDING_CONFORMANCE_PROXY",
                           "RC_GO_PROFILER_BIN", "GOCACHE")},
                       "exitCode": result.returncode, "log": str(log.relative_to(output))})
        print(f"{name}: {'passed' if result.returncode == 0 else 'failed'}", flush=True)
    (output / "checks.json").write_text(json.dumps(checks, indent=2) + "\n")
    if any(item["exitCode"] for item in checks):
        raise SystemExit(1)


if __name__ == "__main__":
    main()
