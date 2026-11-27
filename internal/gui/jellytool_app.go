//go:build !nogui

package gui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/yetone/magpie/internal/appdir"
	"github.com/yetone/magpie/internal/autostart"
	"github.com/yetone/magpie/internal/filelock"
	"github.com/yetone/magpie/internal/gateway"
	"github.com/yetone/magpie/internal/library"
	"github.com/yetone/magpie/internal/plugin"
	"github.com/yetone/magpie/internal/proc"
	"github.com/yetone/magpie/internal/product"
	"github.com/yetone/magpie/internal/settings"
)

// RunJellytool keeps Jellytool's app identity and bundled provider while using
// magpie's normal backend, gateway, plugin host, and feature set. showMain
// false is the login start (`tray`): the tray and gateway, no window.
func RunJellytool(version string, showMain bool) error {
	if !product.Jellytool {
		return errors.New("Jellytool requires a jellytool build")
	}
	if os.Getenv("FRONTEND_DEVSERVER_URL") != "" || os.Getenv("WAILS_UPDATER_HELPER") != "" {
		return errors.New("external frontend and updater helper modes are disabled in Jellytool")
	}
	if _, err := appdir.CheckEnv(); err != nil {
		return err
	}
	// The guard is also taken on macOS, where Wails delegates single-instance
	// handling to the bundle ID. Failure never falls through to an unlocked run.
	unlock, held := filelock.Lock(filepath.Join(appdir.Config(), "jellytool-instance.lock"), time.Millisecond)
	if !held {
		// A shortcut opened while the login start holds the lock: hand the
		// launch to it, as magpie's own second launch does. Wails exits this
		// process once the running one is told; it returns only when no
		// running one answers.
		handOverToRunningJellytool()
		return errors.New(product.Name + " is already running or its data directory cannot be locked")
	}
	defer unlock()
	if runtime.GOOS == "windows" {
		// a login start an older Jellytool wrote under magpie's name
		if err := autostart.Refresh(); err != nil {
			log.Println("open at login:", err)
		}
	}
	if err := ensureJellytoolModelConfig(); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	resources := filepath.Join(filepath.Dir(exe), "jellytool-resources")
	if filepath.Base(filepath.Dir(exe)) == "MacOS" {
		resources = filepath.Join(filepath.Dir(exe), "..", "Resources", "jellytool")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bun := filepath.Join(resources, "bun")
	if runtime.GOOS == "windows" {
		bun += ".exe"
	}
	oldBun, hadBun := os.LookupEnv("MAGPIE_BUN")
	_ = os.Setenv("MAGPIE_BUN", bun)
	defer func() {
		if hadBun {
			_ = os.Setenv("MAGPIE_BUN", oldBun)
		} else {
			_ = os.Unsetenv("MAGPIE_BUN")
		}
	}()
	ps, err := plugin.ActivateJellytool(ctx, resources, appdir.Config())
	if err != nil {
		return err
	}
	defer ps.Close()
	Version, gateway.Version = version, version
	gw := startBackend()
	if _, err := library.EnsureImageServer(); err != nil {
		return err
	}
	h := &jellytoolWindow{host: &host{ready: make(chan struct{})}}
	updateDone := updates.startIn(ctx)
	var once sync.Once
	var stopErr error
	runtimeFailed := make(chan error, 1)
	stop := func() {
		once.Do(func() {
			cancel()
			<-updateDone
			stopServing()
			ps.Close()
			proc.EndProbes()
		})
	}
	defer stop()
	h.app = application.New(application.Options{
		Name: product.Name, Description: "Jellytoken for your coding agents", Icon: jellytoolAppIcon(),
		Assets:         application.AssetOptions{Handler: ownWindow(Handler(h, gw))},
		Mac:            application.MacOptions{ActivationPolicy: application.ActivationPolicyRegular, ApplicationShouldTerminateAfterLastWindowClosed: false},
		Windows:        application.WindowsOptions{WebviewUserDataPath: appdir.WebView()},
		Linux:          application.LinuxOptions{ProgramName: "jellytool", ApplicationID: "org.wails.Jellytool"}, // the id Wails derived from the old Name
		OnShutdown:     stop,
		SingleInstance: jellytoolSingleInstance(h),
	})
	if !webView2Check(h.app) {
		return nil
	}
	h.app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go func() {
			select {
			case <-ctx.Done():
				return
			case <-ps.Done():
				if ctx.Err() != nil {
					return
				}
				runtimeFailed <- errors.New(product.Name + " bundled plugin stopped; restart " + product.Name)
				h.app.Quit()
			}
		}()
	})
	h.main = h.app.Window.NewWithOptions(application.WebviewWindowOptions{Name: "jellytool-main", Title: product.Name, Width: 1100, Height: 760, URL: "/", Hidden: !showMain})
	h.setupJellytoolTray(version)
	// What waits on ready (ShowMain) runs once the window
	// can be shown: on Windows after WebView2's first page, as magpie's
	// main window, since a webview shown before its controller is made
	// crashes on a slow first start.
	markReady := func() { h.readyOnce.Do(func() { close(h.ready) }) }
	if runtime.GOOS == "windows" {
		h.main.OnWindowEvent(events.Windows.WebViewNavigationCompleted, func(*application.WindowEvent) { markReady() })
	} else {
		h.app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) { markReady() })
	}
	err = h.app.Run()
	stop()
	select {
	case failure := <-runtimeFailed:
		return errors.Join(err, stopErr, failure)
	default:
	}
	return errors.Join(err, stopErr)
}

