//go:build jellytool

package plugin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/testenv"
)

func TestJellytoolActiveAdapter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	resources, data := t.TempDir(), t.TempDir()
	if err := PackJellytoolPlugins(ctx, testenv.Bun(t), resources); err != nil {
		t.Fatal(err)
	}
	requests := make(chan string, 20)
	var catalogDenied atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if catalogDenied.Load() && r.URL.Path == "/v1/models" {
			w.WriteHeader(403)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Errorf("wrong plugin authorization")
		}
		requests <- r.URL.Path
		if r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"data":[{"id":"vendor/literal.v1:exact","type":"TEXT"}]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: fixture\n\n")
	}))
	defer srv.Close()
	p, err := startJellytoolPlugins(ctx, resources, data, map[string]any{"testOrigin": srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if !jellytoolActive.CompareAndSwap(nil, p) {
		t.Fatal("already active")
	}
	defer p.Close()
	// A malicious generic plugin registry must never replace this host.
	if err := os.MkdirAll(filepath.Dir(ListPath()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ListPath(), []byte(`{"plugins":[{"spec":"https://evil.invalid/plugin.js"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "plugins.json"), []byte(`{"plugins":[{"spec":"evil"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := APIKey(ctx, "other", 0, nil, "fixture-key", NewAccount); err == nil {
		t.Fatal("other provider admitted")
	}
	saved, err := APIKey(ctx, "jellytoken", 0, nil, "fixture-key", NewAccount)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Account != "jellytoken" {
		t.Fatal(saved)
	}
	if _, err := NextPrompt(ctx, "jellytoken", 0, nil); err != nil {
		t.Fatal(err)
	}
	opts, err := LoaderOptions(ctx, "jellytoken", "jellytoken")
	if err != nil || !opts.Fetch {
		t.Fatal(opts, err)
	}
	if ps := Cached(); len(ps) != 1 || ps[0].ID != "jellytoken" || len(ps[0].Models) != 1 {
		t.Fatal(ps)
	}
	response, err := Fetch(ctx, FetchRequest{Provider: "jellytoken", Account: "jellytoken", Model: "vendor/literal.v1:exact", NPM: "@ai-sdk/openai-compatible", URL: srv.URL + "/v1/chat/completions", Method: "POST", Body: []byte(`{"model":"vendor/literal.v1:exact"}`)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || !strings.Contains(string(b), "data: fixture") {
		t.Fatal(string(b), err)
	}
	for _, method := range []string{"init", "reload", "apiKey", "authorize", "callback", "tools", "checkin"} {
		if err := Call(ctx, method, map[string]any{}, nil); err == nil {
			t.Errorf("arbitrary %s admitted", method)
		}
	}
	select {
	case got := <-requests:
		if got != "/v1/models" {
			t.Fatal(got)
		}
	default:
		t.Fatal("model hook was never reached")
	}

	catalogDenied.Store(true)
	if _, err := Providers(ctx); err != nil {
		t.Fatal(err)
	}
	if ps := Cached(); len(ps) != 1 || len(ps[0].Models) != 0 || ps[0].ListError == "" {
		t.Fatalf("failed permission read retained models: %+v", ps)
	}
	// Replacing a key while its catalog is denied cannot reuse the old catalog.
	if _, err := APIKey(ctx, "jellytoken", 0, nil, "fixture-key", NewAccount); err == nil {
		t.Fatal("a refused candidate key was saved")
	}
	if ps := Cached(); len(ps) != 1 || len(ps[0].Models) != 0 {
		t.Fatalf("replacement key inherited models: %+v", ps)
	}
	catalogDenied.Store(false)
	if _, err := Providers(ctx); err != nil {
		t.Fatal(err)
	}
	if ps := Cached(); len(ps) != 1 || len(ps[0].Models) != 1 {
		t.Fatalf("retry did not recover catalog: %+v", ps)
	}
	if err := SignOut(ctx, "jellytoken", "jellytoken"); err != nil {
		t.Fatal(err)
	}
	if ps := Cached(); len(ps) != 1 || ps[0].SignedIn {
		t.Fatalf("sign-out retained the bundled account: %+v", ps)
	}
	p.Close()
	if _, err := get(ctx); err == nil {
		t.Fatal("closed host was silently replaced")
	}
}

// ActivateJellytoolMockForTest exposes the private loopback seam only to this
// package's external integration tests; it is absent from application builds.
func ActivateJellytoolMockForTest(ctx context.Context, resources, data, origin string) (*JellytoolPlugins, error) {
	p, err := startJellytoolPlugins(ctx, resources, data, map[string]any{"testOrigin": origin})
	if err != nil {
		return nil, err
	}
	if !jellytoolActive.CompareAndSwap(nil, p) {
		p.Close()
		return nil, fmt.Errorf("Jellytool test host already active")
	}
	return p, nil
}
