#!/usr/bin/env python3
"""Merge explicit human review records; never infer review from file reads/tests."""
import argparse
import hashlib
import json
import subprocess
from collections import Counter
from pathlib import Path


# Confirmed generated inventories/evidence for this audit. A new file with a
# similar name remains a manual file until its origin is explicitly checked.
GENERATED_ARTIFACTS = {
    "docs/audit/file-ledger.json", "docs/audit/coverage.json",
    "docs/audit/evidence/query-plans.json", "docs/audit/evidence/contracts.json",
    "docs/audit/module-contracts.json", "docs/audit/module-contract-validation.json",
    "docs/audit/api-a-validation.json", "docs/audit/backend-api-b-validation.json",
    "docs/audit/backend-ui-a-extra-validation.json", "docs/audit/backend-ui-a-validation.json",
    "docs/audit/backend-ui-b-validation.json", "docs/audit/ui-a-validation.json",
    "docs/audit/validation-api-core-supplement.json", "docs/audit/validation-frontend-core.json",
    "docs/audit/validation-frontend-ui-b.json", "docs/audit/review-api-a-core-supplement.json",
    "docs/audit/review-backend-api-a.json", "docs/audit/review-backend-api-b-ui-b.json",
    "docs/audit/review-backend-api-b.json", "docs/audit/review-backend-database-extra.json",
    "docs/audit/review-backend-database.json", "docs/audit/review-backend-ui-a-delegated.json",
    "docs/audit/review-backend-ui-a-extra.json", "docs/audit/review-ledger-tool.json",
    "docs/audit/review-module-contracts.json", "docs/audit/review-root.json",
    "docs/audit/review-root-svg-extra.json", "docs/audit/review-frontend-core.json",
    "docs/audit/review-frontend-locales-zh.json", "docs/audit/review-frontend-ui-a.json",
    "docs/audit/review-frontend-ui-b-delegated.json", "docs/audit/review-frontend-ui-b.json",
    "docs/audit/review-backend-api-a-additions.json",
    "docs/audit/review-admin-config-load-contract.json",
    "docs/audit/admin-config-load-validation.json",
    "docs/audit/review-root-testenv.json", "docs/audit/evidence/ai-db-final.json",
    "docs/audit/validation-root.json",
    "docs/audit/evidence/api-a-current-targeted.json",
    "docs/audit/evidence/ai-public-get.json",
    "docs/audit/review-delegated-boundaries.json",
    "docs/audit/review-ui-a-final-interactions.json",
    "docs/audit/review-manifests/backend_api_a.json", "docs/audit/review-manifests/backend_api_b.json",
    "docs/audit/review-manifests/backend_database.json", "docs/audit/review-manifests/root_backend.json",
    "docs/audit/review-manifests/frontend_core.json", "docs/audit/review-manifests/frontend_ui_a.json",
    "docs/audit/review-manifests/frontend_ui_b.json", "docs/audit/review-manifests/root_frontend.json",
}


def digest(data):
    return hashlib.sha256(data).hexdigest()


def review_rows(value):
    if isinstance(value, list):
        return value
    if "path" in value:
        return [value]
    rows = list(value.get("files", value.get("records", [])))
    rows.extend(value.get("new_files_reviewed", []))
    rows.extend(value.get("additional_files", []))
    return rows


