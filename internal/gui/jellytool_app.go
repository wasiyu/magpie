//go:build !nogui

package gui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/yetone/magpie/internal/appdir"
	"github.com/yetone/magpie/internal/filelock"
	"github.com/yetone/magpie/internal/gateway"
	"github.com/yetone/magpie/internal/library"
	"github.com/yetone/magpie/internal/plugin"
	"github.com/yetone/magpie/internal/proc"
	"github.com/yetone/magpie/internal/product"
	"github.com/yetone/magpie/internal/settings"
)

// RunJellytool keeps Jellytool's app identity and bundled provider while using
// magpie's normal backend, gateway, plugin host, and feature set.
func RunJellytool(version string) error {
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
		return errors.New("Jellytool is already running or its data directory cannot be locked")
	}
	defer unlock()
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
	close(h.ready)
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
		Name: "Jellytool", Description: "Jellytoken for your coding agents", Icon: jellytoolAppIcon(),
		Assets:     application.AssetOptions{Handler: ownWindow(Handler(h, gw))},
		Mac:        application.MacOptions{ActivationPolicy: application.ActivationPolicyRegular, ApplicationShouldTerminateAfterLastWindowClosed: false},
		Windows:    application.WindowsOptions{WebviewUserDataPath: appdir.WebView()},
		Linux:      application.LinuxOptions{ProgramName: "jellytool"},
		OnShutdown: stop,
	})
	h.app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go func() {
			select {
			case <-ctx.Done():
				return
			case <-ps.Done():
				if ctx.Err() != nil {
					return
				}
				runtimeFailed <- errors.New("Jellytool bundled plugin stopped; restart Jellytool")
				h.app.Quit()
			}
		}()
	})
	h.main = h.app.Window.NewWithOptions(application.WebviewWindowOptions{Name: "jellytool-main", Title: "Jellytool", Width: 1100, Height: 760, URL: "/"})
	h.setupJellytoolTray(version)
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

func (h *jellytoolWindow) ShowMain(string) {
	application.InvokeSync(func() { h.main.Show(); h.main.Focus() })
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
		open.SetLabel(strings.ReplaceAll(labels.open, "magpie", "Jellytool"))
		ver.SetLabel(labels.version)
		quit.SetLabel(strings.ReplaceAll(labels.quit, "magpie", "Jellytool"))
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
	h.tray.SetTooltip("Jellytool")
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
