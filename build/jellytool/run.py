#!/usr/bin/env python3
"""Package the current checkout and replace the running Jellytool on macOS."""

import argparse
from datetime import datetime
import fcntl
import importlib.util
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

spec = importlib.util.spec_from_file_location("jellytool_launcher", Path(__file__).with_name("launch.py"))
launcher = importlib.util.module_from_spec(spec)
spec.loader.exec_module(launcher)

LSREGISTER = launcher.LSREGISTER


def background_processes(target):
    """Loaded executables, including renamed/deleted install backups, not argv[0]."""
    gui = launcher.running("com.jellytoken.jellytool.dev")
    result = subprocess.run(
        ["/usr/sbin/lsof", "-n", "-P", "-a", "-u", str(os.getuid()), "-d", "txt", "-Fpn"],
        capture_output=True, text=True, check=True, timeout=10,
    )
    found = {}
    pid = None
    for line in result.stdout.splitlines():
        if line.startswith("p"):
            pid = int(line[1:])
        elif line.startswith("n") and pid is not None and pid not in gui:
            path = Path(line[1:])
            try:
                relative = path.relative_to(target.parent)
            except ValueError:
                continue
            if path.parts[-3:] == ("Contents", "MacOS", "jellytool") and (
                relative.parts[0] == target.name
                or relative.parts[0].startswith((".jellytool-install-", "Jellytool.app.backup-"))
            ):
                found[pid] = str(path)
    return found


def stop_clients(target, stale_only=False):
    def inventory():
        processes = background_processes(target)
        if stale_only:
            current = str(target / "Contents/MacOS/jellytool")
            processes = {pid: path for pid, path in processes.items() if path != current}
        return processes

    processes = inventory()
    if processes:
        print(
            "update: closing old Jellytool MCP connections; in-flight image/video tasks may be interrupted.",
            flush=True,
        )
        launcher.end(processes, False)
        if inventory():
            raise RuntimeError("Jellytool MCP clients restarted during the update; close their connections and retry, then reconnect")
    return bool(processes)


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


def install_app(app, start=None):
    """Keep the old bundle until the fixed-location replacement is verified."""
    applications = Path.home() / "Applications"
    applications.mkdir(parents=True, exist_ok=True)
    target = applications / "Jellytool.app"
    with (applications / ".jellytool-update.lock").open("a+") as guard:
        try:
            fcntl.flock(guard, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise RuntimeError("another Jellytool update is already in progress") from None
        return replace_app(app, target, start)


def replace_app(app, target, start):
    staging = Path(tempfile.mkdtemp(prefix=".jellytool-install-", suffix=".noindex", dir=target.parent))
    staged = staging / target.name
    backup = staging / "old"
    replaced = False
    preserve = False
    try:
        shutil.copytree(app, staged)
        # Copy first: build/copy failures must not disconnect working clients.
        closed_clients = stop_clients(target)
        try:
            if target.exists() or target.is_symlink():
                target.rename(backup)
            staged.rename(target)
            replaced = True
            if start is not None:
                start(target)
            # An external client may have started between inspection and rename.
            closed_clients = stop_clients(target, stale_only=True) or closed_clients
            if start is not None:
                launcher.unregister(app)
                subprocess.run([LSREGISTER, "-f", str(target)], check=True, capture_output=True, text=True, timeout=10)
        except BaseException:
            preserve = True
            try:
                if replaced:
                    # Do not leave the failed new GUI using files we are restoring.
                    if start is not None:
                        launcher.end(launcher.running(launcher.bundle_id(target)), False)
                    target.rename(staging / "failed")
                if backup.exists() or backup.is_symlink():
                    backup.rename(target)
                    if start is not None:
                        subprocess.run([LSREGISTER, "-f", str(target)], check=True, capture_output=True, text=True, timeout=10)
                        print(f"update: restored {target}; reopen the app and reconnect its MCP clients.", flush=True)
                # A racing client may still hold the failed copy after rollback.
                preserve = bool(background_processes(target))
            except BaseException as rollback:
                preserve = True
                saved = backup if backup.exists() or backup.is_symlink() else target
                raise RuntimeError(f"update and rollback failed; recovery files retained at {saved} and {staging}: {rollback}") from rollback
            raise
        if closed_clients:
            print(
                f"update: installed {target}. Reconnect Jellytool MCP clients now "
                "(Claude: /mcp; WorkBuddy: toggle jellytool-image off/on).", flush=True,
            )
        return target
    finally:
        if not preserve:
            shutil.rmtree(staging)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", type=Path, help="Go executable (otherwise PATH, then mise installations)")
    parser.add_argument("--bun", type=Path, help="Bun 1.4.2 executable (otherwise previous builds, then PATH)")
    parser.add_argument("--version", default="dev", help="Jellytool release version (default: dev)")
    parser.add_argument("--build", default="", help="numeric build marker for a local versioned artifact")
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
    bundled.insert(0, Path.home() / "Applications/Jellytool.app/Contents/Resources/jellytool/bun")
    bun = executable([args.bun] if args.bun else [*bundled, shutil.which("bun")], ["--version"], "1.4.2")
    artifacts.mkdir(parents=True, exist_ok=True)
    folder = Path(tempfile.mkdtemp(prefix="jellytool-" + datetime.now().strftime("%Y%m%d-%H%M%S") + "-", suffix=".noindex", dir=artifacts))
    app = folder / "Jellytool.app"
    env = dict(os.environ, PATH=str(go.parent) + os.pathsep + os.environ.get("PATH", ""))
    print(f"Building {app}\nGo: {go}\nBun: {bun}", flush=True)
    subprocess.run([sys.executable, str(root / "build/jellytool/package.py"), "--bun", str(bun), "--output", str(app), "--version", args.version, "--build", args.build, "--update-feed", args.update_feed], cwd=root, env=env, check=True)
    # A failed build leaves the current app running. The existing launcher
    # owns shutdown, the instance lock and native readiness checks.
    def start(installed):
        subprocess.run([sys.executable, str(root / "build/jellytool/launch.py"), str(installed)], cwd=root, env=env, check=True)
    install_app(app, start)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, RuntimeError, subprocess.SubprocessError) as error:
        print(f"jellytool-run: {error}", file=sys.stderr)
        raise SystemExit(1)
