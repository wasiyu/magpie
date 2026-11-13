//go:build jellytool && darwin && cgo && !nogui

package gui

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func TestJellytoolTrayKeepsWindowAvailable(t *testing.T) {
	out, err := runAppKit(t, 30*time.Second, []string{"JELLYTOOL_TEST_TRAY=1"})
	if err != nil || !strings.Contains(string(out), "jellytool tray: ok") {
		t.Fatalf("Jellytool tray: %v\n%s", err, out)
	}
}

func init() {
	if os.Getenv("JELLYTOOL_TEST_TRAY") != "1" {
		return
	}
	app := application.New(application.Options{
		Name: "Jellytool tray regression",
		Mac:  application.MacOptions{ActivationPolicy: application.ActivationPolicyRegular},
		Assets: application.AssetOptions{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "<p>Jellytool tray regression</p>")
		})},
	})
	h := &jellytoolWindow{host: &host{app: app}}
	h.main = app.Window.NewWithOptions(application.WebviewWindowOptions{Name: "jellytool-main", Title: "Jellytool tray regression", Width: 500, Height: 300})
	h.setupJellytoolTray("test")
	fail := func(what string) { fmt.Println("jellytool tray: " + what); os.Exit(1) }
	waitFor := func(what string, ok func() bool) {
		for deadline := time.Now().Add(5 * time.Second); !ok(); time.Sleep(20 * time.Millisecond) {
			if time.Now().After(deadline) {
				fail(what)
			}
		}
	}
	shown := func() bool { return application.InvokeSyncWithResult(func() bool { return windowOpen(h.main) }) }
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go func() {
			waitFor("no native status bar button", func() bool { return trayTipNative("Jellytool") })
			waitFor("window did not open", shown)
			h.main.Close()
			waitFor("close did not hide the window", func() bool { return !shown() })
			if _, alive := app.Window.GetByName("jellytool-main"); !alive {
				fail("close destroyed the window")
			}
			h.ShowMain("")
			waitFor("window could not reopen", shown)
			h.HidePanel()
			waitFor("hide did not leave the window hidden", func() bool { return !shown() })
			h.ShowMain("")
			waitFor("hidden window could not reopen", shown)
			fmt.Println("jellytool tray: ok")
			app.Quit()
		}()
	})
	if err := app.Run(); err != nil {
		fail(err.Error())
	}
	os.Exit(0)
}
