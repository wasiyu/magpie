#!/usr/bin/env python3
"""Build Jellytool archives for several OS/architecture targets."""

import argparse
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import sys
import tempfile
from urllib.parse import urlsplit


TARGET_RE = re.compile(r"^(darwin|linux|windows)/(amd64|arm64)$")
# The bundled Bun 1.4.2 is built for macOS 13 (its LC_BUILD_VERSION minos), so
# the app cannot start below it whatever the Go binary targets. The cgo build
# and LSMinimumSystemVersion both take this one value.
MACOS_MIN = "13.0"
# Bun's JavaScriptCore needs JIT memory and loads unsigned native addons
# under the hardened runtime; the main binary needs none of these.
BUN_ENTITLEMENTS = Path(__file__).with_name("bun.entitlements")
MACHO_MAGIC = {b"\xfe\xed\xfa\xce", b"\xfe\xed\xfa\xcf", b"\xce\xfa\xed\xfe", b"\xcf\xfa\xed\xfe", b"\xca\xfe\xba\xbe"}


def host_goos():
    return {"darwin": "darwin", "linux": "linux", "win32": "windows"}.get(sys.platform, "")


def host_goarch():
    return {"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}.get(platform.machine(), "")


def find_executable(candidates, version_args, expected=None):
    for candidate in candidates:
        if not candidate:
            continue
        path = Path(candidate).expanduser().absolute()
        try:
            result = subprocess.run([str(path), *version_args], capture_output=True, text=True, timeout=5)
        except (OSError, subprocess.SubprocessError):
            continue
        if result.returncode == 0 and (expected is None or result.stdout.strip() == expected):
            return path
    raise RuntimeError("no usable runtime found; specify --go and --bun")


def validate_options(args):
    if args.version != "dev" and not re.fullmatch(r"(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)", args.version):
        raise ValueError("version must be dev or a release version such as 0.1.0")
    if args.build and not re.fullmatch(r"[0-9]+(?:\.[0-9]+)*", args.build):
        raise ValueError("build must contain only numeric dot-separated components")
    try:
        feed = urlsplit(args.update_feed)
        valid_feed = feed.scheme == "https" and bool(feed.hostname) and feed.username is None and not feed.fragment
    except ValueError:
        valid_feed = False
    if args.update_feed and (not valid_feed or re.search(r'''[\s'"\\]''', args.update_feed)):
        raise ValueError("update-feed must be an HTTPS URL without credentials, fragments or whitespace")
    if args.version != "dev" and not args.update_feed and not args.build:
        raise ValueError("a release version requires --update-feed, unless --build is set for a local production artifact")
    if args.version == "dev" and args.build:
        raise ValueError("a build marker requires a release version")
    if args.notary_profile and not args.sign_identity:
        raise ValueError("--notary-profile requires --sign-identity")
    if args.sign_identity and not args.sign_identity.startswith("Developer ID Application:"):
        raise ValueError('--sign-identity must name a "Developer ID Application: …" certificate')


def parse_bun_options(options):
    mapped, plain = {}, []
    for option in options:
        if "=" in option:
            target, path = option.split("=", 1)
            if not TARGET_RE.fullmatch(target) or not path:
                raise ValueError("--bun mappings must look like target=/absolute/bun")
            mapped[target] = Path(path)
        else:
            plain.append(Path(option))
    if len(plain) > 1:
        raise ValueError("use one plain --bun path or target=/absolute/bun mappings")
    return mapped, plain[0] if plain else None


def target_bun(target, host, mapped, plain):
    if target in mapped:
        return mapped[target]
    if target == host and plain is not None:
        return plain
    if target != host:
        raise ValueError(f"missing target Bun for {target}; pass --bun {target}=/absolute/bun")
    return find_executable([shutil.which("bun")], ["--version"], "1.4.2")


def package_name(target):
    goos, goarch = target.split("/")
    platform_name = {
        "darwin/arm64": "macOS-Apple-Silicon",
        "darwin/amd64": "macOS-Intel",
        "windows/arm64": "Windows-ARM64",
        "windows/amd64": "Windows-x64",
        "linux/arm64": "Linux-ARM64",
        "linux/amd64": "Linux-x64",
    }[f"{goos}/{goarch}"]
    return "Jellytool-" + platform_name


def stage_icon(root, resources):
    iconset = resources.parent.parent / "jellytool.iconset"
    iconset.mkdir()
    try:
        for size in (16, 32, 128, 256, 512):
            for scale in (1, 2):
                name = f"icon_{size}x{size}" + ("@2x" if scale == 2 else "") + ".png"
                subprocess.run([
                    "sips", "-z", str(size * scale), str(size * scale),
                    str(root / "internal/gui/assets/icons/jellytool-app.png"),
                    "--out", str(iconset / name),
                ], check=True, stdout=subprocess.DEVNULL)
        subprocess.run(["iconutil", "-c", "icns", str(iconset), "-o", str(resources / "jellytool.icns")], check=True)
    finally:
        shutil.rmtree(iconset)


def windows_resources(root, go, goarch, icon, version, env):
    """Write the .syso go build links into Jellytool.exe and return its path;
    the caller removes it after the build. It holds the app artwork as icon
    group #3, the one Wails gives the window and taskbar (see
    TestWindowsIconIsResourceThree) and the exe's icon that its shortcuts
    and Installed apps show; magpie's manifest; and a version block naming
    the product, which Task Manager and Startup apps show."""
    res = json.loads((root / "build/windows/winres.json").read_text())
    res["RT_GROUP_ICON"] = {"#3": {"0000": icon.name}}
    manifest = res["RT_MANIFEST"]["#1"]["0409"]
    manifest["identity"]["name"], manifest["description"] = "com.jellytoken.jellytool", "Jellytool"
    res["RT_VERSION"]["#1"]["0000"]["info"]["0409"].update(
        CompanyName="Jellytoken", FileDescription="Jellytoken助手", ProductName="Jellytoken助手",
        OriginalFilename="Jellytool.exe", LegalCopyright="Jellytoken")
    # go-winres reads a file the spec names relative to the spec
    spec = icon.parent / "winres.json"
    spec.write_text(json.dumps(res, ensure_ascii=False), encoding="utf-8")
    numeric = version if version != "dev" else "0.0.0"
    subprocess.run([
        str(go), "run", "github.com/tc-hib/go-winres@v0.3.3", "make", "--in", str(spec), "--arch", goarch,
        "--out", str(root / "rsrc"), "--file-version", numeric, "--product-version", numeric,
    ], cwd=root, env=env, check=True)
    return root / f"rsrc_windows_{goarch}.syso"


def copy_common_files(root, package_dir, resources):
    shutil.copyfile(root / "internal/gui/assets/jellytool-guide.md", resources / "使用说明.md")
    shutil.copyfile(root / "LICENSE", resources / "LICENSE")
    intro, separator, notices = (root / "THIRD_PARTY_NOTICES").read_text().partition("\n----------------")
    (resources / "THIRD_PARTY_NOTICES").write_text(intro.replace("magpie", "Jellytool") + separator + notices)


def codesign(path, identity, entitlements=None):
    command = ["codesign", "--force", "--options", "runtime", "--timestamp", "--sign", identity]
    if entitlements:
        command += ["--entitlements", str(entitlements)]
    subprocess.run(command + [str(path)], check=True)


def signed_bun_copy(bun, folder, identity):
    """Sign a copy of Bun, so the runtime lock the packer writes hashes the
    signed bytes. Re-signing the bundled bun after packing changes its digest
    and Jellytool refuses to start."""
    copy = Path(folder) / "bun"
    shutil.copyfile(bun, copy)
    copy.chmod(0o755)
    codesign(copy, identity, BUN_ENTITLEMENTS)
    return copy


def is_macho(path):
    with path.open("rb") as stream:
        head = stream.read(8)
    # 0xcafebabe is also a Java class; a fat Mach-O has a small arch count there.
    return head[:4] in MACHO_MAGIC and (head[:4] != b"\xca\xfe\xba\xbe" or int.from_bytes(head[4:8], "big") < 32)


def sign_app(app, identity):
    """Seal the bundle from the inside out. Bun was signed before packing and
    is left untouched; any other Mach-O in the resources is signed here."""
    bun = app / "Contents/Resources/jellytool/bun"
    subprocess.run(["codesign", "--verify", "--strict", str(bun)], check=True)
    for path in sorted((app / "Contents/Resources").rglob("*"), key=lambda p: -len(p.parts)):
        if path != bun and path.is_file() and not path.is_symlink() and is_macho(path):
            codesign(path, identity)
    codesign(app, identity)
    subprocess.run(["codesign", "--verify", "--deep", "--strict", str(app)], check=True)


def notarize(archive, profile):
    """Submit to Apple's notary service, staple the ticket and check Gatekeeper."""
    result = subprocess.run([
        "xcrun", "notarytool", "submit", str(archive), "--keychain-profile", profile,
        "--wait", "--timeout", "45m", "--output-format", "json",
    ], capture_output=True, text=True, timeout=50 * 60)
    try:
        reply = json.loads(result.stdout)
    except ValueError:
        reply = {}
    if reply.get("status") != "Accepted":
        detail = result.stderr.strip() or result.stdout.strip()
        if reply.get("id"):
            log = subprocess.run(["xcrun", "notarytool", "log", reply["id"], "--keychain-profile", profile],
                                 capture_output=True, text=True, timeout=120)
            detail = log.stdout.strip() or log.stderr.strip() or detail
        raise RuntimeError(f"notarization of {archive.name} ended {reply.get('status', 'without a status')}:\n{detail}")
    subprocess.run(["xcrun", "stapler", "staple", str(archive)], check=True)
    subprocess.run(["spctl", "--assess", "--type", "install", "-vv", str(archive)], check=True)


# The NSIS stub is 32-bit, so $PROGRAMFILES is "Program Files (x86)"; both
# Windows builds are 64-bit (x64, ARM64) and MultiUser puts a per-machine
# install in $PROGRAMFILES64. The script explains each step.
#
# The setup may run elevated (an administrator in either mode), so the
# finish page opens Jellytool through Explorer, as the signed-in user, not
# as administrator.
NSIS_TEMPLATE = r"""Unicode true
!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\Jellytool"
!define OLD_DIR "$LOCALAPPDATA\Programs\Jellytool"
!define PACKAGE_ARCH "@ARCH@"
; An administrator chooses: for every user in Program Files (the default, one
; UAC prompt at start) or for themself. A standard user gets no prompt and
; installs for themself under %LOCALAPPDATA%\Programs, which needs no admin.
; /AllUsers and /CurrentUser pick it for a silent install. The mode of an
; earlier install, and its folder, come from InstallLocation in HKLM or HKCU.
!define MULTIUSER_EXECUTIONLEVEL Highest
!define MULTIUSER_MUI
!define MULTIUSER_USE_PROGRAMFILES64
!define MULTIUSER_INSTALLMODE_COMMANDLINE
!define MULTIUSER_INSTALLMODE_INSTDIR "Jellytool"
!define MULTIUSER_INSTALLMODE_INSTDIR_REGISTRY_KEY "${UNINST_KEY}"
!define MULTIUSER_INSTALLMODE_INSTDIR_REGISTRY_VALUENAME "InstallLocation"
!define MULTIUSER_INSTALLMODE_DEFAULT_REGISTRY_KEY "${UNINST_KEY}"
!define MULTIUSER_INSTALLMODE_DEFAULT_REGISTRY_VALUENAME "InstallLocation"
!include "MultiUser.nsh"
!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "WinVer.nsh"
!include "x64.nsh"
!include "FileFunc.nsh"
Name "Jellytoken助手"
OutFile "@ARCHIVE@"
SetCompressor /SOLID lzma
@ICON_DEFINES@Var OldDir
!insertmacro MULTIUSER_PAGE_INSTALLMODE
!define MUI_PAGE_CUSTOMFUNCTION_LEAVE DirectoryLeave
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_TEXT "打开 Jellytoken助手"
!define MUI_FINISHPAGE_RUN_FUNCTION OpenApp
!insertmacro MUI_PAGE_FINISH
!define MUI_PAGE_CUSTOMFUNCTION_LEAVE un.ConfirmLeave
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "SimpChinese"

; Counts, into $0, the Jellytool.exe and Bun plugin host
; (jellytool-resources\bun.exe) processes run from the folders in DIRS
; ("|" between them), and with STOP "1" ends them first. Files can't be
; replaced or removed while they run, and MCP clients start Jellytool.exe
; on their own. Nothing else installed in or beside a folder is matched.
; The folders reach PowerShell through the environment, so a quote in one
; can't break the command; WMI's ExecutablePath is read because a 32-bit
; PowerShell's Get-Process can't see a 64-bit process's path. If
; PowerShell is blocked the count is 0, and a locked file still asks.
!macro RunStopScript DIRS STOP
  System::Call 'Kernel32::SetEnvironmentVariable(t "JELLYTOOL_DIRS", t "${DIRS}")'
  System::Call 'Kernel32::SetEnvironmentVariable(t "JELLYTOOL_STOP", t "${STOP}")'
  nsExec::ExecToStack `"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -Command "$$n=@(Get-CimInstance Win32_Process|?{$$p=$$_.ExecutablePath;$$p -and @($$env:JELLYTOOL_DIRS -split '\|'|?{$$_}|%{$$d=$$_.TrimEnd('\')+'\';$$p -ieq ($$d+'Jellytool.exe') -or $$p.StartsWith($$d+'jellytool-resources\',[StringComparison]::OrdinalIgnoreCase)}) -contains $$true});if($$env:JELLYTOOL_STOP){$$n|%{Stop-Process -Id $$_.ProcessId -Force -EA 0};Start-Sleep -Milliseconds 800};$$n.Count"`
  Pop $0
  Pop $0
!macroend

; Removes what an install put in DIR, asking again while a file stays locked
; (a process the stop missed, or a virus scanner still reading bun.exe).
; Only these names are removed, whatever else the folder holds.
!macro RemoveFrom DIR
  ${Do}
    Delete "${DIR}\Jellytool.exe"
    RMDir /r "${DIR}\jellytool-resources"
    ${IfNot} ${FileExists} "${DIR}\Jellytool.exe"
    ${AndIfNot} ${FileExists} "${DIR}\jellytool-resources\*.*"
      ${Break}
    ${EndIf}
    ${If} ${Cmd} `MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "Jellytoken助手 的文件仍被占用，无法删除：$\r$\n${DIR}$\r$\n$\r$\n请退出 Jellytoken助手（包括托盘图标），并在 Claude、WorkBuddy 等客户端中断开 jellytool-image，然后点「重试」。" /SD IDCANCEL IDCANCEL`
      Abort "仍有文件被占用：${DIR}"
    ${EndIf}
    !insertmacro RunStopScript "${DIR}" "1"
  ${Loop}
!macroend

; Warns about a system the app may not start on, and lets the user go on:
; a wrong guess must never stop an install. Go 1.26 and Bun 1.4 need 64-bit
; Windows 10 1809 (build 17763) or later. An ARM64 build runs only on ARM64;
; an x64 build also on Windows 11 ARM64, which emulates x64 (Windows 10 on
; ARM emulates 32-bit x86 only). At most one warning shows. A silent
; install (/S) goes on.
!macro WarnSystem TEXT
  ${If} ${Cmd} `MessageBox MB_YESNO|MB_ICONEXCLAMATION|MB_DEFBUTTON2 "${TEXT}$\r$\n$\r$\n仍要继续安装吗？" /SD IDYES IDNO`
    Abort
  ${EndIf}
!macroend

Function CheckSystem
  ${If} ${IsNativeIA32}
    !insertmacro WarnSystem "当前是 32 位 Windows。Jellytoken助手 是 64 位程序，装上后无法运行，需要 64 位的 Windows 10（1809 及以上）或 Windows 11。"
  ${ElseIfNot} ${AtLeastBuild} 17763
    !insertmacro WarnSystem "当前 Windows 版本较旧（低于 Windows 10 1809），Jellytoken助手 装上后可能无法打开，建议先更新 Windows。$\r$\n可按 Win+R 输入 winver 查看系统版本。"
  !if "${PACKAGE_ARCH}" == "ARM64"
  ${ElseIfNot} ${IsNativeARM64}
    !insertmacro WarnSystem "这是 ARM 电脑（如骁龙笔记本）用的安装包，当前电脑装上后无法运行，请改下载 Windows x64 安装包。"
  !else
  ${ElseIf} ${IsNativeARM64}
  ${AndIfNot} ${AtLeastWin11}
    !insertmacro WarnSystem "当前是 Windows 10 的 ARM 电脑，x64 安装包装上后无法运行，请改下载 Windows ARM64 安装包。"
  !endif
  ${EndIf}
FunctionEnd

Function .onInit
  Call CheckSystem
  SetRegView 64
  ; MultiUser sets the folder for its mode; one given with /D still wins.
  StrCpy $R0 $INSTDIR
  !insertmacro MULTIUSER_INIT
  ${If} $R0 != ""
    StrCpy $INSTDIR $R0
  ${EndIf}
FunctionEnd

Function OpenApp
  Exec '"$WINDIR\explorer.exe" "$INSTDIR\Jellytool.exe"'
FunctionEnd

; Sets $OldDir to another per-user install to move here, or "". Builds
; before the install-mode page went per user and wrote only HKCU's
; UninstallString ("<dir>\Uninstall.exe"), in a folder their user may have
; chosen; a per-user install in another folder is moved the same way.
Function FindOldDir
  ReadRegStr $OldDir HKCU "${UNINST_KEY}" "UninstallString"
  ${If} $OldDir != ""
    StrCpy $0 $OldDir 1
    ${If} $0 == '"'
      StrCpy $OldDir $OldDir "" 1
      StrCpy $OldDir $OldDir -1
    ${EndIf}
    ${GetParent} $OldDir $OldDir
  ${Else}
    StrCpy $OldDir "${OLD_DIR}"
  ${EndIf}
  ${If} $OldDir == $INSTDIR
  ${OrIfNot} ${FileExists} "$OldDir\Jellytool.exe"
    StrCpy $OldDir ""
  ${EndIf}
FunctionEnd

; Asked when the folder is confirmed, before anything changes; Cancel
; stays on the page.
Function DirectoryLeave
  ${If} $MultiUser.InstallMode == "CurrentUser"
    ReadRegStr $1 HKLM "${UNINST_KEY}" "InstallLocation"
    ${If} $1 != ""
    ${AndIf} $1 != $INSTDIR
      ${If} ${Cmd} `MessageBox MB_YESNO|MB_ICONEXCLAMATION|MB_DEFBUTTON2 "本机已经为所有用户安装了 Jellytoken助手：$\r$\n$1$\r$\n$\r$\n只为自己再装一份，开始菜单里会出现两个 Jellytoken助手。仍要继续吗？" /SD IDYES IDNO`
        Abort
      ${EndIf}
    ${EndIf}
  ${EndIf}
  Call FindOldDir
  !insertmacro RunStopScript "$INSTDIR|$OldDir" ""
  ${If} $0 > 0
    ${If} ${Cmd} `MessageBox MB_OKCANCEL|MB_ICONINFORMATION "安装前需要关闭正在运行的 Jellytoken助手。$\r$\nClaude、WorkBuddy 等客户端里的 jellytool-image 连接也会断开，装好后需重新连接。$\r$\n$\r$\n点「确定」关闭并继续安装。" /SD IDOK IDCANCEL`
      Abort
    ${EndIf}
  ${EndIf}
FunctionEnd

Section
  ; A silent install has no pages, so it stops here without asking.
  Call FindOldDir
  !insertmacro RunStopScript "$INSTDIR|$OldDir" "1"
  ; An upgrade starts from an empty folder, so no file a newer build dropped
  ; is left in jellytool-resources.
  !insertmacro RemoveFrom "$INSTDIR"
  ${If} $OldDir != ""
    !insertmacro RemoveFrom "$OldDir"
    Delete "$OldDir\Uninstall.exe"
    RMDir "$OldDir"
  ${EndIf}
  ${If} $MultiUser.InstallMode == "AllUsers"
    SetShellVarContext current
    Delete "$SMPROGRAMS\Jellytoken助手.lnk"
    Delete "$SMPROGRAMS\Jellytool.lnk"
    Delete "$DESKTOP\Jellytoken助手.lnk"
    DeleteRegKey HKCU "${UNINST_KEY}"
    SetShellVarContext all
  ${EndIf}
  SetOutPath "$INSTDIR"
  File /r "@SOURCE@/*"
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  Delete "$SMPROGRAMS\Jellytool.lnk"
  CreateShortcut "$SMPROGRAMS\Jellytoken助手.lnk" "$INSTDIR\Jellytool.exe"
  CreateShortcut "$DESKTOP\Jellytoken助手.lnk" "$INSTDIR\Jellytool.exe"
  WriteRegStr SHCTX "${UNINST_KEY}" "DisplayName" "Jellytoken助手"
  WriteRegStr SHCTX "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\Jellytool.exe"
  WriteRegStr SHCTX "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr SHCTX "${UNINST_KEY}" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegDWORD SHCTX "${UNINST_KEY}" "NoModify" 1
  WriteRegDWORD SHCTX "${UNINST_KEY}" "NoRepair" 1
SectionEnd

; The uninstaller removes the install it sits in: per machine when HKLM's
; InstallLocation names its folder, which needs an administrator, else per
; user. MultiUser's own pick would follow the account, not the folder.
Function un.onInit
  SetRegView 64
  StrCpy $R0 $INSTDIR
  !insertmacro MULTIUSER_UNINIT
  ReadRegStr $R1 HKLM "${UNINST_KEY}" "InstallLocation"
  ${If} $R1 == $R0
    ${If} $MultiUser.Privileges != "Admin"
    ${AndIf} $MultiUser.Privileges != "Power"
      MessageBox MB_OK|MB_ICONSTOP "Jellytoken助手 是为本机所有用户安装的，卸载需要管理员权限，请用管理员账户卸载。" /SD IDOK
      Quit
    ${EndIf}
    Call un.MultiUser.InstallMode.AllUsers
  ${Else}
    Call un.MultiUser.InstallMode.CurrentUser
  ${EndIf}
  StrCpy $INSTDIR $R0
FunctionEnd

Function un.ConfirmLeave
  !insertmacro RunStopScript "$INSTDIR" ""
  ${If} $0 > 0
    ${If} ${Cmd} `MessageBox MB_OKCANCEL|MB_ICONINFORMATION "卸载前需要关闭正在运行的 Jellytoken助手。$\r$\nClaude、WorkBuddy 等客户端里的 jellytool-image 连接也会断开。$\r$\n$\r$\n点「确定」关闭并继续卸载。" /SD IDOK IDCANCEL`
      Abort
    ${EndIf}
  ${EndIf}
FunctionEnd

Section "Uninstall"
  !insertmacro RunStopScript "$INSTDIR" "1"
  !insertmacro RemoveFrom "$INSTDIR"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"
  Delete "$SMPROGRAMS\Jellytoken助手.lnk"
  Delete "$SMPROGRAMS\Jellytool.lnk"
  Delete "$DESKTOP\Jellytoken助手.lnk"
  DeleteRegKey SHCTX "${UNINST_KEY}"
SectionEnd
"""


def nsis_script(archive, source, arch, icon=None):
    """icon is the app's .ico (build_target writes it) for the setup and
    uninstaller; without one they keep NSIS's own."""
    if arch not in ("x64", "ARM64"):
        raise ValueError(f"no Windows installer for {arch}")
    icons = f'!define MUI_ICON "{icon}"\n!define MUI_UNICON "{icon}"\n' if icon else ""
    return (NSIS_TEMPLATE.replace("@ARCHIVE@", str(archive)).replace("@SOURCE@", str(source))
            .replace("@ARCH@", arch).replace("@ICON_DEFINES@", icons))


def archive_package(package_dir, target):
    if target.startswith("darwin/"):
        (package_dir / "Applications").symlink_to("/Applications", target_is_directory=True)
        archive = package_dir.with_suffix(".dmg")
        subprocess.run([
            "hdiutil", "create", "-volname", "Jellytoken助手 - 拖入 Applications 安装",
            "-srcfolder", str(package_dir), "-size", "512m", "-format", "UDZO", str(archive),
        ], check=True)
    elif target.startswith("windows/"):
        archive = package_dir.parent / (package_dir.name + "-Setup.exe")
        script = package_dir.parent / (package_dir.name + ".nsi")
        icon = package_dir.parent / (package_dir.name + ".ico")
        script.write_text(nsis_script(archive, package_dir, "ARM64" if target.endswith("/arm64") else "x64",
                                      icon if icon.exists() else None), encoding="utf-8-sig")
        try:
            subprocess.run(["makensis", str(script)], check=True)
        finally:
            script.unlink()
            icon.unlink(missing_ok=True)
    else:
        archive = package_dir.with_suffix(".zip")
        shutil.make_archive(str(archive.with_suffix("")), "zip", root_dir=package_dir.parent, base_dir=package_dir.name)
    shutil.rmtree(package_dir)
    return archive.name


def build_target(root, go, target, bun, staging, args, env, host):
    goos, goarch = target.split("/")
    name = package_name(target)
    package_dir = staging / name
    package_dir.mkdir()
    if goos == "darwin":
        app = package_dir / "Jellytool.app"
        binary = app / "Contents/MacOS/jellytool"
        resources = app / "Contents/Resources/jellytool"
    else:
        app = package_dir
        binary = package_dir / ("Jellytool.exe" if goos == "windows" else "jellytool")
        resources = package_dir / "jellytool-resources"
    binary.parent.mkdir(parents=True, exist_ok=True)
    resources.mkdir(parents=True, exist_ok=True)
    ldflags = f"-X main.version={args.version}"
    if args.update_feed:
        ldflags += f" -X github.com/yetone/magpie/internal/update.JellytoolFeed={args.update_feed}"
    if goos == "windows":
        ldflags += " -H windowsgui"
    build_env = dict(env, GOOS=goos, GOARCH=goarch, CGO_ENABLED="0" if goos == "windows" else "1")
    if goos == "darwin":
        # Without a target clang builds cgo objects for the SDK's own macOS
        # version, which may call APIs missing from older systems.
        build_env["MACOSX_DEPLOYMENT_TARGET"] = MACOS_MIN
        for variable in ("CGO_CFLAGS", "CGO_LDFLAGS"):
            build_env[variable] = (env.get(variable, "-O2 -g") + f" -mmacosx-version-min={MACOS_MIN}").strip()
    tags = ["jellytool", "production"]
    if goos == "linux":
        tags.append("gtk3")
    syso = None
    try:
        if goos == "windows":
            # one .ico for the exe and, beside the package, the setup
            # (archive_package)
            icon = staging / (name + ".ico")
            subprocess.run([str(go), "run", "build/jellytool/ico.go", "internal/gui/assets/icons/jellytool-app.png", str(icon)],
                           cwd=root, env=env, check=True)
            with tempfile.TemporaryDirectory(prefix="jellytool-winres-") as winres:
                shutil.copyfile(icon, Path(winres) / "jellytool.ico")
                syso = windows_resources(root, go, goarch, Path(winres) / "jellytool.ico", args.version, env)
        subprocess.run([
            str(go), "build", "-tags", ",".join(tags), "-trimpath", "-ldflags", ldflags,
            "-o", str(binary), ".",
        ], cwd=root, env=build_env, check=True)
    finally:
        if syso is not None:
            syso.unlink(missing_ok=True)
    sign = goos == "darwin" and args.sign_identity
    with tempfile.TemporaryDirectory(prefix="jellytool-bun-") as signing:
        packed_bun = signed_bun_copy(bun.resolve(strict=True), signing, args.sign_identity) if sign else bun.resolve(strict=True)
        pack = [str(go), "run", "./cmd/jellytool-pack", str(packed_bun), str(resources)]
        if target != host:
            pack += ["--target", target]
        pack_env = dict(env)
        for variable in ("GOOS", "GOARCH", "CGO_ENABLED"):
            pack_env.pop(variable, None)
        subprocess.run(pack, cwd=root, env=pack_env, check=True)
    copy_common_files(root, package_dir, resources)
    if goos == "darwin":
        stage_icon(root, app / "Contents/Resources")
        # Finder shows the bundle's file name unless the bundle names itself
        # per language. macOS uses it only while the file name matches the
        # plain CFBundleDisplayName, so both stay Jellytool (the path updates use).
        for lang in ["Base", "en", "zh-Hans", "zh_CN", "zh-Hant", "zh_TW", "ja", "de"]:
            strings = app / "Contents/Resources" / f"{lang}.lproj/InfoPlist.strings"
            strings.parent.mkdir(parents=True, exist_ok=True)
            strings.write_text('CFBundleName = "Jellytoken助手";\nCFBundleDisplayName = "Jellytoken助手";\n', encoding="utf-8")
        import plistlib
        with (app / "Contents/Info.plist").open("wb") as stream:
            plistlib.dump({
                "CFBundleName": "Jellytool", "CFBundleDisplayName": "Jellytool",
                "CFBundleIdentifier": "com.jellytoken.jellytool.dev", "CFBundleExecutable": "jellytool",
                "CFBundleIconFile": "jellytool.icns", "CFBundlePackageType": "APPL",
                "CFBundleShortVersionString": args.version if args.version != "dev" else "0.0.0",
                "CFBundleVersion": args.build or (args.version if args.version != "dev" else "0.0.0"),
                "LSMinimumSystemVersion": MACOS_MIN, "NSHighResolutionCapable": True, "LSHasLocalizedDisplayName": True,
            }, stream)
        if sign:
            sign_app(app, args.sign_identity)
    name = archive_package(package_dir, target)
    if sign:
        archive = staging / name
        subprocess.run(["codesign", "--force", "--timestamp", "--sign", args.sign_identity, str(archive)], check=True)
        if args.notary_profile:
            notarize(archive, args.notary_profile)
    return name


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", type=Path, help="Go executable (otherwise PATH, then mise installations)")
    parser.add_argument("--bun", action="append", default=[], metavar="PATH|TARGET=PATH", help="Bun 1.4.2; repeat for cross targets")
    parser.add_argument("--target", action="append", help="target such as darwin/arm64 (repeatable; defaults to host plus Windows)")
    parser.add_argument("--output", type=Path, required=True, help="new directory for the target archives")
    parser.add_argument("--version", default="dev", help="Jellytool release version")
    parser.add_argument("--build", default="", help="numeric build marker for a local versioned artifact")
    parser.add_argument("--update-feed", default="", help="HTTPS URL of the Jellytool update JSON")
    parser.add_argument("--sign-identity", default=os.environ.get("JELLYTOOL_SIGN_IDENTITY", ""),
                        help='macOS: "Developer ID Application: …" certificate to sign the app and DMG (env JELLYTOOL_SIGN_IDENTITY)')
    parser.add_argument("--notary-profile", default=os.environ.get("JELLYTOOL_NOTARY_PROFILE", ""),
                        help="macOS: notarytool keychain profile; notarizes and staples each DMG (env JELLYTOOL_NOTARY_PROFILE)")
    args = parser.parse_args(argv)
    try:
        validate_options(args)
        host = f"{host_goos()}/{host_goarch()}"
        if not TARGET_RE.fullmatch(host):
            raise ValueError("multi-platform packaging requires a supported macOS, Linux or Windows host")
        targets = args.target or [host] + [t for t in ("windows/amd64", "windows/arm64") if t != host]
        targets = list(dict.fromkeys(targets))
        for target in targets:
            if not TARGET_RE.fullmatch(target):
                raise ValueError(f"unsupported target: {target}")
            if target.split("/")[0] not in (host_goos(), "windows"):
                raise ValueError(f"{target} needs a native {target.split('/')[0]} build host")
        mapped, plain = parse_bun_options(args.bun)
        root = Path(__file__).resolve().parents[2]
        mise = Path.home() / ".local/share/mise/installs/go"
        go = find_executable([args.go] if args.go else [shutil.which("go"), mise / "latest/bin/go", *sorted(mise.glob("*/bin/go"), reverse=True)], ["version"])
        output = args.output.resolve()
        if output.exists():
            raise ValueError(f"output already exists: {output}")
        output.parent.mkdir(parents=True, exist_ok=True)
        staging = Path(tempfile.mkdtemp(prefix="." + output.name + "-", dir=output.parent))
        env = dict(os.environ, PATH=str(go.parent) + os.pathsep + os.environ.get("PATH", ""))
        try:
            print(f"Building {', '.join(targets)} in {output}", flush=True)
            for target in targets:
                bun = target_bun(target, host, mapped, plain)
                print(f"  {target} (Bun: {bun})", flush=True)
                build_target(root, go, target, bun, staging, args, env, host)
            staging.rename(output)
        except BaseException:
            shutil.rmtree(staging)
            raise
    except (OSError, RuntimeError, ValueError, subprocess.SubprocessError) as error:
        print(f"jellytool-package-multi: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
