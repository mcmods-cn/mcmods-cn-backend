import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import isolated_environment as environment


class EnvironmentOwnershipTests(unittest.TestCase):
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
