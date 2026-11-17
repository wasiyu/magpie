package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/testenv"
)

func TestJellytoolBundleContract(t *testing.T) {
	bun := testenv.Bun(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	resources, data := t.TempDir(), t.TempDir()
	configRoot := filepath.Join(t.TempDir(), "config")
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	if err := PackJellytoolPlugins(ctx, bun, resources); err != nil {
		t.Fatal(err)
	}
	const full = `{"data":[{"id":"vendor/text.v1:exact","type":"TEXT"},{"id":"vendor/image","type":"IMAGE"},{"id":"vendor/video","type":"VIDEO"}]}`
	var mu sync.Mutex
	status, reply := 200, full
	requireXKey := false
	type request struct {
		path, body string
		header     http.Header
	}
	var seen []request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, request{r.URL.Path, string(b), r.Header.Clone()})
		if r.URL.Path == "/v1/models" {
			if requireXKey && r.Header.Get("X-API-Key") == "" {
				w.WriteHeader(401)
				return
			}
			w.Header().Set("Location", "/redirect-must-not-be-followed")
			w.WriteHeader(status)
			fmt.Fprint(w, reply)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Fixture", "jellytoken")
		if r.URL.Query().Get("redirect") == "1" {
			w.Header().Set("Location", srvURLForRedirect(r))
			w.WriteHeader(307)
			return
		}
		if r.URL.Query().Get("reject") == "1" {
			w.WriteHeader(429)
			fmt.Fprint(w, "fixture rate limit")
			return
		}
		fmt.Fprint(w, "data: {\"fixture\":true}\n\n")
		w.(http.Flusher).Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	// Neither a third-party list nor ambient preload/runtime configuration may
	// enter the private host. A malicious plugin would leave this marker.
	marker := filepath.Join(data, "executed")
	evil := filepath.Join(data, "evil.js")
	os.WriteFile(evil, []byte(`await Bun.write(`+fmt.Sprintf("%q", marker)+`, "executed"); export default async () => ({})`), 0o600)
	os.WriteFile(filepath.Join(data, "plugins.json"), []byte(`{"plugins":[{"spec":`+fmt.Sprintf("%q", evil)+`}]}`), 0o600)
	t.Setenv("BUN_OPTIONS", "--preload="+evil)
	t.Setenv("MAGPIE_BUN", "/must/not/use/ambient/bun")
	p, err := startJellytoolPlugins(ctx, resources, data, map[string]any{"testOrigin": srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	providers := func() Provider {
		t.Helper()
		ps, err := p.Providers(ctx)
		if err != nil || len(ps) != 1 || ps[0].ID != "jellytoken" {
			t.Fatalf("Providers = %+v, %v", ps, err)
		}
		return ps[0]
	}
	pp := providers()
	if pp.Name != "Jellytoken" || pp.SignedIn || len(pp.Models) != 0 || len(pp.Methods) != 1 || pp.Methods[0].Type != "api" {
		t.Fatalf("supplier-page contract before sign-in: %+v", pp)
	}
	if err := p.APIKey(ctx, " \n "); err == nil {
		t.Fatal("blank credential was accepted")
	}
	const key = "synthetic-jellytoken-key"
	if err := p.APIKey(ctx, key); err != nil {
		t.Fatal(err)
	}
	pp = providers()
	if !pp.SignedIn || pp.FellBack || len(pp.Models) != 3 {
		t.Fatalf("signed-in catalog: %+v", pp)
	}
	for i, want := range []struct{ id, output string }{{"vendor/text.v1:exact", "text"}, {"vendor/image", "image"}, {"vendor/video", "video"}} {
		m := pp.Models[i]
		if m.ID != want.id || !reflect.DeepEqual(m.Outputs, []string{want.output}) || m.Released != "" || m.Cost != nil || m.Reasoning || m.ImageSaid {
			t.Fatalf("catalog entry lost identity/type or invented metadata: %+v", m)
		}
	}
	for _, route := range []struct{ path, npm string }{{"chat/completions", "@ai-sdk/openai-compatible"}, {"responses", "@ai-sdk/openai"}, {"messages", "@ai-sdk/anthropic"}} {
		body := `{"model":"vendor/text.v1:exact","stream":true,"fixture":"unchanged"}`
		r := FetchRequest{Provider: "jellytoken", Model: "vendor/text.v1:exact", NPM: route.npm, URL: srv.URL + "/v1/" + route.path, Method: "POST", Body: []byte(body), Headers: map[string]string{"content-type": "application/json", "authorization": "Bearer gateway-secret", "x-api-key": "gateway-secret", "cookie": "session=gateway-secret"}}
		res, err := p.Fetch(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		b, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil || res.StatusCode != 200 || res.Header.Get("X-Fixture") != "jellytoken" || string(b) != "data: {\"fixture\":true}\n\ndata: [DONE]\n\n" {
			t.Fatalf("%s did not stream unchanged: %s, %v", route.path, b, readErr)
		}
		mu.Lock()
		last := seen[len(seen)-1]
		mu.Unlock()
		if last.path != "/v1/"+route.path || last.body != body || last.header.Get("Cookie") != "" {
			t.Fatalf("request changed or leaked ambient credentials: %+v", last)
		}
		if route.path == "messages" {
			if last.header.Get("X-API-Key") != key || last.header.Get("Authorization") != "" || last.header.Get("Anthropic-Version") != "2023-06-01" {
				t.Fatalf("Anthropic auth: %v", last.header)
			}
		} else if last.header.Get("Authorization") != "Bearer "+key || last.header.Get("X-API-Key") != "" {
			t.Fatalf("OpenAI auth: %v", last.header)
		}
	}
	for _, query := range []string{"redirect=1", "reject=1"} {
		res, err := p.Fetch(ctx, FetchRequest{Provider: "jellytoken", Model: "vendor/text.v1:exact", URL: srv.URL + "/v1/responses?" + query, Method: "POST", Body: []byte(`{}`)})
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != 307 && res.StatusCode != 429 {
			t.Fatalf("upstream status changed: %d", res.StatusCode)
		}
	}
	for _, url := range []string{srv.URL + "/v1/not-supported", "http://127.0.0.1:9/v1/responses", srv.URL + "/redirect-must-not-be-followed"} {
		if res, err := p.Fetch(ctx, FetchRequest{Provider: "jellytoken", URL: url, Method: "POST"}); err == nil {
			res.Body.Close()
			t.Fatalf("unsupported destination accepted: %s", url)
		}
	}
	if _, err := p.Fetch(ctx, FetchRequest{Provider: "third-party"}); err == nil {
		t.Fatal("third-party provider was accepted")
	}
	// An authentication refusal retries once using X-API-Key.
	mu.Lock()
	requireXKey = true
	mu.Unlock()
	if pp := providers(); pp.FellBack || len(pp.Models) != 3 {
		t.Fatalf("X-API-Key fallback: %+v", pp)
	}
	mu.Lock()
	requireXKey = false
	mu.Unlock()
	for _, failure := range []struct {
		status int
		body   string
	}{
		{200, `{"success":false,"message":"` + key + `","data":[{"id":"fake","type":"TEXT"}]}`},
		{200, `{"code":42,"data":[]}`},
		{200, `{"error":{"message":"` + key + `"}}`},
		{200, `not-json ` + key},
		{200, `{"data":[]}`},
		{200, `{"data":[{"id":"odd","type":"AUDIO"}]}`},
		{200, strings.Repeat("x", 8*1024*1024+1)},
		{302, "redirect " + key},
		{403, key},
		{500, key},
	} {
		mu.Lock()
		status, reply = failure.status, failure.body
		mu.Unlock()
		pp := providers()
		if !pp.FellBack || pp.ListError == "" || strings.Contains(pp.ListError, key) || len(pp.Models) != 0 {
			t.Fatalf("failure was treated as an authorized catalog or leaked a key: %+v", pp)
		}
	}
	for _, catalog := range []struct{ body, id string }{
		{`{"data":[{"id":"same","type":"TEXT"},{"id":"same","type":"TEXT"}]}`, "same"},
		{`{"data":[null,{}, {"id":"good","type":"AUDIO"},{"id":"good","type":"TEXT"},{"id":"bad","type":"AUDIO"},{"id":"good","type":"TEXT"}]}`, "good"},
	} {
		mu.Lock()
		status, reply = 200, catalog.body
		mu.Unlock()
		if pp := providers(); pp.FellBack || len(pp.Models) != 1 || pp.Models[0].ID != catalog.id {
			t.Fatalf("valid models were discarded with malformed or duplicate entries: %+v", pp)
		}
	}
	mu.Lock()
	status, reply = 200, full
	mu.Unlock()
	if err := p.APIKey(ctx, "replacement-key"); err != nil {
		t.Fatal(err)
	}
	if pp := providers(); pp.FellBack || len(pp.Accounts) != 1 || len(pp.Models) != 3 {
		t.Fatalf("key replacement did not recover: %+v", pp)
	}
	stored, err := os.ReadFile(filepath.Join(data, "plugin-auth.json"))
	if err != nil || strings.Contains(string(stored), key) || !strings.Contains(string(stored), "replacement-key") {
		t.Fatalf("private credential replacement failed: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("a third-party plugin/preload was executed")
	}
	if _, err := os.Stat(filepath.Join(configRoot, "magpie")); !os.IsNotExist(err) {
		t.Fatal("the private host wrote to magpie's data directory")
	}
	mu.Lock()
	defer mu.Unlock()
	if slices.ContainsFunc(seen, func(r request) bool { return r.path == "/redirect-must-not-be-followed" }) {
		t.Fatal("followed an upstream redirect with credentials")
	}
}

func TestJellytoolBundleTargetLock(t *testing.T) {
	bun := filepath.Join(t.TempDir(), "bun.exe")
	if err := os.WriteFile(bun, []byte("target-bun"), 0o700); err != nil {
		t.Fatal(err)
	}
	resources := t.TempDir()
	if err := PackJellytoolPluginsFor(context.Background(), bun, resources, "windows", "amd64"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(resources, "bun.exe")); err != nil {
		t.Fatal(err)
	}
	var lock jellytoolRuntimeLock
	b, err := os.ReadFile(filepath.Join(resources, "runtime-lock.json"))
	if err != nil || json.Unmarshal(b, &lock) != nil {
		t.Fatalf("runtime lock: %v", err)
	}
	if lock.OS != "windows" || lock.Arch != "amd64" || lock.Version != BunVersion {
		t.Fatalf("runtime lock = %+v", lock)
	}
}

func srvURLForRedirect(r *http.Request) string {
	return "http://" + r.Host + "/redirect-must-not-be-followed"
}

func TestJellytoolBundleIntegrity(t *testing.T) {
	bun := testenv.Bun(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	resources, data := t.TempDir(), t.TempDir()
	if err := PackJellytoolPlugins(ctx, bun, resources); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.js", "package.json", "recommendations.json", bunExe(), "runtime-lock.json"} {
		path := filepath.Join(resources, name)
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(path, []byte("tampered"), 0o700)
		p, err := StartJellytoolPlugins(ctx, resources, data)
		if err == nil {
			p.Close()
			t.Fatalf("tampered %s was accepted", name)
		}
		if !strings.Contains(err.Error(), "integrity verification") && !strings.Contains(err.Error(), "runtime lock") {
			t.Fatalf("tampered %s reached startup instead of verification: %v", name, err)
		}
		os.WriteFile(path, original, 0o700)
	}
	p, err := StartJellytoolPlugins(ctx, resources, data)
	if err != nil {
		t.Fatal(err)
	}
	p.Close()
	// An edited local plugin must fail closed on the next launch too.
	files, _ := filepath.Glob(filepath.Join(data, "plugins", "*", "index.js"))
	if len(files) != 1 {
		t.Fatalf("private registration: %v", files)
	}
	os.WriteFile(files[0], []byte("export default async () => ({})"), 0o600)
	if p, err := StartJellytoolPlugins(ctx, resources, data); err == nil {
		p.Close()
		t.Fatal("edited private plugin was executed")
	}
	// A host-only app update gets a new generation instead of treating its
	// own new host.js as an edit to the previously registered version.
	previousHost := hostJS
	hostJS = append(slices.Clone(hostJS), []byte("\n// next app host\n")...)
	t.Cleanup(func() { hostJS = previousHost })
	p, err = StartJellytoolPlugins(ctx, resources, data)
	if err != nil {
		t.Fatalf("host-only app update did not register a fresh generation: %v", err)
	}
	p.Close()
	// A lock for another platform cannot select a different runtime.
	lock := jellytoolRuntimeLock{Version: BunVersion, OS: "other", Arch: "other", SHA256: strings.Repeat("0", 64)}
	b, _ := json.Marshal(lock)
	os.WriteFile(filepath.Join(resources, "runtime-lock.json"), b, 0o600)
	if p, err := StartJellytoolPlugins(ctx, resources, t.TempDir()); err == nil {
		p.Close()
		t.Fatal("a bundle for another platform was accepted")
	}
}
