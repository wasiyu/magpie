package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/testenv"
)

// A real private Bun host checks drafts against two local upstreams. Only a
// successful save may replace the key/config, and its protocol must change
// the model contract and the destination/auth of actual plugin requests.
func TestJellytoolConnectionDraftsAndCommit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	resources, data := t.TempDir(), t.TempDir()
	if err := PackJellytoolPlugins(ctx, testenv.Bun(t), resources); err != nil {
		t.Fatal(err)
	}
	serve := func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			if r.Header.Get("Authorization") == "Bearer bad-key" || r.Header.Get("X-API-Key") == "bad-key" {
				w.WriteHeader(403)
				return
			}
			fmt.Fprint(w, `{"data":[{"id":"vendor/text:exact","type":"TEXT"}]}`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/responses") && strings.HasPrefix(r.URL.Path, "/draft/") {
			w.WriteHeader(404)
			return
		}
		if r.Header.Get("Authorization") == "Bearer bad-key" || r.Header.Get("X-API-Key") == "bad-key" || r.Header.Get("X-Goog-Api-Key") == "bad-key" {
			w.WriteHeader(401)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if strings.Contains(r.URL.Path, ":generateContent") || strings.Contains(r.URL.Path, ":streamGenerateContent") {
			if r.Header.Get("X-Goog-Api-Key") != "new-key" || body["contents"] == nil {
				t.Error("Gemini protocol or key was not used")
			}
		} else if strings.HasSuffix(r.URL.Path, "/messages") {
			if r.Header.Get("X-API-Key") == "" || body["messages"] == nil {
				t.Error("Anthropic protocol or key was not used")
			}
		} else if strings.HasSuffix(r.URL.Path, "/systemone") && body["questions"] == nil {
			t.Error("System One probe did not use its real request format")
		}
		switch {
		case strings.Contains(r.URL.Path, "GenerateContent") || strings.Contains(r.URL.Path, "generateContent"):
			fmt.Fprint(w, `{"candidates":[]}`)
		case strings.HasSuffix(r.URL.Path, "/messages"):
			fmt.Fprint(w, `{"type":"message","content":[]}`)
		case strings.HasSuffix(r.URL.Path, "/responses"):
			fmt.Fprint(w, `{"object":"response","output":[]}`)
		case strings.HasSuffix(r.URL.Path, "/systemone"):
			fmt.Fprint(w, `{"answers":{"ok":true}}`)
		default:
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ping"}}]}`)
		}
	}
	a := httptest.NewServer(http.HandlerFunc(serve))
	defer a.Close()
	b := httptest.NewServer(http.HandlerFunc(serve))
	defer b.Close()
	p, err := startJellytoolPlugins(ctx, resources, data, map[string]any{"testOrigin": a.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { p.Close() }()
	call := func(in map[string]any) (map[string]any, error) {
		var result map[string]any
		err := p.h.call(ctx, "jellytool", in, &result)
		if _, leaked := result["auth"]; leaked {
			t.Fatal("auth leaked to the caller")
		}
		return result, err
	}
	defaults, err := call(map[string]any{"action": "config"})
	if err != nil || defaults["api"] != "responses" {
		t.Fatalf("new connections must default to Responses: %v, %v", defaults, err)
	}
	if err := p.APIKey(ctx, "old-key"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(data, "plugin-auth.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := call(map[string]any{"action": "save", "key": "bad-key"}); err == nil {
		t.Fatal("invalid candidate key was accepted")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("a refused key replaced the saved account")
	}
	result, err := call(map[string]any{"action": "detect", "baseURL": b.URL + "/draft/v1", "key": "new-key", "models": []string{"vendor/text:exact"}})
	if err != nil {
		t.Fatal(err)
	}
	rows := result["results"].([]any)
	if len(rows) != 5 {
		t.Fatalf("wanted all five protocols, got %d", len(rows))
	}
	for _, raw := range rows {
		r := raw.(map[string]any)
		if r["protocol"] == "responses" && r["ok"] != false {
			t.Fatal("a 404 was reported as supported")
		}
		if r["protocol"] != "responses" && r["ok"] != true {
			t.Fatalf("a valid protocol was not detected: %v", r)
		}
	}
	after, _ = os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("detecting a draft saved its key or endpoint")
	}
	for _, tc := range []struct{ api, npm, path, body string }{
		{"openai", "@ai-sdk/openai-compatible", "/prefix/v1/chat/completions", `{"model":"vendor/text:exact","messages":[]}`},
		{"responses", "@ai-sdk/openai", "/prefix/v1/responses", `{"model":"vendor/text:exact","input":"ping"}`},
		{"anthropic", "@ai-sdk/anthropic", "/prefix/v1/messages", `{"model":"vendor/text:exact","messages":[]}`},
		{"gemini", "@ai-sdk/google", "/prefix/v1beta/models/vendor%2Ftext:exact:generateContent", `{"contents":[]}`},
		{"decide", "@ai-sdk/openai-compatible", "/prefix/v1/systemone", `{"model":"vendor/text:exact","questions":{}}`},
	} {
		if _, err := call(map[string]any{"action": "save", "baseURL": b.URL + "/prefix/v1", "api": tc.api, "key": "new-key"}); err != nil {
			t.Fatal(err)
		}
		ps, err := p.Providers(ctx)
		if err != nil || len(ps) != 1 || len(ps[0].Models) != 1 || ps[0].Models[0].NPM != tc.npm {
			t.Fatalf("%s didn't change the runtime model protocol: %+v, %v", tc.api, ps, err)
		}
		resp, err := p.Fetch(ctx, FetchRequest{Provider: "jellytoken", URL: b.URL + tc.path, Method: "POST", Body: []byte(tc.body)})
		if err != nil {
			t.Fatalf("%s: %v", tc.api, err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("%s: %s", tc.api, resp.Status)
		}
	}
	p.Close()
	p, err = startJellytoolPlugins(ctx, resources, data, map[string]any{"testOrigin": a.URL})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := call(map[string]any{"action": "config"})
	if err != nil || saved["baseURL"] != b.URL+"/prefix/v1" || saved["api"] != "decide" || saved["signedIn"] != true {
		t.Fatalf("connection did not survive restart: %v, %v", saved, err)
	}
	if _, err := call(map[string]any{"action": "save", "baseURL": "https://user:secret@invalid.example/v1"}); err == nil {
		t.Fatal("credentials in Base URL were accepted")
	}
}
