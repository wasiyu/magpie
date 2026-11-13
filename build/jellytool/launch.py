#!/usr/bin/env python3
"""Open a local Jellytool build, replacing the instance already running.

Jellytool holds a single-instance lock in its data root, so `open -n` --
the way a *second* instance is asked for -- starts a Jellytool that finds
the lock taken and exits before it draws anything. The click looks like
"the app will not start", and every retry fails the same way, while a
Jellytool from an older build keeps the lock.

This opens a build the way a person means it: end whatever Jellytool is
still up, wait for the lock to be free, then ask LaunchServices for this
build once (`open`, without `-n`) and confirm that it really came up, so
a failed launch is reported instead of assumed.
"""

import argparse
import errno
import fcntl
import os
import plistlib
import re
import signal
import subprocess
import sys
import time
from pathlib import Path

LSAPPINFO = "/usr/bin/lsappinfo"

TERM_WAIT = 5.0  # seconds a Jellytool gets to quit on SIGTERM
LOCK_WAIT = 5.0  # seconds to wait for the instance lock to be released
START_WAIT = 15.0  # seconds to wait for the new build to show up
OPEN_TRIES = 3
STABLE_WAIT = 1.0  # the ready process must remain alive through this interval


def die(message: str):
    print(f"launch: {message}", file=sys.stderr)
    raise SystemExit(1)


def bundle_id(app: Path) -> str:
    info = app / "Contents" / "Info.plist"
    if not info.is_file():
        die(f"{app} is not an app bundle (no Contents/Info.plist)")
    with info.open("rb") as handle:
        metadata = plistlib.load(handle)
    ident = metadata.get("CFBundleIdentifier")
    if not ident:
        die(f"{info} has no CFBundleIdentifier")
    executable = metadata.get("CFBundleExecutable", "")
    if not executable or not os.access(app / "Contents" / "MacOS" / executable, os.X_OK):
        die(f"{info} has no runnable CFBundleExecutable")
    return ident


def running(ident: str) -> dict[int, str]:
    """The PIDs and bundle paths of every running instance of ident."""
    out = subprocess.run(
        [LSAPPINFO, "list"], capture_output=True, text=True, check=True, timeout=5
    ).stdout
    found: dict[int, str] = {}
    for block in re.split(r"\n(?=\s*\d+\) )", out):
        got = re.search(r'bundleID="?([^"\s]+)"?', block)
        if not got or got.group(1) != ident:
            continue
        pid = re.search(r"pid = (\d+)", block)
        if not pid:
            continue
        path = re.search(r'bundle path="([^"]+)"', block)
        found[int(pid.group(1))] = path.group(1) if path else "?"
    return found


def alive(pids) -> list[int]:
    live = []
    for pid in pids:
        try:
            os.kill(pid, 0)
        except ProcessLookupError:
            continue
        except PermissionError:
            live.append(pid)
            continue
        live.append(pid)
    return live


def wait_gone(pids, timeout: float) -> list[int]:
    deadline = time.monotonic() + timeout
    while True:
        live = alive(pids)
        if not live or time.monotonic() >= deadline:
            return live
        time.sleep(0.1)


