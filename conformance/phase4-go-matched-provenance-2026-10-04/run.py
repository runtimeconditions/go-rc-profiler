#!/usr/bin/env python3
"""Record installed-Go-profiler acceptance against the official fixture assembly.

This is test infrastructure. It only installs modules into external, isolated
Go caches; no fixture proxy or package is built into the profiler.
"""

import argparse
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import sys
import zipfile

import rfc8785
import yaml


RESOURCES = (
    "runtimeconditions.bindings.yaml",
    "runtimeconditions.binding-model.yaml",
    "runtimeconditions.extension.yaml",
    "runtimeconditions.binding-release.yaml",
)
MODULE_PREFIX = "example.com/runtimeconditions/conformance/"
SOURCE_NAMES = {"api_url", "café", "patch9"}


def digest(data):
    return hashlib.sha256(data).hexdigest()


def file_digest(path):
    return digest(path.read_bytes())


def run(command, cwd, env):
    return subprocess.run(command, cwd=cwd, env=env, text=True,
                          capture_output=True, timeout=180)


def require(ok, message):
    if not ok:
        raise AssertionError(message)


def copy_checked(source, target, expected_hash=None):
    require(source.is_file(), f"missing {source}")
    if expected_hash:
        require(file_digest(source) == expected_hash, f"hash mismatch: {source}")
    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(source, target)
    return file_digest(target)


def package_module(package_key):
    require(package_key.startswith("conformance-"), package_key)
    return MODULE_PREFIX + package_key.removeprefix("conformance-")


def check_archive(package):
    archive = Path(package["archive"])
    require(file_digest(archive) == package["sha256"], f"archive hash: {archive}")
    with zipfile.ZipFile(archive) as z:
        release = None
        for name in RESOURCES:
            matches = [entry for entry in z.namelist() if entry.endswith("/" + name)]
            require(len(matches) == 1, f"archive {archive}: {name} count {len(matches)}")
            require(digest(z.read(matches[0])) == package["resources"][name],
                    f"archive resource hash: {archive} {name}")
            if name == "runtimeconditions.binding-release.yaml":
                release = yaml.safe_load(z.read(matches[0]))
        return release["provenance"]["profiler"]["sha256"]


def make_canary_proxy(official_archive, cache_root):
    """Make a separate Go proxy whose module source has an execution panic.

    The original archive and all four binding resources remain untouched. Go
    downloads this new archive normally, so the module-integrity check still
    runs against the bytes actually used by the supplemental canary.
    """
    proxy = cache_root / "canary-proxy"
    version_dir = proxy / "example.com/runtimeconditions/conformance/owned-kind-interface/@v"
    version_dir.mkdir(parents=True)
    for name in ("v1.0.0.info", "v1.0.0.mod", "list"):
        source = official_archive.parent / name
        if source.is_file():
            shutil.copyfile(source, version_dir / name)
    archive = version_dir / "v1.0.0.zip"
    inserted = 'func init() { panic("binding package must not execute") }'
    old_hash = None
    new_hash = None
    with zipfile.ZipFile(official_archive) as source, zipfile.ZipFile(archive, "w") as target:
        for entry in source.infolist():
            content = source.read(entry.filename)
            if entry.filename.endswith("/bindings.go"):
                old_hash = digest(content)
                content += ("\n" + inserted + "\n").encode()
                new_hash = digest(content)
            target.writestr(entry, content)
    require(old_hash is not None and new_hash is not None, "canary binding source missing")
    return proxy, archive, old_hash, new_hash, inserted


def source_policy(root):
    roots = [root / name for name in ("extensions", "spec", "go-rc-profiler")]
    for path in roots:
        require(path.is_dir(), f"missing source checkout for deny rule: {path}")
    # The profiler process and all child Go tooling run with these read denies.
    return "(version 1)(allow default)" + "".join(
        f'(deny file-read* (subpath "{path}"))' for path in roots)


