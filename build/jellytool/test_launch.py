"""Offline launcher checks: python3 -B -m unittest discover -s build/jellytool -v."""

import fcntl
import importlib.util
import os
from pathlib import Path
import plistlib
import shutil
import signal
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
import uuid

SOURCE = Path(__file__).with_name("launch.py")
spec = importlib.util.spec_from_file_location("launcher", SOURCE)
launcher = importlib.util.module_from_spec(spec)
spec.loader.exec_module(launcher)


class LockTests(unittest.TestCase):
    def test_unregister_accepts_only_an_already_absent_registration(self):
        for status, message, succeeds in [
            (0, "", True),
            (1, "failed to scan /build.noindex/Jellytool.app: -10814\n from spotlight\n", True),
            (1, "failed to scan /build.noindex/Jellytool.app: -54\n", False),
        ]:
            with self.subTest(message=message), patch.object(launcher.subprocess, "run", return_value=subprocess.CompletedProcess([], status, "", message)):
                if succeeds:
                    launcher.unregister(Path("/build.noindex/Jellytool.app"))
                else:
                    with self.assertRaises(subprocess.CalledProcessError):
                        launcher.unregister(Path("/build.noindex/Jellytool.app"))

    def test_unreadable_lock_is_an_error(self):
        with patch.object(Path, "open", side_effect=PermissionError("denied")):
            with self.assertRaises(SystemExit):
                launcher.wait_lock_free(Path("/unreadable.lock"), 0, False)

    def test_directory_is_an_error(self):
        with tempfile.TemporaryDirectory() as folder:
            with self.assertRaises(SystemExit):
                launcher.wait_lock_free(Path(folder), 0, False)

    def test_flock_and_release(self):
        with tempfile.TemporaryDirectory() as folder:
            lock = Path(folder) / "instance.lock"
            self.assertTrue(launcher.wait_lock_free(lock, 0, False))
            with lock.open("w") as handle:
                fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
                self.assertFalse(launcher.wait_lock_free(lock, 0, False))
            self.assertTrue(launcher.wait_lock_free(lock, 0, False))

    def test_portable_and_relative_xdg_match_appdir(self):
        with tempfile.TemporaryDirectory() as folder:
            home = Path(folder)
            app = home / "build" / "Jellytool.app"
            with patch.object(Path, "home", return_value=home), patch.dict(os.environ, {"XDG_CONFIG_HOME": "relative"}):
                self.assertEqual(launcher.lock_path(app), home / ".config/jellytool/jellytool-instance.lock")
                (app.parent / "jellytool-data").mkdir(parents=True)
                self.assertEqual(launcher.lock_path(app), app.parent / "jellytool-data/jellytool-instance.lock")

    def test_only_launchservices_600_is_retried(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            stale = subprocess.CompletedProcess([], 1, "", "Launch failed: -600")
            invalid = subprocess.CompletedProcess([], 1, "", "bad executable")
            with patch.object(launcher, "running", return_value={}), patch.object(launcher, "open_build", side_effect=[stale, invalid]) as opening, patch.object(launcher.time, "sleep"):
                with self.assertRaises(SystemExit):
                    launcher.launch(root / "Jellytool.app", "test", root / "instance.lock", [], 5, False)
                self.assertEqual(opening.call_count, 2)

    def test_launchservices_retries_are_bounded(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            stale = subprocess.CompletedProcess([], 1, "", "Launch failed: -600")
            with patch.object(launcher, "running", return_value={}), patch.object(launcher, "open_build", return_value=stale) as opening, patch.object(launcher.time, "sleep"):
                with self.assertRaises(SystemExit):
                    launcher.launch(root / "Jellytool.app", "test", root / "instance.lock", [], 5, False)
                self.assertEqual(opening.call_count, 3)


@unittest.skipUnless(sys.platform == "darwin", "requires macOS AppKit and LaunchServices")
class NativeLaunchTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.compiler_dir = tempfile.TemporaryDirectory(prefix="jellytool-launch-compiler-", suffix=".noindex")
        cls.addClassCleanup(cls.compiler_dir.cleanup)
        source = Path(cls.compiler_dir.name) / "app.m"
        cls.native = source.with_suffix("")
        source.write_text(r'''
#import <Cocoa/Cocoa.h>
#include <sys/file.h>
#include <fcntl.h>
#include <signal.h>
#include <unistd.h>
int main() {
 @autoreleasepool {
  NSString *root = [[[[NSBundle mainBundle] bundlePath] stringByDeletingLastPathComponent] stringByDeletingLastPathComponent];
  NSString *mode = [NSString stringWithContentsOfFile:[root stringByAppendingPathComponent:@"mode"] encoding:NSUTF8StringEncoding error:nil];
  if ([mode isEqualToString:@"ignore"]) signal(SIGTERM, SIG_IGN);
  int fd = open([[root stringByAppendingPathComponent:@"instance.lock"] fileSystemRepresentation], O_RDWR | O_CREAT, 0600);
  if (flock(fd, LOCK_EX | LOCK_NB)) { fprintf(stderr, "instance lock unavailable\n"); return 23; }
  [NSApplication sharedApplication];
  if ([mode isEqualToString:@"fail"]) {
   fprintf(stderr, "fixture: plugin initialization failed\n");
   sleep(2); return 24;
  }
  if ([mode isEqualToString:@"hang"]) { sleep(60); return 25; }
  [NSApp setActivationPolicy:NSApplicationActivationPolicyProhibited];
  [NSApp run];
 }
 return 0;
}
''')
        subprocess.run(["/usr/bin/clang", "-w", "-framework", "Cocoa", str(source), "-o", str(cls.native)], check=True, capture_output=True)

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="jellytool-launch-test-", suffix=".noindex")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.ident = "com.jellytool.launch-test." + uuid.uuid4().hex
        self.ids = {self.ident}
        self.apps = []
        self.lock = self.root / "instance.lock"
        self.env = dict(os.environ, HOME=str(self.root / "home"), XDG_CONFIG_HOME=str(self.root / "config"), XDG_CACHE_HOME=str(self.root / "cache"))
        Path(self.env["HOME"]).mkdir()
        self.addCleanup(self.stop_apps)

    def bundle(self, name, ident=None):
        ident = ident or self.ident
        self.ids.add(ident)
        app = self.root / name / "Jellytool.app"
        self.apps.append(app)
        (app / "Contents/MacOS").mkdir(parents=True)
        shutil.copy2(self.native, app / "Contents/MacOS/jellytool")
        with (app / "Contents/Info.plist").open("wb") as handle:
            plistlib.dump({"CFBundleIdentifier": ident, "CFBundleExecutable": "jellytool", "CFBundleName": "JellytoolLaunchTest", "CFBundlePackageType": "APPL", "LSUIElement": True}, handle)
        return app

    def command(self, app, *options):
        return [sys.executable, "-B", str(SOURCE), str(app), "--lock", str(self.lock), "--timeout", "8", *options]

    def invoke(self, app, *options):
        result = subprocess.run(self.command(app, *options), env=self.env, capture_output=True, text=True, timeout=30)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        return result

    def stop_apps(self):
        for ident in self.ids:
            pids = launcher.running(ident)
            for pid in pids:
                try:
                    os.kill(pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
            launcher.wait_gone(pids, 5)
        for app in self.apps:
            launcher.unregister(app)

    def test_ready_then_replace_other_path(self):
        old, new = self.bundle("old"), self.bundle("new")
        self.invoke(old)
        first = launcher.running(self.ident)
        self.assertEqual(len(first), 1)
        self.assertTrue(launcher.finished_launching(next(iter(first))))
        self.assertFalse(launcher.wait_lock_free(self.lock, 0, False))
        self.invoke(new)
        second = launcher.running(self.ident)
        self.assertEqual(len(second), 1)
        self.assertFalse(set(first) & set(second))
        self.assertEqual(Path(next(iter(second.values()))).resolve(), new)

    def test_startup_failure_is_not_success_or_retried(self):
        app = self.bundle("fail")
        (self.root / "mode").write_text("fail")
        result = subprocess.run(self.command(app), env=self.env, capture_output=True, text=True, timeout=20)
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertEqual(result.stdout.count("launch: open "), 1, result.stdout)
        self.assertIn("fixture: plugin initialization failed", result.stderr)
        self.assertTrue(launcher.wait_lock_free(self.lock, 0, False))

    def test_hung_startup_is_stopped(self):
        app = self.bundle("hang")
        (self.root / "mode").write_text("hang")
        result = subprocess.run(self.command(app, "--timeout", "3"), env=self.env, capture_output=True, text=True, timeout=20)
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertIn("did not become ready", result.stderr)
        self.assertTrue(launcher.wait_lock_free(self.lock, 0, False))

    def test_sigkill_fallback_and_other_bundle_id(self):
        other = self.ident + ".old"
        old, new = self.bundle("old", other), self.bundle("new")
        (self.root / "mode").write_text("ignore")
        self.invoke(old)
        (self.root / "mode").write_text("normal")
        result = self.invoke(new)
        self.assertIn("ignored SIGTERM; killing it", result.stdout)
        self.assertFalse(launcher.alive(launcher.running(other)))

    def test_parallel_launch_is_rejected(self):
        app = self.bundle("parallel")
        guard = self.lock.with_suffix(".launch.lock")
        with guard.open("w") as handle:
            fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
            result = subprocess.run(self.command(app), env=self.env, capture_output=True, text=True, timeout=10)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("another launch is already in progress", result.stderr)
        self.assertFalse(launcher.running(self.ident))

    def test_dry_run_does_not_stop_or_write(self):
        app = self.bundle("preview")
        self.invoke(app)
        before = launcher.running(self.ident)
        log = self.lock.with_suffix(".launch.log")
        contents = log.read_bytes()
        self.invoke(app, "--dry-run")
        self.assertEqual(launcher.running(self.ident), before)
        self.assertEqual(log.read_bytes(), contents)


if __name__ == "__main__":
    unittest.main()
