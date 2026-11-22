import io
import json
import unittest

from real_service_cases import verify


class RealServiceReceiptTests(unittest.TestCase):
    def verify_events(self, actions):
        events = [dict(Package="pkg", Action=action, Test=name, Output="private fixture data")
                  for action, name in actions]
        receipt = io.StringIO()
        issues = verify(events, {("pkg", "TestOwned")}, receipt)
        self.assertNotIn("private fixture data", receipt.getvalue())
        return issues, [json.loads(line) for line in receipt.getvalue().splitlines()]

    def test_package_case_and_subtest_pass(self):
        issues, receipt = self.verify_events([("run", "TestOwned"), ("run", "TestOwned/native"),
                                             ("pass", "TestOwned/native"), ("pass", "TestOwned"), ("pass", None)])
        self.assertEqual(issues, [])
        self.assertEqual([event["test"] for event in receipt], ["TestOwned/native", "TestOwned"])

    def test_unmatched_or_interrupted_test_fails(self):
        for events in ([('pass', None)], [('run', 'TestOwned'), ('pass', None)],
                       [('pass', 'TestOwned'), ('pass', None)], [('run', 'TestOwned'), ('pass', 'TestOwned')]):
            with self.subTest(events=events):
                self.assertTrue(self.verify_events(events)[0])

    def test_subtest_skip_blocks_a_green_package(self):
        issues, _ = self.verify_events([("run", "TestOwned"), ("run", "TestOwned/native"),
                                       ("skip", "TestOwned/native"), ("pass", "TestOwned"), ("pass", None)])
        self.assertEqual(len(issues), 1)
        self.assertIn("TestOwned/native: skip", issues[0])

    def test_failed_assertion_only_retains_locations_and_allowed_status_codes(self):
        events = [
            dict(Package="pkg", Action="run", Test="TestOwned"),
            dict(Package="pkg", Action="output", Test="TestOwned",
                 Output="    realtime_test.go:41: /ready=503 want200: private fixture data\n"),
            dict(Package="pkg", Action="output", Test="TestOwned",
                 Output="    realtime_test.go:42: private output: private fixture data\n"),
            dict(Package="pkg", Action="fail", Test="TestOwned", Elapsed=0.2),
            dict(Package="pkg", Action="fail"),
        ]
        receipt = io.StringIO()
        self.assertTrue(verify(events, {("pkg", "TestOwned")}, receipt))
        self.assertNotIn("private fixture data", receipt.getvalue())
        self.assertEqual(json.loads(receipt.getvalue())["test_locations"], [
            {"source": "realtime_test.go:41", "path": "/ready", "observed": 503, "expected": 200},
            {"source": "realtime_test.go:42"},
        ])
