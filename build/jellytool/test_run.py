"""Offline checks for the one-command package/launch flow."""

import importlib.util
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("jellytool_run", Path(__file__).with_name("run.py"))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class RunTests(unittest.TestCase):
    def test_bun_discovery_skips_the_wrong_version(self):
        with patch.object(runner.subprocess, "run", side_effect=[
            subprocess.CompletedProcess([], 0, "1.3.13\n"),
            subprocess.CompletedProcess([], 0, "1.4.2\n"),
        ]):
            self.assertEqual(runner.executable(["/old/bun", "/bundled/bun"], ["--version"], "1.4.2"), Path("/bundled/bun"))

    def test_failed_package_does_not_launch(self):
        with tempfile.TemporaryDirectory(prefix="jellytool run ") as folder:
            with patch.object(runner.sys, "platform", "darwin"), patch.object(runner.Path, "home", return_value=Path(folder)), patch.object(runner, "executable", side_effect=[Path("/go/bin/go"), Path("/bun/bun")]), patch.object(runner.subprocess, "run", side_effect=subprocess.CalledProcessError(1, "package")) as commands:
                with self.assertRaises(subprocess.CalledProcessError):
                    runner.main([])
                self.assertEqual(commands.call_count, 1)
                self.assertEqual(Path(commands.call_args.args[0][1]).name, "package.py")

    def test_install_copies_to_user_applications(self):
        with tempfile.TemporaryDirectory(prefix="jellytool run ") as folder:
            source = Path(folder) / "build" / "Jellytool.app"
            (source / "Contents/MacOS").mkdir(parents=True)
            (source / "Contents/MacOS/jellytool").write_text("binary")
            with patch.object(runner.Path, "home", return_value=Path(folder)):
                installed = runner.install_app(source)
            self.assertEqual(installed, Path(folder) / "Applications/Jellytool.app")
            self.assertEqual((installed / "Contents/MacOS/jellytool").read_text(), "binary")

    def test_success_launches_the_fixed_app_and_keeps_build_staging(self):
        with tempfile.TemporaryDirectory(prefix="jellytool run ") as folder:
            installed = Path(folder) / "Applications/Jellytool.app"
            with patch.object(runner.sys, "platform", "darwin"), patch.object(runner.Path, "home", return_value=Path(folder)), patch.object(runner, "executable", side_effect=[Path("/go/bin/go"), Path("/bun/bun")] * 2), patch.object(runner, "install_app", return_value=installed) as install, patch.object(runner.subprocess, "run") as commands:
                runner.main([])
                runner.main([])
                for package, launch in zip(commands.call_args_list[::2], commands.call_args_list[1::2]):
                    self.assertEqual(Path(package.args[0][1]).name, "package.py")
                    self.assertEqual(Path(launch.args[0][1]).name, "launch.py")
                    self.assertEqual(launch.args[0][-1], str(installed))
                    self.assertTrue(package.kwargs["env"]["PATH"].startswith("/go/bin:"))
                self.assertEqual(install.call_count, 2)
                self.assertNotEqual(install.call_args_list[0].args[0], install.call_args_list[1].args[0])


if __name__ == "__main__":
    unittest.main()
