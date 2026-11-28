//go:build !nogui && darwin

package gui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/filelock"
	"github.com/yetone/magpie/internal/update"
)

// fakeJellytoolMove points the move at a temporary Applications folder and
// records the dialogs and the relaunch. stuck is what update.Stuck says.
type fakeJellytoolMove struct {
	apps, bundle string
	answer       bool
	asked, told  []string
	relaunched   []string
	admin        []string
	running      []int
	ended        [][]int
	endErr       error
	events       []string
}

func newFakeJellytoolMove(t *testing.T, stuck string) *fakeJellytoolMove {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("the move copies with macOS's ditto and xattr")
	}
	dir := t.TempDir()
	f := &fakeJellytoolMove{apps: filepath.Join(dir, "Applications"), bundle: filepath.Join(dir, "Volumes", "Jellytool", "Jellytool.app")}
	writeTestFile(t, filepath.Join(f.bundle, "Contents", "MacOS", "jellytool"), "new app")
	writeTestFile(t, filepath.Join(f.bundle, "Contents", "Resources", "jellytool", "bun"), "bun")
	if err := os.MkdirAll(f.apps, 0o755); err != nil {
		t.Fatal(err)
	}
	saved := []any{jellytoolApplications, jellytoolStuck, jellytoolAsk, jellytoolTell, jellytoolMoveAsAdmin, jellytoolRelaunch, jellytoolRunning, jellytoolEnd}
	t.Cleanup(func() {
		jellytoolApplications = saved[0].(string)
		jellytoolStuck = saved[1].(func(string) string)
		jellytoolAsk = saved[2].(func(string, string, string) bool)
		jellytoolTell = saved[3].(func(string))
		jellytoolMoveAsAdmin = saved[4].(func(string) error)
		jellytoolRelaunch = saved[5].(func(string) error)
		jellytoolRunning = saved[6].(func(string) []int)
		jellytoolEnd = saved[7].(func(string, []int) error)
	})
	jellytoolApplications = f.apps
	jellytoolStuck = func(b string) string {
		if b == f.bundle {
			return stuck
		}
		return ""
	}
	jellytoolAsk = func(text, no, yes string) bool { f.asked = append(f.asked, text); return f.answer }
	jellytoolTell = func(text string) { f.told = append(f.told, text) }
	jellytoolMoveAsAdmin = func(script string) error { f.admin = append(f.admin, script); return errors.New("no admin in tests") }
	jellytoolRelaunch = func(b string) error {
		f.events = append(f.events, "relaunch")
		f.relaunched = append(f.relaunched, b)
		return nil
	}
	jellytoolRunning = func(string) []int { return f.running }
	jellytoolEnd = func(_ string, pids []int) error {
		f.events = append(f.events, "end")
		f.ended = append(f.ended, pids)
		return f.endErr
	}
	return f
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestJellytoolInstalledInPlaceIsNotAsked(t *testing.T) {
	f := newFakeJellytoolMove(t, "")
	if offerJellytoolMove(f.bundle) || len(f.asked) != 0 {
		t.Fatalf("an app that can stay where it is was asked to move: %q", f.asked)
	}
}

func TestJellytoolMoveDeclinedKeepsRunning(t *testing.T) {
	f := newFakeJellytoolMove(t, "read-only")
	if offerJellytoolMove(f.bundle) {
		t.Fatal("a declined move ended this launch")
	}
	if len(f.asked) != 1 || len(f.relaunched) != 0 {
		t.Fatalf("asked %d, relaunched %q", len(f.asked), f.relaunched)
	}
	if _, err := os.Stat(filepath.Join(f.apps, "Jellytool.app")); !os.IsNotExist(err) {
		t.Fatalf("a declined move copied the app: %v", err)
	}
}

