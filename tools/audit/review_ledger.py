#!/usr/bin/env python3
"""Publish human review evidence with current local fingerprints; never invent reads."""

import argparse
import collections
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import subprocess

REPOS = ("mcmods-cn-backend", "mcmods-cn-frontend")
OWNERS = ("root", "backend_a", "backend_b", "backend_c", "database_docs",
          "frontend_pages", "frontend_shared")
COMPLETE = {"complete", "reviewed", "rechecked", "re-reviewed", "rereviewed"}
OUTPUTS = ("review-records.json", "file-review.json", "FILE_REVIEW.md")
PROOFS = {
    "root-offline-records.json": REPOS[0],
    "backend_a-remote-review.json": REPOS[0],
    "backend_b-offline-reviews.json": REPOS[0],
    "backend_c-offline.json": REPOS[0],
    "database-docs-offline-extra-reads.json": REPOS[0],
    "frontend_shared-offline-backend.json": REPOS[0],
    "frontend-pages-offline-baseline.json": REPOS[1],
    "backend_c-b-extra-final.json": REPOS[0],
    "database-docs-a-extra-final.json": REPOS[0],
    "frontend_pages-cross-review-proofs.json": REPOS[1],
    "frontend-shared-additional-backend-proof.json": REPOS[0],
    "root-c-final-proof.json": REPOS[0],
    "root-b-extra-final.json": REPOS[0],
    "root-a-extra-final.json": REPOS[0],
    "root-fp-extra-final.json": REPOS[1],
    "backend_c-b-extra2-final.json": REPOS[0],
    "fp-db-final-proof.json": REPOS[1],
    "frontend-shared-application-proof.json": REPOS[1],
    "database-b-extra-final.json": REPOS[0],
    "backend_a-borrowed-global-final.json": REPOS[0],
    "backend_c-b028-final-proof.json": REPOS[0],
    "backend_c-b031-final-proof.json": REPOS[0],
    "frontend-shared-category-proof.json": REPOS[1],
    "frontend-shared-changelog-fix-proof.json": REPOS[0],
}
PREFERRED = {
    (REPOS[0], "internal/httpapi/admin_economy_handlers.go"): "backend_b",
    (REPOS[0], "internal/httpapi/project_file_handlers.go"): "backend_c",
    (REPOS[0], "internal/httpapi/project_file_pagination.go"): "backend_c",
    (REPOS[1], "app/_lib/blueprint-csv.mts"): "frontend_shared",
    (REPOS[1], "app/_lib/blueprint-csv.test.mts"): "frontend_shared",
}
for _path in ("docs/oss-object-layout.md", "REPORT_AND_MODERATION_DESIGN.md",
              "LOG_SHARE_SECURITY.md", "FAVORITE_MODPACK_EXPORT.md",
              "SYSTEM_NOTIFICATION_LOCALIZATION.md", "PROJECT_FOLLOW_NOTIFICATIONS.md",
              "PROJECT_AUTO_UPDATE_DESIGN.md"):
    PREFERRED[(REPOS[0], _path)] = "database_docs"


def read_json(path):
    """Read only explicit review JSON, never arbitrary environment/config files."""
    if path.suffix != ".json" or path.name.startswith(".env"):
        raise ValueError("Review input must be an explicit .json evidence file")

    def unique_keys(pairs):
        value = {}
        for key, item in pairs:
            if key in value:
                raise ValueError("Duplicate JSON key in review evidence")
            value[key] = item
        return value

    return json.loads(path.read_text(encoding="utf-8"), object_pairs_hook=unique_keys)


def rows(value):
    if isinstance(value, list):
        return value
    if isinstance(value, dict):
        for key in ("records", "entries", "files"):
            if isinstance(value.get(key), list):
                return value[key]
    return []


def read_ranges(row):
    value = row.get("read_ranges", row.get("readRanges", row.get("read", [])))
    if not value and row.get("range"):
        value = [row["range"]]
    if not isinstance(value, list):
        return None
    for interval in value:
        if (not isinstance(interval, (list, tuple)) or len(interval) != 2
                or any(type(n) is not int for n in interval)
                or interval[0] < 1 or interval[1] < interval[0]):
            return None
    return [list(interval) for interval in value]


def covered(row, lines):
    intervals = read_ranges(row)
    if intervals is None:
        return False
    end = 0
    for start, stop in sorted(intervals):
        if start > end + 1 or stop > lines + 1:
            return False
        end = max(end, stop)
    # Fixed-ref viewers sometimes show a terminal blank line after splitlines().
    return end >= lines


