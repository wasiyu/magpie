//go:build !nogui && darwin

package gui

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/yetone/magpie/internal/appdir"
	"github.com/yetone/magpie/internal/filelock"
	"github.com/yetone/magpie/internal/proc"
	"github.com/yetone/magpie/internal/product"
	"github.com/yetone/magpie/internal/settings"
	"github.com/yetone/magpie/internal/update"
)

// Jellytool opened from its disk image, or from a copy macOS moved aside
// (App Translocation) because it was never moved out of the download,
// offers to put itself in Applications. Run from there it is gone when the
// image is ejected, has no Launchpad entry and can't update itself.
//
// These are variables so tests can stand in for the system.
var (
	jellytoolApplications = "/Applications"
	jellytoolStuck        = update.Stuck
	jellytoolAsk          = askDialog
	jellytoolTell         = tellDialog
	jellytoolMoveAsAdmin  = func(script string) error {
		return update.AsAdmin(product.Name+" is moving itself to Applications.", script)
	}
	jellytoolRelaunch = func(bundle string) error { return update.Relaunch(bundle, true, false) }
	// jellytoolRunning is the pids of the Jellytool that holds the instance
	// lock at path; jellytoolEnd asks them to quit and waits for the lock.
	jellytoolRunning = lockHolders
	jellytoolEnd     = endLockHolders
)

// jellytoolInstanceLock is the file the running Jellytool holds locked.
func jellytoolInstanceLock() string { return filepath.Join(appdir.Config(), "jellytool-instance.lock") }

// jellytoolMoveStage is the folder beside Applications' copy the app is
// copied into first, on the same disk, so the swap is two renames.
const jellytoolMoveStage = ".jellytool-move"

// offerJellytoolMove asks to move the app at bundle into Applications when
// it runs where it can't stay. It runs before this launch takes the
// instance lock: opening a new disk image while an older Jellytool runs is
// how an update is installed by hand, and the lock held by that one would
// end this launch before it asked anything (the Dock icon bounces and goes).
// That one is asked to quit first, then the app is moved.
//
// It returns true when the app was moved and the copy there will be opened
// once this process exits: the caller then returns without starting
// anything. Any other answer, or a failed move (said in a dialog), lets
// this launch go on where it is.
func offerJellytoolMove(bundle string) bool {
	if bundle == "" || appdir.Portable() != "" {
		return false
	}
	stuck := jellytoolStuck(bundle)
	if stuck == "" {
		return false
	}
	dest := filepath.Join(jellytoolApplications, filepath.Base(bundle))
	_, err := os.Lstat(dest)
	lock := jellytoolInstanceLock()
	running := jellytoolRunning(lock)
	words := jellytoolMoveWords(jellytoolMoveLang(), err == nil, len(running) > 0)
	if !jellytoolAsk(words.ask, words.later, words.move) {
		return false
	}
	if len(running) > 0 {
		if err := jellytoolEnd(lock, running); err != nil {
			log.Printf("quit the running Jellytool %v: %v", running, err)
			jellytoolTell(fmt.Sprintf(words.failed, err))
			return false
		}
	}
	if err := moveJellytool(bundle, dest); err != nil {
		log.Printf("move to Applications (%s): %v", stuck, err)
		if !errors.Is(err, update.ErrCanceled) { // the password prompt closed: the user said no
			jellytoolTell(fmt.Sprintf(words.failed, err))
		}
		return false
	}
	if err := jellytoolRelaunch(dest); err != nil {
		log.Println("open the moved app:", err)
		jellytoolTell(fmt.Sprintf(words.reopen, dest))
	}
	return true
}

// moveJellytool copies bundle to dest, without the quarantine mark the
// download put on it (a copy that keeps it is moved aside by macOS again
// on its next launch, and would ask again), then puts the copy in place of
// any app already there. dest is never left missing: the old app is put
// back when the new one can't take its name. Where Applications is not
// this user's to change, the same steps run after the administrator's
// password.
func moveJellytool(bundle, dest string) error {
	stage := filepath.Join(filepath.Dir(dest), jellytoolMoveStage)
	err := moveJellytoolAsUser(bundle, dest, stage)
	if errors.Is(err, fs.ErrPermission) {
		return jellytoolMoveAsAdmin(jellytoolMoveScript(bundle, dest, stage))
	}
	return err
}

