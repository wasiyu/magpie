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
        script.write_text(r'''Unicode true
!include "MUI2.nsh"
Name "Jellytoken助手"
OutFile "ARCHIVE"
InstallDir "$LOCALAPPDATA\Programs\Jellytool"
RequestExecutionLevel user
SetCompressor /SOLID lzma
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "SimpChinese"
Section
SetOutPath "$INSTDIR"
File /r "SOURCE/*"
WriteUninstaller "$INSTDIR\Uninstall.exe"
Delete "$SMPROGRAMS\Jellytool.lnk"
CreateShortcut "$SMPROGRAMS\Jellytoken助手.lnk" "$INSTDIR\Jellytool.exe"
WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Jellytool" "DisplayName" "Jellytoken助手"
WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Jellytool" "UninstallString" '$\"$INSTDIR\Uninstall.exe$\"'
SectionEnd
Section "Uninstall"
Delete "$SMPROGRAMS\Jellytoken助手.lnk"
Delete "$SMPROGRAMS\Jellytool.lnk"
Delete "$INSTDIR\Jellytool.exe"
Delete "$INSTDIR\Uninstall.exe"
RMDir /r "$INSTDIR\jellytool-resources"
RMDir "$INSTDIR"
DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Jellytool"
SectionEnd
'''.replace("ARCHIVE", str(archive)).replace("SOURCE", str(package_dir)), encoding="utf-8-sig")
        try:
            subprocess.run(["makensis", str(script)], check=True)
        finally:
            script.unlink()
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
    subprocess.run([
        str(go), "build", "-tags", ",".join(tags), "-trimpath", "-ldflags", ldflags,
        "-o", str(binary), ".",
    ], cwd=root, env=build_env, check=True)
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
