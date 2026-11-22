import unittest

from review_snapshot import evaluate, file_category


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
        self.assertEqual(file_category("public/mc-icons/icon-exp.svg")[0], "image")
        self.assertEqual(file_category("app/interactive.svg")[0], "first-party-source")
        self.assertEqual(file_category("tools/testing/example_test.py")[0], "test-or-fixture")

    def test_read_after_end_of_file_is_rejected(self):
        with self.assertRaises(ValueError):
            self.evaluate([proof(read_ranges=[[1, 7]])])


if __name__ == "__main__":
    unittest.main()