func moveJellytoolAsUser(bundle, dest, stage string) error {
	os.RemoveAll(stage)
	if err := os.Mkdir(stage, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	staged := filepath.Join(stage, filepath.Base(dest))
	if out, err := proc.Command("ditto", bundle, staged).CombinedOutput(); err != nil {
		return fmt.Errorf("copy: %v: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := proc.Command("xattr", "-dr", "com.apple.quarantine", staged).CombinedOutput(); err != nil {
		return fmt.Errorf("clear quarantine: %v: %s", err, strings.TrimSpace(string(out)))
	}
	aside := filepath.Join(stage, "old.app")
	if _, err := os.Lstat(dest); err == nil {
		if err := os.Rename(dest, aside); err != nil {
			return err
		}
	}
	if err := os.Rename(staged, dest); err != nil {
		os.Rename(aside, dest) // put the old one back
		return err
	}
	return nil
}

// jellytoolMoveScript is moveJellytoolAsUser for /bin/sh, run as root.
func jellytoolMoveScript(bundle, dest, stage string) string {
	q := update.ShellQuote
	staged, aside := q(filepath.Join(stage, filepath.Base(dest))), q(filepath.Join(stage, "old.app"))
	return "rm -rf " + q(stage) + " && mkdir " + q(stage) +
		" && ditto " + q(bundle) + " " + staged +
		" && xattr -dr com.apple.quarantine " + staged +
		" && { [ ! -e " + q(dest) + " ] || mv " + q(dest) + " " + aside + "; }" +
		" && if mv " + staged + " " + q(dest) + "; then rm -rf " + q(stage) + "; else [ ! -e " + aside + " ] || mv " + aside + " " + q(dest) + "; rm -rf " + q(stage) + "; exit 1; fi"
}

type jellytoolMoveText struct{ ask, move, later, failed, reopen string }

func jellytoolMoveLang() string { return trayLang(settings.Load().Lang, systemLang) }

func jellytoolMoveWords(lang string, replaces, running bool) jellytoolMoveText {
	if strings.HasPrefix(lang, "zh") {
		w := jellytoolMoveText{
			ask:    product.Name + " 还没有安装到「应用程序」文件夹，现在是从安装盘（DMG）或临时位置运行的。\n\n移过去之后，推出安装盘也能继续用，也能在启动台里找到。",
			move:   "移到「应用程序」",
			later:  "以后再说",
			failed: "没能移到「应用程序」：%v\n\n请把安装盘里的 Jellytool 拖到「应用程序」文件夹，再从那里打开。",
			reopen: "已移到 %s，但没能自动打开。请从「应用程序」里打开它。",
		}
		if running {
			w.ask += "\n\n正在运行的 Jellytool 会先退出。"
		}
		if replaces {
			w.ask += "\n\n「应用程序」里已有的 Jellytool 会被替换。"
		}
		return w
	}
	w := jellytoolMoveText{
		ask:    product.Name + " is running from its disk image or a temporary location, not from the Applications folder.\n\nMoved there, it keeps working after the disk image is ejected and shows in Launchpad.",
		move:   "Move to Applications",
		later:  "Not Now",
		failed: "Couldn't move to Applications: %v\n\nDrag Jellytool from the disk image into the Applications folder and open it from there.",
		reopen: "Moved to %s, but it couldn't be opened. Open it from Applications.",
	}
	if running {
		w.ask += "\n\nThe Jellytool that is running quits first."
	}
	if replaces {
		w.ask += "\n\nThe Jellytool already in Applications will be replaced."
	}
	return w
}

// lockHolders is the pids that have the file at path open: the running
// Jellytool, which holds it locked while it runs (its children don't
// inherit it, Go opens files close-on-exec). None when lsof finds none or
// fails.
func lockHolders(path string) []int {
	out, _ := proc.Command("lsof", "-t", "--", path).Output()
	var pids []int
	for _, f := range strings.Fields(string(out)) {
		if pid, err := strconv.Atoi(f); err == nil && pid > 0 && pid != os.Getpid() {
			pids = append(pids, pid)
		}
	}
	return pids
}

// jellytoolQuitWait is how long a running Jellytool gets to quit on
// SIGTERM (its gateway drains for up to 2s) before it is killed.
var jellytoolQuitWait = 15 * time.Second

// endLockHolders asks pids to quit (SIGTERM, which Wails turns into the
// app's own quit) and waits until the lock at path is free, killing them
// when they don't let go in time. The lock is let go of again at once:
// the moved copy takes it when it opens.
func endLockHolders(path string, pids []int) error {
	for _, pid := range pids {
		syscall.Kill(pid, syscall.SIGTERM)
	}
	if unlock, held := filelock.Lock(path, jellytoolQuitWait); held {
		unlock()
		return nil
	}
	for _, pid := range pids {
		syscall.Kill(pid, syscall.SIGKILL)
	}
	if unlock, held := filelock.Lock(path, 3*time.Second); held {
		unlock()
		return nil
	}
	return fmt.Errorf("the running %s (pid %v) did not quit", product.Name, pids)
}

// askDialog shows a system dialog with two buttons and says whether yes
// was pressed. Escape, a closed dialog or osascript failing all read as no.
func askDialog(text, no, yes string) bool {
	q := appleQuote
	out, err := proc.Command("osascript",
		"-e", "display dialog "+q(text)+" with title "+q(product.Name)+
			" buttons {"+q(no)+", "+q(yes)+"} default button 2 cancel button 1 with icon note",
	).Output()
	return err == nil && strings.TrimSpace(string(out)) == "button returned:"+yes
}

// tellDialog shows a system dialog with an OK button.
func tellDialog(text string) {
	proc.Command("osascript", "-e", "display dialog "+appleQuote(text)+" with title "+appleQuote(product.Name)+" buttons {\"OK\"} default button 1 with icon caution").Run()
}

// appleQuote is s as an AppleScript string literal.
func appleQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
