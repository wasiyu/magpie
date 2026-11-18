#!/usr/bin/env python3
"""Build Jellytool archives for several OS/architecture targets."""

import argparse
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


def archive_package(package_dir, target):
    if target.startswith("darwin/"):
        (package_dir / "Applications").symlink_to("/Applications", target_is_directory=True)
        archive = package_dir.with_suffix(".dmg")
        subprocess.run([
            "hdiutil", "create", "-volname", "Jellytool - 拖入 Applications 安装",
            "-srcfolder", str(package_dir), "-size", "512m", "-format", "UDZO", str(archive),
        ], check=True)
    elif target.startswith("windows/"):
        archive = package_dir.parent / (package_dir.name + "-Setup.exe")
        script = package_dir.parent / (package_dir.name + ".nsi")
        script.write_text(r'''Unicode true
!include "MUI2.nsh"
Name "Jellytool"
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
CreateShortcut "$SMPROGRAMS\Jellytool.lnk" "$INSTDIR\Jellytool.exe"
WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Jellytool" "DisplayName" "Jellytool"
WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Jellytool" "UninstallString" '$\"$INSTDIR\Uninstall.exe$\"'
SectionEnd
Section "Uninstall"
Delete "$SMPROGRAMS\Jellytool.lnk"
Delete "$INSTDIR\Jellytool.exe"
Delete "$INSTDIR\Uninstall.exe"
RMDir /r "$INSTDIR\jellytool-resources"
RMDir "$INSTDIR"
DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Jellytool"
SectionEnd
'''.replace("ARCHIVE", str(archive)).replace("SOURCE", str(package_dir)))
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
    tags = ["jellytool", "production"]
    if goos == "linux":
        tags.append("gtk3")
    subprocess.run([
        str(go), "build", "-tags", ",".join(tags), "-trimpath", "-ldflags", ldflags,
        "-o", str(binary), ".",
    ], cwd=root, env=build_env, check=True)
    pack = [str(go), "run", "./cmd/jellytool-pack", str(bun.resolve(strict=True)), str(resources)]
    if target != host:
        pack += ["--target", target]
    pack_env = dict(env)
    for variable in ("GOOS", "GOARCH", "CGO_ENABLED"):
        pack_env.pop(variable, None)
    subprocess.run(pack, cwd=root, env=pack_env, check=True)
    copy_common_files(root, package_dir, resources)
    if goos == "darwin":
        stage_icon(root, app / "Contents/Resources")
        import plistlib
        with (app / "Contents/Info.plist").open("wb") as stream:
            plistlib.dump({
                "CFBundleName": "Jellytool", "CFBundleDisplayName": "Jellytool",
                "CFBundleIdentifier": "com.jellytoken.jellytool.dev", "CFBundleExecutable": "jellytool",
                "CFBundleIconFile": "jellytool.icns", "CFBundlePackageType": "APPL",
                "CFBundleShortVersionString": args.version if args.version != "dev" else "0.0.0",
                "CFBundleVersion": args.build or (args.version if args.version != "dev" else "0.0.0"),
                "LSMinimumSystemVersion": "12.0", "NSHighResolutionCapable": True,
            }, stream)
    return archive_package(package_dir, target)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", type=Path, help="Go executable (otherwise PATH, then mise installations)")
    parser.add_argument("--bun", action="append", default=[], metavar="PATH|TARGET=PATH", help="Bun 1.4.2; repeat for cross targets")
    parser.add_argument("--target", action="append", help="target such as darwin/arm64 (repeatable; defaults to host plus Windows)")
    parser.add_argument("--output", type=Path, required=True, help="new directory for the target archives")
    parser.add_argument("--version", default="dev", help="Jellytool release version")
    parser.add_argument("--build", default="", help="numeric build marker for a local versioned artifact")
    parser.add_argument("--update-feed", default="", help="HTTPS URL of the Jellytool update JSON")
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
