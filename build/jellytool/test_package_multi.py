"""Offline checks for the multi-platform Jellytool packager."""

import importlib.util
import json
from pathlib import Path
import plistlib
import shutil
import subprocess
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location("jellytool_package_multi", Path(__file__).with_name("package-multi.py"))
packager = importlib.util.module_from_spec(spec)
spec.loader.exec_module(packager)


class PackageMultiTests(unittest.TestCase):
    def test_common_files_do_not_add_distribution_documents(self):
        root = Path(__file__).resolve().parents[2]
        with tempfile.TemporaryDirectory() as folder:
            source = Path(folder) / "Jellytool-macOS-Apple-Silicon"
            resources = source / "Jellytool.app/Contents/Resources/jellytool"
            resources.mkdir(parents=True)
            packager.copy_common_files(root, source, resources)
            self.assertEqual([p.name for p in source.iterdir()], ["Jellytool.app"])
            self.assertTrue((resources / "LICENSE").is_file())
            self.assertTrue((resources / "THIRD_PARTY_NOTICES").is_file())

    def test_macos_disk_image_contains_applications_link(self):
        with tempfile.TemporaryDirectory() as folder:
            source = Path(folder) / "Jellytool-macOS-Apple-Silicon"
            source.mkdir()
            (source / "Jellytool.app").mkdir()

            def create_image(command, **kwargs):
                self.assertEqual((source / "Applications").readlink(), Path("/Applications"))
                self.assertTrue((source / "Jellytool.app").is_dir())
                self.assertEqual(command[:2], ["hdiutil", "create"])
                Path(command[-1]).touch()

            with patch.object(packager.subprocess, "run", side_effect=create_image):
                self.assertEqual(packager.archive_package(source, "darwin/arm64"), "Jellytool-macOS-Apple-Silicon.dmg")
            self.assertFalse(source.exists())
            self.assertTrue(source.with_suffix(".dmg").exists())

    def build_darwin(self, folder, sign_identity="", notary_profile="", notary_reply=None):
        """Run build_target for darwin/arm64 with every external command faked.
        Returns the recorded (command, env) calls."""
        folder = Path(folder)
        bun = folder / "bun-input"
        bun.write_bytes(b"\xcf\xfa\xed\xfe original bun")
        staging = folder / "staging"
        staging.mkdir()
        calls = []

        def run(command, **kwargs):
            command = [str(part) for part in command]
            calls.append((command, kwargs.get("env")))
            if command[1:3] == ["run", "./cmd/jellytool-pack"]:
                resources = Path(command[4])
                shutil.copyfile(command[3], resources / "bun")
                (resources / "runtime-lock.json").write_text("{}")
            if command[:2] == ["xcrun", "notarytool"] and command[2] == "submit":
                return subprocess.CompletedProcess(command, 0, json.dumps(notary_reply or {"id": "n1", "status": "Accepted"}), "")
            if command[:2] == ["xcrun", "notarytool"] and command[2] == "log":
                return subprocess.CompletedProcess(command, 0, "bun: The signature does not include a secure timestamp.", "")
            if command[0] == "codesign" and "--sign" in command and command[-1].endswith("/bun"):
                Path(command[-1]).write_bytes(Path(command[-1]).read_bytes() + b" signed")
            return subprocess.CompletedProcess(command, 0, "", "")

        def archive(package_dir, target):
            (staging / "Jellytool-macOS-Apple-Silicon.dmg").touch()
            return "Jellytool-macOS-Apple-Silicon.dmg"

        args = SimpleNamespace(version="0.1.0", build="1", update_feed="",
                               sign_identity=sign_identity, notary_profile=notary_profile)
        with patch.object(packager.subprocess, "run", side_effect=run), \
                patch.object(packager, "stage_icon"), \
                patch.object(packager, "copy_common_files"), \
                patch.object(packager, "archive_package", side_effect=archive):
            packager.build_target(folder, Path("/go/bin/go"), "darwin/arm64", bun, staging, args, {"PATH": "/bin"}, "darwin/arm64")
        self.assertEqual(bun.read_bytes(), b"\xcf\xfa\xed\xfe original bun", "the caller's Bun must not be modified")
        return calls

    def test_darwin_build_targets_the_bundled_bun_minimum(self):
        with tempfile.TemporaryDirectory() as folder:
            calls = self.build_darwin(folder)
            env = next(env for command, env in calls if command[1] == "build")
            plist = plistlib.loads((Path(folder) / "staging/Jellytool-macOS-Apple-Silicon/Jellytool.app/Contents/Info.plist").read_bytes())
        self.assertEqual(env["MACOSX_DEPLOYMENT_TARGET"], packager.MACOS_MIN)
        self.assertIn(f"-mmacosx-version-min={packager.MACOS_MIN}", env["CGO_CFLAGS"])
        self.assertIn(f"-mmacosx-version-min={packager.MACOS_MIN}", env["CGO_LDFLAGS"])
        self.assertEqual(plist["LSMinimumSystemVersion"], packager.MACOS_MIN)
        self.assertFalse([c for c, _ in calls if c[0] in ("codesign", "xcrun", "spctl")], "unsigned builds run no signing tools")

    def test_signed_build_signs_bun_before_the_lock_is_written(self):
        identity = "Developer ID Application: Example (TEAM123456)"
        with tempfile.TemporaryDirectory() as folder:
            calls = self.build_darwin(folder, identity, "notary")
            app = Path(folder) / "staging/Jellytool-macOS-Apple-Silicon/Jellytool.app"
            commands = [c for c, _ in calls]
            pack = next(i for i, c in enumerate(commands) if c[1:3] == ["run", "./cmd/jellytool-pack"])
            bun_sign = next(i for i, c in enumerate(commands) if c[0] == "codesign" and "--entitlements" in c)
            self.assertLess(bun_sign, pack)
            self.assertEqual(commands[pack][3], commands[bun_sign][-1], "the packer copies the signed Bun")
            self.assertIn(str(packager.BUN_ENTITLEMENTS), commands[bun_sign])
            # After packing, the bundled bun is only verified, never signed again.
            bundled = str(app / "Contents/Resources/jellytool/bun")
            self.assertEqual([c[:2] for c in commands[pack:] if c[-1] == bundled], [["codesign", "--verify"]])
            names = [c[0] if c[0] != "xcrun" else c[1] for c in commands[pack + 1:]]
            self.assertEqual(names, ["codesign", "codesign", "codesign", "codesign", "notarytool", "stapler", "spctl"])
            self.assertEqual(commands[pack + 2][-1], str(app))
            self.assertNotIn("--entitlements", commands[pack + 2])
            self.assertTrue(commands[-4][-1].endswith(".dmg"))

    def test_rejected_notarization_reports_apples_log(self):
        with tempfile.TemporaryDirectory() as folder:
            with self.assertRaisesRegex(RuntimeError, "Invalid(.|\n)*secure timestamp"):
                self.build_darwin(folder, "Developer ID Application: Example (TEAM123456)", "notary",
                                  {"id": "n1", "status": "Invalid"})

    def test_signing_options_are_checked(self):
        base = dict(version="dev", build="", update_feed="")
        with self.assertRaisesRegex(ValueError, "requires --sign-identity"):
            packager.validate_options(SimpleNamespace(**base, sign_identity="", notary_profile="p"))
        with self.assertRaisesRegex(ValueError, "Developer ID Application"):
            packager.validate_options(SimpleNamespace(**base, sign_identity="Apple Development: x", notary_profile=""))

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
            (staging / (packager.package_name(target) + (".dmg" if target.startswith("darwin/") else "-Setup.exe"))).touch()

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
            self.assertEqual(sorted(path.name for path in output.iterdir()), ["Jellytool-Windows-x64-Setup.exe", "Jellytool-macOS-Apple-Silicon.dmg"])

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
