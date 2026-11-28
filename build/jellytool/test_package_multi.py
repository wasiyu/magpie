"""Offline checks for the multi-platform Jellytool packager."""

import importlib.util
import io
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

    def test_windows_installer_opens_the_app_and_starts_it_at_login(self):
        script = packager.nsis_script("out.exe", "pkg", "x64", icon="app.ico")
        section = script[script.index("\nSection\n"):script.index("SectionEnd")]
        uninstall = script[script.index('Section "Uninstall"'):]
        # The finish page opens it through Explorer: started by an elevated
        # setup it would run as administrator.
        self.assertIn("!define MUI_FINISHPAGE_RUN_FUNCTION OpenApp", script)
        self.assertLess(script.index("MUI_PAGE_INSTFILES"), script.index("MUI_PAGE_FINISH"))
        self.assertLess(script.index("MUI_PAGE_FINISH"), script.index("MUI_UNPAGE_CONFIRM"))
        opener = script[script.index("Function OpenApp"):]
        self.assertIn('Exec \'"$WINDIR\\explorer.exe" "$INSTDIR\\Jellytool.exe"\'', opener[:opener.index("FunctionEnd")])
        # A desktop shortcut beside the Start menu's, in the same shell context.
        self.assertIn('CreateShortcut "$DESKTOP\\Jellytoken助手.lnk" "$INSTDIR\\Jellytool.exe"', section)
        self.assertIn('Delete "$DESKTOP\\Jellytoken助手.lnk"', uninstall)
        # The login start is the value internal/autostart reads (product.ID,
        # "exe" tray), written on a first install only: read before
        # InstallLocation is written, so a reinstall keeps the user's choice.
        run = 'WriteRegStr HKCU "${RUN_KEY}" "jellytool" \'"$INSTDIR\\Jellytool.exe" tray\''
        self.assertIn(run, section)
        self.assertLess(section.index('ReadRegStr $Installed SHCTX "${UNINST_KEY}" "InstallLocation"'),
                        section.index('WriteRegStr SHCTX "${UNINST_KEY}" "InstallLocation"'))
        self.assertIn('${If} $Installed == ""\n    ' + run, section)
        self.assertIn('DeleteRegValue HKCU "${RUN_KEY}" "jellytool"', uninstall)
        self.assertIn('DeleteRegValue HKCU "${APPROVED_KEY}" "jellytool"', uninstall)
        # The setup and uninstaller take the app's icon when there is one.
        self.assertIn('!define MUI_ICON "app.ico"\n!define MUI_UNICON "app.ico"', script)
        self.assertNotIn("MUI_ICON", packager.nsis_script("out.exe", "pkg", "x64"))
        self.assertNotIn("@ICON_DEFINES@", packager.nsis_script("out.exe", "pkg", "x64"))

    def test_windows_installer_lets_a_standard_user_install_for_themself(self):
        script = packager.nsis_script("out.exe", "pkg", "x64")
        # Highest: an administrator is asked once and may pick either mode; a
        # standard user is not asked and installs for themself.
        self.assertIn("!define MULTIUSER_EXECUTIONLEVEL Highest", script)
        self.assertNotIn("RequestExecutionLevel admin", script)
        self.assertIn("!insertmacro MULTIUSER_PAGE_INSTALLMODE", script)
        # The 32-bit NSIS stub would put $PROGRAMFILES under "(x86)".
        self.assertIn("!define MULTIUSER_USE_PROGRAMFILES64", script)
        self.assertNotIn("$PROGRAMFILES\\", script)
        # The entry goes where the mode says, and its folder is read back.
        self.assertIn('WriteRegStr SHCTX "${UNINST_KEY}" "InstallLocation" "$INSTDIR"', script)
        # Only the login start is the user's own (HKCU Run, as Settings' Open
        # at login reads it); the uninstall entry follows the mode.
        self.assertNotRegex(script, r'WriteReg\w+ HK(LM|CU) (?!"\$\{RUN_KEY\}")')
        self.assertIn('!define MULTIUSER_INSTALLMODE_INSTDIR_REGISTRY_VALUENAME "InstallLocation"', script)
        # The registry is read and written in its 64-bit view before MultiUser looks.
        init = script[script.index("Function .onInit"):]
        self.assertLess(init.index("SetRegView 64"), init.index("MULTIUSER_INIT"))
        uninit = script[script.index("Function un.onInit"):]
        self.assertLess(uninit.index("SetRegView 64"), uninit.index("MULTIUSER_UNINIT"))
        # The uninstaller follows its own folder, and a per-machine one needs an admin.
        self.assertIn('ReadRegStr $R1 HKLM "${UNINST_KEY}" "InstallLocation"', uninit)
        self.assertIn("Quit", uninit[:uninit.index("FunctionEnd")])
        for placeholder in ("@ARCHIVE@", "@SOURCE@", "@ARCH@", "@ICON_DEFINES@"):
            self.assertNotIn(placeholder, script)

    def test_windows_installer_asks_before_closing_running_copies(self):
        script = packager.nsis_script("out.exe", "pkg", "x64")
        install = script[script.index("\nSection\n"):script.index("SectionEnd")]
        uninstall = script[script.index('Section "Uninstall"'):]
        # Asked when the folder (or the uninstall) is confirmed, Cancel staying on the page.
        self.assertIn("!define MUI_PAGE_CUSTOMFUNCTION_LEAVE DirectoryLeave\n!insertmacro MUI_PAGE_DIRECTORY", script)
        self.assertIn("!define MUI_PAGE_CUSTOMFUNCTION_LEAVE un.ConfirmLeave\n!insertmacro MUI_UNPAGE_CONFIRM", script)
        for name in ("Function DirectoryLeave", "Function un.ConfirmLeave"):
            leave = script[script.index(name):]
            leave = leave[:leave.index("FunctionEnd")]
            self.assertIn('!insertmacro RunStopScript "$INSTDIR', leave)
            self.assertNotIn('" "1"', leave, "asking stops nothing")
            self.assertIn("MB_OKCANCEL", leave)
            self.assertIn("IDCANCEL`\n      Abort", leave)
        # Then a running bun.exe or Jellytool.exe, which locks its file, is ended first.
        self.assertLess(install.index('!insertmacro RunStopScript "$INSTDIR|$OldDir" "1"'), install.index("File /r"))
        self.assertLess(uninstall.index('!insertmacro RunStopScript "$INSTDIR" "1"'), uninstall.index("RemoveFrom"))
        # Only the app and its Bun host are matched, never other programs in the folder.
        stop = script[script.index("!macro RunStopScript"):]
        stop = stop[:stop.index("!macroend")]
        self.assertIn("'Jellytool.exe'", stop)
        self.assertIn("'jellytool-resources\\'", stop)
        self.assertNotIn("Get-Process", stop)
        self.assertNotIn("${DIRS}", stop.split("nsExec::", 1)[1])
        # Scanners flag a hidden PowerShell with its policy bypassed.
        self.assertNotIn("Bypass", script)
        # An earlier per-user install is found from its own HKCU entry and moved.
        self.assertIn('ReadRegStr $OldDir HKCU "${UNINST_KEY}" "UninstallString"', script)
        self.assertLess(install.index('!insertmacro RemoveFrom "$OldDir"'), install.index('DeleteRegKey HKCU "${UNINST_KEY}"'))
        # An upgrade starts from an empty folder: no stale resource is kept.
        self.assertLess(install.index('!insertmacro RemoveFrom "$INSTDIR"'), install.index("File /r"))
        # No relative jump: a macro or plugin call after it would move its target.
        self.assertNotRegex(script, r"ID(YES|NO|OK|RETRY|CANCEL) [+-]\d")

    def test_windows_installer_warns_but_never_blocks_on_the_system(self):
        for arch in ("x64", "ARM64"):
            script = packager.nsis_script("out.exe", "pkg", arch)
            self.assertIn(f'!define PACKAGE_ARCH "{arch}"', script)
            init = script[script.index("Function .onInit"):]
            self.assertTrue(init.split("\n")[1].strip() == "Call CheckSystem", "checked before anything else")
            check = script[script.index("Function CheckSystem"):script.index("Function .onInit")]
            self.assertIn("${IsNativeIA32}", check)
            self.assertIn("${AtLeastBuild} 17763", check)
            self.assertEqual(check.count("!insertmacro WarnSystem"), 4)
            self.assertNotIn("Abort", check, "only the user's No ends the install")
            self.assertIn("IDNO`\n    Abort", script)
            warn = script[script.index("!macro WarnSystem"):script.index("Function CheckSystem")]
            self.assertIn("MB_YESNO", warn)
            self.assertIn("/SD IDYES", warn, "a silent install goes on")
        with self.assertRaisesRegex(ValueError, "no Windows installer"):
            packager.nsis_script("out.exe", "pkg", "x86")

    @unittest.skipUnless(shutil.which("makensis"), "makensis not installed")
    def test_windows_installer_script_compiles(self):
        with tempfile.TemporaryDirectory() as folder:
            source = Path(folder) / "pkg"
            (source / "jellytool-resources").mkdir(parents=True)
            (source / "Jellytool.exe").write_bytes(b"MZ")
            (source / "jellytool-resources/bun.exe").write_bytes(b"MZ")
            for arch in ("x64", "ARM64"):
                script = Path(folder) / f"{arch}.nsi"
                out = Path(folder) / f"{arch}.exe"
                script.write_text(packager.nsis_script(out, source, arch), encoding="utf-8-sig")
                subprocess.run(["makensis", "-WX", str(script)], check=True, stdout=subprocess.DEVNULL)
                self.assertTrue(out.is_file())
            # and with an icon: magpie's tray .ico stands in for the app's
            icon = Path(__file__).resolve().parents[2] / "internal/gui/icon-windows.ico"
            script = Path(folder) / "icon.nsi"
            out = Path(folder) / "icon.exe"
            script.write_text(packager.nsis_script(out, source, "x64", icon), encoding="utf-8-sig")
            subprocess.run(["makensis", "-WX", str(script)], check=True, stdout=subprocess.DEVNULL)
            self.assertTrue(out.is_file())

    def test_windows_build_links_the_app_icon_and_removes_its_resource(self):
        with tempfile.TemporaryDirectory() as folder:
            folder = Path(folder)
            repo = Path(__file__).resolve().parents[2]
            (folder / "build/windows").mkdir(parents=True)
            shutil.copyfile(repo / "build/windows/winres.json", folder / "build/windows/winres.json")
            staging = folder / "staging"
            staging.mkdir()
            bun = folder / "bun.exe"
            bun.write_bytes(b"MZ")
            seen = {}

            def run(command, **kwargs):
                command = [str(part) for part in command]
                if command[1:3] == ["run", "build/jellytool/ico.go"]:
                    Path(command[-1]).write_bytes(b"ico")
                elif "github.com/tc-hib/go-winres@v0.3.3" in command:
                    spec = json.loads(Path(command[command.index("--in") + 1]).read_text())
                    seen["spec"] = spec
                    seen["icon"] = (Path(command[command.index("--in") + 1]).parent / spec["RT_GROUP_ICON"]["#3"]["0000"]).read_bytes()
                    seen["version"] = command[command.index("--file-version") + 1]
                    (folder / "rsrc_windows_amd64.syso").write_bytes(b"syso")
                elif command[1] == "build":
                    seen["linked"] = (folder / "rsrc_windows_amd64.syso").exists()
                    seen["ico beside the package"] = (staging / "Jellytool-Windows-x64.ico").exists()
                    raise subprocess.CalledProcessError(1, command)
                return subprocess.CompletedProcess(command, 0, "", "")

            args = SimpleNamespace(version="0.2.3", build="", update_feed="", sign_identity="", notary_profile="")
            with patch.object(packager.subprocess, "run", side_effect=run), \
                    self.assertRaises(subprocess.CalledProcessError):
                packager.build_target(folder, Path("/go/bin/go"), "windows/amd64", bun, staging, args, {"PATH": "/bin"}, "darwin/arm64")
            # icon group #3 is the window's and taskbar's (TestWindowsIconIsResourceThree)
            self.assertEqual(list(seen["spec"]["RT_GROUP_ICON"]), ["#3"])
            self.assertEqual(seen["icon"], b"ico")
            self.assertEqual(seen["spec"]["RT_VERSION"]["#1"]["0000"]["info"]["0409"]["ProductName"], "Jellytoken助手")
            self.assertEqual(seen["version"], "0.2.3")
            self.assertTrue(seen["linked"] and seen["ico beside the package"])
            # even a failed build leaves no .syso for the next magpie build
            self.assertFalse((folder / "rsrc_windows_amd64.syso").exists())

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


    def test_failed_notarization_keeps_the_signed_dmg_marked(self):
        with tempfile.TemporaryDirectory() as folder:
            with self.assertRaises(packager.NotarizationFailed) as failed:
                self.build_darwin(folder, "Developer ID Application: Example (TEAM123456)", "notary",
                                  {"id": "n1", "status": "Invalid"})
            staging = Path(folder) / "staging"
            self.assertEqual([p.name for p in staging.glob("*.dmg")], ["Jellytool-macOS-Apple-Silicon-unnotarized.dmg"])
            self.assertEqual(failed.exception.archive, staging / "Jellytool-macOS-Apple-Silicon-unnotarized.dmg")

    def test_unanswered_notarization_is_kept_and_names_the_locked_screen(self):
        def run(command, **kwargs):
            raise subprocess.TimeoutExpired(command, kwargs.get("timeout"))
        with patch.object(packager.subprocess, "run", side_effect=run):
            with self.assertRaisesRegex(packager.NotarizationFailed, "locked"):
                packager.notarize(Path("/tmp/x.dmg"), "notary")

    def test_failed_staple_is_a_notarization_failure(self):
        def run(command, **kwargs):
            if command[:3] == ["xcrun", "notarytool", "submit"]:
                return subprocess.CompletedProcess(command, 0, json.dumps({"id": "n1", "status": "Accepted"}), "")
            if command[:2] == ["xcrun", "stapler"]:
                return subprocess.CompletedProcess(command, 65, "", "Could not validate ticket")
            return subprocess.CompletedProcess(command, 0, "", "")
        with patch.object(packager.subprocess, "run", side_effect=run):
            with self.assertRaisesRegex(packager.NotarizationFailed, "accepted, but xcrun stapler staple failed"):
                packager.notarize(Path("/tmp/x.dmg"), "notary")

    def test_unnotarized_dmg_is_published_with_the_other_targets_and_fails(self):
        def fake_build(root, go, target, bun, staging, args, env, host):
            if target.startswith("darwin/"):
                kept = staging / "Jellytool-macOS-Apple-Silicon-unnotarized.dmg"
                kept.touch()
                raise packager.NotarizationFailed(kept, "had no answer")
            (staging / "Jellytool-Windows-x64-Setup.exe").touch()

        with tempfile.TemporaryDirectory() as folder, \
                patch.object(packager.sys, "platform", "darwin"), \
                patch.object(packager.platform, "machine", return_value="arm64"), \
                patch.object(packager, "find_executable", return_value=Path("/go/bin/go")), \
                patch.object(packager, "build_target", side_effect=fake_build), \
                patch.object(packager.sys, "stderr", new_callable=io.StringIO) as stderr:
            output = Path(folder) / "dist"
            result = packager.main([
                "--output", str(output), "--target", "darwin/arm64", "--target", "windows/amd64",
                "--bun", "/bun/darwin", "--bun", "windows/amd64=/bun/windows.exe",
                "--sign-identity", "Developer ID Application: Example (TEAM123456)", "--notary-profile", "notary",
            ])
            self.assertEqual(result, 1)
            self.assertEqual(sorted(p.name for p in output.iterdir()),
                             ["Jellytool-Windows-x64-Setup.exe", "Jellytool-macOS-Apple-Silicon-unnotarized.dmg"])
            self.assertIn(f"--notarize-only {output.resolve() / 'Jellytool-macOS-Apple-Silicon-unnotarized.dmg'} --notary-profile notary",
                          stderr.getvalue())
            self.assertIn("NOT notarized", stderr.getvalue())

    def notarize_only(self, folder, authority="Developer ID Application: Example (TEAM123456)", status="Accepted", extra=()):
        kept = Path(folder) / "Jellytool-macOS-Apple-Silicon-unnotarized.dmg"
        kept.write_bytes(b"signed dmg")
        calls = []

        def run(command, **kwargs):
            command = [str(part) for part in command]
            calls.append(command)
            if command[:2] == ["codesign", "-dvv"]:
                return subprocess.CompletedProcess(command, 0, "", f"Executable={command[-1]}\nAuthority={authority}\n")
            if command[:3] == ["xcrun", "notarytool", "submit"]:
                return subprocess.CompletedProcess(command, 0, json.dumps({"id": "n1", "status": status}), "")
            return subprocess.CompletedProcess(command, 0, "", "")

        with patch.object(packager.subprocess, "run", side_effect=run), \
                patch.object(packager.sys, "stderr", new_callable=io.StringIO), \
                patch.object(packager.sys, "stdout", new_callable=io.StringIO):
            result = packager.main(["--notarize-only", str(kept), *extra])
        return result, kept, calls

    def test_notarize_only_finishes_a_kept_dmg_and_drops_the_mark(self):
        with tempfile.TemporaryDirectory() as folder:
            result, kept, calls = self.notarize_only(folder, extra=("--notary-profile", "notary"))
            self.assertEqual(result, 0)
            self.assertFalse(kept.exists())
            self.assertEqual((Path(folder) / "Jellytool-macOS-Apple-Silicon.dmg").read_bytes(), b"signed dmg")
            names = [c[1] if c[0] == "xcrun" else c[0] for c in calls]
            self.assertEqual(names, ["codesign", "codesign", "notarytool", "stapler", "spctl"])

    def test_notarize_only_refuses_a_dmg_not_signed_with_developer_id(self):
        with tempfile.TemporaryDirectory() as folder:
            result, kept, calls = self.notarize_only(folder, authority="Apple Development: Example (TEAM123456)",
                                                     extra=("--notary-profile", "notary"))
            self.assertEqual(result, 1)
            self.assertTrue(kept.exists())
            self.assertFalse([c for c in calls if c[0] == "xcrun"], "an unsigned DMG was sent to Apple")

    def test_notarize_only_failure_leaves_the_file_to_try_again(self):
        with tempfile.TemporaryDirectory() as folder:
            result, kept, _ = self.notarize_only(folder, status="Invalid", extra=("--notary-profile", "notary"))
            self.assertEqual(result, 1)
            self.assertTrue(kept.exists())
            self.assertFalse((Path(folder) / "Jellytool-macOS-Apple-Silicon.dmg").exists())

    def test_notarize_only_requires_a_profile(self):
        with tempfile.TemporaryDirectory() as folder, patch.dict(packager.os.environ, {"JELLYTOOL_NOTARY_PROFILE": ""}):
            result, kept, calls = self.notarize_only(folder)
            self.assertEqual(result, 1)
            self.assertEqual(calls, [])

if __name__ == "__main__":
    unittest.main()