// The copy loses the download's quarantine mark: kept, macOS moves the
// copy in Applications aside again on its next launch, and it asks again.
func TestJellytoolMoveCopiesWithoutQuarantineAndOpensTheCopy(t *testing.T) {
	f := newFakeJellytoolMove(t, "translocated")
	f.answer = true
	if out, err := exec.Command("xattr", "-w", "com.apple.quarantine", "0081;65a1b2c3;Safari;", f.bundle).CombinedOutput(); err != nil {
		t.Fatalf("xattr: %v %s", err, out)
	}
	writeTestFile(t, filepath.Join(f.apps, "Other.app", "keep"), "other")
	if !offerJellytoolMove(f.bundle) {
		t.Fatalf("the move failed: told %q", f.told)
	}
	dest := filepath.Join(f.apps, "Jellytool.app")
	if got := readTestFile(t, filepath.Join(dest, "Contents", "MacOS", "jellytool")); got != "new app" {
		t.Fatalf("Applications has %q", got)
	}
	// ditto keeps the mark, rewritten (0081 becomes 0281): look for the name.
	if out, err := exec.Command("xattr", "-r", dest).CombinedOutput(); err != nil || strings.Contains(string(out), "com.apple.quarantine") {
		t.Fatalf("the copy keeps the quarantine mark (%v): %s", err, out)
	}
	if len(f.relaunched) != 1 || f.relaunched[0] != dest {
		t.Fatalf("relaunched %q, want the copy %s", f.relaunched, dest)
	}
	if got := readTestFile(t, filepath.Join(f.apps, "Other.app", "keep")); got != "other" {
		t.Fatal("another app in Applications was touched")
	}
	if _, err := os.Stat(filepath.Join(f.apps, jellytoolMoveStage)); !os.IsNotExist(err) {
		t.Fatalf("the staging folder is left: %v", err)
	}
	if readTestFile(t, filepath.Join(f.bundle, "Contents", "MacOS", "jellytool")) != "new app" {
		t.Fatal("the disk image's copy changed")
	}
}

func TestJellytoolMoveReplacesTheAppAlreadyThereAndSaysSo(t *testing.T) {
	f := newFakeJellytoolMove(t, "read-only")
	f.answer = true
	dest := filepath.Join(f.apps, "Jellytool.app")
	writeTestFile(t, filepath.Join(dest, "Contents", "MacOS", "jellytool"), "old app")
	writeTestFile(t, filepath.Join(dest, "Contents", "Resources", "stale"), "stale")
	if !offerJellytoolMove(f.bundle) {
		t.Fatalf("the move failed: told %q", f.told)
	}
	if !strings.Contains(f.asked[0], "replaced") && !strings.Contains(f.asked[0], "替换") {
		t.Fatalf("the question doesn't say the app there is replaced: %q", f.asked[0])
	}
	if got := readTestFile(t, filepath.Join(dest, "Contents", "MacOS", "jellytool")); got != "new app" {
		t.Fatalf("Applications has %q", got)
	}
	if _, err := os.Stat(filepath.Join(dest, "Contents", "Resources", "stale")); !os.IsNotExist(err) {
		t.Fatal("the old app's files were merged into the new one")
	}
}

// Applications not this user's to change: the move goes to the
// administrator's prompt, and when that fails the old app is untouched,
// the user is told to drag it in, and this launch keeps running.
func TestJellytoolMoveWithoutPermissionAsksAdminThenSaysWhatToDo(t *testing.T) {
	f := newFakeJellytoolMove(t, "read-only")
	f.answer = true
	dest := filepath.Join(f.apps, "Jellytool.app")
	writeTestFile(t, filepath.Join(dest, "Contents", "MacOS", "jellytool"), "old app")
	if err := os.Chmod(f.apps, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(f.apps, 0o755) })
	if offerJellytoolMove(f.bundle) {
		t.Fatal("a failed move ended this launch")
	}
	if len(f.admin) != 1 || !strings.Contains(f.admin[0], "xattr -dr com.apple.quarantine") {
		t.Fatalf("admin scripts %q", f.admin)
	}
	if len(f.told) != 1 || len(f.relaunched) != 0 {
		t.Fatalf("told %q, relaunched %q", f.told, f.relaunched)
	}
	if got := readTestFile(t, filepath.Join(dest, "Contents", "MacOS", "jellytool")); got != "old app" {
		t.Fatalf("the app already there changed: %q", got)
	}
}

