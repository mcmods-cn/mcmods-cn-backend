#!/usr/bin/env python3
"""Inventory pinned Git trees and check declared review evidence against them."""

import argparse
from collections import Counter, defaultdict
from fnmatch import fnmatchcase
import hashlib
import json
from pathlib import Path, PurePosixPath
import re
import subprocess
import tempfile

from review_ledger import covered, fingerprint, protected_input, read_json, read_ranges, rows, safe_text


REPOSITORIES = {"mcmods-cn-backend", "mcmods-cn-frontend", "mcmods_exporter"}
IMAGE_SUFFIXES = {".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".avif"}
ARCHIVE_SUFFIXES = {".zip", ".gz", ".jar", ".pdf", ".exe"}
SOURCE_SUFFIXES = {".go", ".java", ".ts", ".tsx", ".mts", ".mjs", ".js", ".cjs", ".py", ".sh", ".sql", ".css", ".svg"}
REVIEW_FIELDS = ("responsibility", "focus_symbols", "call_chain", "callers", "callees", "side_effects", "invariants", "findings", "decision", "validation")
INSPECTION_CATEGORIES = {"dependency-lock", "build-tool-distribution", "image", "archive-or-binary", "vendored", "generated-evidence", "structured-fixture-data"}


def file_category(path):
    """Suggest an inspection method; categorization is not review evidence."""
    name = PurePosixPath(path)
    suffix = name.suffix.lower()
    if name.name in {"package-lock.json", "go.sum", "gradle.lockfile"}:
        return "dependency-lock", "Provenance, dependency resolution and lock consistency"
    if name.name in {"gradlew", "gradlew.bat", "gradle-wrapper.jar"}:
        return "build-tool-distribution", "Upstream source, distribution checksum and executable entry point"
    if suffix in IMAGE_SUFFIXES:
        return "image", "Source or fixture provenance, decode and dimensions"
    if suffix in ARCHIVE_SUFFIXES:
        return "archive-or-binary", "Provenance, digest and bounded format inspection"
    if name.parts[0] == "vendor":
        return "vendored", "Upstream dependency, license and vendoring integrity"
    if path in {"docs/audit/20261002/file-review.json", "docs/audit/20261002/review-records.json", "docs/audit/20261002/FILE_REVIEW.md"}:
        return "generated-evidence", "Trace the generator, input records and digest"
    if path.startswith(("docs/audit/", "docs/history/", "docs/remediation/")):
        return "historical-candidate", "Determine current authority before reviewing or archiving"
    if suffix == ".md":
        return "documentation", "Read; confirm current setup, safety and cited entry points"
    if name.parts[0] == ".github" or suffix in {".gradle", ".properties", ".yaml", ".yml", ".toml"} or name.name in {"Dockerfile", "Makefile", ".env.example", "go.mod"}:
        return "build-or-configuration", "Read; trace inputs, side effects and deployment boundaries"
    if suffix == ".json" and ("testdata" in name.parts or "fixtures" in name.parts or "test" in name.parts):
        return "structured-fixture-data", "Parse and verify sample provenance, values and invariants"
    if ("testdata" in name.parts or "fixtures" in name.parts or "test" in name.parts or "browser-tests" in name.parts
            or "_test." in name.name or ".test." in name.name):
        return "test-or-fixture", "Read fixture and test logic or parse the data and check provenance"
    if suffix in SOURCE_SUFFIXES:
        return "first-party-source", "Read continuously by symbol; trace callers and state effects"
    return "structured-input", "Read and validate the consumer, format and source"


def git(root, *args):
    return subprocess.check_output(["git", "-C", root, *args])


def git_entries(root, commit):
    resolved = git(root, "rev-parse", "--verify", f"{commit}^{{commit}}").decode().strip()
    if resolved != commit:
        raise ValueError("The configured snapshot must be an exact commit hash")
    records = git(root, "ls-tree", "-r", "-z", commit).split(b"\0")
    for raw in filter(None, records):
        descriptor, path = raw.split(b"\t", 1)
        mode, kind, oid = descriptor.decode().split()
        yield mode, kind, oid, path.decode("utf-8")


def read_blob(process, oid):
    process.stdin.write(oid.encode() + b"\n")
    process.stdin.flush()
    header = process.stdout.readline().decode().strip().split()
    if len(header) != 3 or header[:2] != [oid, "blob"]:
        raise ValueError("Git blob descriptor did not match the inventory")
    data = process.stdout.read(int(header[2]))
    if len(data) != int(header[2]) or process.stdout.read(1) != b"\n":
        raise ValueError("Git blob ended unexpectedly")
    return data


def clean_detail(value, roots):
    if isinstance(value, list):
        return [clean_detail(item, roots) for item in value]
    if isinstance(value, dict):
        allowed = {"callers", "callees", "effects", "scope", "evidence", "reason", "status", "result", "command", "tests", "environment"}
        return {name: clean_detail(text, roots) for name, text in value.items() if name in allowed}
    if isinstance(value, (int, float, bool)) or value is None:
        return value
    return safe_text(value, roots)


def evaluate(records, commit, oid, sha256, lines, roots, *, reread=False, inspection_allowed=False):
    current, stale = [], []
    for source, record in records:
        ranges = read_ranges(record)
        if ranges is None:
            raise ValueError(f"Invalid read ranges in {source}")
        method = record.get("inspection", "")
        if not ranges and not method and record.get("status") != "reviewed":
            continue
        claimed_hash = fingerprint(record, "final_sha256", "sha256")
        claimed_blob = fingerprint(record, "blob_sha", "git_blob")
        recorded_commit = fingerprint(record, "baseline_commit", "commit", "ref")
        matches = (recorded_commit and (claimed_hash or claimed_blob)
                   and (not claimed_hash or claimed_hash == sha256)
                   and (not claimed_blob or claimed_blob == oid))
        if not matches or (reread and recorded_commit != commit):
            stale.append(source)
            continue
        if ranges and (lines is None or any(stop > lines for _, stop in ranges)):
            raise ValueError(f"Read range outside the matched source in {source}")
        basis = record.get("semantic_review")
        current.append({**{key: clean_detail(record[key], roots) for key in REVIEW_FIELDS if key in record},
                        "source": source, "commit": recorded_commit, "read_ranges": ranges,
                        "declared_status": record.get("status", "reviewing"),
                        "semantic_basis": safe_text(basis, roots) if isinstance(basis, str) else "",
                        "inspection": safe_text(method, roots)})
    ranges = sorted({tuple(interval) for record in current for interval in record["read_ranges"]})
    read_complete = lines is not None and bool(current) and covered({"read_ranges": ranges}, lines)
    inspected = inspection_allowed and any(record["inspection"] for record in current)
    signed_off = any(record["declared_status"] == "reviewed" for record in current)
    if signed_off and (read_complete or inspected):
        status = "reviewed"
    elif current:
        status = "reviewing"
    else:
        status = "stale" if stale else "pending"
    return {"status": status, "reading": {"ranges": ranges, "complete": read_complete},
            "semantic": {"declared": any(record["semantic_basis"] for record in current)},
            "runtime": {"reported": any(record.get("validation") for record in current)},
            "inspected": inspected, "evidence": current, "previous_evidence": sorted(set(stale))}


def collect(workspace, snapshots, proof_paths):
    roots = {repo: workspace / repo for repo in snapshots}
    if set(roots) != REPOSITORIES:
        raise ValueError("Snapshots must name all three repositories")
    proof = defaultdict(list)
    for path in sorted(proof_paths):
        for row in rows(read_json(path)):
            repo, name = row.get("repo"), row.get("path")
            if repo not in roots or not isinstance(name, str) or not name or PurePosixPath(name).is_absolute() or ".." in PurePosixPath(name).parts:
                raise ValueError(f"Invalid repository-relative proof path in {path.name}")
            proof[repo, name].append((path.name, row))
    files, counts = [], {}
    for repo, specification in snapshots.items():
        root, commit = roots[repo], specification["commit"]
        counters = Counter()
        command = ["git", "-C", root, "cat-file", "--batch"]
        with subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE) as reader:
            for mode, kind, oid, path in git_entries(root, commit):
                category, method = file_category(path)
                owner = next((assignee for pattern, assignee in specification.get("assign", []) if fnmatchcase(path, pattern)), specification["default_owner"])
                filespec = {"repo": repo, "path": path, "commit": commit, "blob_sha": oid, "mode": mode, "owner": owner,
                            "category": category, "method": method, "size": None, "sha256": None, "lines": None}
                if protected_input(path):
                    evidence = {"status": "excluded", "reason": "Potential credential file; Git object identity only",
                                "reading": {"ranges": [], "complete": False}, "semantic": {"declared": False}, "runtime": {"reported": False}}
                else:
                    if kind == "blob":
                        data = read_blob(reader, oid)
                        binary = b"\0" in data or mode == "120000" or category in {"image", "archive-or-binary"}
                        filespec.update(size=len(data), sha256=hashlib.sha256(data).hexdigest(), lines=None if binary else len(data.splitlines()))
                    evidence = evaluate(proof.pop((repo, path), ()), commit, oid, filespec["sha256"], filespec["lines"], roots,
                                        reread=path in specification.get("review_again", ()), inspection_allowed=category in INSPECTION_CATEGORIES)
                counters[(category, evidence["status"])] += 1
                files.append({**filespec, **evidence})
            reader.stdin.close()
            if reader.wait():
                raise ValueError("Git did not complete the pinned inventory")
        counts[repo] = {status: sum(count for (_, state), count in counters.items() if state == status)
                        for status in ("pending", "reviewing", "reviewed", "stale", "excluded")}
    absent = [{"repo": repo, "path": path, "sources": [source for source, _ in records]}
              for (repo, path), records in sorted(proof.items())]
    return {"format": 1, "boundary": "Metadata and human declarations only. A runtime reference is not verified by this tool.",
            "snapshots": {repo: spec["commit"] for repo, spec in snapshots.items()},
            "counts": counts, "outside_snapshot": absent, "files": files}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--workspace", type=Path, required=True)
    parser.add_argument("--snapshots", type=Path, required=True)
    parser.add_argument("--proof", type=Path, action="append", default=[])
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--require-complete", action="store_true")
    args = parser.parse_args(argv)
    output = args.output.absolute()
    if output.is_symlink():
        parser.error("Output must not be a symlink")
    try:
        ledger = collect(args.workspace.resolve(), read_json(args.snapshots), args.proof)
        with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", dir=output.parent, delete=False) as stream:
            path = Path(stream.name)
            try:
                json.dump(ledger, stream, ensure_ascii=False, separators=(",", ":"))
                stream.write("\n")
                stream.close()
                path.replace(output)
            finally:
                path.unlink(missing_ok=True)
        print(json.dumps(ledger["counts"], ensure_ascii=False))
        return int(args.require_complete and (bool(ledger["outside_snapshot"]) or any(
            item["status"] not in {"excluded", "reviewed"} or item["status"] == "reviewed" and not (item["semantic"]["declared"] or item["inspected"])
            for item in ledger["files"])))
    except (OSError, subprocess.CalledProcessError, KeyError, ValueError, TypeError) as error:
        parser.exit(2, f"Cannot verify snapshot review inventory ({type(error).__name__}).\n")


if __name__ == "__main__":
    raise SystemExit(main())
