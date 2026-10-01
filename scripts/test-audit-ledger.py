#!/usr/bin/env python3
"""Black-box audit inventory regression tests using disposable real Git repos."""
import argparse
import hashlib
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


LEDGER_SCRIPT = Path(__file__).with_name("audit-ledger.py")


class AuditLedgerTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="mcmods-ledger-test-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.audit = self.root / "docs/audit"
        self.audit.mkdir(parents=True)
        self.environment = {
            key: value for key, value in os.environ.items()
            if not key.startswith("GIT_")
        }
        self.environment.update(GIT_CONFIG_NOSYSTEM="1", GIT_CONFIG_GLOBAL=os.devnull,
                                GIT_TERMINAL_PROMPT="0")
        self.git("-c", "init.templateDir=", "init", "--quiet")
        self.content = b"first\nsecond\nthird\n"
        (self.root / "sample.ts").write_bytes(self.content)
        self.git("add", "sample.ts")
        self.git("-c", "user.name=Audit fixture", "-c", "user.email=audit@example.invalid",
                 "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "Synthetic fixture")
        self.baseline_commit = self.git("rev-parse", "HEAD").stdout.strip()
        self.baseline_sha = hashlib.sha256(self.content).hexdigest()
        self.baseline = [{"path": "sample.ts", "baseline_commit": self.baseline_commit,
                          "baseline_sha256": self.baseline_sha, "exclusion": None}]
        self.write_baseline()

    def git(self, *arguments):
        return subprocess.run(["git", *arguments], cwd=self.root, env=self.environment,
                              capture_output=True, text=True, check=True, timeout=10)

    def write_baseline(self):
        (self.audit / "file-ledger.json").write_text(json.dumps(self.baseline), encoding="utf-8")

    def review(self, **changes):
        record = {"path": "sample.ts", "status": "已完整审查", "sha256": self.baseline_sha,
                  "content_read": True, "semantics_reviewed": True, "behavior_verified": False,
                  "read_ranges": [[1, 3]]}
        record.update(changes)
        # This is an explicitly registered generated review inventory in both
        # versions of the real tool. The sample files themselves are manual.
        (self.audit / "review-root.json").write_text(json.dumps([record]), encoding="utf-8")

    def run_tool(self):
        process = subprocess.run([sys.executable, str(LEDGER_SCRIPT), "--check"],
                                 cwd=self.root, env=self.environment, capture_output=True,
                                 text=True, timeout=10)
        self.assertIn(process.returncode, (0, 1), process.stderr)
        rows = json.loads((self.audit / "file-ledger.json").read_text(encoding="utf-8"))
        coverage = json.loads((self.audit / "coverage.json").read_text(encoding="utf-8"))
        return process, {row["path"]: row for row in rows}, coverage

    def assert_incomplete(self, process, rows, coverage):
        self.assertEqual(process.returncode, 1)
        self.assertNotIn(rows["sample.ts"]["status"], ("已完整审查", "已复查"))
        self.assertFalse(rows["sample.ts"]["content_read"])
        self.assertFalse(rows["sample.ts"]["semantic_review"])
        self.assertEqual(coverage["complete_manual_files"], 0)

    def test_current_fingerprint_and_all_line_ranges_complete(self):
        for ranges in ([[1, 3]], [[1, 1], [2, 3]]):
            with self.subTest(ranges=ranges):
                self.review(read_ranges=ranges)
                process, rows, coverage = self.run_tool()
                self.assertEqual(process.returncode, 0, process.stderr)
                self.assertEqual(rows["sample.ts"]["status"], "已完整审查")
                self.assertEqual(rows["sample.ts"]["final_review_sha256"], self.baseline_sha)
                self.assertEqual(coverage["complete_manual_files"], 1)
                self.assertEqual(coverage["baseline_inventory_files"], 1)

    def test_unread_and_nonboolean_content_flags_cannot_complete(self):
        for value in (False, "false", "true", 1, [True], None):
            with self.subTest(value=value):
                self.review(content_read=value)
                self.assert_incomplete(*self.run_tool())

    def test_every_semantic_flag_alias_requires_boolean_true(self):
        for alias in ("semantic_review", "semantic_reviewed", "semantics_reviewed"):
            for value in (False, "false", "true", 1, [True], None):
                with self.subTest(alias=alias, value=value):
                    self.review(**{alias: value})
                    self.assert_incomplete(*self.run_tool())

    def test_behavior_evidence_requires_boolean_true(self):
        for value in (True, False, "false", "true", 1, [True], None):
            with self.subTest(value=value):
                self.review(behavior_verified=value)
                process, rows, coverage = self.run_tool()
                self.assertEqual(process.returncode, 0)
                self.assertEqual(coverage["complete_manual_files"], 1)
                self.assertEqual(rows["sample.ts"]["behavior_verified"], value is True)

    def test_missing_or_gapped_read_ranges_cannot_complete(self):
        for ranges in ([], [[1, 2]], [[1, 1], [3, 3]]):
            with self.subTest(ranges=ranges):
                self.review(read_ranges=ranges)
                self.assert_incomplete(*self.run_tool())

    def test_stale_fingerprint_cannot_complete(self):
        self.review(sha256="0" * 64)
        process, rows, coverage = self.run_tool()
        self.assert_incomplete(process, rows, coverage)
        self.assertEqual(rows["sample.ts"]["status"], "修改后待复查")

    def test_deleted_file_stays_in_original_denominator(self):
        self.review()
        self.assertEqual(self.run_tool()[0].returncode, 0)
        self.git("rm", "--quiet", "sample.ts")
        process, rows, coverage = self.run_tool()
        self.assert_incomplete(process, rows, coverage)
        self.assertEqual(coverage["baseline_inventory_files"], 1)
        self.assertTrue(rows["sample.ts"]["deleted"])
        self.assertEqual(rows["sample.ts"]["baseline_sha256"], self.baseline_sha)
        self.assertIsNone(rows["sample.ts"]["final_sha256"])

    def test_manual_json_is_not_excluded_by_filename(self):
        self.review()
        names = ("review-policy.json", "policy-validation.json", "validation-policy.json")
        for name in names:
            (self.audit / name).write_text('{"policy":"manually maintained"}', encoding="utf-8")
        process, rows, coverage = self.run_tool()
        self.assertEqual(process.returncode, 1)
        self.assertEqual(coverage["unreviewed_manual_files"], len(names))
        for name in names:
            with self.subTest(name=name):
                row = rows["docs/audit/" + name]
                self.assertIsNone(row["exclusion"])
                self.assertEqual(row["status"], "未审查")

    def test_former_name_only_exclusion_does_not_persist(self):
        self.review()
        path = "docs/audit/policy-validation.json"
        content = b'{"policy":"manually maintained"}'
        (self.root / path).write_bytes(content)
        self.baseline.append({"path": path, "baseline_commit": self.baseline_commit,
                              "baseline_sha256": hashlib.sha256(content).hexdigest(),
                              "exclusion": "机器生成测试执行结果；旧名称规则"})
        self.write_baseline()
        process, rows, coverage = self.run_tool()
        self.assertEqual(process.returncode, 1)
        self.assertIsNone(rows[path]["exclusion"])
        self.assertEqual(rows[path]["status"], "未审查")
        self.assertEqual(coverage["baseline_inventory_files"], 2)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ledger-script", type=Path, default=LEDGER_SCRIPT,
                        help="run the same assertions against a saved pre-fix tool")
    options, remaining = parser.parse_known_args()
    LEDGER_SCRIPT = options.ledger_script.resolve(strict=True)
    unittest.main(argv=[sys.argv[0], *remaining])