type jellytoolWindow struct{ *host }

// jellytoolSingleInstance hands a second launch on Windows to the running
// Jellytool: the login start holds the instance lock, and a shortcut opened
// then would otherwise end without a word. A second launch with `tray` (the
// login start racing a shortcut) opens nothing; any other shows the window.
// h is nil in the second launch, which only notifies. The Mac reopens the
// app through LaunchServices; Linux keeps the lock's error.
func jellytoolSingleInstance(h *jellytoolWindow) *application.SingleInstanceOptions {
	if runtime.GOOS != "windows" {
		return nil
	}
	return &application.SingleInstanceOptions{
		UniqueID: jellytoolInstanceID(),
		OnSecondInstanceLaunch: func(d application.SecondInstanceData) {
			if h == nil || (len(d.Args) == 2 && d.Args[1] == "tray") {
				return
			}
			h.ShowMain("")
		},
	}
}

// handOverToRunningJellytool tells the running Jellytool of this launch,
// and Wails then exits this process. It asks only once the running one has
// made its single-instance mutex: asked before, Wails here would take that
// mutex itself, and the running one, reaching it next, would take itself
// for the second launch and exit. It returns when none answers in time.
func handOverToRunningJellytool() {
	si := jellytoolSingleInstance(nil)
	if si == nil {
		return
	}
	for deadline := time.Now().Add(30 * time.Second); !jellytoolInstanceHeld(si.UniqueID); time.Sleep(200 * time.Millisecond) {
		if time.Now().After(deadline) {
			return
		}
	}
	application.New(application.Options{Name: product.Name, SingleInstance: si})
}

// jellytoolInstanceID covers the data root, as the instance lock does, so a
// portable build runs on its own.
func jellytoolInstanceID() string {
	sum := sha256.Sum256([]byte(appdir.Config()))
	return "com.jellytoken.jellytool.i" + hex.EncodeToString(sum[:6])
}

// ShowMain waits for ready: a login start's window is hidden, and a tray
// click before WebView2 has its first page would show it too early.
func (h *jellytoolWindow) ShowMain(string) {
	h.whenReady(func() { application.InvokeSync(func() { h.main.Show(); h.main.Focus() }) })
}
func (h *jellytoolWindow) HidePanel()          { application.InvokeSync(func() { h.main.Hide() }) }
func (h *jellytoolWindow) FitPanel(int, Glide) {}

// A closed window leaves the local gateway available to connected Agents.
// The tray owns reopening and quitting; it has no updater or quick panel.
func (h *jellytoolWindow) setupJellytoolTray(version string) {
	menu := h.app.NewMenu()
	open := menu.Add("").OnClick(func(*application.Context) { h.ShowMain("") })
	menu.AddSeparator()
	ver := menu.Add("").SetEnabled(false)
	quit := menu.Add("").OnClick(func(*application.Context) { h.app.Quit() })
	relabel := func() {
		labels := trayMenuLabels(trayLang(settings.Load().Lang, systemLang), version, "", false) // Jellytool's tray has no Pause item
		open.SetLabel(strings.ReplaceAll(labels.open, "magpie", product.Name))
		ver.SetLabel(labels.version)
		quit.SetLabel(strings.ReplaceAll(labels.quit, "magpie", product.Name))
		menu.Update()
	}
	relabel()
	onLang = func() { application.InvokeSync(relabel) }
	h.tray = h.app.SystemTray.New()
	if icon, template := jellytoolTrayIconFor(runtime.GOOS, traySchemeLight()); template {
		h.tray.SetTemplateIcon(icon)
	} else {
		h.tray.SetIcon(icon)
	}
	if runtime.GOOS == "linux" {
		go watchTrayScheme(func(light bool) {
			icon, _ := jellytoolTrayIconFor("linux", light)
			h.tray.SetIcon(icon)
		})
	}
	h.tray.SetTooltip(product.Name)
	h.tray.SetMenu(menu)
	// Match magpie's window mode: a primary click opens the main interface.
	// Wails supplies the menu as the secondary-click action when a menu is set.
	h.tray.OnClick(func() { h.ShowMain("") })
	h.main.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		e.Cancel()
		h.main.Hide()
	})
	h.app.Event.RegisterApplicationEventHook(events.Mac.ApplicationShouldHandleReopen, func(e *application.ApplicationEvent) {
		h.ShowMain("")
		e.Cancel()
	})
}