def semantic(row):
    for key in ("semantic_review", "semantic", "semanticReview"):
        if key in row:
            value = row[key]
            if isinstance(value, bool):
                return value
            return (isinstance(value, str) and len(value.strip()) > 10
                    and value.strip().lower() not in {
                        "not_reviewed", "not reviewed", "not_verified", "not verified"})
    return row.get("state") in {
        "baseline_complete_semantic_review",
        "baseline_semantic_reviewed_final_workspace_unavailable",
    }


def fingerprint(row, *keys):
    return next((row[key] for key in keys if isinstance(row.get(key), str)
                 and re.fullmatch(r"[0-9a-f]{40}|[0-9a-f]{64}", row[key])), "")


def matches_proof(row, sha256, blob, lines):
    if row.get("status") in {"partial", "unreviewed", "modified_pending_recheck"}:
        return False
    final = fingerprint(row, "final_sha256", "final_review_version")
    recorded_blob = fingerprint(row, "blob", "blob_sha", "git_blob", "git_blob_sha",
                                "sha", "baseline_blob")
    return semantic(row) and covered(row, lines) and (
        final == sha256 or recorded_blob == blob)


def safe_text(value, roots):
    text = str(value or "")
    for repo, root in roots.items():
        text = text.replace(str(root), repo)
    text = re.sub(r"(?:postgres(?:ql)?|redis|nats|https?)://\S+", "[URL omitted]", text)
    text = re.sub(r"(?:[A-Za-z]:[\\/]|/(?:workspace|tmp|home|root|mnt|private|Users|var|opt|srv|run|secrets)/)[^\s,;]+",
                  "[private path omitted]", text)
    if re.search(r"(?:password|api[_-]?key|access[_-]?token|secret)\s*[:=]\s*\S+", text, re.I):
        return "[credential-like detail omitted]"
    return text[:4000]


def safe_metadata(value, roots):
    """Keep declared review metadata, never read its referenced files or logs."""
    if isinstance(value, str):
        return safe_text(value, roots)
    if isinstance(value, (bool, int, float)) or value is None:
        return value
    if isinstance(value, list):
        return [safe_metadata(item, roots) for item in value]
    if isinstance(value, dict):
        allowed = {"status", "evidence", "top_level_tests", "subtests", "skipped",
                   "boundary", "command", "tests", "reason", "result", "scope", "environment"}
        return {key: safe_metadata(item, roots) for key, item in value.items() if key in allowed}
    return "[unsupported metadata omitted]"


def normalize(row, default_repo, owner, source, kind, roots):
    repo = row.get("repo", row.get("repository", default_repo))
    repo = {"backend": REPOS[0], "frontend": REPOS[1]}.get(repo, repo)
    path = row.get("path", "")
    if (repo not in REPOS or not isinstance(path, str) or not path
            or PurePosixPath(path).is_absolute() or ".." in PurePosixPath(path).parts
            or "\\" in path):
        raise ValueError("Review record requires a known repository and relative path")
    intervals = read_ranges(row)
    if intervals is None:
        raise ValueError("Malformed human read interval in review evidence")
    note = row.get("focus", row.get("final_review_focus", row.get("review_focus", row.get("semantic_review_basis", ""))))
    if not note and isinstance(row.get("semantic_review"), str):
        note = row["semantic_review"]
    return {
        "repo": repo, "path": path, "owner": owner, "source": source,
        "kind": kind, "status": str(row.get("status", row.get("state", ""))),
        "read_ranges": intervals, "semantic_review": semantic(row),
        "final_sha256": fingerprint(row, "final_sha256", "final_review_version"),
        "blob_sha": fingerprint(row, "blob", "blob_sha", "git_blob", "git_blob_sha",
                                "sha", "baseline_blob"),
        "baseline": fingerprint(row, "baseline", "baseline_commit", "ref"),
        "baseline_sha256": fingerprint(row, "sha256", "baseline_sha256"),
        "category": safe_text(row.get("category", ""), roots),
        "focus": safe_text(note, roots),
        "focus_symbols": safe_metadata(row.get("focus_symbols", row.get("symbols", [])), roots),
        "responsibility": safe_metadata(row.get("responsibility", ""), roots),
        "call_chain": safe_metadata(row.get("call_chain", ""), roots),
        "validation": safe_metadata(row.get("validation", []), roots),
        "exclusion": safe_text(row.get("exclusion", ""), roots),
        "findings": [item for item in row.get("findings", []) if isinstance(item, str)
                     and re.fullmatch(r"[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)+", item)],
        "behavior_claim": row.get("behavior_verified") is True,
        "deletion_reviewed": row.get("deletion_reviewed") is True,
        "deletion_reason": safe_text(row.get("deletion_reason", ""), roots),
    }


