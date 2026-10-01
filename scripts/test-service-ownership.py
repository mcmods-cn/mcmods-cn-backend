#!/usr/bin/env python3
"""Exercise the real ownership checker with owned children on random ports."""
import importlib.util
import json
import os
import shlex
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path


CHECKER = Path(__file__).with_name("service-process-owner.py")
SPEC = importlib.util.spec_from_file_location("service_process_owner", CHECKER)
OWNER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(OWNER)


@unittest.skipUnless(sys.platform.startswith("linux"), "Linux /proc socket ownership is required")
class ServiceOwnershipTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="mcmods-test-services.")
        self.addCleanup(self.temporary.cleanup)
        self.state = Path(self.temporary.name)
        (self.state / "identity.json").write_text(json.dumps({"directory": str(self.state), "uid": os.getuid(), "database": "mcmods_audit"}))
        (self.state / "owned-directory").write_text(str(self.state))
        self.config = self.state / "nats.conf"
        self.config.write_text("synthetic owned config")

    def child(self, config_argument=None):
        # The process owns only a random loopback listening socket; the tests
        # exercise real PID/fd/config evidence without probing fixed services.
        code = "import socket,time;s=socket.socket();s.bind(('127.0.0.1',0));s.listen();print(s.getsockname()[1],flush=True);time.sleep(60)"
        child = subprocess.Popen([sys.executable, "-u", "-c", code, "-c", str(config_argument or self.config)], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)

        def cleanup():
            # Only terminate the Popen object created by this test, never a
            # target derived from a file, a fixed service port or shared state.
            child.terminate()
            try:
                child.wait(timeout=3)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait(timeout=3)
            child.stdout.close()
            child.stderr.close()

        self.addCleanup(cleanup)
        port = int(child.stdout.readline().strip())
        (self.state / "nats.pid").write_text(str(child.pid))
        (self.state / "nats-started.pid").write_text(str(child.pid))
        return child, port

    def cli(self, action="verify-state", optimized=False):
        return subprocess.run([sys.executable] + (["-O"] if optimized else []) + [str(CHECKER), action, str(self.state)], capture_output=True, text=True, timeout=5)

    def test_exact_loopback_socket_belongs_to_new_child(self):
        child, port = self.child()
        self.assertTrue(OWNER.owns_listener(child.pid, port))
        self.assertFalse(OWNER.owns_listener(os.getpid(), port))
        self.assertEqual(OWNER.verify_process(self.state, "nats", require_launch=True, port=port), child.pid)

    def test_old_task_state_can_be_verified_for_stop_but_not_new_readiness(self):
        child, port = self.child()
        (self.state / "nats-started.pid").unlink()
        self.assertEqual(OWNER.verify_process(self.state, "nats", port=port), child.pid)
        with self.assertRaises(OWNER.OwnershipError):
            OWNER.verify_process(self.state, "nats", require_launch=True, port=port)

    def test_unrelated_listener_cannot_replace_new_launch_pid(self):
        child, port = self.child()
        (self.state / "nats-started.pid").write_text(str(os.getpid()))
        with self.assertRaises(OWNER.OwnershipError):
            OWNER.verify_process(self.state, "nats", require_launch=True, port=port)
        self.assertIsNone(child.poll())

    def test_config_argument_must_match_exactly_not_contain_path(self):
        child, port = self.child(str(self.config) + ".unowned")
        with self.assertRaises(OWNER.OwnershipError):
            OWNER.verify_process(self.state, "nats", require_launch=True, port=port)
        self.assertIsNone(child.poll())

    def test_socket_on_another_port_is_not_owned_target(self):
        child, port = self.child()
        # Port zero cannot be a LISTEN endpoint; no external/fixed-port connect.
        with self.assertRaises(OWNER.OwnershipError):
            OWNER.verify_process(self.state, "nats", require_launch=True, port=0)
        self.assertTrue(OWNER.owns_listener(child.pid, port))

    def test_missing_new_child_process_is_rejected(self):
        child, port = self.child()
        child.terminate()
        child.wait(timeout=3)
        with self.assertRaises(OSError):
            OWNER.verify_process(self.state, "nats", require_launch=True, port=port)

    def test_invalid_pid_cannot_signal_process_group(self):
        (self.state / "nats.pid").write_text("0")
        result = self.cli("stop")
        self.assertNotEqual(result.returncode, 0)

    def test_owned_zombie_is_already_stopped_without_listener_or_signal(self):
        child, _ = self.child()
        child.terminate()
        deadline = time.monotonic() + 3
        stat = Path(f"/proc/{child.pid}/stat")
        while stat.read_text().rsplit(")", 1)[1].split()[0] != "Z":
            self.assertLess(time.monotonic(), deadline, "owned child did not exit")
            time.sleep(.01)
        # Leave the child unreaped while the real CLI examines its UID/state.
        # No socket is present and no signal is needed for this exited PID.
        result = self.cli("stop")
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_marker_rejection_survives_python_optimization(self):
        (self.state / "owned-directory").write_text("mismatched private synthetic marker")
        for optimized in (False, True):
            with self.subTest(optimized=optimized):
                result = self.cli(optimized=optimized)
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn("private synthetic marker", result.stderr)

    def test_invalid_marker_cannot_stop_child_even_in_optimized_mode(self):
        child, _ = self.child()
        (self.state / "owned-directory").write_text("not owned")
        result = self.cli("stop", optimized=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIsNone(child.poll())

    def test_state_identity_cannot_claim_another_directory(self):
        identity = json.loads((self.state / "identity.json").read_text())
        identity["directory"] = str(self.state) + ".other"
        (self.state / "identity.json").write_text(json.dumps(identity))
        self.assertNotEqual(self.cli().returncode, 0)

    def test_symlink_state_and_symlink_config_are_rejected(self):
        link = self.state / "aliased-state"
        link.symlink_to(self.state, target_is_directory=True)
        with self.assertRaises(OWNER.OwnershipError):
            OWNER.owned_state(str(link))
        child, port = self.child()
        other = self.state / "other.conf"
        other.write_text("synthetic")
        self.config.unlink()
        self.config.symlink_to(other)
        with self.assertRaises(OWNER.OwnershipError):
            OWNER.verify_process(self.state, "nats", port=port)
        self.assertIsNone(child.poll())

    def test_valid_identity_cli_accepts_both_normal_and_optimized_mode(self):
        self.assertEqual(self.cli().returncode, 0)
        self.assertEqual(self.cli(optimized=True).returncode, 0)

    def test_optional_search_environment_clears_inherited_targets(self):
        script = CHECKER.with_name("test-services.sh").read_text()
        generator = script.split("python3 - <<'PY'\n", 1)[1].split("\nPY\n", 1)[0]
        env = {**os.environ, "MCMODS_TEST_STATE": str(self.state), "PG_BIN": "/synthetic/pg/bin", "MCMODS_RUN_TYPESENSE_INTEGRATION": "1", "MCMODS_TEST_TYPESENSE_URL": "http://unverified.invalid:8108", "MCMODS_TEST_TYPESENSE_KEY": "unrelated-private-key"}
        env.pop("TYPESENSE_SERVER", None)
        # Execute only the actual fixture generator, never service startup.
        result = subprocess.run([sys.executable, "-c", generator], env=env, capture_output=True, text=True, timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)
        values = {}
        for line in (self.state / "env.sh").read_text().splitlines():
            assignment = shlex.split(line)[1]
            key, value = assignment.split("=", 1)
            values[key] = value
        for key in ("MCMODS_RUN_TYPESENSE_INTEGRATION", "MCMODS_TEST_TYPESENSE_URL", "MCMODS_TEST_TYPESENSE_KEY"):
            self.assertTrue(key in values, "missing explicit optional search scope variable")
            self.assertEqual(values[key], "")


if __name__ == "__main__":
    unittest.main()
