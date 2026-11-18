"""Offline checks for the multi-platform Jellytool packager."""

import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location("jellytool_package_multi", Path(__file__).with_name("package-multi.py"))
packager = importlib.util.module_from_spec(spec)
spec.loader.exec_module(packager)


class PackageMultiTests(unittest.TestCase):
    def test_target_bun_requires_an_explicit_cross_platform_binary(self):
        with self.assertRaisesRegex(ValueError, "missing target Bun for windows/amd64"):
            packager.target_bun("windows/amd64", "darwin/arm64", {}, None)

    def test_target_bun_accepts_target_mapping(self):
        path = Path("/tmp/bun-windows.exe")
        self.assertEqual(packager.target_bun("windows/amd64", "darwin/arm64", {"windows/amd64": path}, None), path)

    def test_main_publishes_all_requested_targets_only_after_success(self):
        calls = []

        def fake_build(root, go, target, bun, staging, args, env, host):
            calls.append((target, bun, staging))
            (staging / (packager.package_name(target) + ".zip")).touch()

        with tempfile.TemporaryDirectory() as folder, \
                patch.object(packager.sys, "platform", "darwin"), \
                patch.object(packager.platform, "machine", return_value="arm64"), \
                patch.object(packager, "find_executable", return_value=Path("/go/bin/go")), \
                patch.object(packager, "build_target", side_effect=fake_build):
            output = Path(folder) / "dist"
            result = packager.main([
                "--output", str(output),
                "--target", "darwin/arm64",
                "--target", "windows/amd64",
                "--bun", "/bun/darwin",
                "--bun", "windows/amd64=/bun/windows.exe",
            ])
            self.assertEqual(result, 0)
            self.assertEqual([call[0] for call in calls], ["darwin/arm64", "windows/amd64"])
            self.assertEqual(sorted(path.name for path in output.iterdir()), ["Jellytool-Windows-x64.zip", "Jellytool-macOS-Apple-Silicon.zip"])

    def test_failed_target_does_not_publish_partial_output(self):
        with tempfile.TemporaryDirectory() as folder, \
                patch.object(packager.sys, "platform", "darwin"), \
                patch.object(packager.platform, "machine", return_value="arm64"), \
                patch.object(packager, "find_executable", return_value=Path("/go/bin/go")), \
                patch.object(packager, "build_target", side_effect=RuntimeError("build failed")):
            output = Path(folder) / "dist"
            self.assertEqual(packager.main(["--output", str(output), "--target", "darwin/arm64", "--bun", "/bun/darwin"]), 1)
            self.assertFalse(output.exists())


if __name__ == "__main__":
    unittest.main()