func TestJellytoolMoveCanceledPasswordSaysNothing(t *testing.T) {
	f := newFakeJellytoolMove(t, "read-only")
	f.answer = true
	jellytoolMoveAsAdmin = func(string) error { return update.ErrCanceled }
	if err := os.Chmod(f.apps, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(f.apps, 0o755) })
	if offerJellytoolMove(f.bundle) || len(f.told) != 0 {
		t.Fatalf("a closed password prompt: told %q", f.told)
	}
}

// The script the administrator's prompt runs does what the user's own
// move does, and puts the old app back when the new one can't take its
// place. Run here as this user in a folder this user owns.
func TestJellytoolMoveScriptKeepsTheOldAppWhenTheSwapFails(t *testing.T) {
	f := newFakeJellytoolMove(t, "read-only")
	dest := filepath.Join(f.apps, "Jellytool.app")
	stage := filepath.Join(f.apps, jellytoolMoveStage)
	writeTestFile(t, filepath.Join(dest, "Contents", "MacOS", "jellytool"), "old app")
	if out, err := exec.Command("/bin/sh", "-c", jellytoolMoveScript(f.bundle, dest, stage)).CombinedOutput(); err != nil {
		t.Fatalf("script: %v %s", err, out)
	}
	if got := readTestFile(t, filepath.Join(dest, "Contents", "MacOS", "jellytool")); got != "new app" {
		t.Fatalf("after the script Applications has %q", got)
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatal("the script left its staging folder")
	}
	// A bundle that can't be copied: nothing of the old app is lost.
	if out, err := exec.Command("/bin/sh", "-c", jellytoolMoveScript(filepath.Join(f.apps, "missing.app"), dest, stage)).CombinedOutput(); err == nil {
		t.Fatalf("copying a missing app succeeded: %s", out)
	}
	if got := readTestFile(t, filepath.Join(dest, "Contents", "MacOS", "jellytool")); got != "new app" {
		t.Fatalf("a failed script changed Applications: %q", got)
	}
}

func TestJellytoolMoveWordsQuoteForAppleScript(t *testing.T) {
	if got := appleQuote(`a "b" \c`); got != `"a \"b\" \\c"` {
		t.Fatalf("appleQuote = %s", got)
	}
	for _, lang := range []string{"zh", "zh-TW", "en", "ja", "de"} {
		w := jellytoolMoveWords(lang, false, false)
		if w.ask == "" || w.move == "" || w.later == "" || !strings.Contains(w.failed, "%v") || !strings.Contains(w.reopen, "%s") {
			t.Fatalf("%s: incomplete words %+v", lang, w)
		}
	}
}

// An older Jellytool still running holds the instance lock, which would end
// this launch before it asked anything: the question says it quits first,
// it is ended before the move, and the moved copy opens after that.
func TestJellytoolMoveEndsTheRunningJellytoolFirst(t *testing.T) {
	f := newFakeJellytoolMove(t, "read-only")
	f.answer = true
	f.running = []int{4242}
	dest := filepath.Join(f.apps, "Jellytool.app")
	writeTestFile(t, filepath.Join(dest, "Contents", "MacOS", "jellytool"), "old app")
	if !offerJellytoolMove(f.bundle) {
		t.Fatalf("the move failed: told %q", f.told)
	}
	if !strings.Contains(f.asked[0], "先退出") && !strings.Contains(f.asked[0], "quits first") {
		t.Fatalf("the question doesn't say the running one quits: %q", f.asked[0])
	}
	if len(f.ended) != 1 || f.ended[0][0] != 4242 || strings.Join(f.events, ",") != "end,relaunch" {
		t.Fatalf("ended %v, events %v", f.ended, f.events)
	}
	if got := readTestFile(t, filepath.Join(dest, "Contents", "MacOS", "jellytool")); got != "new app" {
		t.Fatalf("Applications has %q", got)
	}
}

