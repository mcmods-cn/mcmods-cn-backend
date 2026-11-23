import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import isolated_environment as environment

OWNER = "a" * 32
DATABASE = "test_mcmods_audit_0123456789abcdef"


class EnvironmentOwnershipTests(unittest.TestCase):
    def test_run_refuses_an_environment_pointing_outside_its_labelled_services(self):
        with tempfile.TemporaryDirectory() as directory:
            state = Path(directory)
            (state / "resources.json").write_text(json.dumps({
                "owner": OWNER, "database": DATABASE,
                "containers": {"postgres": "p", "redis": "r", "nats": "n"},
            }))
            url = "postgres://mcmods:synthetic@not-the-owned-db.example.test:15432/" + DATABASE + "?sslmode=disable"
            (state / "environment.json").write_text(json.dumps({
                "DATABASE_URL": url, "MCMODS_TEST_DATABASE_URL": url,
                "REDIS_ADDR": "127.0.0.1:16379", "NATS_URL": "nats://127.0.0.1:14222",
            }))
            def owned_container(name, owner):
                port = {"p": 5432, "r": 6379, "n": 4222}[name]
                binding = {"HostIp": "127.0.0.1", "HostPort": str(port + 10000)}
                return {"NetworkSettings": {"Ports": {f"{port}/tcp": [binding]}}}

            with patch.object(environment, "verify_container", side_effect=owned_container), \
                    patch.object(environment.subprocess, "run") as command:
                with self.assertRaisesRegex(RuntimeError, "owned PostgreSQL"):
                    environment.run(state, ["synthetic-integration-command"])
                command.assert_not_called()

    def test_real_service_profile_uses_owned_containers_for_endpoints_and_native_binaries(self):
        with tempfile.TemporaryDirectory() as directory:
            state = Path(directory) / "services"
            owner_labels = {}
            ports = {"postgres": 5432, "redis": 6379, "nats": 4222, "typesense": 8108}
            copied = []

            def fake_docker(*args, **options):
                if args[0] == "create":
                    service = args[args.index("--name") + 1].split("-")[2]
                    owner_labels[service] = args[args.index("--label") + 1].split("=", 1)[1]
                    if service == "typesense":
                        self.assertIn(environment.TYPESENSE_IMAGE, args)
                        self.assertNotIn(options["environment_overlay"]["TYPESENSE_API_KEY"], args)
                    return service
                if args[0] == "inspect":
                    service = args[1]
                    binding = {"HostIp": "127.0.0.1", "HostPort": str(10000 + ports[service])}
                    return json.dumps([{"Config": {"Labels": {environment.LABEL: owner_labels[service]}},
                                        "NetworkSettings": {"Ports": {f"{ports[service]}/tcp": [binding]}}}])
                if args[0] == "cp":
                    copied.append(args[1])
                    Path(args[2]).write_text("synthetic executable")

            with patch.object(environment, "docker", side_effect=fake_docker), \
                    patch.object(environment, "ready_typesense") as ping, patch.object(environment.subprocess, "run") as command:
                environment.up(state, "go", real_services=True)
                values = json.loads((state / "environment.json").read_text())
                self.assertEqual(command.call_args.args[0], ["go", "run", "./cmd/db-initialize", "--bootstrap-admin"])
                self.assertEqual(command.call_args.kwargs["env"]["DATABASE_URL"], values["DATABASE_URL"])
                ping.assert_called_once_with(values["MCMODS_TEST_TYPESENSE_URL"])
                self.assertEqual(set(copied), {"redis:/usr/local/bin/redis-server", "typesense:/opt/typesense-server"})
                self.assertEqual(values["MCMODS_TEST_NATS_URL"], values["NATS_URL"])
                for service in ("REDIS", "NATS", "TYPESENSE"):
                    self.assertEqual(values[f"MCMODS_RUN_{service}_INTEGRATION"], "1")
                for service in ("REDIS", "TYPESENSE"):
                    executable = Path(values[f"MCMODS_TEST_{service}_BINARY"])
                    self.assertEqual(executable.parent, state)
                    self.assertEqual(executable.stat().st_mode & 0o777, 0o700)
                self.assertEqual(values["TYPESENSE_API_KEY"], "")
                self.assertTrue(values["MCMODS_TEST_TYPESENSE_API_KEY"])
                (state / "keep.txt").write_text("operator backup")
                environment.down(state)
                self.assertEqual([p.name for p in state.iterdir()], ["keep.txt"])

    def test_refuses_existing_directory_before_touching_docker(self):
        with tempfile.TemporaryDirectory() as directory, patch.object(environment, "docker") as docker:
            with self.assertRaises(RuntimeError):
                environment.up(Path(directory), "go")
            docker.assert_not_called()

    def test_failed_port_binding_keeps_the_created_container_owned_and_recoverable(self):
        with tempfile.TemporaryDirectory() as directory:
            state = Path(directory) / "failed-environment"
            owner = None

            def fake_docker(*args, **options):
                nonlocal owner
                if args[0] == "create":
                    owner = args[args.index("--label") + 1].split("=", 1)[1]
                    return "created-postgres"
                if args[0] == "start":
                    raise subprocess.CalledProcessError(125, "docker start")
                if args[0] == "inspect":
                    return json.dumps([{"Config": {"Labels": {environment.LABEL: owner}}}])

            with patch.object(environment, "docker", side_effect=fake_docker) as docker:
                with self.assertRaises(subprocess.CalledProcessError):
                    environment.up(state, "go")
                record = json.loads((state / "resources.json").read_text())
                self.assertEqual(record["containers"], {"postgres": "created-postgres"})
                self.assertFalse((state / "environment.json").exists())
                environment.down(state)
                self.assertEqual(docker.call_args.args, ("rm", "-f", "-v", "created-postgres"))
                self.assertFalse(state.exists())

    def test_owner_mismatch_prevents_any_removal(self):
        with tempfile.TemporaryDirectory() as directory:
            state = Path(directory)
            (state / "resources.json").write_text(json.dumps({
                "owner": OWNER, "database": DATABASE, "containers": {"postgres": "candidate"},
            }))
            with patch.object(environment, "docker", return_value=json.dumps([{"Config": {"Labels": {environment.LABEL: "theirs"}}}])) as docker:
                with self.assertRaises(RuntimeError):
                    environment.down(state)
                self.assertEqual([call.args[0] for call in docker.call_args_list], ["inspect"])
            self.assertTrue((state / "resources.json").exists())

    def test_down_refuses_an_invalid_owner_before_inspection_or_removal(self):
        for owner in (None, "", "not-an-owner-label"):
            with self.subTest(owner=owner), tempfile.TemporaryDirectory() as directory:
                state = Path(directory)
                recorded = state / "resources.json"
                recorded.write_text(json.dumps({
                    "owner": owner, "database": DATABASE,
                    "containers": {"postgres": "not-owned"},
                }))
                unlabelled = json.dumps([{"Config": {"Labels": {"other.service": "unrelated"}}}])
                with patch.object(environment, "docker", return_value=unlabelled) as docker:
                    with self.assertRaisesRegex(RuntimeError, "owned isolated environment record"):
                        environment.down(state)
                    docker.assert_not_called()
                self.assertTrue(recorded.is_file())

    def test_written_environment_files_are_private(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "environment.json"
            environment.write_private(path, "synthetic fixture")
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)

    def test_down_preserves_additional_backups_after_owned_resource_removal(self):
        with tempfile.TemporaryDirectory() as directory:
            state = Path(directory)
            (state / "resources.json").write_text(json.dumps({
                "owner": OWNER, "database": DATABASE, "containers": {"postgres": "owned"},
            }))
            (state / "environment.json").write_text("{}")
            (state / "activate.sh").write_text("# synthetic fixture\n")
            backup = state / "function-repair-before.json"
            backup.write_text("preserved private repair backup")
            with patch.object(environment, "docker", return_value=json.dumps([{"Config": {"Labels": {environment.LABEL: OWNER}}}])) as docker:
                environment.down(state)
                self.assertEqual([call.args[0] for call in docker.call_args_list], ["inspect", "rm"])
                self.assertEqual(docker.call_args_list[-1].args, ("rm", "-f", "-v", "owned"))
            self.assertEqual(backup.read_text(), "preserved private repair backup")
            self.assertEqual(sorted(path.name for path in state.iterdir()), [backup.name])

    def test_rebuild_provisions_an_independent_scope_after_checking_the_database_owner(self):
        owner_db = "test_mcmods_audit_0123456789abcdef"
        record = {"database": owner_db, "containers": {"postgres": "verified-pg", "typesense": "verified-search"}}
        state, replacement = Path("existing"), Path("replacement")
        with patch.object(environment, "read_owned_state", return_value=(record, {})) as verified, \
                patch.object(environment, "docker", return_value=f"{owner_db}|another_role\n") as docker, \
                patch.object(environment, "up") as up, patch.object(environment, "down") as down:
            with self.assertRaisesRegex(RuntimeError, "database name or owner"):
                environment.rebuild(state, replacement, "go")
            up.assert_not_called()
            docker.return_value = f"{owner_db}|mcmods\n"
            environment.rebuild(state, replacement, "go")
            self.assertEqual(verified.call_args.args, (state,))
            self.assertEqual(docker.call_args.args[:7], ("exec", "verified-pg", "psql", "-XAt", "-U", "mcmods", "-d"))
            up.assert_called_once_with(replacement, "go", real_services=True)
            down.assert_not_called()


if __name__ == "__main__":
    unittest.main()