def binding_dirs(workload, env, cache, packages):
    listed = run(["go", "list", "-mod=readonly", "-deps", "-f",
                  "{{if .Module}}{{.ImportPath}}|{{.Dir}}{{end}}", "./..."],
                 workload, {**env, "GOPROXY": "off"})
    require(listed.returncode == 0, f"go list: {listed.stderr}")
    imported = set()
    for line in listed.stdout.splitlines():
        if line.startswith(MODULE_PREFIX):
            module, directory = line.split("|", 1)
            path = Path(directory).resolve()
            require(path.is_relative_to((cache / "modules").resolve()),
                    f"binding outside isolated module cache: {path}")
            imported.add(module)
    require(imported, f"no imported binding declarations in {workload}")
    modules = run(["go", "list", "-m", "-mod=readonly", "-f",
                   "{{.Path}}|{{.Dir}}", "all"],
                  workload, {**env, "GOPROXY": "off"})
    require(modules.returncode == 0, f"go list -m: {modules.stderr}")
    dirs = {}
    for line in modules.stdout.splitlines():
        if line.startswith(MODULE_PREFIX):
            module, directory = line.split("|", 1)
            path = Path(directory).resolve()
            require(path.is_relative_to((cache / "modules").resolve()),
                    f"binding module outside isolated cache: {path}")
            dirs[module] = path
    require(imported <= set(dirs), f"imported binding missing from module graph: {imported - set(dirs)}")
    definitions = {}
    for module, path in dirs.items():
        require(module in packages, f"unlisted binding module: {module}")
        package = packages[module]
        require((path / "bindings.go").is_file(),
                f"binding declarations missing beside resources: {module}")
        for name in RESOURCES:
            require(file_digest(path / name) == package["resources"][name],
                    f"downloaded resource hash: {module}/{name}")
        definition = yaml.safe_load((path / "runtimeconditions.extension.yaml").read_bytes())
        extension_id = definition["metadata"]["id"]
        require(extension_id not in definitions, f"duplicate extension ID: {extension_id}")
        definitions[extension_id] = definition
    return dirs, definitions


def closure(direct, definitions):
    found = set()
    pending = list(direct)
    while pending:
        extension_id = pending.pop()
        require(extension_id in definitions, f"missing installed dependency: {extension_id}")
        if extension_id not in found:
            found.add(extension_id)
            pending.extend(definitions[extension_id].get("spec", {}).get("dependencies", []))
    return sorted(found)