def end(instances: dict[int, str], dry: bool) -> None:
    """Quit every instance, SIGKILL the ones that will not quit."""
    if not instances:
        return
    for pid, path in sorted(instances.items()):
        print(f"launch: stopping Jellytool pid {pid} ({path})")
        if dry:
            continue
        try:
            os.kill(pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
    if dry:
        return
    left = wait_gone(instances, TERM_WAIT)
    for pid in left:
        print(f"launch: pid {pid} ignored SIGTERM; killing it")
        try:
            os.kill(pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
    left = wait_gone(left, 3.0)
    if left:
        die(f"could not stop pid(s) {', '.join(map(str, left))}")


def lock_path(app: Path | None = None) -> Path:
    base = Path(os.environ.get("XDG_CONFIG_HOME", ""))
    if not base.is_absolute():
        base = Path.home() / ".config"
    config = base / "jellytool"
    if app is not None:
        beside = app.parent
        data = beside / "jellytool-data"
        marker = beside / ".jellytool-portable"
        shared = {Path("/Applications"), Path.home(), Path.home() / "Applications"}
        shared.update(Path.home() / name for name in ("Downloads", "Desktop", "Documents"))

        def has_files(folder):
            return any((folder / name).is_file() for name in ("providers.json", "logins.json", "settings.json"))

        # Match appdir.Resolve: a shared folder's data needs existing state
        # and no installed state, unless the portable marker opts in.
        if (marker.exists() and not marker.is_dir()) or (
            data.is_dir() and (beside not in shared or has_files(data) and not has_files(config))
        ):
            config = data
    return config / "jellytool-instance.lock"


def holders(path: Path) -> dict[int, str]:
    """Identify processes with this lock file open, including other bundle IDs."""
    result = subprocess.run(
        ["/usr/sbin/lsof", "-t", str(path)], capture_output=True, text=True, timeout=5
    )
    if result.returncode not in (0, 1):
        die(f"cannot identify owners of {path}: {result.stderr.strip()}")
    found = {}
    for value in result.stdout.split():
        pid = int(value)
        if pid == os.getpid():
            continue
        command = subprocess.run(
            ["/bin/ps", "-p", str(pid), "-o", "comm="],
            capture_output=True, text=True, timeout=5,
        ).stdout.strip()
        if command:
            found[pid] = command
    return found


def wait_lock_free(path: Path, timeout: float, dry: bool) -> bool:
    """Whether path can be locked now -- i.e. no Jellytool is holding it."""
    if dry:
        return True
    deadline = time.monotonic() + timeout
    while True:
        try:
            with path.open("r+") as handle:
                try:
                    fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
                except OSError as error:
                    if error.errno not in (errno.EAGAIN, errno.EACCES):
                        raise
                else:
                    fcntl.flock(handle, fcntl.LOCK_UN)
                    return True
        except FileNotFoundError:
            return True
        except OSError as error:
            die(f"cannot check instance lock {path}: {error}")
        if time.monotonic() >= deadline:
            return False
        time.sleep(0.1)


def finished_launching(pid: int) -> bool:
    # LaunchServices lists a PID before main() runs. AppKit's readiness flag
    # changes only after native application initialization has completed.
    script = '''ObjC.import("AppKit"); function run(argv) {
        var a = $.NSRunningApplication.runningApplicationWithProcessIdentifier(Number(argv[0]));
        return Boolean(a.finishedLaunching) && !Boolean(a.terminated);
    }'''
    result = subprocess.run(
        ["/usr/bin/osascript", "-l", "JavaScript", "-e", script, str(pid)],
        capture_output=True, text=True, check=True, timeout=5,
    )
    return result.stdout.strip() == "true"


def open_build(app: Path, args: list[str], dry: bool, log: Path) -> subprocess.CompletedProcess:
    command = ["open", str(app)]
    command += ["--stdout", str(log), "--stderr", str(log)]
    # Keep the launched app's data root consistent with the lock we inspect.
    for name in ("HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "MAGPIE_ADDR"):
        if name in os.environ:
            command += ["--env", f"{name}={os.environ[name]}"]
    if args:
        command += ["--args", *args]
    print("launch: " + " ".join(command))
    if dry:
        return subprocess.CompletedProcess(command, 0, "", "")
    return subprocess.run(command, capture_output=True, text=True, check=False, timeout=10)


def launch(app: Path, ident: str, lock: Path, args: list[str], timeout: float, dry: bool) -> int:
    log = lock.with_suffix(".launch.log")
    before = running(ident)
    if not wait_lock_free(lock, 0, dry):
        # Only stop Jellytool owners of this exact file; unrelated lock users
        # are reported, never killed. This also covers another bundle ID.
        before.update({pid: exe for pid, exe in holders(lock).items() if Path(exe).name == "jellytool"})
    if not dry:
        # Check the output destination before stopping a working instance.
        with os.fdopen(os.open(log, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600), "w"):
            pass
    end(before, dry)
    if not wait_lock_free(lock, LOCK_WAIT, dry):
        owners = ", ".join(f"{pid} ({exe})" for pid, exe in holders(lock).items())
        die(f"{lock} is still held: {owners or 'owner could not be identified'}")

    def failed(reason):
        details = log.read_text(errors="replace")[-8000:].strip() if log.is_file() else ""
        die(f"{reason}\nlaunch log: {log}" + (f"\n{details}" if details else ""))

    for attempt in range(1, OPEN_TRIES + 1):
        result = open_build(app, args, dry, log)
        if dry:
            return 0
        if result.returncode == 0:
            break
        message = (result.stderr or result.stdout).strip()
        if "-600" not in message or attempt == OPEN_TRIES:
            failed(f"open failed: {message or result.returncode}")
        print(f"launch: LaunchServices is retiring the old process; retry {attempt}/{OPEN_TRIES}")
        time.sleep(0.5 * attempt)

    deadline = time.monotonic() + timeout
    seen = {}
    ready_since = None
    ready_pid = None
    while time.monotonic() < deadline:
        now = running(ident)
        here = {pid: path for pid, path in now.items() if pid not in before and Path(path).resolve() == app}
        seen.update(here)
        live = alive(here)
        if seen and not alive(seen):
            failed("Jellytool exited during startup; no automatic relaunch")
        pid = next(iter(live), None)
        if pid is not None and finished_launching(pid) and not wait_lock_free(lock, 0, False):
            if ready_pid != pid:
                ready_pid, ready_since = pid, time.monotonic()
            if time.monotonic() - ready_since >= STABLE_WAIT and alive([pid]):
                print(f"launch: {app.name} is ready as pid {pid}; log: {log}")
                return 0
        else:
            ready_pid, ready_since = None, None
        time.sleep(0.2)
    # Do not leave a hung startup behind to hold the lock on the next attempt.
    live = set(alive(seen))
    end({pid: path for pid, path in seen.items() if pid in live}, False)
    failed(f"Jellytool did not become ready within {timeout:g}s; no automatic relaunch")


def main() -> int:
    # Everything after -- goes to Jellytool itself, as `open … --args` takes
    # it: argparse would otherwise consume the separator itself.
    argv = sys.argv[1:]
    app_args: list[str] = []
    if "--" in argv:
        split = argv.index("--")
        argv, app_args = argv[:split], argv[split + 1 :]

    parser = argparse.ArgumentParser(
        description="Open a local Jellytool build, replacing any instance running.",
        epilog="Example: build/jellytool/launch.py "
        "$HOME/.cache/jellytool-build/artifacts/<name>/Jellytool.app\n"
        "         build/jellytool/launch.py <app> -- app",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument("app", type=Path, help="the Jellytool.app to open")
    parser.add_argument(
        "--lock",
        type=Path,
        default=None,
        help="the instance lock to wait on (default: XDG config)",
    )
    parser.add_argument(
        "--dry-run", action="store_true", help="print the steps, change nothing"
    )
    parser.add_argument(
        "--timeout",
        type=float,
        default=START_WAIT,
        help=f"seconds to wait for the build to come up (default {START_WAIT:g})",
    )
    parsed = parser.parse_args(argv)
    if sys.platform != "darwin":
        parser.error("this launcher requires macOS")
    if not 0 < parsed.timeout < float("inf"):
        parser.error("--timeout must be a finite positive number")

    app = parsed.app.expanduser()
    if not app.is_dir():
        die(f"{app} does not exist")
    app = app.resolve()
    if app.suffix != ".app":
        die(f"{app} is not an .app")
    ident = bundle_id(app)
    lock = (parsed.lock or lock_path(app)).expanduser().resolve()
    if parsed.dry_run:
        return launch(app, ident, lock, app_args, parsed.timeout, True)
    lock.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    # Keep simultaneous verification sessions from killing each other's builds.
    with lock.with_suffix(".launch.lock").open("a+") as guard:
        try:
            fcntl.flock(guard, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            die(f"another launch is already in progress for {lock.parent}")
        return launch(app, ident, lock, app_args, parsed.timeout, False)


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        die(str(error))
