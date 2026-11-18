package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/testenv"
)

// Official task API fields are used under every saved text protocol. The
// gateway-facing adapter accepts multipart edits and exposes resumable videos.
func TestJellytoolMediaRoutes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	image := []byte("\x89PNG\r\n\x1a\n\x00\xff")
	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	_ = mw.WriteField("model", "vendor/image:exact")
	_ = mw.WriteField("prompt", "circle")
	part, err := mw.CreateFormFile("image", "ref.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(image)
	_ = mw.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-key" || r.Header.Get("Cookie") != "" || r.Header.Get("X-API-Key") != "" {
			t.Error("media credentials were not replaced")
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/prefix/v1/models":
			fmt.Fprint(w, `{"data":[{"id":"vendor/image:exact","type":"IMAGE"},{"id":"vendor/video:exact","type":"VIDEO"}]}`)
		case r.Method == "POST" && r.URL.Path == "/prefix/api/ai/tasks":
			var body struct {
				ModelKey, CallbackID     string
				ImageParams, VideoParams map[string]any
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.CallbackID == "" || !strings.HasPrefix(body.ModelKey, "vendor/") {
				t.Error("task lost model or idempotency ID")
			}
			if body.ModelKey == "vendor/refused" {
				fmt.Fprint(w, `{"taskId":"failed","status":"failed","error":"fixture refusal"}`)
				return
			}
			if refs, ok := body.ImageParams["refImages"].([]any); ok && len(refs) > 0 && !strings.HasPrefix(fmt.Sprint(refs[0]), "https://") {
				t.Error("reference URL changed")
			}
			fmt.Fprint(w, `{"taskId":"task.123","status":"completed","resultUrlPublic":"https://cdn.example.com/image.png"}`)
		case r.Method == "GET" && r.URL.Path == "/prefix/api/ai/tasks/task.123":
			fmt.Fprint(w, `{"taskId":"task.123","status":"completed","progress":100,"resultUrlPublic":"https://cdn.example.com/video.mp4"}`)
		default:
			t.Errorf("unsupported upstream request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	resources, data := t.TempDir(), t.TempDir()
	if err := PackJellytoolPlugins(ctx, testenv.Bun(t), resources); err != nil {
		t.Fatal(err)
	}
	p, err := startJellytoolPlugins(ctx, resources, data, map[string]any{"testOrigin": srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for _, api := range []string{"openai", "responses", "anthropic", "gemini", "decide"} {
		var config map[string]any
		if err := p.h.call(ctx, "jellytool", map[string]any{"action": "save", "api": api, "baseURL": srv.URL + "/prefix/v1", "key": "fixture-key"}, &config); err != nil {
			t.Fatal(err)
		}
		ps, err := p.Providers(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range ps[0].Models {
			if m.NPM != "@ai-sdk/openai-compatible" || m.Decides {
				t.Fatalf("%s changed media protocol: %+v", api, m)
			}
		}
		base := srv.URL + "/prefix/v1"
		if api == "gemini" {
			base += "beta"
		}
		call := func(method, path, ct string, body []byte) (*http.Response, []byte) {
			t.Helper()
			res, err := p.Fetch(ctx, FetchRequest{Provider: "jellytoken", Model: "vendor/image:exact", URL: base + path, Method: method, Headers: map[string]string{"content-type": ct, "authorization": "Bearer wrong", "cookie": "secret", "x-api-key": "wrong"}, Body: body})
			if err != nil {
				t.Fatalf("%s %s: %v", method, path, err)
			}
			b, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			return res, b
		}
		for _, body := range [][]byte{[]byte(`{"model":"vendor/image:exact","prompt":"circle","size":"16:9"}`), []byte(`{"model":"vendor/image:exact","prompt":"blue circle","reference_images":["https://example.com/ref.png"]}`)} {
			res, b := call("POST", "/images/generations", "application/json", body)
			if res.StatusCode != 200 || !bytes.Contains(b, []byte(`"url":"https://cdn.example.com/image.png"`)) {
				t.Fatalf("image result: %s", b)
			}
		}
		if _, err := p.Fetch(ctx, FetchRequest{Provider: "jellytoken", Model: "vendor/image:exact", URL: base + "/images/edits", Method: "POST", Headers: map[string]string{"content-type": mw.FormDataContentType()}, Body: form.Bytes()}); err == nil || !strings.Contains(err.Error(), "public https URL") {
			t.Fatal("local multipart editing was not refused clearly")
		}
		_, b := call("POST", "/videos", "application/json", []byte(`{"model":"vendor/video:exact","prompt":"circle","seconds":"1"}`))
		var v struct{ ID string }
		if err := json.Unmarshal(b, &v); err != nil || v.ID == "" {
			t.Fatalf("video result: %s %v", b, err)
		}
		res, b := call("GET", "/videos/"+v.ID, "", nil)
		if res.StatusCode != 200 || !bytes.Contains(b, []byte(`"status":"completed"`)) {
			t.Fatalf("video status: %s", b)
		}
		res, _ = call("GET", "/videos/"+v.ID+"/content", "", nil)
		if res.StatusCode != 307 || res.Header.Get("Location") != "https://cdn.example.com/video.mp4" {
			t.Fatal("video URL did not reach guarded gateway download")
		}
		if res, err := p.Fetch(ctx, FetchRequest{Provider: "jellytoken", URL: base + "/images/generations", Method: "POST", Body: []byte(`{"model":"vendor/refused","prompt":"circle"}`)}); err == nil {
			res.Body.Close()
			t.Fatal("a failed task was reported as an image")
		} else if !strings.Contains(err.Error(), "fixture refusal") {
			t.Fatalf("task refusal lost its reason: %v", err)
		}
		for _, route := range []struct{ method, path string }{{"GET", "/images/generations"}, {"DELETE", "/videos/video_123"}, {"POST", "/videos/video_123"}, {"GET", "/videos"}, {"GET", "/videos/video_123/secret"}, {"POST", "/images/not-supported"}} {
			res, err := p.Fetch(ctx, FetchRequest{Provider: "jellytoken", URL: base + route.path, Method: route.method})
			if err == nil {
				res.Body.Close()
				t.Fatalf("unsupported route admitted: %s %s", route.method, route.path)
			}
		}
	}
}