def inspect_catalog(catalog, records, counts):
    positive = catalog["positive"]
    negative = catalog["negative"]
    require(len(positive) == counts["positiveDeclarations"] == 19, "positive inventory count")
    require(len(negative) == counts["negativeDeclarations"] == 6, "negative inventory count")
    by_id = {item["id"]: item for item in positive + negative}
    require(len(by_id) == len(records) == 25, "duplicate or missing declaration ID")
    emitted = []
    deferred = []
    for record in records:
        item = by_id[record["id"]]
        require(item["case"] == record["case"], f"case mismatch: {record['id']}")
        require(record["outcome"] == ("positive" if item in positive else "negative"),
                f"outcome mismatch: {record['id']}")
        require(record.get("dependencySchema") == item.get("dependencySchema"),
                f"dependency-schema coordinate mismatch: {record['id']}")
        go = item["languages"]["go"]
        if "generated" in go:
            require(record.get("emittedDeclaration") == go["generated"],
                    f"generated call mismatch: {record['id']}")
            emitted.append((record["id"], go["generated"]))
        else:
            require("emittedDeclaration" not in record,
                    f"unexpected emitted call: {record['id']}")
        if "coversDeferred" in go:
            require(record.get("coversDeferred") == go["coversDeferred"],
                    f"deferred call mismatch: {record['id']}")
            deferred.extend((record["id"], entry) for entry in go["coversDeferred"])
    require(len(emitted) == counts["emittedCalls"] == 11, "emitted call count")
    require(len(deferred) == counts["completedDeferredDeclarations"] == 1,
            "deferred call count")
    return {"positive": 19, "negative": 6, "emittedCalls": emitted,
            "completedDeferredDeclarations": deferred}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("fixtures", "results", "profiler", "schema", "catalog",
                 "source-root", "cache-root", "output"):
        parser.add_argument("--" + name, required=True, type=Path)
    args = parser.parse_args()
    paths = {key.replace("_", "-"): value.resolve() for key, value in vars(args).items()}
    fixtures, results, profiler = paths["fixtures"], paths["results"], paths["profiler"]
    schema, catalog_path = paths["schema"], paths["catalog"]
    source_root, cache_root, output = paths["source-root"], paths["cache-root"], paths["output"]
    require(cache_root != output and not output.is_relative_to(cache_root),
            "cache root must be external to durable evidence")
    require(not cache_root.exists(), f"cache root must be fresh: {cache_root}")
    cache_root.mkdir(parents=True)
    output.mkdir(parents=True, exist_ok=True)
    inventory_path = results / "inventory.yaml"
    inventory = yaml.safe_load(inventory_path.read_bytes())
    catalog = yaml.safe_load(catalog_path.read_bytes())
    fixture_source = source_root / "extensions/tooling/extension-bindings/fixtures"
    assembler = fixture_source / "assemble.py"
    preparer = fixture_source / "prepare_results.py"
    require(file_digest(catalog_path) == inventory["catalogSha256"], "catalog changed")
    require(file_digest(schema) == inventory["coreSchemaSha256"], "core schema changed")
    require(file_digest(preparer) == inventory["preparerSha256"], "preparer changed")
    require(inventory["languages"]["go"]["packages"], "no Go packages")
    require(inventory["languages"]["go"]["declarations"], "no Go declarations")
    reviewed = inspect_catalog(catalog, inventory["languages"]["go"]["declarations"],
                               inventory["languages"]["go"])
    package_records = inventory["languages"]["go"]["packages"]
    packages = {package_module(item["packageKey"]): item for item in package_records}
    require(len(packages) == len(package_records) == 14, "binding package count")
    release_profiler_hashes = {}
    for package in package_records:
        require(Path(package["archive"]).is_relative_to(fixtures / "go" / "proxy"),
                f"archive outside specified assembly: {package['archive']}")
        release_profiler_hashes[package_module(package["packageKey"])] = check_archive(package)
    schema_doc = yaml.safe_load(schema.read_bytes())
    schema_semantic = digest(rfc8785.dumps(schema_doc))
    require(schema_doc["$id"] == "https://runtimeconditions.io/schemas/profile/0.1.0/runtimeconditions.profile.schema.yaml",
            "unexpected core schema identity")
    require(schema_doc["x-runtimeconditions-version"] == "0.1.0", "core schema version")
    require(schema_semantic == "49890a0f3e7276d1e480d654176672d977df9c63094f3a24983b0a8102e1a3e3",
            "core schema semantic digest")
    policy = source_policy(source_root)
    denied_files = [catalog_path, schema, source_root / "go-rc-profiler/main.go"]
    denial_controls = []
    for path in denied_files:
        denied = run(["sandbox-exec", "-p", policy, "/bin/cat", str(path)],
                     cache_root, os.environ.copy())
        require(denied.returncode != 0 and "Operation not permitted" in denied.stderr,
                f"source-read sandbox policy did not deny {path}")
        denial_controls.append({"path": str(path), "command": ["sandbox-exec", "-p", policy,
                                                        "/bin/cat", str(path)],
                                "exitCode": denied.returncode, "stderr": denied.stderr})
    go_version = subprocess.run(["go", "version"], capture_output=True, text=True, check=True).stdout.strip()
    binary_build = subprocess.run(["go", "version", "-m", str(profiler)],
                                  capture_output=True, text=True, check=True).stdout
    copy_checked(inventory_path, output / "inventory.yaml")
    copy_checked(catalog_path, output / "reviewed-conformance.yaml")
    summary = {
        "format": "runtimeconditions.io/go-phase4-acceptance/v1",
        "status": "running",
        "invocation": [sys.executable, *sys.argv],
        "assembledFixtureTree": str(fixtures),
        "preparedResultTree": str(results),
        "externalCacheRoot": str(cache_root),
        "runner": str(Path(__file__).resolve()),
        "runnerSha256": file_digest(Path(__file__)),
        "profiler": {"path": str(profiler), "sha256": file_digest(profiler),
                     "goBuildInfo": binary_build},
        "coreSchema": {"source": str(schema), "id": schema_doc["$id"],
                       "version": schema_doc["x-runtimeconditions-version"],
                       "sourceSha256": file_digest(schema), "semanticSha256": schema_semantic},
        "goVersion": go_version,
        "catalogSha256": file_digest(catalog_path),
        "inventorySha256": file_digest(inventory_path),
        "fixtureAssemblyTools": {
            "assembler": str(assembler), "assemblerSha256": file_digest(assembler),
            "preparer": str(preparer), "preparerSha256": file_digest(preparer),
        },
        "reviewedCoverage": reviewed,
        "fixturePackages": [{"module": package_module(item["packageKey"]),
                              "archive": item["archive"], "archiveSha256": item["sha256"],
                              "resourceSha256": item["resources"]} for item in package_records],
        "fixtureReleaseProfilerProvenance": {
            "byModule": release_profiler_hashes,
            "matchesTestedBinary": all(value == file_digest(profiler)
                                       for value in release_profiler_hashes.values()),
        },
        "runtimeBoundary": {"dependencyRetrieval": "go mod download all",
                            "profilerGOPROXY": "off",
                            "macOSSandboxPolicy": policy,
                            "denialControls": denial_controls,
                            "sourceCheckoutReadsDenied": [str(source_root / name) for name in
                                                          ("extensions", "spec", "go-rc-profiler")]},
        "workloads": [], "normalizerNegatives": [], "bindingCodeCanary": None,
    }
    records = inventory["languages"]["go"]["declarations"]
    for record in records:
        case_id = record["id"]
        source_workload = results / record["workload"]
        workload = cache_root / case_id / "workload"
        shutil.copytree(source_workload, workload, ignore=shutil.ignore_patterns("profile.yaml"))
        require("replace " not in (workload / "go.mod").read_text(),
                f"local module replacement: {case_id}")
        require('func init() { panic("workload source must not execute") }' in
                (workload / "main.go").read_text(), f"missing execution sentinel: {case_id}")
        require(file_digest(workload / "main.go") == record["sourceSha256"],
                f"source hash: {case_id}")
        expected = results / record["expected"]
        require(file_digest(expected) == record["expectedSha256"],
                f"expected oracle hash: {case_id}")
        oracle = output / "oracles" / expected.name
        copy_checked(expected, oracle, record["expectedSha256"])
        cache = cache_root / case_id
        env = os.environ.copy()
        env.update(record["environment"])
        require(env["GOPROXY"] == (fixtures / "go" / "proxy").as_uri(),
                f"wrong fixture proxy: {case_id}")
        env.update(GOMODCACHE=str(cache / "modules"), GOCACHE=str(cache / "build"),
                   GOTOOLCHAIN="local")
        installed = run(record["install"], workload, env)
        require(installed.returncode == 0, f"{case_id}: install: {installed.stderr}")
        dirs, definitions = binding_dirs(workload, env, cache, packages)
        require(closure(record["directContributors"], definitions) ==
                record["extensionClosure"], f"{case_id}: extension closure mismatch")
        if case_id.startswith("dependency-schema-"):
            root_id = "urn:runtimeconditions:conformance:dependency-schema-only:root"
            dependency_id = "urn:runtimeconditions:conformance:dependency-schema-only:dependency"
            coordinate = dependency_id + "#schema:command-limit"
            require(record["directContributors"] == [root_id] and
                    record["extensionClosure"] == sorted([root_id, dependency_id]) and
                    set(record["directContributors"]) < set(record["extensionClosure"]),
                    f"{case_id}: dependency is not validation-only")
            require(record["dependencySchema"] == coordinate, f"{case_id}: schema coordinate")
            require(any(item["id"] == "command-limit" for item in
                        definitions[dependency_id]["spec"]["schemas"]),
                    f"{case_id}: dependency schema missing")
        command = [str(profiler), *record["profileCommand"][1:]]
        require(command[-1] == "profile.yaml", f"unexpected output: {case_id}")
        offline = {**env, "GOPROXY": "off"}
        sandboxed = ["sandbox-exec", "-p", policy, *command]
        actual = run(sandboxed, workload, offline)
        output_profile = workload / "profile.yaml"
        row = {"id": case_id, "case": record["case"], "outcome": record["outcome"],
               "installCommand": record["install"], "installExitCode": installed.returncode,
               "installProxy": env["GOPROXY"], "cache": str(cache),
               "resolvedBindingDirectories": {name: str(path) for name, path in sorted(dirs.items())},
               "declarationSourcePresentBesideResources": True,
               "allFourResourcesMatchArchive": True,
               "workloadExecutionSentinelPresent": True,
               "directContributors": record["directContributors"],
               "resolvedExtensionClosure": closure(record["directContributors"], definitions),
               "profileCommand": sandboxed, "profilerGOPROXY": "off",
               "exitCode": actual.returncode, "oracleSha256": record["expectedSha256"]}
        if record["outcome"] == "positive":
            require(actual.returncode == 0, f"{case_id}: profiler: {actual.stderr}")
            require(output_profile.is_file(), f"{case_id}: missing profile")
            content = output_profile.read_bytes()
            require(content == expected.read_bytes(), f"{case_id}: YAML bytes differ")
            profile = yaml.safe_load(content)
            require(sorted(profile["extensions"]) == record["directContributors"],
                    f"{case_id}: emitted contributors differ")
            if case_id == "source-names":
                condition = profile["conditions"][0]
                require(SOURCE_NAMES.issubset(condition), "serialized source names changed")
                require(condition["interface"]["type"] == "http_server2_url",
                        "serialized interface name changed")
                row["serializedSourceNames"] = sorted(SOURCE_NAMES | {"http_server2_url"})
            actual_file = output / "actual" / "profiles" / f"{case_id}.yaml"
            copy_checked(output_profile, actual_file)
            repeated = run(sandboxed, workload, offline)
            require(repeated.returncode == 0 and output_profile.read_bytes() == content,
                    f"{case_id}: repetition differed: {repeated.stderr}")
            row.update(status="passed", actual=str(actual_file.relative_to(output)),
                       actualSha256=digest(content), exactYamlBytes=True, repeatedBytesIdentical=True)
        else:
            oracle_data = yaml.safe_load(expected.read_bytes())
            if case_id == "dependency-schema-invalid":
                require(oracle_data["stage"] == "schema" and
                        oracle_data["coordinate"] == record["dependencySchema"] and
                        record["dependencySchema"] in oracle_data["stderr"],
                        "dependency-only schema rejection oracle mismatch")
            normalized = actual.stderr.replace(str(workload), "<workload>")
            require(actual.returncode != 0, f"{case_id}: negative succeeded")
            require(not output_profile.exists(), f"{case_id}: profile emitted on failure")
            require(normalized == oracle_data["stderr"],
                    f"{case_id}: diagnostic differs:\nactual={normalized!r}\nexpected={oracle_data['stderr']!r}")
            actual_file = output / "actual" / "diagnostics" / f"{case_id}.stderr.txt"
            actual_file.parent.mkdir(parents=True, exist_ok=True)
            actual_file.write_text(normalized)
            row.update(status="passed", actual=str(actual_file.relative_to(output)),
                       actualSha256=file_digest(actual_file), failureStage=oracle_data["stage"],
                       exactDiagnostic=True, noProfileOutput=True)
        summary["workloads"].append(row)
        print(f"{case_id}: {row['status']}", flush=True)

    summary["reviewedCoverage"]["directContributorDiffersFromClosureCases"] = [
        row["id"] for row in summary["workloads"]
        if row["directContributors"] != row["resolvedExtensionClosure"]
    ]
    for record in inventory["normalizerNegatives"]:
        oracle = results / record["expected"]
        copy_checked(oracle, output / "oracles" / oracle.name, record["expectedSha256"])
        command = record["command"]
        require(file_digest(Path(command[0])) == command[command.index("--normalizer-sha256") + 1],
                f"normalizer binary hash: {record['case']}")
        actual = run(command, results, os.environ.copy())
        model_output = Path(command[command.index("--output") + 1])
        require(actual.returncode != 0 and not model_output.exists(),
                f"normalizer negative succeeded or emitted model: {record['case']}")
        require(actual.stderr == record["stderr"],
                f"normalizer diagnostic differs: {record['case']}")
        actual_file = output / "actual" / "diagnostics" / f"{record['case']}.stderr.txt"
        actual_file.write_text(actual.stderr)
        summary["normalizerNegatives"].append({
            "case": record["case"], "status": "passed", "command": command,
            "exitCode": actual.returncode, "noModelOutput": True,
            "actual": str(actual_file.relative_to(output)),
            "actualSha256": file_digest(actual_file),
            "oracleSha256": record["expectedSha256"], "exactDiagnostic": True,
        })
        print(f"{record['case']}: passed", flush=True)

    # Supplemental execution sentinel: publish a variant *only* through a new
    # external Go proxy, then download it into a new isolated module cache.
    # Mutating an installed module would correctly fail `go mod verify`.
    owned_record = next(item for item in records if item["id"] == "owned")
    owned_package = packages[MODULE_PREFIX + "owned-kind-interface"]
    canary_proxy, canary_archive, before, after, injected = make_canary_proxy(
        Path(owned_package["archive"]), cache_root)
    canary_cache = cache_root / "binding-canary"
    owned_workload = canary_cache / "workload"
    shutil.copytree(results / owned_record["workload"], owned_workload,
                    ignore=shutil.ignore_patterns("profile.yaml", "go.sum"))
    canary_env = os.environ.copy()
    canary_env.update(owned_record["environment"])
    canary_env.update(GOMODCACHE=str(canary_cache / "modules"),
                      GOCACHE=str(canary_cache / "build"), GOTOOLCHAIN="local",
                      GOPROXY=canary_proxy.as_uri())
    canary_install = run(owned_record["install"], owned_workload, canary_env)
    require(canary_install.returncode == 0, f"canary install failed: {canary_install.stderr}")
    canary_dirs, _ = binding_dirs(owned_workload, canary_env, canary_cache, packages)
    canary_source = canary_dirs[MODULE_PREFIX + "owned-kind-interface"] / "bindings.go"
    require(file_digest(canary_source) == after, "canary source was not downloaded")
    canary_command = [str(profiler), *owned_record["profileCommand"][1:-1], "canary.yaml"]
    canary_env["GOPROXY"] = "off"
    canary = run(["sandbox-exec", "-p", policy, *canary_command], owned_workload, canary_env)
    canary_profile = owned_workload / "canary.yaml"
    require(canary.returncode == 0 and canary_profile.read_bytes() ==
            (results / owned_record["expected"]).read_bytes(),
            f"binding execution canary failed: {canary.stderr}")
    summary["bindingCodeCanary"] = {
        "status": "passed", "workload": "owned", "downloadedBindingSource": str(canary_source),
        "derivedExternalProxy": str(canary_proxy),
        "derivedArchive": str(canary_archive), "derivedArchiveSha256": file_digest(canary_archive),
        "sourceSha256BeforeInjection": before, "sourceSha256AfterInjection": after,
        "injectedSource": injected, "installCommand": owned_record["install"],
        "installProxy": canary_proxy.as_uri(), "profilerGOPROXY": "off",
        "command": ["sandbox-exec", "-p", policy, *canary_command],
        "exitCode": canary.returncode, "profileSha256": file_digest(canary_profile),
        "matchesUnmodifiedOracle": True,
    }
    summary["status"] = ("passed" if
                         summary["fixtureReleaseProfilerProvenance"]["matchesTestedBinary"]
                         else "behavior-passed-provenance-pending")
    require(summary["reviewedCoverage"]["directContributorDiffersFromClosureCases"] ==
            ["dependency-schema-only", "dependency-schema-invalid"],
            "installed corpus did not distinguish direct contributors from closure")
    summary["counts"] = {"positive": 19, "negative": 6, "packages": 14,
                         "emittedCalls": 11, "completedDeferredDeclarations": 1}
    (output / "result.yaml").write_text(yaml.safe_dump(summary, sort_keys=False,
                                                         allow_unicode=True, width=1000))
    print(f"all Go Phase 4 installed behavior checks passed; status: {summary['status']}",
          flush=True)


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(f"acceptance failed: {error}", file=sys.stderr)
        raise
