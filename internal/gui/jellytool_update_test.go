//go:build jellytool

package gui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/settings"
	"github.com/yetone/magpie/internal/update"
)

func TestJellytoolUpdateDownloadPage(t *testing.T) {
	jellytoolFixture(t)
	old, oldV := updates, Version
	updates, Version = &updater{}, "0.1.0"
	t.Cleanup(func() { updates, Version = old, oldV })
	w := &openedURLs{}
	h := Handler(w, nil)
	rel := update.Release{Product: "jellytool", Version: "0.2.0", URL: "https://downloads.example/jellytool/0.2.0", Notes: "- Faster\n<!-- lang:zh -->\n- 更快"}
	var requests atomic.Int32
	var release atomic.Value
	release.Store(rel)
	feed := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/latest" {
			t.Errorf("unexpected update request: %s", r.URL)
		}
		json.NewEncoder(rw).Encode(release.Load().(update.Release))
	}))
	defer feed.Close()
	t.Setenv("JELLYTOOL_UPDATE_FEED", feed.URL+"/latest")
	t.Setenv("MAGPIE_UPDATE_FEED", "http://127.0.0.1:1/magpie-must-not-be-asked")
	t.Setenv("MAGPIE_NOTES_FEED", "http://127.0.0.1:1/magpie-must-not-be-asked")
	// Even a writable executable and auto-restart copied from old settings
	// cannot turn a Jellytool check or Download into a self-install.
	exe := filepath.Join(t.TempDir(), "jellytool")
	if err := os.WriteFile(exe, []byte("unchanged"), 0700); err != nil {
		t.Fatal(err)
	}
	updates.exe = exe
	updates.self, _ = os.Stat(exe)
	rel.Assets = map[string]update.Asset{update.BinaryAsset(): {URL: feed.URL + "/asset"}}
	release.Store(rel)
	post := func(path string) updateJSON {
		t.Helper()
		rw := httptest.NewRecorder()
		h.ServeHTTP(rw, httptest.NewRequest("POST", path, strings.NewReader("{}")))
		var j updateJSON
		if rw.Code != 200 || json.Unmarshal(rw.Body.Bytes(), &j) != nil {
			t.Fatalf("%s: %d %s", path, rw.Code, rw.Body)
		}
		return j
	}
	j := post("/api/update/check?lang=zh")
	if j.State != "available" || !j.Manual || j.Current != "0.1.0" || j.Latest != "0.2.0" || j.Notes != "- 更快" || updates.staged != "" {
		t.Fatalf("expected download-page notice: %+v", j)
	}
	for _, path := range []string{"/api/update/notes?lang=zh", "/api/whatsnew?all=1&last=10"} {
		rw := httptest.NewRecorder()
		h.ServeHTTP(rw, httptest.NewRequest("GET", path, nil))
		if rw.Code != 200 || requests.Load() != 1 || strings.Contains(rw.Body.String(), "magpie-releases") {
			t.Fatalf("notes must stay on Jellytool's feed: %s: %d %s, requests %d", path, rw.Code, rw.Body, requests.Load())
		}
	}
	post("/api/update/install")
	if len(w.urls) != 1 || w.urls[0] != rel.URL || w.quits.Load() != 0 || requests.Load() != 1 {
		t.Fatalf("Download must only open the browser: %+v, quits %d, requests %d", w.urls, w.quits.Load(), requests.Load())
	}
	if b, err := os.ReadFile(exe); err != nil || string(b) != "unchanged" {
		t.Fatal("Download changed the executable", err)
	}
	// Skip uses the existing preference and survives other settings saves.
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, httptest.NewRequest("POST", "/api/settings/update-skip", strings.NewReader(`{"version":"0.2.0"}`)))
	if rw.Code != 200 || settings.Load().UpdateSkip != "0.2.0" {
		t.Fatalf("skip wasn't saved: %d %s", rw.Code, rw.Body)
	}
	rw = httptest.NewRecorder()
	h.ServeHTTP(rw, httptest.NewRequest("POST", "/api/settings", strings.NewReader(`{"theme":"dark"}`)))
	if rw.Code != 200 || settings.Load().UpdateSkip != "0.2.0" {
		t.Fatal("another preference erased the skipped version")
	}
	if s := settings.Load(); s.UpdateEvery != 1440 {
		t.Fatalf("default check interval: %d", s.UpdateEvery)
	}
	for _, version := range []string{"0.1.0", "0.0.9"} {
		rel.Version = version
		release.Store(rel)
		if j := post("/api/update/check"); j.State != "latest" {
			t.Fatalf("equal/older release offered: %+v", j)
		}
	}
	// A failed read is an error, retaining the last good release, not 'latest'.
	t.Setenv("JELLYTOOL_UPDATE_FEED", "http://127.0.0.1:1/offline")
	if j := post("/api/update/check"); j.State != "error" || j.Latest != "0.0.9" || !j.Manual {
		t.Fatalf("failed check: %+v", j)
	}
	updates.state, updates.staged = "ready", "must-not-install"
	post("/api/update/install")
	if updates.install(false) || len(w.urls) != 1 || w.quits.Load() != 0 {
		t.Fatal("Jellytool accepted a staged self-install")
	}
}

func TestJellytoolUpdateChecksAfterStartup(t *testing.T) {
	jellytoolFixture(t)
	oldV := Version
	Version = "0.1.0"
	t.Cleanup(func() { Version = oldV })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(update.Release{Product: "jellytool", Version: "0.2.0", URL: "https://downloads.example/jellytool"})
	}))
	defer srv.Close()
	t.Setenv("JELLYTOOL_UPDATE_FEED", srv.URL)
	u := &updater{}
	ctx, cancel := context.WithCancel(context.Background())
	done := u.startIn(ctx)
	t.Cleanup(func() { cancel(); <-done })
	// Observe the state, not a fixed sleep after the startup timer.
	for limit := time.Now().Add(15 * time.Second); time.Now().Before(limit); time.Sleep(10 * time.Millisecond) {
		if j := u.json(); j.State == "available" {
			if !j.Manual || j.Latest != "0.2.0" || u.bundle != "" || u.exe != "" {
				t.Fatalf("scheduled check: %+v", j)
			}
			return
		}
	}
	t.Fatal("startup never checked for a Jellytool release")
}

func TestJellytoolUpdateUnconfiguredAndShutdown(t *testing.T) {
	old := update.JellytoolFeed
	update.JellytoolFeed = ""
	t.Cleanup(func() { update.JellytoolFeed = old })
	t.Setenv("JELLYTOOL_UPDATE_FEED", "")
	t.Setenv("MAGPIE_UPDATE_FEED", "http://127.0.0.1:1/magpie")
	u := &updater{}
	ctx, cancel := context.WithCancel(context.Background())
	done := u.startIn(ctx)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("update loop outlived the Jellytool app")
	}
	if j := u.json(); j.State != "unconfigured" || !j.Manual || u.bundle != "" || u.exe != "" || update.Feed() != "" {
		t.Fatalf("unconfigured build fell back to magpie: %+v", j)
	}
}