func TestJellytoolMoveNotAskedToEndWhenDeclined(t *testing.T) {
	f := newFakeJellytoolMove(t, "read-only")
	f.running = []int{4242}
	if offerJellytoolMove(f.bundle) || len(f.ended) != 0 {
		t.Fatalf("a declined move ended the running Jellytool: %v", f.ended)
	}
}

func TestJellytoolMoveStopsWhenTheRunningOneWontQuit(t *testing.T) {
	f := newFakeJellytoolMove(t, "read-only")
	f.answer = true
	f.running = []int{4242}
	f.endErr = errors.New("did not quit")
	dest := filepath.Join(f.apps, "Jellytool.app")
	writeTestFile(t, filepath.Join(dest, "Contents", "MacOS", "jellytool"), "old app")
	if offerJellytoolMove(f.bundle) || len(f.told) != 1 || len(f.relaunched) != 0 {
		t.Fatalf("moved past a Jellytool that kept running: told %q, relaunched %q", f.told, f.relaunched)
	}
	if got := readTestFile(t, filepath.Join(dest, "Contents", "MacOS", "jellytool")); got != "old app" {
		t.Fatalf("the running app's bundle was replaced: %q", got)
	}
}

// holdLock starts a process that holds path locked as Jellytool does
// (flock), quitting on SIGTERM unless ignoreTerm. perl ships with macOS.
func holdLock(t *testing.T, path string, ignoreTerm bool) *exec.Cmd {
	t.Helper()
	term := `$SIG{TERM} = sub { exit 0 };`
	if ignoreTerm {
		term = `$SIG{TERM} = "IGNORE";`
	}
	ready := path + ".ready"
	cmd := exec.Command("perl", "-e", `use Fcntl ":flock"; `+term+` open(my $f, ">", $ARGV[0]) or die; flock($f, LOCK_EX) or die; open(my $r, ">", $ARGV[1]); close $r; sleep 60;`, path, ready)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	t.Cleanup(func() { cmd.Process.Kill(); <-done })
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(ready); err == nil {
			return cmd
		}
		if time.Now().After(deadline) {
			t.Fatal("the lock holder never took the lock")
		}
	}
}

func TestJellytoolLockHoldersQuitOrAreKilled(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("lsof and perl as on macOS")
	}
	defer func(w time.Duration) { jellytoolQuitWait = w }(jellytoolQuitWait)
	jellytoolQuitWait = time.Second
	for _, ignoreTerm := range []bool{false, true} {
		lock := filepath.Join(t.TempDir(), "jellytool-instance.lock")
		holder := holdLock(t, lock, ignoreTerm)
		pids := lockHolders(lock)
		if len(pids) != 1 || pids[0] != holder.Process.Pid {
			t.Fatalf("lockHolders = %v, want [%d]", pids, holder.Process.Pid)
		}
		if err := endLockHolders(lock, pids); err != nil {
			t.Fatalf("ignoreTerm=%v: %v", ignoreTerm, err)
		}
		if unlock, held := filelock.Lock(lock, 0); !held {
			t.Fatalf("ignoreTerm=%v: the lock is still held", ignoreTerm)
		} else {
			unlock()
		}
		if got := lockHolders(lock); len(got) != 0 {
			t.Fatalf("ignoreTerm=%v: still held by %v", ignoreTerm, got)
		}
	}
	if got := lockHolders(filepath.Join(t.TempDir(), "none.lock")); len(got) != 0 {
		t.Fatalf("a lock nobody holds has holders %v", got)
	}
}