def load_evidence(evidence_dir, snapshot, extra, roots):
    result, missing = [], []
    if snapshot:
        value = read_json(snapshot)
        for row in rows(value):
            item = normalize(row, row.get("repo"), row.get("owner", "UNASSIGNED"),
                             Path(row.get("source", "review-records.json")).name,
                             row.get("kind", "proof"), roots)
            # Published snapshots are declarations, not a new human review.
            item["behavior_claim"] = row.get("behavior_claim") is True
            result.append(item)
    if evidence_dir:
        sources = [(owner + ".json", None, owner, "ownership") for owner in OWNERS]
        sources += [(name, repo, "INDEPENDENT", "proof") for name, repo in PROOFS.items()]
        for name, repo, owner, kind in sources:
            path = evidence_dir / name
            if not path.exists():
                missing.append(name)
                continue
            value = read_json(path)
            default = value.get("repository", repo) if isinstance(value, dict) else repo
            result += [normalize(row, default, owner, name, kind, roots) for row in rows(value)]
            if isinstance(value, dict) and isinstance(value.get("validation04"), dict):
                row = dict(value["validation04"])
                row["read_ranges"] = sum((row.get(key, []) for key in
                                          ("root_complete", "db_complete", "fs_complete")), [])
                row["semantic_review"] = True
                result.append(normalize(row, repo, owner, name + "#validation04", "proof", roots))
    for path in extra:
        value = read_json(path)
        default = value.get("repository", REPOS[0]) if isinstance(value, dict) else REPOS[0]
        result += [normalize(row, default, "INDEPENDENT", path.name, "proof", roots)
                   for row in rows(value)]
    return result, missing


def git(root, *args):
    process = subprocess.run(["git", *args], cwd=root, capture_output=True)
    if process.returncode:
        raise ValueError("Local Git inventory failed; no remote operation was attempted")
    return process.stdout.decode("utf-8")


def protected_input(path):
    name = PurePosixPath(path).name
    return (name == ".env" or name.startswith(".env.")
            and name not in {".env.example", ".env.sample", ".env.template"}
            or name.endswith((".pem", ".key")))


def maintainable_asset(path):
    p = PurePosixPath(path)
    return p.suffix in {".svg", ".sql"} or (p.suffix == ".json" and
        any(part in {"locales", "_locales", "translations", "messages", "i18n"} for part in p.parts))


