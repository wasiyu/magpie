#!/usr/bin/env python3
"""Package the current checkout and replace the running Jellytool on macOS."""

import argparse
from datetime import datetime
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile


def executable(candidates, version_args, expected=None):
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
    raise RuntimeError("No usable runtime found; specify --go and --bun (Bun 1.4.2 is required).")


def install_app(app):
    """Copy a completed build to the stable user Applications location."""
    applications = Path.home() / "Applications"
    applications.mkdir(parents=True, exist_ok=True)
    target = applications / "Jellytool.app"
    staging = Path(tempfile.mkdtemp(prefix=".jellytool-install-", dir=applications))
    staged = staging / target.name
    backup = staging / "old"
    try:
        shutil.copytree(app, staged)
        if target.exists() or target.is_symlink():
            target.rename(backup)
        try:
            staged.rename(target)
        except Exception:
            if backup.exists():
                backup.rename(target)
            raise
        return target
    finally:
        shutil.rmtree(staging, ignore_errors=True)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", type=Path, help="Go executable (otherwise PATH, then mise installations)")
    parser.add_argument("--bun", type=Path, help="Bun 1.4.2 executable (otherwise previous builds, then PATH)")
    parser.add_argument("--version", default="dev", help="Jellytool release version (default: dev)")
    parser.add_argument("--update-feed", default="", help="HTTPS URL of the Jellytool update JSON")
    args = parser.parse_args(argv)
    if sys.platform != "darwin":
        parser.error("this command requires macOS")
    root = Path(__file__).resolve().parents[2]
    artifacts = Path.home() / ".cache/jellytool-build/artifacts"
    mise = Path.home() / ".local/share/mise/installs/go"
    go_candidates = [shutil.which("go"), mise / "latest/bin/go", *sorted(mise.glob("*/bin/go"), reverse=True)]
    go = executable([args.go] if args.go else go_candidates, ["version"])
    bundled = sorted(artifacts.glob("*/Jellytool.app/Contents/Resources/jellytool/bun"), key=lambda p: p.stat().st_mtime, reverse=True)
    bun = executable([args.bun] if args.bun else [*bundled, shutil.which("bun")], ["--version"], "1.4.2")
    artifacts.mkdir(parents=True, exist_ok=True)
    folder = Path(tempfile.mkdtemp(prefix="jellytool-" + datetime.now().strftime("%Y%m%d-%H%M%S") + "-", dir=artifacts))
    app = folder / "Jellytool.app"
    env = dict(os.environ, PATH=str(go.parent) + os.pathsep + os.environ.get("PATH", ""))
    print(f"Building {app}\nGo: {go}\nBun: {bun}", flush=True)
    subprocess.run([sys.executable, str(root / "build/jellytool/package.py"), "--bun", str(bun), "--output", str(app), "--version", args.version, "--update-feed", args.update_feed], cwd=root, env=env, check=True)
    # A failed build leaves the current app running. The existing launcher
    # owns shutdown, the instance lock and native readiness checks.
    installed = install_app(app)
    subprocess.run([sys.executable, str(root / "build/jellytool/launch.py"), str(installed)], cwd=root, env=env, check=True)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, RuntimeError, subprocess.SubprocessError) as error:
        print(f"jellytool-run: {error}", file=sys.stderr)
        raise SystemExit(1)
