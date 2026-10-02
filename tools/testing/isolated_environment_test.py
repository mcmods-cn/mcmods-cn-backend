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


if __name__ == "__main__":
    unittest.main()