def inventory(roots, evidence, generated):
    by_key = collections.defaultdict(list)
    for row in evidence:
        by_key[(row["repo"], row["path"])].append(row)
    files, stats, duplicates, versions = [], {}, [], {}
    for repo, root in roots.items():
        paths = sorted(set(filter(None, git(root, "ls-files", "-z", "--cached", "--others",
                                           "--exclude-standard").split("\0"))))
        algorithm = git(root, "rev-parse", "--show-object-format").strip()
        versions[repo] = {"head": git(root, "rev-parse", "HEAD").strip(),
                          "branch": git(root, "branch", "--show-current").strip()}
        counts = collections.Counter()
        for path in paths:
            file = root / path
            candidates = by_key[(repo, path)]
            assigned = [r for r in candidates if r["kind"] == "ownership"]
            # Root's borrowed final reads supplement the original module owner.
            original = [r for r in assigned if r["owner"] != "root"] or assigned
            preferred = PREFERRED.get((repo, path))
            row = next((r for r in original if r["owner"] == preferred), original[0] if original else {})
            owner = row.get("owner", "UNASSIGNED")
            if len({r["owner"] for r in original}) > 1:
                duplicates.append({"repo": repo, "path": path, "owners": sorted({r["owner"] for r in original}),
                                   "canonical_owner": owner})
            reason, sha, blob, lines, valid = "", None, None, None, []
            missing = not file.exists() and not file.is_symlink()
            if protected_input(path):
                status, reason = "excluded", "Potential private credential file: content not read or hashed"
            elif missing:
                status = "complete" if row.get("deletion_reviewed") and row.get("deletion_reason") else "partial"
                reason = row.get("deletion_reason", "Tracked deletion requires explicit review record; retained in denominator")
            elif not file.is_file() and not file.is_symlink():
                status, reason = "excluded", "Git submodule or non-file object; project boundary must be reviewed separately"
            else:
                data = os.fsencode(os.readlink(file)) if file.is_symlink() else file.read_bytes()
                sha = hashlib.sha256(data).hexdigest()
                blob = hashlib.new(algorithm, b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest()
                lines = len(data.splitlines())
                valid = [r for r in candidates if matches_proof(r, sha, blob, lines)
                         and (r["kind"] == "proof" or r["status"] in COMPLETE)]
                if (repo, path) in generated:
                    status, reason = "excluded", "Generated ledger output; review generator and human source records, not self-referential bytes"
                elif row.get("status") == "excluded" and row.get("exclusion") and not maintainable_asset(path):
                    status, reason = "excluded", row["exclusion"]
                elif valid:
                    status = "complete"
                elif any(r["semantic_review"] or r["read_ranges"] for r in candidates):
                    status = "partial"
                else:
                    status = "unreviewed"
            matching = next((r for r in valid if r["owner"] == owner), valid[0] if valid else None)
            current_reads = [r for r in candidates if sha and (
                r["final_sha256"] == sha or r["blob_sha"] == blob
                or not r["final_sha256"] and r["baseline_sha256"] == sha)]
            read_record = matching or next((r for r in current_reads if r["owner"] == owner),
                                          current_reads[0] if current_reads else None)
            ranges = read_record["read_ranges"] if read_record else []
            details = matching or row
            behavior_evidence = [{"source": r["source"], "human_behavior_claim": r.get("behavior_claim", False),
                                  "version_matches_current": r in current_reads,
                                  "validation": r.get("validation", [])} for r in candidates
                                 if r.get("validation") or r.get("behavior_claim")]
            counts[status] += 1
            files.append({"repo": repo, "path": path, "owner": owner,
                          "category": row.get("category", PurePosixPath(path).suffix or "config"),
                          "sha256": sha, "blob_sha": blob, "lines": lines,
                          "status": status, "read_ranges": ranges,
                          "content_read": covered(read_record, lines) if read_record and lines is not None else False,
                          "semantic_review": bool(matching),
                          "behavior_verified": False,
                          "behavior_boundary": "This tool does not execute tests or validate inherited behavior claims",
                          "behavior_claim": any(r.get("behavior_claim", False) for r in current_reads),
                          "behavior_evidence": behavior_evidence,
                          "validation_documents": ["docs/audit/20261002/VERIFICATION.md", "docs/audit/20261002/ISSUES.md"],
                          "focus": details.get("focus", ""),
                          "focus_symbols": details.get("focus_symbols") or row.get("focus_symbols", []),
                          "responsibility": details.get("responsibility") or row.get("responsibility", ""),
                          "call_chain": details.get("call_chain") or row.get("call_chain", ""),
                          "findings": sorted({f for r in candidates for f in r["findings"]}),
                          "proofs": sorted({r["source"] for r in valid}),
                          "exclusion": reason, "tracked_deletion": missing,
                          "record_sources": sorted({r["source"] for r in candidates})})
        stats[repo] = {"total": sum(counts.values()), **{key: counts[key] for key in
                        ("complete", "partial", "unreviewed", "excluded")}}
    return {"versions": versions, "stats": stats, "files": files,
            "duplicate_ownership": duplicates,
            "unassigned": [{"repo": r["repo"], "path": r["path"]} for r in files
                           if r["owner"] == "UNASSIGNED" and r["status"] != "excluded"]}


def markdown(report):
    output = ["# 逐文件审查台账", "", "此文件由 `tools/audit/review_ledger.py` 生成。",
              "`review-records.json` 是人工读取证据的脱敏快照；生成、散列或历史 PASS 不构成人工阅读或行为验证。",
              "只枚举 Git 跟踪及未忽略的新文件；node_modules、vendor/build 等被忽略依赖和产物不进入分母。",
              "完整审查要求当前指纹匹配、人工语义声明和连续完整读取区间；旧指纹、漏段或部分审查仍为未完成。",
              "自引用台账输出明确合理排除；人工 SVG、SQL、翻译 JSON 不因格式被排除。缺失的跟踪文件保留为删除项。",
              "行为测试结果请查本轮验证记录；此工具不执行或继承行为 PASS。", "",
              "脱敏的人工验证引用与原始行为声明分别保留在 JSON 的 `validation` / `behavior_evidence` / `behavior_claim` 中。",
              "`version_matches_current` 仅表示内容指纹匹配；声明是否成立请结合 [VERIFICATION.md](VERIFICATION.md) 与 [ISSUES.md](ISSUES.md) 复核。", "",
              "| 仓库 | 总数 | 完整 | 部分 | 未审 | 排除 |", "| --- | ---: | ---: | ---: | ---: | ---: |"]
    for repo, count in report["stats"].items():
        output.append(f"| {repo} | {count['total']} | {count['complete']} | {count['partial']} | {count['unreviewed']} | {count['excluded']} |")
    output += ["", "复现（Python 标准库，无网络或生产服务）：", "", "```sh",
               "python3 -m unittest discover -s tools/audit -p 'test_review_ledger.py' -v",
               "python3 tools/audit/review_ledger.py --backend-root . --frontend-root ../mcmods-cn-frontend \\",
               "  --review-records docs/audit/20261002/review-records.json --output-dir docs/audit/20261002",
               "```", "", "也可用 `--evidence-dir <人工证据目录>` 收集原始台账，使用重复 `--additional-proof <JSON>` 加入新增独立证据。",
               "默认退出0表示生成成功，不表示全部完成；`--require-complete` 会对部分/未审项返回1。", "",
               "| 仓库/路径 | 归属 | 行数 | 状态 | 实际读段 | SHA-256 | 排除/删除依据 |",
               "| --- | --- | ---: | --- | --- | --- | --- |"]
    for row in report["files"]:
        ranges = ", ".join(f"{a}–{b}" for a, b in row["read_ranges"])
        values = [f"{row['repo']}/{row['path']}", row["owner"], str(row["lines"] or 0),
                  row["status"], ranges, row["sha256"] or "未读取", row["exclusion"]]
        output.append("| " + " | ".join(v.replace("|", "\\|").replace("\n", " ") for v in values) + " |")
    return "\n".join(output) + "\n"


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--backend-root", type=Path, required=True)
    parser.add_argument("--frontend-root", type=Path, required=True)
    parser.add_argument("--evidence-dir", type=Path)
    parser.add_argument("--review-records", type=Path)
    parser.add_argument("--additional-proof", type=Path, action="append", default=[])
    parser.add_argument("--output-dir", "--output", type=Path, required=True)
    parser.add_argument("--require-complete", action="store_true")
    args = parser.parse_args(argv)
    if not args.evidence_dir and not args.review_records:
        parser.error("Supply --evidence-dir or --review-records; scans cannot create human review evidence")
    roots = dict(zip(REPOS, (args.backend_root.resolve(), args.frontend_root.resolve())))
    try:
        evidence, missing = load_evidence(args.evidence_dir, args.review_records, args.additional_proof, roots)
        output = args.output_dir.resolve()
        output.mkdir(parents=True, exist_ok=True)
        if any((output / name).is_symlink() for name in OUTPUTS):
            raise ValueError("Generated outputs must not overwrite symlink targets")
        snapshot = {"format_version": 1, "derived": True,
                    "boundary": "Sanitized human declarations, not automatic semantic review or behavior PASS",
                    "records": evidence}
        (output / OUTPUTS[0]).write_text(json.dumps(snapshot, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        # Ensure all three generated outputs enter their own final file list on first run.
        for name in OUTPUTS[1:]:
            (output / name).touch(exist_ok=True)
        generated = set()
        for repo, root in roots.items():
            for name in OUTPUTS:
                try:
                    generated.add((repo, (output / name).relative_to(root).as_posix()))
                except ValueError:
                    pass
        report = inventory(roots, evidence, generated)
        report["missing_evidence_sources"] = missing
        report["generation_boundary"] = "Metadata-only local scan; no tests, installs, remote Git or services executed"
        # Self-referential generated files have no stable content fingerprint in this snapshot.
        for row in report["files"]:
            if (row["repo"], row["path"]) in generated:
                row.update(sha256=None, blob_sha=None, lines=None, read_ranges=[],
                           content_read=False, semantic_review=False, proofs=[])
        (output / OUTPUTS[1]).write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        (output / OUTPUTS[2]).write_text(markdown(report), encoding="utf-8")
        print(json.dumps(report["stats"], ensure_ascii=False))
        pending = any(count["partial"] or count["unreviewed"] for count in report["stats"].values())
        return int(args.require_complete and pending)
    except (OSError, ValueError, TypeError, json.JSONDecodeError):
        parser.exit(2, "Invalid or inaccessible review evidence/local repository; no completion claim produced\n")


if __name__ == "__main__":
    raise SystemExit(main())
