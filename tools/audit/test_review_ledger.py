"""Synthetic local evidence tests; they do not constitute project review evidence."""

import contextlib
import hashlib
import io
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import review_ledger as ledger


def record(repo, path, content, owner="backend_b", status="reviewed", intervals=None):
    return {"repo": repo, "path": path, "owner": owner, "kind": "ownership",
            "source": "synthetic-review.json", "status": status,
            "semantic_review": True, "read_ranges": intervals or [[1, len(content.splitlines())]],
            "final_sha256": hashlib.sha256(content).hexdigest(), "blob_sha": "",
            "baseline_sha256": "", "category": "test", "focus": "Synthetic review declaration",
            "exclusion": "", "findings": []}


class ReviewEvidenceTests(unittest.TestCase):
    def test_all_supported_containers_and_explicit_false(self):
        for key in ("records", "entries", "files"):
            self.assertEqual(ledger.rows({key: [{"path": "sample"}]}), [{"path": "sample"}])
        self.assertFalse(ledger.semantic({"semantic_review": False,
                                         "state": "baseline_complete_semantic_review"}))
        for value in ("false", "not reviewed", "not_verified", ""):
            self.assertFalse(ledger.semantic({"semantic_review": value}))
        value = ledger.normalize({"repository": "backend", "path": "sample.go",
                                  "read": [[1, 2]], "semantic": "Human scope review"},
                                 None, "root", "proof.json", "proof", {})
        self.assertEqual(value["repo"], ledger.REPOS[0])
        self.assertEqual(value["read_ranges"], [[1, 2]])

    def test_gaps_invalid_intervals_and_truncation_cannot_be_complete(self):
        for intervals in ([[1, 2], [4, 6]], [[0, 6]], [[True, 6]], [[1, 100]], [[6, 1]], "all"):
            self.assertFalse(ledger.covered({"read_ranges": intervals}, 6))
        self.assertTrue(ledger.covered({"read_ranges": [[1, 3], [4, 7]]}, 6))
        self.assertTrue(ledger.covered({"read_ranges": []}, 0))

    def test_current_fingerprint_or_exact_blob_required(self):
        row = {"semantic_review": True, "read_ranges": [[1, 3]], "final_sha256": "a" * 64}
        self.assertTrue(ledger.matches_proof(row, "a" * 64, "b" * 40, 3))
        self.assertFalse(ledger.matches_proof(row, "c" * 64, "b" * 40, 3))
        row.update(final_sha256="", blob_sha="b" * 40)
        self.assertTrue(ledger.matches_proof(row, "c" * 64, "b" * 40, 3))
        row["status"] = "partial"
        self.assertFalse(ledger.matches_proof(row, "c" * 64, "b" * 40, 3))

    def test_duplicate_json_and_path_traversal_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "proof.json"
            path.write_text('{"semantic_review":false,"semantic_review":true}')
            with self.assertRaises(ValueError):
                ledger.read_json(path)
        for path in ("../secret", "/private/file", "folder\\secret"):
            with self.assertRaises(ValueError):
                ledger.normalize({"path": path}, ledger.REPOS[0], "root", "proof.json", "proof", {})

    def test_publish_keeps_sanitized_validation_and_private_detail(self):
        row = {"path": "a.go", "semantic_review": True, "read_ranges": [[1, 2]],
               "focus": "Check /private/source and postgresql://user:synthetic@host/db",
               "validation": ["/private/task/log password=SYNTHETIC"],
               "final_sha256": "a" * 64}
        value = ledger.normalize(row, ledger.REPOS[0], "root", "proof.json", "proof", {})
        self.assertEqual(value["validation"], ["[credential-like detail omitted]"])
        self.assertNotIn("synthetic@", json.dumps(value))
        self.assertNotIn("/private/", json.dumps(value))
        normalized = ledger.normalize({"path": "a.go", "focus_symbols": ["WriteConfig"],
            "responsibility": "Persist settings", "call_chain": "PUT -> WriteConfig -> DB",
            "validation": [{"status": "PASS", "tests": 3, "evidence": "/tmp/private/log",
                            "raw_response": "must not publish"}]},
            ledger.REPOS[0], "root", "proof.json", "proof", {})
        self.assertEqual(normalized["focus_symbols"], ["WriteConfig"])
        self.assertEqual(normalized["validation"][0]["tests"], 3)
        self.assertNotIn("raw_response", normalized["validation"][0])


class LocalInventoryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.roots = {repo: Path(self.temp.name) / repo for repo in ledger.REPOS}
        for root in self.roots.values():
            root.mkdir()
            subprocess.run(["git", "init", "-q", str(root)], check=True)
            (root / "a.txt").write_text("one\ntwo\nthree\n")
            subprocess.run(["git", "add", "a.txt"], cwd=root, check=True)
            subprocess.run(["git", "-c", "user.name=Synthetic", "-c", "user.email=synthetic@example.invalid",
                            "commit", "-qm", "Synthetic local baseline"], cwd=root, check=True)

    def rows(self, evidence):
        return ledger.inventory(self.roots, evidence, set())["files"]

    def test_independent_proof_and_root_borrowed_read_keep_module_owner(self):
        repo = ledger.REPOS[0]
        source = (self.roots[repo] / "a.txt").read_bytes()
        original = record(repo, "a.txt", source, intervals=[[1, 1]], status="partial")
        original["call_chain"] = "Declared reader -> service -> repository"
        independent = record(repo, "a.txt", source, owner="root")
        value = next(r for r in self.rows([independent, original]) if r["repo"] == repo)
        self.assertEqual(value["owner"], "backend_b")
        self.assertEqual(value["status"], "complete")
        self.assertEqual(value["read_ranges"], [[1, 3]])
        self.assertFalse(value["behavior_verified"])
        self.assertEqual(value["call_chain"], original["call_chain"])

    def test_stale_and_partial_reads_remain_incomplete(self):
        repo = ledger.REPOS[0]
        source = (self.roots[repo] / "a.txt").read_bytes()
        entry = record(repo, "a.txt", source, intervals=[[1, 1]], status="partial")
        value = next(r for r in self.rows([entry]) if r["repo"] == repo)
        self.assertEqual(value["status"], "partial")
        self.assertEqual(value["read_ranges"], [[1, 1]])
        (self.roots[repo] / "a.txt").write_text("different\n")
        value = next(r for r in self.rows([entry]) if r["repo"] == repo)
        self.assertEqual(value["status"], "partial")
        self.assertEqual(value["read_ranges"], [])

    def test_manual_assets_not_excluded_and_private_env_not_read(self):
        repo, root = ledger.REPOS[0], self.roots[ledger.REPOS[0]]
        entries = []
        for path in ("manual.svg", "manual.sql", "translations/en-US.json"):
            file = root / path
            file.parent.mkdir(parents=True, exist_ok=True)
            file.write_text("synthetic\n")
            entry = record(repo, path, file.read_bytes(), status="excluded")
            entry["exclusion"] = "Incorrect generated exclusion"
            entries.append(entry)
        (root / ".env").write_text("SYNTHETIC_PRIVATE_VALUE")
        original_read = Path.read_bytes

        def safe_read(path):
            if path.name == ".env":
                raise AssertionError("private environment must never be read")
            return original_read(path)

        with patch.object(Path, "read_bytes", safe_read):
            result = self.rows(entries)
        for value in result:
            if value["path"] in {"manual.svg", "manual.sql", "translations/en-US.json"}:
                self.assertEqual(value["status"], "partial")
            if value["path"] == ".env":
                self.assertIsNone(value["sha256"])
                self.assertEqual(value["status"], "excluded")

    def test_tracked_deletion_stays_in_denominator(self):
        (self.roots[ledger.REPOS[0]] / "a.txt").unlink()
        value = next(r for r in self.rows([]) if r["repo"] == ledger.REPOS[0])
        self.assertTrue(value["tracked_deletion"])
        self.assertEqual(value["status"], "partial")

    def test_snapshot_replay_self_outputs_and_require_complete(self):
        output = self.roots[ledger.REPOS[0]] / "audit"
        input_file = Path(self.temp.name) / "input.json"
        evidence = [record(repo, "a.txt", (root / "a.txt").read_bytes()) for repo, root in self.roots.items()]
        input_file.write_text(json.dumps({"records": evidence}))
        args = ["--backend-root", str(self.roots[ledger.REPOS[0]]), "--frontend-root", str(self.roots[ledger.REPOS[1]]),
                "--review-records", str(input_file), "--output-dir", str(output), "--require-complete"]
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(ledger.main(args), 0)
            before = json.loads((output / "file-review.json").read_text())
            args[args.index(str(input_file))] = str(output / "review-records.json")
            self.assertEqual(ledger.main(args), 0)
            after = json.loads((output / "file-review.json").read_text())
        self.assertEqual(before["stats"], after["stats"])
        self.assertEqual(before["files"], after["files"])
        self.assertEqual(before["stats"][ledger.REPOS[0]]["excluded"], 3)
        (self.roots[ledger.REPOS[0]] / "unread.go").write_text("package unread\n")
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(ledger.main(args), 1)

    def test_generated_output_symlink_never_overwrites_target(self):
        output = Path(self.temp.name) / "output"
        output.mkdir()
        sentinel = Path(self.temp.name) / "sentinel"
        sentinel.write_text("must remain")
        (output / "review-records.json").symlink_to(sentinel)
        input_file = Path(self.temp.name) / "input.json"
        input_file.write_text("[]")
        with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit) as result:
            ledger.main(["--backend-root", str(self.roots[ledger.REPOS[0]]), "--frontend-root", str(self.roots[ledger.REPOS[1]]),
                         "--review-records", str(input_file), "--output-dir", str(output)])
        self.assertEqual(result.exception.code, 2)
        self.assertEqual(sentinel.read_text(), "must remain")


if __name__ == "__main__":
    unittest.main()
