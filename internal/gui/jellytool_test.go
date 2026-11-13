//go:build jellytool

package gui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/appdir"
	"github.com/yetone/magpie/internal/gateway"
	"github.com/yetone/magpie/internal/plugin"
	"github.com/yetone/magpie/internal/proc"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/settings"
	"github.com/yetone/magpie/internal/testenv"
)

func jellytoolFixture(t *testing.T) http.Handler {
	t.Helper()
	home := t.TempDir()
	testenv.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	appdir.UseExecutable("")
	var ps []plugin.Provider
	if err := json.Unmarshal([]byte(`[{"id":"jellytoken","name":"Jellytoken","npm":"@ai-sdk/openai-compatible","api":"https://aiservice.jellytoken.com/v1","single":true,"signedIn":true,"methods":[{"type":"api","label":"API key"}],"accounts":[{"key":"jellytoken","type":"api"}],"models":[{"id":"vendor/text.v1:exact","name":"vendor/text.v1:exact","npm":"@ai-sdk/openai-compatible","outputs":["text"]},{"id":"vendor/other.v2:exact","name":"vendor/other.v2:exact","npm":"@ai-sdk/openai-compatible","outputs":["text"]},{"id":"vendor/image","outputs":["image"]}]}]`), &ps); err != nil {
		t.Fatal(err)
	}
	plugin.UseCached(ps)
	t.Cleanup(func() { plugin.UseCached(nil); provider.Changed() })
	gw := gateway.New()
	t.Cleanup(func() { served.Store(nil) })
	return Handler(webHost{}, gw)
}

