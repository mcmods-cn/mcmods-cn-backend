"""Fail-before-side-effect guards for the real API acceptance runner."""
import importlib.util
import json
import os
from pathlib import Path
import secrets
import selectors
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location("live_acceptance", Path(__file__).with_name("live_acceptance.py"))
live = importlib.util.module_from_spec(spec)
spec.loader.exec_module(live)


class AcceptanceGuardTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.owner = secrets.token_hex(16)
        self.parent = {"owner": self.owner, "database": "test_mcmods_audit_" + secrets.token_hex(8),
                       "containers": {"postgres": "owned-postgres", "redis": "owned-redis", "nats": "owned-nats"}}
        self.environment = {"DATABASE_URL": "postgres://mcmods@127.0.0.1:15432/" + self.parent["database"] + "?sslmode=disable",
                            "REDIS_ADDR": "127.0.0.1:16379", "NATS_URL": "nats://127.0.0.1:14222"}
        self.environment["MCMODS_TEST_DATABASE_URL"] = self.environment["DATABASE_URL"]
        live.STATE = self.root / "state"
        live.STATE.mkdir()
        live.SCOPE = self.root / "new-scope"
        live.BASE = live.SCOPE
        live.FE = self.root / "frontend-source"
        live.COPY = self.root / "frontend-copy"
        live.ADOPT_PREPARED_COPY = True
        for p in (live.FE, live.COPY):
            p.mkdir()
            (p / "package-lock.json").write_text("{}")
        (live.COPY / "node_modules").mkdir()
        (live.STATE / "resources.json").write_text(json.dumps(self.parent))
        self.write_environment()

    def write_environment(self):
        (live.STATE / "environment.json").write_text(json.dumps(self.environment))

    def test_non_loopback_parent_fails_before_any_sql(self):
        self.environment["DATABASE_URL"] = self.environment["DATABASE_URL"].replace("127.0.0.1", "public.invalid")
        self.environment["MCMODS_TEST_DATABASE_URL"] = self.environment["DATABASE_URL"]
        self.write_environment()
        info = {"NetworkSettings": {"Ports": {"5432/tcp": [{"HostIp": "127.0.0.1", "HostPort": "15432"}]}}}
        with mock.patch.object(live.owned, "verify_container", return_value=info), mock.patch.object(live, "sql") as sql:
            with self.assertRaisesRegex(RuntimeError, "identity mismatch"):
                live.guarded_parent()
            sql.assert_not_called()

    def test_unlabelled_container_fails_before_any_sql(self):
        with mock.patch.object(live.owned, "verify_container", side_effect=RuntimeError("owner mismatch")), mock.patch.object(live, "sql") as sql:
            with self.assertRaisesRegex(RuntimeError, "owner mismatch"):
                live.guarded_parent()
            sql.assert_not_called()

    def test_dsn_query_cannot_override_the_checked_host(self):
        self.environment["DATABASE_URL"] += "&host=public.invalid"
        self.environment["MCMODS_TEST_DATABASE_URL"] = self.environment["DATABASE_URL"]
        self.write_environment()
        info = {"NetworkSettings": {"Ports": {"5432/tcp": [{"HostIp": "127.0.0.1", "HostPort": "15432"}]}}}
        with mock.patch.object(live.owned, "verify_container", return_value=info), mock.patch.object(live, "sql") as sql:
            with self.assertRaisesRegex(RuntimeError, "identity mismatch"):
                live.guarded_parent()
            sql.assert_not_called()

    def test_redis_cannot_target_another_service_even_with_owned_pg(self):
        self.environment["REDIS_ADDR"] = "public.invalid:16379"
        self.write_environment()
        info = {"NetworkSettings": {"Ports": {"5432/tcp": [{"HostIp": "127.0.0.1", "HostPort": "15432"}],
                                               "6379/tcp": [{"HostIp": "127.0.0.1", "HostPort": "16379"}],
                                               "4222/tcp": [{"HostIp": "127.0.0.1", "HostPort": "14222"}]}}}
        with mock.patch.object(live.owned, "verify_container", return_value=info), mock.patch.object(live, "sql") as sql:
            with self.assertRaisesRegex(RuntimeError, "service endpoint identity mismatch"):
                live.guarded_parent()
            sql.assert_not_called()

    def test_same_source_copy_is_refused_without_writing_owner_marker(self):
        live.COPY = live.FE
        with self.assertRaisesRegex(RuntimeError, "independent disposable"):
            live.copy_guard(self.parent)
        self.assertFalse((live.FE / ".mcmods-live-copy-owner.json").exists())

    def test_dotenv_copy_is_refused_even_when_explicitly_adopting(self):
        (live.COPY / ".env.local").write_text("SYNTHETIC_ONLY=true\n")
        with self.assertRaisesRegex(RuntimeError, "dotenv"):
            live.copy_guard(self.parent)
        self.assertFalse((live.COPY / ".mcmods-live-copy-owner.json").exists())

    def test_foreign_copy_owner_is_never_replaced(self):
        marker = live.COPY / ".mcmods-live-copy-owner.json"
        value = {"parent_owner": secrets.token_hex(16), "frontend_source": str(live.FE)}
        marker.write_text(json.dumps(value))
        with self.assertRaisesRegex(RuntimeError, "another owner"):
            live.copy_guard(self.parent)
        self.assertEqual(json.loads(marker.read_text()), value)

    def test_frontend_components_are_copied_and_fingerprinted_from_a_fresh_source(self):
        live.SCOPE.mkdir()
        component = live.FE / "components" / "skin" / "Canvas.tsx"
        component.parent.mkdir(parents=True)
        component.write_text("export const Canvas = () => null;\n")
        manifest = live.frontend_sync()
        relative = "components/skin/Canvas.tsx"
        self.assertIn(relative, manifest)
        self.assertEqual((live.COPY / relative).read_bytes(), component.read_bytes())
        self.assertEqual(manifest[relative], live.fingerprint(component))

    def test_frontend_components_changes_update_both_copy_and_manifest(self):
        live.SCOPE.mkdir()
        component = live.FE / "components" / "Canvas.tsx"
        component.parent.mkdir()
        component.write_text("export const revision = 1;\n")
        first = live.frontend_sync()
        component.write_text("export const revision = 2;\n")
        second = live.frontend_sync()
        relative = "components/Canvas.tsx"
        self.assertIn(relative, first)
        self.assertNotEqual(first[relative], second[relative])
        self.assertEqual((live.COPY / relative).read_bytes(), component.read_bytes())
        self.assertEqual(second[relative], live.fingerprint(component))

    def test_frontend_components_stale_files_are_refused_without_deleting_them(self):
        live.SCOPE.mkdir()
        stale = live.COPY / "components" / "Unknown.tsx"
        stale.parent.mkdir()
        stale.write_text("synthetic unknown file")
        with self.assertRaisesRegex(RuntimeError, "unexpected stale source"):
            live.frontend_sync()
        self.assertEqual(stale.read_text(), "synthetic unknown file")

    def test_frontend_components_symlink_is_refused_before_copying_inputs(self):
        live.SCOPE.mkdir()
        foreign = self.root / "foreign"
        foreign.mkdir()
        (live.COPY / "components").symlink_to(foreign, target_is_directory=True)
        with self.assertRaisesRegex(RuntimeError, "symlinked frontend-copy"):
            live.frontend_sync()
        self.assertFalse((live.SCOPE / "frontend-source.json").exists())

    def test_frontend_proxy_is_copied_and_fingerprinted(self):
        live.SCOPE.mkdir()
        proxy = live.FE / "proxy.ts"
        proxy.write_text("export const proxy = () => 'synthetic CSP boundary';\n")
        manifest = live.frontend_sync()
        self.assertIn("proxy.ts", manifest)
        self.assertEqual((live.COPY / "proxy.ts").read_bytes(), proxy.read_bytes())
        self.assertEqual(manifest["proxy.ts"], live.fingerprint(proxy))

    def test_frontend_root_config_symlink_cannot_overwrite_another_file(self):
        live.SCOPE.mkdir()
        (live.FE / "next.config.ts").write_text("export default {};\n")
        foreign = self.root / "foreign-config"
        foreign.write_text("synthetic file must stay intact")
        (live.COPY / "next.config.ts").symlink_to(foreign)
        with self.assertRaisesRegex(RuntimeError, "symlinked frontend-copy"):
            live.frontend_sync()
        self.assertEqual(foreign.read_text(), "synthetic file must stay intact")

    def test_frontend_removed_proxy_is_refused_without_deleting_stale_copy(self):
        live.SCOPE.mkdir()
        proxy = live.COPY / "proxy.ts"
        proxy.write_text("synthetic old proxy")
        with self.assertRaisesRegex(RuntimeError, "unexpected stale source"):
            live.frontend_sync()
        self.assertEqual(proxy.read_text(), "synthetic old proxy")

    def test_live_runtime_cannot_be_cleaned_by_a_false_stopped_record(self):
        live.SCOPE.mkdir()
        record = {"parent_owner": self.owner, "parent_container": self.parent["containers"]["postgres"],
                  "database": "test_mcmods_live_" + secrets.token_hex(10), "created": True, "runtime_started": True}
        (live.SCOPE / "scope.json").write_text(json.dumps(record))
        (live.SCOPE / "backend-a-live-current-result.json").write_text(json.dumps({"own_process_groups_stopped": False}))
        with mock.patch.object(live, "guarded_parent", return_value=(self.parent, self.environment)), mock.patch.object(live, "sql") as sql, mock.patch.object(live.subprocess, "run") as run:
            with self.assertRaisesRegex(RuntimeError, "runtime is not confirmed stopped"):
                live.cleanup()
            sql.assert_not_called()
            run.assert_not_called()

    def scope_environment(self):
        live.SCOPE.mkdir()
        record = {"parent_owner": self.owner, "parent_container": "owned-postgres",
                  "database": "test_mcmods_live_" + secrets.token_hex(10), "created": True,
                  "runtime_started": False, "nonce_namespace": secrets.token_hex(16)}
        nonce = record["nonce_namespace"]
        url = self.environment["DATABASE_URL"].replace(self.parent["database"], record["database"])
        environment = dict(self.environment, DATABASE_URL=url, MCMODS_TEST_DATABASE_URL=url,
                           DB_NAME=record["database"], DB_RESET_ON_START="false", DB_RESET_CONFIRM="",
                           APP_ENV="test", MCMODS_SKIP_DOTENV="true", SMTP_ENABLED="false", TYPESENSE_ENABLED="false",
                           REDIS_ENABLED="true", REDIS_NAMESPACE="live_"+nonce,
                           NATS_ENABLED="true", NATS_JETSTREAM_ENABLED="true",
                           NATS_SUBJECT_PREFIX="live."+nonce, NATS_JETSTREAM_STREAM="LIVE_"+nonce.upper(),
                           SEED_ADMIN_PASSWORD="synthetic-owned-seed")
        for key in ("MODRINTH_TOKEN", "CURSEFORGE_API_KEY", "GITHUB_TOKEN", "SMTP_PASSWORD", "SMTP_USERNAME",
                    "SMTP_HOST", "REDIS_USERNAME", "REDIS_PASSWORD", "NATS_TOKEN", "NATS_USERNAME", "NATS_PASSWORD",
                    "TYPESENSE_API_KEY", "YGGDRASIL_PRIVATE_KEY_BASE64", "TURNSTILE_SECRET_KEY", "TURNSTILE_SITE_KEY"):
            environment[key] = ""
        self.save_scope(record, environment)
        (live.SCOPE/"backend-current").write_bytes(b"synthetic-not-executable")
        (live.SCOPE/"backend-source.json").write_text("{}")
        (live.SCOPE/"frontend-source.json").write_text("{}")
        return record, environment

    def save_scope(self, record, environment):
        live.private_json(live.SCOPE/"environment.json", environment)
        record["environment_sha256"] = live.fingerprint(live.SCOPE/"environment.json")
        live.private_json(live.SCOPE/"scope.json", record)

    def test_parent_database_and_foreign_namespace_fail_before_start_even_with_matching_hash(self):
        record, original = self.scope_environment()
        cases = [("DATABASE_URL", self.environment["DATABASE_URL"]),
                 ("MCMODS_TEST_DATABASE_URL", self.environment["DATABASE_URL"]),
                 ("REDIS_NAMESPACE", "live_"+secrets.token_hex(16)),
                 ("NATS_SUBJECT_PREFIX", "live."+secrets.token_hex(16)),
                 ("SMTP_PASSWORD", "synthetic-but-forbidden")]
        for key, value in cases:
            with self.subTest(key=key):
                environment = dict(original, **{key: value})
                self.save_scope(record, environment)
                with mock.patch.object(live, "guarded_parent", return_value=(self.parent, self.environment)), mock.patch.object(live.subprocess, "Popen") as start, mock.patch.object(live, "build_current") as build:
                    with self.assertRaisesRegex(RuntimeError, "exact child|external credentials"):
                        live.run()
                    start.assert_not_called()
                    build.assert_not_called()

    def test_old_successful_receipt_is_revoked_before_first_new_process(self):
        record, _ = self.scope_environment()
        result = live.SCOPE/"backend-a-live-current-result.json"
        live.private_json(result, {"own_process_groups_stopped": True, "run_id": "old"})
        observed = []
        def intercept_start(*args, **kwargs):
            receipt = json.loads(result.read_text())
            scope = json.loads((live.SCOPE/"scope.json").read_text())
            observed.append((receipt["own_process_groups_stopped"], receipt["run_id"] == scope["runtime_run_id"]))
            raise RuntimeError("controlled startup failure before any process")
        with mock.patch.object(live, "guarded_parent", return_value=(self.parent, self.environment)), mock.patch.object(live, "source_files", return_value={}), mock.patch.object(live, "frontend_sync", return_value={}), mock.patch.object(live.subprocess, "Popen", side_effect=intercept_start):
            self.assertEqual(live.run(), 1)
        self.assertEqual(observed, [(False, True)])

    def test_previous_success_for_a_different_run_cannot_authorize_cleanup_or_reuse(self):
        record, environment = self.scope_environment()
        record.update(runtime_started=True, runtime_run_id=secrets.token_hex(16))
        self.save_scope(record, environment)
        live.private_json(live.SCOPE/"backend-a-live-current-result.json",
                          {"own_process_groups_stopped": True, "run_id": secrets.token_hex(16)})
        with mock.patch.object(live, "guarded_parent", return_value=(self.parent, self.environment)), mock.patch.object(live, "sql") as sql, mock.patch.object(live.subprocess, "run") as command, mock.patch.object(live.subprocess, "Popen") as start:
            with self.assertRaisesRegex(RuntimeError, "runtime is not confirmed stopped"):
                live.cleanup()
            with self.assertRaisesRegex(RuntimeError, "previous runtime is not confirmed stopped"):
                live.run()
            sql.assert_not_called()
            command.assert_not_called()
            start.assert_not_called()

    def test_reaped_session_leader_does_not_hide_its_live_child(self):
        nonce = secrets.token_hex(16)
        child_pid = self.root/"child.pid"
        code = "import subprocess,sys;from pathlib import Path;p=subprocess.Popen([sys.executable,'-c','import time;time.sleep(60)'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL);Path(sys.argv[1]).write_text(str(p.pid))"
        leader = subprocess.Popen([sys.executable, "-c", code, str(child_pid)],
                                  env=os.environ | {"MCMODS_LIVE_PROCESS_OWNER": nonce},
                                  start_new_session=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            self.assertEqual(leader.wait(timeout=5), 0)
            pid = int(child_pid.read_text())
            self.assertNotEqual((Path('/proc')/str(pid)/'stat').read_text().rsplit(')',1)[1].split()[0], 'Z')
            self.assertTrue(live.stop_owned_groups([leader], nonce, grace_seconds=1, force_seconds=1))
            path = Path('/proc')/str(pid)/'stat'
            self.assertTrue(not path.exists() or path.read_text().rsplit(')',1)[1].split()[0] == 'Z')
        finally:
            live.stop_owned_groups([leader], nonce, grace_seconds=1, force_seconds=1)

    def test_real_exit_between_pidfd_pin_and_environ_read_is_safe(self):
        owner = secrets.token_hex(16)
        process = subprocess.Popen([sys.executable, "-c", "import time;time.sleep(60)"],
                                   env=os.environ | {"MCMODS_LIVE_PROCESS_OWNER": owner}, start_new_session=True)
        original_open = os.pidfd_open
        pinned = []
        def exit_after_pin(pid, *args):
            handle = original_open(pid, *args)
            if pid == process.pid:
                pinned.append(handle)
                process.terminate()
                # Keep the actual child unreaped: Linux denies environ reads
                # after it becomes a zombie, despite the earlier live stat.
                with selectors.DefaultSelector() as selector:
                    selector.register(handle, selectors.EVENT_READ)
                    self.assertTrue(selector.select(5), "actual pidfd must confirm child exit")
            return handle
        try:
            with mock.patch.object(live.os, "pidfd_open", side_effect=exit_after_pin):
                self.assertEqual(live.owned_group_handles([process], owner), [])
            self.assertEqual(len(pinned), 1)
            with self.assertRaises(OSError):
                os.fstat(pinned[0])
        finally:
            if process.poll() is None:
                process.kill()
            process.wait(timeout=5)

    def test_live_kernel_permission_denial_remains_fail_closed(self):
        owner = secrets.token_hex(16)
        code = "import ctypes,time;assert ctypes.CDLL(None).prctl(4,0,0,0,0)==0;print('ready',flush=True);time.sleep(60)"
        process = subprocess.Popen([sys.executable, "-c", code], stdout=subprocess.PIPE, text=True,
                                   env=os.environ | {"MCMODS_LIVE_PROCESS_OWNER": owner}, start_new_session=True)
        handle = os.pidfd_open(process.pid)
        try:
            with selectors.DefaultSelector() as selector:
                selector.register(process.stdout, selectors.EVENT_READ)
                self.assertTrue(selector.select(5))
            self.assertEqual(process.stdout.readline().strip(), "ready")
            with self.assertRaises(PermissionError):
                live.owned_group_handles([process], owner)
            with selectors.DefaultSelector() as selector:
                selector.register(handle, selectors.EVENT_READ)
                self.assertFalse(selector.select(0), "a live unreadable process must remain alive, not be ignored/signalled")
        finally:
            os.close(handle)
            process.terminate()
            process.wait(timeout=5)
            process.stdout.close()

    def test_live_process_with_another_nonce_is_never_authorized(self):
        process = subprocess.Popen([sys.executable, "-c", "import time;time.sleep(60)"],
                                   env=os.environ | {"MCMODS_LIVE_PROCESS_OWNER": secrets.token_hex(16)}, start_new_session=True)
        try:
            with self.assertRaisesRegex(RuntimeError, "lacks this live run owner"):
                live.owned_group_handles([process], secrets.token_hex(16))
            self.assertIsNone(process.poll())
        finally:
            process.terminate()
            process.wait(timeout=5)


if __name__ == "__main__":
    unittest.main()
