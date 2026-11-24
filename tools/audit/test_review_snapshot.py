import unittest

from review_snapshot import INSPECTION_CATEGORIES, evaluate, file_category


COMMIT = "a" * 40
BLOB = "b" * 40
SHA = "c" * 64


def proof(**overrides):
    return ("reviewer.json", {
        "baseline_commit": COMMIT, "blob_sha": BLOB, "final_sha256": SHA,
        "status": "reviewed", "semantic_review": "Traced the input and the transaction owner.",
        **overrides,
    })


class ReviewSnapshotTests(unittest.TestCase):
    def evaluate(self, records, *, lines=6, **options):
        return evaluate(records, COMMIT, BLOB, SHA, lines, {}, **options)

    def test_inventory_without_human_review_remains_pending(self):
        item = self.evaluate([])
        self.assertEqual(item["status"], "pending")
        self.assertFalse(item["reading"]["complete"])
        self.assertFalse(item["semantic"]["declared"])

    def test_runtime_reference_cannot_fill_a_gap_in_reading(self):
        item = self.evaluate([proof(read_ranges=[[1, 2], [4, 6]], validation=["Focused Go test passed"])])
        self.assertEqual(item["status"], "reviewing")
        self.assertFalse(item["reading"]["complete"])
        self.assertTrue(item["runtime"]["reported"])

    def test_matching_manual_ranges_and_semantics_are_distinct(self):
        item = self.evaluate([proof(read_ranges=[[1, 3], [4, 6]])])
        self.assertEqual(item["status"], "reviewed")
        self.assertTrue(item["reading"]["complete"])
        self.assertTrue(item["semantic"]["declared"])
        self.assertFalse(item["runtime"]["reported"])

    def test_reviewed_count_requires_a_semantic_conclusion_or_valid_inspection(self):
        for note in ("", "   ", "not reviewed", "not_verified", False):
            with self.subTest(note=note):
                item = self.evaluate([proof(read_ranges=[[1, 6]], semantic_review=note)])
                self.assertTrue(item["reading"]["complete"])
                self.assertFalse(item["semantic"]["declared"])
                self.assertEqual(item["status"], "reviewing")
        item = self.evaluate([proof(semantic_review="", inspection="  ")], inspection_allowed=True)
        self.assertFalse(item["inspected"])
        self.assertEqual(item["status"], "reviewing")

    def test_mismatched_fingerprint_and_changed_caller_require_review(self):
        for evidence, options in (
            (proof(final_sha256="d" * 64, read_ranges=[[1, 6]]), {}),
            (proof(baseline_commit="e" * 40, read_ranges=[[1, 6]]), {"reread": True}),
        ):
            with self.subTest(evidence=evidence):
                item = self.evaluate([evidence], **options)
                self.assertEqual(item["status"], "stale")
                self.assertFalse(item["reading"]["complete"])

    def test_manual_inspection_is_allowed_for_locks_and_binaries(self):
        evidence = [proof(inspection="Verified dependency integrity against the producer lock file.")]
        item = self.evaluate(evidence, inspection_allowed=True)
        self.assertEqual(item["status"], "reviewed")
        self.assertFalse(item["reading"]["complete"])
        self.assertTrue(item["inspected"])
        self.assertEqual(self.evaluate(evidence)["status"], "reviewing")
        self.assertEqual(file_category("package-lock.json")[0], "dependency-lock")
        self.assertEqual(file_category("src/test/resources/vector.json")[0], "structured-fixture-data")
        self.assertEqual(file_category("testdata/embedded-profile.icc")[0], "archive-or-binary")
        self.assertEqual(file_category("public/mc-icons/icon-exp.svg")[0], "image")
        self.assertEqual(file_category("app/interactive.svg")[0], "first-party-source")
        self.assertEqual(file_category("tools/testing/example_test.py")[0], "test-or-fixture")

    def test_read_after_end_of_file_is_rejected(self):
        with self.assertRaises(ValueError):
            self.evaluate([proof(read_ranges=[[1, 7]])])

    def test_archived_inventory_inspection_does_not_replace_source_review(self):
        declared = [proof(semantic_review="", inspection="Checked the producer, archive digest and structure.")]
        for path, expected_inspection in (
            ("docs/audit/20261002/LEGACY_REVIEW.json", True),
            ("docs/audit/full-project-audit/database/SCHEMA_CATALOG.md", True),
            ("docs/audit/full-project-audit/modules/API_ROUTE_INVENTORY.md", True),
            ("docs/audit/full-project-audit/modules/FILE_COVERAGE_INVENTORY.md", True),
            ("load-results/anti-abuse-comment-aggregated.json", True),
            ("test-results/load/redis-nats-db-observer.json", True),
            ("load-results/unclassified-run.json", False),
            ("test-results/load/shared-integration.json", False),
            ("docs/audit/20261002/ISSUES.md", False),
            ("docs/audit/20261002/findings.json", True),
            ("docs/audit/20261002/verification-results.json", False),
            ("docs/audit/20261003/acceptance-results.json", False),
            ("docs/audit/full-project-audit/API_ROUTE_INVENTORY.md", False),
            ("docs/audit/full-project-audit/modules/handler.go", False),
            ("internal/httpapi/server.go", False),
        ):
            with self.subTest(path=path):
                category, _ = file_category(path)
                verdict = self.evaluate(declared, inspection_allowed=category in INSPECTION_CATEGORIES)
                self.assertEqual(verdict["status"], "reviewed" if expected_inspection else "reviewing")
                self.assertFalse(verdict["reading"]["complete"])
                self.assertFalse(verdict["semantic"]["declared"])
                self.assertEqual(verdict["inspected"], expected_inspection)


if __name__ == "__main__":
    unittest.main()