func TestJellytoolAdmission(t *testing.T) {
	h := jellytoolFixture(t)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/providers", nil))
	var state providersJSON
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(state.Providers, func(p providerJSON) bool { return p.ID == "jellytoken" }) {
		t.Fatalf("unexpected supplier state: %s", w.Body.String())
	}
	body := `{"id":"jellytoken","models":["vendor/text.v1:exact"]}`
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/save", strings.NewReader(body)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	p, err := provider.Find("jellytoken")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Models) != 1 || p.Models[0] != "vendor/text.v1:exact" || !p.IsPlugin() {
		t.Fatalf("provider boundary lost: %+v", p)
	}
	for _, action := range []string{"off", "on"} {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/"+action, strings.NewReader(`{"id":"jellytoken"}`)))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		p, err = provider.Find("jellytoken")
		if err != nil || p.Off != (action == "off") || len(p.Models) != 1 || p.Models[0] != "vendor/text.v1:exact" {
			t.Fatalf("switch lost the supplier selection: %+v %v", p, err)
		}
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/settings", strings.NewReader(`{"theme":"dark","lang":"zh","lan":true,"port":3425,"workbuddyCheckin":true,"proxy":"http://evil.invalid"}`)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	s := settings.Load()
	if s.Theme != "dark" || s.Lang != "zh" {
		t.Fatalf("basic settings were not preserved: %+v", s)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/?mode=panel&view=plugins", nil))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/?view=providers" {
		t.Fatal("panel redirect changed")
	}
	// GET state must not acquire or change real Agent settings. The test home
	// holds no Agent fixtures; the application directory is independent too.
	if filepath.Base(appdir.Config()) != "jellytool" || filepath.Base(appdir.Cache()) != "jellytool" {
		t.Fatal("shared magpie directories")
	}
}

func TestJellytoolPage(t *testing.T) {
	page := jellytoolPage()
	for _, want := range []string{`src="plugins.js"`, `href="manifest.json"`, `src="jellytool.js"`} {
		if !bytes.Contains(page, []byte(want)) {
			t.Errorf("page is missing %s", want)
		}
	}
	if bytes.Contains(page, []byte(`<title>magpie</title>`)) {
		t.Fatal("page kept magpie title")
	}
	if !bytes.Contains(page, []byte(`name="apple-mobile-web-app-title" content="Jellytool"`)) {
		t.Fatal("home-screen title kept the upstream name")
	}
	h := jellytoolFixture(t)
	for _, path := range []string{"/", "/index.html"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("<title>Jellytool</title>")) {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/manifest.json", nil))
	var manifest struct {
		Name      string `json:"name"`
		ShortName string `json:"short_name"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &manifest); err != nil || manifest.Name != "Jellytool" || manifest.ShortName != "Jellytool" {
		t.Fatalf("manifest = %s (%v)", w.Body, err)
	}
}

func TestJellytoolHomeScreenIcon(t *testing.T) {
	h := webGuard("jellytool-brand-test", "0123456789abcdef0123456789abcdef", 0, jellytoolFixture(t))
	for _, path := range []string{"/manifest.json", "/icons/jellytoken.png"} {
		for _, method := range []string{"GET", "HEAD"} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("%s %s without cookie: %d", method, path, w.Code)
			}
		}
	}
	for _, request := range []struct{ method, path string }{{"POST", "/icons/jellytoken.png"}, {"GET", "/api/state"}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(request.method, request.path, nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without cookie: %d %s", request.method, request.path, w.Code, w.Body)
		}
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept", "text/html")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "<h1>Jellytool web</h1>") {
		t.Fatalf("sign-in page: %d %s", w.Code, w.Body)
	}
}

func TestJellytoolBrowser(t *testing.T) {
	if os.Getenv("JELLYTOOL_TEST_BROWSER") != "1" {
		t.Skip("set JELLYTOOL_TEST_BROWSER=1 to run Chromium and WebKit")
	}
	h := jellytoolFixture(t)
	server := httptest.NewServer(h)
	defer server.Close()
	script, err := filepath.Abs("tests/jellytool.test.cjs")
	if err != nil {
		t.Fatal(err)
	}
	p2, err := filepath.Abs("tests/jellytool-recommendations.test.cjs")
	if err != nil {
		t.Fatal(err)
	}
	cmd := proc.Command("node", "--test", "--test-concurrency=1", script, p2)
	cmd.Env = append(os.Environ(), "JELLYTOOL_TEST_URL="+server.URL)
	b, err := cmd.CombinedOutput()
	t.Log(string(b))
	if err != nil {
		t.Fatal(err)
	}
}

func TestJellytoolSelectionBoundary(t *testing.T) {
	h := jellytoolFixture(t)
	codex := filepath.Join(os.Getenv("HOME"), ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(codex), 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte("# untouched fixture\nmodel = \"original-model\"\n")
	if err := os.WriteFile(codex, original, 0600); err != nil {
		t.Fatal(err)
	}
	save := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/save", strings.NewReader(body)))
		return w
	}
	// An empty explicit selection must stay empty, rather than reverting to all.
	if w := save(`{"id":"jellytoken","models":[]}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if p, err := provider.Find("jellytoken"); err != nil || p == nil {
		t.Fatalf("empty selection was not saved: %+v %v", p, err)
	}
	if w := save(`{"id":"jellytoken","models":["vendor/text.v1:exact"]}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	ps := plugin.Cached()
	ps[0].Models = ps[0].Models[1:] // permission snapshot no longer lists the first ID
	plugin.UseCached(ps)
	if w := save(`{"id":"jellytoken","models":["vendor/text.v1:exact"]}`); w.Code != 200 {
		t.Fatal("magpie provider editor rejected a typed model", w.Code, w.Body.String())
	}
	ps[0].SignedIn = false
	plugin.UseCached(ps)
	if w := save(`{"id":"jellytoken","models":["vendor/other.v2:exact"]}`); w.Code != 200 {
		t.Fatal("magpie provider editor rejected a typed model after sign-out")
	}
	b, err := os.ReadFile(codex)
	if err != nil || !bytes.Equal(b, original) {
		t.Fatalf("supplier save changed Agent config: %s %v", b, err)
	}
}

func TestJellytoolWorkBuddyAPI(t *testing.T) {
	h := jellytoolFixture(t)
	home := os.Getenv("HOME")
	path := filepath.Join(home, ".workbuddy", "models.json")
	original := []byte(`{"models":[{"id":"legacy/model","vendor":"magpie","apiKey":"magpie-workbuddy","url":"http://127.0.0.1:3425/v1/chat/completions"}],"availableModels":["legacy/model"],"other":1}`)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	call := func(route, body string) {
		t.Helper()
		w := httptest.NewRecorder()
		method := "POST"
		if body == "" {
			method = "GET"
		}
		h.ServeHTTP(w, httptest.NewRequest(method, route, strings.NewReader(body)))
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", route, w.Code, w.Body)
		}
	}
	call("/api/providers", "")
	call("/api/provider/save", `{"id":"jellytoken","models":["vendor/text.v1:exact","vendor/other.v2:exact"]}`)
	before, _ := os.ReadFile(path)
	if !bytes.Equal(before, original) {
		t.Fatal("supplier/status changed WorkBuddy before connect")
	}
	call("/api/agents/connect/workbuddy", `{}`)
	connected, _ := os.ReadFile(path)
	for _, want := range []string{`"vendor": "jellytool"`, "jellytool-workbuddy", "127.0.0.1:3435", "legacy/model", "vendor/text.v1:exact"} {
		if !bytes.Contains(connected, []byte(want)) {
			t.Fatalf("missing %s in %s", want, connected)
		}
	}
	call("/api/agent-models/workbuddy", `{"hidden":["jellytoken/vendor/other.v2:exact"]}`)
	picked, _ := os.ReadFile(path)
	if bytes.Contains(picked, []byte("vendor/other.v2:exact")) || !bytes.Contains(picked, []byte("legacy/model")) {
		t.Fatalf("Agent list edit failed: %s", picked)
	}
	call("/api/agents/disconnect/workbuddy", `{}`)
	restored, _ := os.ReadFile(path)
	if !bytes.Equal(restored, original) {
		t.Fatalf("disconnect did not restore original: %s", restored)
	}
	call("/api/agents/connect/workbuddy", `{}`)
	call("/api/agents/disconnect/workbuddy", `{}`)
	restored, _ = os.ReadFile(path)
	if !bytes.Equal(restored, original) {
		t.Fatal("repeat connection lost original")
	}
}
