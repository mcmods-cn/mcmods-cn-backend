import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import isolated_environment as environment


class EnvironmentOwnershipTests(unittest.TestCase):
    def test_run_refuses_an_environment_pointing_outside_its_labelled_services(self):
        with tempfile.TemporaryDirectory() as directory:
            state = Path(directory)
            database = "test_mcmods_audit_0123456789abcdef"
            (state / "resources.json").write_text(json.dumps({
                "owner": "a" * 32, "database": database,
                "containers": {"postgres": "p", "redis": "r", "nats": "n"},
            }))
            url = "postgres://mcmods:synthetic@not-the-owned-db.example.test:15432/" + database + "?sslmode=disable"
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
                if args[0] == "run":
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
                    patch.object(environment, "ready_typesense") as ping, patch.object(environment.subprocess, "run"):
                environment.up(state, "go", real_services=True)
                values = json.loads((state / "environment.json").read_text())
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

    def test_owner_mismatch_prevents_any_removal(self):
        with tempfile.TemporaryDirectory() as directory:
            state = Path(directory)
            (state / "resources.json").write_text(json.dumps({"owner": "ours", "containers": {"postgres": "candidate"}}))
            with patch.object(environment, "docker", return_value=json.dumps([{"Config": {"Labels": {environment.LABEL: "theirs"}}}])) as docker:
                with self.assertRaises(RuntimeError):
                    environment.down(state)
                self.assertEqual([call.args[0] for call in docker.call_args_list], ["inspect"])
            self.assertTrue((state / "resources.json").exists())

    def test_written_environment_files_are_private(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "environment.json"
            environment.write_private(path, "synthetic fixture")
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)

    def test_down_preserves_additional_backups_after_owned_resource_removal(self):
        with tempfile.TemporaryDirectory() as directory:
            state = Path(directory)
            (state / "resources.json").write_text(json.dumps({"owner": "ours", "containers": {"postgres": "owned"}}))
            (state / "environment.json").write_text("{}")
            (state / "activate.sh").write_text("# synthetic fixture\n")
            backup = state / "function-repair-before.json"
            backup.write_text("preserved private repair backup")
            with patch.object(environment, "docker", return_value=json.dumps([{"Config": {"Labels": {environment.LABEL: "ours"}}}])) as docker:
                environment.down(state)
                self.assertEqual([call.args[0] for call in docker.call_args_list], ["inspect", "rm"])
                self.assertEqual(docker.call_args_list[-1].args, ("rm", "-f", "-v", "owned"))
            self.assertEqual(backup.read_text(), "preserved private repair backup")
            self.assertEqual(sorted(path.name for path in state.iterdir()), [backup.name])


if __name__ == "__main__":
    unittest.main()
