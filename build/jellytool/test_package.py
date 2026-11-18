"""The published app and its binary must advertise the same Jellytool version."""
from pathlib import Path
import plistlib
import runpy
import sys
import tempfile
import unittest
from unittest.mock import patch


class PackageTests(unittest.TestCase):
    def package(self, folder, *options):
        output = Path(folder) / "Jellytool.app"
        bun = Path(folder) / "bun"
        bun.touch()
        argv = ["package.py", "--bun", str(bun), "--output", str(output), *options]
        with patch.object(sys, "argv", argv), patch.object(sys, "platform", "darwin"), patch("subprocess.run") as commands:
            runpy.run_path(str(Path(__file__).with_name("package.py")), run_name="__main__")
        return output, commands

    def test_release_version_and_feed_are_embedded(self):
        with tempfile.TemporaryDirectory() as folder:
            output, commands = self.package(folder, "--version", "0.2.0", "--update-feed", "https://downloads.example/latest.json")
            args = commands.call_args_list[0].args[0]
            flags = args[args.index("-ldflags") + 1]
            self.assertIn("main.version=0.2.0", flags)
            self.assertIn("update.JellytoolFeed=https://downloads.example/latest.json", flags)
            with (output / "Contents/Info.plist").open("rb") as stream:
                info = plistlib.load(stream)
            self.assertEqual(info["CFBundleShortVersionString"], "0.2.0")
            self.assertEqual(info["CFBundleVersion"], "0.2.0")

    def test_release_requires_an_https_feed_and_a_version(self):
        for options in [("--version", "0.2.0"), ("--version", "bad"), ("--update-feed", "http://example/latest"), ("--update-feed", "https://user:secret@example/latest"), ("--update-feed", "https://example/latest -X main.version=9.0.0")]:
            with self.subTest(options=options), tempfile.TemporaryDirectory() as folder:
                with self.assertRaises(SystemExit):
                    self.package(folder, *options)
                self.assertFalse((Path(folder) / "Jellytool.app").exists())

    def test_original_mit_license_is_shipped_verbatim(self):
        root = Path(__file__).resolve().parents[2]
        license_text = (root / "LICENSE").read_bytes()
        with tempfile.TemporaryDirectory() as folder:
            output, _ = self.package(folder)
            resources = output / "Contents/Resources"
            self.assertEqual((resources / "LICENSE").read_bytes(), license_text)
            notices = (resources / "THIRD_PARTY_NOTICES").read_bytes()
            self.assertIn(license_text, notices)
            original_tail = (root / "THIRD_PARTY_NOTICES").read_bytes().split(b"\n----------------", 1)[1]
            self.assertTrue(notices.endswith(original_tail), "third-party statements must not be rebranded")


if __name__ == "__main__":
    unittest.main()