def ranges_cover(ranges, count):
    end = 0
    for lower, upper in sorted(ranges):
        if lower > end + 1:
            return False
        end = max(end, upper)
    return end >= count


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="fail if a manual file lacks a current complete review")
    args = parser.parse_args()
    root = Path(subprocess.check_output(["git", "rev-parse", "--show-toplevel"], text=True).strip())
    directory = root / "docs/audit"
    target = directory / "file-ledger.json"
    baseline_rows = json.loads(target.read_text())
    baseline = {row["path"]: row for row in baseline_rows}
    records = {}
    for source in sorted(directory.glob("review-*.json")):
        for row in review_rows(json.loads(source.read_text())):
            if isinstance(row, dict) and isinstance(row.get("path"), str):
                records.setdefault(row["path"], []).append((source.relative_to(root).as_posix(), row))
    listed = subprocess.check_output(["git", "ls-files", "-co", "--exclude-standard", "-z"]).decode().split("\0")
    paths = sorted(set(baseline) | {"docs/audit/coverage.json"} | {p for p in listed if p and not p.startswith(".audit-tools/")})
    final = []
    for path in paths:
        previous = baseline.get(path, {})
        file = root / path
        data = file.read_bytes() if file.is_file() else None
        fingerprint = digest(data) if data is not None else None
        count = len(data.splitlines()) if data is not None else 0
        exclusion = previous.get("exclusion") if previous.get("baseline_sha256") else None
        if exclusion and exclusion.startswith("机器生成"):
            exclusion = None  # Do not perpetuate the former name-only rule.
        if path in GENERATED_ARTIFACTS:
            exclusion = "明确登记的生成审查清单/执行证据；生成器与人工结论另审，不继承测试结论，自引用清单省略自身指纹"
        candidates = records.get(path, [])
        matching = [(source, row) for source, row in candidates if row.get("sha256", row.get("final_review_sha256")) == fingerprint]
        completed = []
        for source, row in matching:
            semantic = row.get("semantic_review", row.get("semantic_reviewed", row.get("semantics_reviewed", False)))
            if row.get("status") in ("已完整审查", "已复查") and row.get("content_read") is True and semantic is True and ranges_cover(row.get("read_ranges", []), count):
                completed.append((source, row))
        selected = (completed or matching or candidates)[-1] if (completed or matching or candidates) else (None, {})
        source, review = selected
        if exclusion:
            status = "合理排除"
        elif completed:
            status = review["status"]
        elif candidates and any(row.get("content_read") is True for _, row in candidates):
            status = "修改后待复查" if not matching else "部分审查"
        else:
            status = "未审查"
        row = {
            "repository": previous.get("repository", root.name), "path": path,
            "baseline_commit": previous.get("baseline_commit", baseline_rows[0]["baseline_commit"]),
            "baseline_sha256": previous.get("baseline_sha256"), "lines": count,
            "category": previous.get("category", file.suffix or "configuration"),
            "read_ranges": review.get("read_ranges", []), "responsibility": review.get("responsibility", ""),
            "call_chain": review.get("call_chain", []), "issues": review.get("issues", []),
            "validation": review.get("validation", []), "status": status,
            "content_read": bool(completed), "semantic_review": bool(completed),
            "behavior_verified": bool(completed and review.get("behavior_verified") is True),
            "review_evidence": source, "exclusion": exclusion,
            "final_sha256": None if path in ("docs/audit/file-ledger.json", "docs/audit/coverage.json") else fingerprint,
            "final_review_sha256": fingerprint if completed else None,
            "deleted": data is None,
        }
        final.append(row)
    counts = Counter(row["status"] for row in final)
    coverage = {
        "baseline_commit": baseline_rows[0]["baseline_commit"], "tracked_head": subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip(),
        "final_inventory_files": len(final), "baseline_inventory_files": sum(row.get("baseline_sha256") is not None for row in final),
        "counts": dict(sorted(counts.items())),
        "complete_manual_files": counts["已完整审查"] + counts["已复查"],
        "partial_manual_files": counts["部分审查"] + counts["修改后待复查"],
        "unreviewed_manual_files": counts["未审查"], "excluded_files": counts["合理排除"],
        "scope": "Git tracked baseline plus final nonignored project files; deletions retained. Task-only .audit-tools excluded. Locks/binaries/generated evidence exclusions explicit per file.",
        "verification_scope": "Human content/semantic review is separate from behavior/tests; behavior_verified is limited to each cited assertion, never whole-file exhaustive coverage.",
        "fingerprint_note": "Self-referential file-ledger/coverage fingerprints omitted; review inventories must match final file SHA256 and cover every line. Reading/scanning alone never marks a file reviewed.",
    }
    target.write_text(json.dumps(final, ensure_ascii=False, indent=2) + "\n")
    (directory / "coverage.json").write_text(json.dumps(coverage, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps(coverage, ensure_ascii=False, indent=2))
    if args.check and (coverage["partial_manual_files"] or coverage["unreviewed_manual_files"]):
        raise SystemExit(1)


if __name__ == "__main__":
    main()
