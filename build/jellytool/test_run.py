"""Offline checks for the one-command package/launch flow."""

import importlib.util
import fcntl
from pathlib import Path
import plistlib
import shutil
import subprocess
import sys
import tempfile
import threading
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("jellytool_run", Path(__file__).with_name("run.py"))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class RunTests(unittest.TestCase):
    def bundle(self, root, name, content):
        app = root / name / "Jellytool.app"
        executable = app / "Contents/MacOS/jellytool"
        executable.parent.mkdir(parents=True)
        executable.write_text(content)
        executable.chmod(0o755)
        with (app / "Contents/Info.plist").open("wb") as stream:
            plistlib.dump({"CFBundleIdentifier": "com.jellytoken.jellytool.dev", "CFBundleExecutable": "jellytool"}, stream)
        return app

    def test_update_stops_stdio_clients_before_replacing_files(self):
        with tempfile.TemporaryDirectory(suffix=".noindex") as folder:
            home = Path(folder)
            source = self.bundle(home, "build", "new")
            target = self.bundle(home, "Applications", "old")
            inventory = "p123\nftxt\nn" + str(target / "Contents/MacOS/jellytool") + "\n"
            def command(args, **kwargs):
                return subprocess.CompletedProcess(args, 0, inventory if "lsof" in Path(args[0]).name else "", "")
            def stop(processes, dry):
                nonlocal inventory
                self.assertEqual(processes, {123: str(target / "Contents/MacOS/jellytool")})
                self.assertEqual((target / "Contents/MacOS/jellytool").read_text(), "old")
                inventory = ""
            with patch.object(runner.Path, "home", return_value=home), patch.object(runner.subprocess, "run", side_effect=command), patch.object(runner.launcher, "end", side_effect=stop) as stopping:
                runner.install_app(source)
                stopping.assert_called_once()
            self.assertEqual((target / "Contents/MacOS/jellytool").read_text(), "new")

    def test_competing_update_is_rejected(self):
        with tempfile.TemporaryDirectory(suffix=".noindex") as folder:
            home = Path(folder)
            source = self.bundle(home, "build", "new")
            target = self.bundle(home, "Applications", "old")
            with (target.parent / ".jellytool-update.lock").open("w") as guard:
                fcntl.flock(guard, fcntl.LOCK_EX | fcntl.LOCK_NB)
                with patch.object(runner.Path, "home", return_value=home):
                    with self.assertRaisesRegex(RuntimeError, "update.*progress"):
                        runner.install_app(source)
            self.assertEqual((target / "Contents/MacOS/jellytool").read_text(), "old")

    def test_failed_rollback_retains_the_old_bundle(self):
        with tempfile.TemporaryDirectory(suffix=".noindex") as folder:
            home = Path(folder)
            source = self.bundle(home, "build", "new")
            target = self.bundle(home, "Applications", "old")
            rename = Path.rename
            def fail_commit(path, destination):
                if Path(destination) == target and path != target:
                    raise OSError("injected rename failure")
                return rename(path, destination)
            with patch.object(runner.Path, "home", return_value=home), patch.object(runner.Path, "rename", fail_commit), patch.object(runner.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, "", "")):
                with self.assertRaises((OSError, RuntimeError)):
                    runner.install_app(source)
            saved = list(target.parent.glob(".jellytool-install-*/old/Contents/MacOS/jellytool"))
            self.assertEqual(len(saved), 1, "failed rollback must not delete the only old copy")
            self.assertEqual(saved[0].read_text(), "old")

    def test_copy_failure_leaves_running_clients_and_target_alone(self):
        with tempfile.TemporaryDirectory(suffix=".noindex") as folder:
            home = Path(folder)
            source = self.bundle(home, "build", "new")
            target = self.bundle(home, "Applications", "old")
            with patch.object(runner.Path, "home", return_value=home), patch.object(runner.shutil, "copytree", side_effect=OSError("copy failed")), patch.object(runner, "stop_clients") as stop:
                with self.assertRaisesRegex(OSError, "copy failed"):
                    runner.install_app(source)
                stop.assert_not_called()
            self.assertEqual((target / "Contents/MacOS/jellytool").read_text(), "old")

    def test_failed_native_start_restores_old_files(self):
        for failure in (RuntimeError("native startup failed"), KeyboardInterrupt()):
            with self.subTest(failure=type(failure).__name__), tempfile.TemporaryDirectory(suffix=".noindex") as folder:
                home = Path(folder)
                source = self.bundle(home, "build", "new")
                target = self.bundle(home, "Applications", "old")
                def failed_start(installed):
                    self.assertEqual((installed / "Contents/MacOS/jellytool").read_text(), "new")
                    self.assertEqual(len(list(target.parent.glob(".jellytool-install-*/old"))), 1)
                    raise failure
                with patch.object(runner.Path, "home", return_value=home), patch.object(runner, "background_processes", return_value={}), patch.object(runner.launcher, "running", return_value={}), patch.object(runner.subprocess, "run"):
                    with self.assertRaises(type(failure)):
                        runner.install_app(source, failed_start)
                self.assertEqual((target / "Contents/MacOS/jellytool").read_text(), "old")
                self.assertFalse(list(target.parent.glob(".jellytool-install-*")))

    def test_process_inventory_errors_do_not_replace_the_app(self):
        with tempfile.TemporaryDirectory(suffix=".noindex") as folder:
            home = Path(folder)
            source = self.bundle(home, "build", "new")
            target = self.bundle(home, "Applications", "old")
            with patch.object(runner.Path, "home", return_value=home), patch.object(runner, "background_processes", side_effect=subprocess.CalledProcessError(1, "lsof")):
                with self.assertRaises(subprocess.CalledProcessError):
                    runner.install_app(source)
            self.assertEqual((target / "Contents/MacOS/jellytool").read_text(), "old")

    def test_inventory_finds_renamed_backups_and_leaves_gui_and_other_apps(self):
        target = Path("/test home/Applications/Jellytool.app")
        paths = [target / "Contents/MacOS/jellytool", target.parent / ".jellytool-install-x.noindex/old/Contents/MacOS/jellytool", target.parent / "Jellytool.app.backup-old/Contents/MacOS/jellytool", Path("/unrelated/Jellytool.app/Contents/MacOS/jellytool")]
        inventory = "".join(f"p{pid}\nn{path}\n" for pid, path in enumerate(paths, 1))
        with patch.object(runner.launcher, "running", return_value={1: str(target)}), patch.object(runner.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, inventory, "")):
            self.assertEqual(runner.background_processes(target), {2: str(paths[1]), 3: str(paths[2])})

    def test_post_swap_only_stops_old_mappings(self):
        target = Path("/test/Applications/Jellytool.app")
        old = str(target.parent / ".jellytool-install-x.noindex/old/Contents/MacOS/jellytool")
        current = str(target / "Contents/MacOS/jellytool")
        with patch.object(runner, "background_processes", side_effect=[{1: old, 2: current}, {2: current}]), patch.object(runner.launcher, "end") as stop:
            runner.stop_clients(target, stale_only=True)
            stop.assert_called_once_with({1: old}, False)

    def test_restarting_clients_are_an_error_instead_of_update_success(self):
        target = Path("/test/Applications/Jellytool.app")
        with patch.object(runner, "background_processes", side_effect=[{1: "old"}, {2: "old"}]), patch.object(runner.launcher, "end"):
            with self.assertRaisesRegex(RuntimeError, "restarted.*reconnect"):
                runner.stop_clients(target)

    @unittest.skipUnless(sys.platform == "darwin", "requires macOS lsof")
    def test_native_stdio_process_is_ended_before_swap(self):
        with tempfile.TemporaryDirectory(suffix=".noindex") as folder:
            home = Path(folder).resolve()
            source = self.bundle(home, "build", "new")
            target = self.bundle(home, "Applications", "old")
            shutil.copyfile("/bin/sleep", target / "Contents/MacOS/jellytool")
            old = subprocess.Popen([str(target / "Contents/MacOS/jellytool"), "60"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            reaper = threading.Thread(target=old.wait)
            reaper.start()
            try:
                with patch.object(runner.Path, "home", return_value=home):
                    runner.install_app(source)
                self.assertIsNotNone(old.poll(), "an installed update must not leave its old stdio binary running")
                self.assertEqual((target / "Contents/MacOS/jellytool").read_text(), "new")
            finally:
                if old.poll() is None:
                    old.terminate()
                reaper.join(timeout=5)
                if reaper.is_alive():
                    old.kill()
                    reaper.join()

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
            source = self.bundle(Path(folder), "build", "binary")
            with patch.object(runner.Path, "home", return_value=Path(folder)), patch.object(runner, "background_processes", return_value={}):
                installed = runner.install_app(source)
            self.assertEqual(installed, Path(folder) / "Applications/Jellytool.app")
            self.assertEqual((installed / "Contents/MacOS/jellytool").read_text(), "binary")

    def test_success_launches_the_fixed_app_and_keeps_build_staging(self):
        with tempfile.TemporaryDirectory(prefix="jellytool run ") as folder:
            installed = Path(folder) / "Applications/Jellytool.app"
            def install_and_start(app, start):
                start(installed)
                return installed
            with patch.object(runner.sys, "platform", "darwin"), patch.object(runner.Path, "home", return_value=Path(folder)), patch.object(runner, "executable", side_effect=[Path("/go/bin/go"), Path("/bun/bun")] * 2), patch.object(runner, "install_app", side_effect=install_and_start) as install, patch.object(runner.subprocess, "run") as commands:
                runner.main(["--version", "0.1.0", "--build", "20261115.1"])
                runner.main(["--version", "0.1.0", "--build", "20261115.2"])
                for package, launch in zip(commands.call_args_list[::2], commands.call_args_list[1::2]):
                    self.assertEqual(Path(package.args[0][1]).name, "package.py")
                    self.assertEqual(Path(launch.args[0][1]).name, "launch.py")
                    self.assertEqual(launch.args[0][-1], str(installed))
                    self.assertTrue(package.kwargs["env"]["PATH"].startswith("/go/bin:"))
                self.assertIn("20261115.1", commands.call_args_list[0].args[0])
                self.assertIn("20261115.2", commands.call_args_list[2].args[0])
                self.assertEqual(install.call_count, 2)
                self.assertNotEqual(install.call_args_list[0].args[0], install.call_args_list[1].args[0])
                self.assertTrue(all(call.args[0].parent.name.endswith(".noindex") for call in install.call_args_list), "build staging must be excluded from Spotlight")


if __name__ == "__main__":
    unittest.main()
