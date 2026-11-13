//go:build jellytool

package plugin_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/appdir"
	"github.com/yetone/magpie/internal/gateway"
	"github.com/yetone/magpie/internal/plugin"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/settings"
	"github.com/yetone/magpie/internal/testenv"
)

// Real MCP HTTP requests pass through the gateway and packaged private Bun
// host. Output kinds must work even for names the built-in catalog doesn't know.
func TestJellytoolMediaThroughGateway(t *testing.T) {
	plugin.Settle()
	home := t.TempDir()
	testenv.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Cleanup(plugin.Settle)
	appdir.UseExecutable("")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aXioAAAAASUVORK5CYII=")
	mp4 := []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom-test")
	var mu sync.Mutex
	var seen []string
	var privateURL, privateReached atomic.Bool
	privateFile := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		privateReached.Store(true)
		w.Write(mp4)
	}))
	defer privateFile.Close()
	var up *httptest.Server
	up = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/made.png" || r.URL.Path == "/made.mp4" {
			if r.Header.Get("Authorization") != "" {
				t.Error("API key leaked to media download")
			}
			if r.URL.Path == "/made.png" {
				w.Header().Set("Content-Type", "image/png")
				w.Write(png)
			} else {
				w.Header().Set("Content-Type", "video/mp4")
				w.Write(mp4)
			}
			return
		}
		if r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Error("upstream did not get the plugin's key")
		}
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/models":
			fmt.Fprint(w, `{"data":[{"id":"fixture-art:exact","type":"IMAGE"},{"id":"fixture-motion:exact","type":"VIDEO"},{"id":"gpt-image-text-only","type":"TEXT"}]}`)
		case "POST /api/ai/tasks":
			var body struct {
				ModelKey, CallbackID     string
				ImageParams, VideoParams map[string]any
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.CallbackID == "" {
				t.Error("no idempotency ID")
			}
			id := "art"
			if body.VideoParams != nil {
				id = "motion"
				if body.ModelKey != "fixture-motion:exact" || body.VideoParams["duration"] != float64(1) {
					t.Errorf("video task: %+v", body)
				}
			} else {
				if body.ModelKey != "fixture-art:exact" {
					t.Errorf("image task: %+v", body)
				}
				if body.ImageParams["prompt"] == "blue circle" {
					refs, _ := body.ImageParams["refImages"].([]any)
					if len(refs) != 1 || !strings.HasPrefix(fmt.Sprint(refs[0]), up.URL) {
						t.Error("reference URL was lost")
					}
				}
			}
			fmt.Fprintf(w, `{"taskId":%q,"status":"queued"}`, id)
		case "GET /api/ai/tasks/art":
			fmt.Fprintf(w, `{"taskId":"art","status":"completed","resultUrlPublic":%q}`, up.URL+"/made.png")
		case "GET /api/ai/tasks/motion":
			at := up.URL + "/made.mp4"
			if privateURL.Load() {
				at = privateFile.URL + "/secret.mp4"
			}
			fmt.Fprintf(w, `{"taskId":"motion","status":"completed","progress":100,"resultUrlPublic":%q}`, at)
		default:
			t.Errorf("unexpected upstream request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer up.Close()
	resources := t.TempDir()
	if err := plugin.PackJellytoolPlugins(ctx, testenv.Bun(t), resources); err != nil {
		t.Fatal(err)
	}
	private, err := plugin.ActivateJellytoolMockForTest(ctx, resources, appdir.Config(), up.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer private.Close()
	defer provider.Changed()
	if _, err := plugin.APIKey(ctx, "jellytoken", 0, nil, "fixture-key", plugin.NewAccount); err != nil {
		t.Fatal(err)
	}
	provider.Changed()
	s := settings.Load()
	s.ImageGen = "jellytoken/fixture-art:exact"
	s.FetchPrivate = []string{strings.TrimPrefix(up.URL, "http://")}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	p, err := provider.Find("jellytoken")
	if err != nil {
		t.Fatal(err)
	}
	if ms := gateway.Drawers(*p); len(ms) != 1 || ms[0].ID != "fixture-art:exact" {
		t.Fatalf("image catalog: %+v", ms)
	}
	if ms := gateway.Videomakers(*p); len(ms) != 1 || ms[0].ID != "fixture-motion:exact" {
		t.Fatalf("video catalog: %+v", ms)
	}
	front := httptest.NewServer(gateway.New().Front())
	defer front.Close()
	type answer struct {
		Content []struct{ Type, Text, Data string }
		IsError bool
		Meta    map[string]json.RawMessage `json:"_meta"`
	}
	call := func(name string, args map[string]any) answer {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
		req, _ := http.NewRequestWithContext(ctx, "POST", front.URL+gateway.ImageMCPPath, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var reply struct {
			Result answer
			Error  any
		}
		if err := json.NewDecoder(res.Body).Decode(&reply); err != nil || res.StatusCode != 200 || reply.Error != nil || reply.Result.IsError {
			t.Fatalf("%s failed: HTTP %d %+v %v", name, res.StatusCode, reply, err)
		}
		return reply.Result
	}
	for _, args := range []map[string]any{{"prompt": "red circle"}, {"prompt": "blue circle", "reference_images": []string{up.URL + "/made.png"}}} {
		a := call("generate_image", args)
		if len(a.Content) != 2 || a.Content[1].Data != base64.StdEncoding.EncodeToString(png) {
			t.Fatalf("image result: %+v", a)
		}
		var files []gateway.MadeFile
		if err := json.Unmarshal(a.Meta[gateway.FilesMeta], &files); err != nil || len(files) != 1 {
			t.Fatalf("image files: %+v %v", files, err)
		}
		res, err := http.Get(files[0].URL)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || !bytes.Equal(got, png) {
			t.Fatal("image download changed")
		}
	}
	a := call("generate_video", map[string]any{"prompt": "moving circle", "seconds": 1})
	var files []gateway.MadeFile
	if err := json.Unmarshal(a.Meta[gateway.FilesMeta], &files); err != nil || len(files) != 1 {
		t.Fatalf("video files: %+v %v, answer %+v", files, err, a)
	}
	res, err := http.Get(files[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !bytes.Equal(got, mp4) {
		t.Fatalf("video download: HTTP %d %q", res.StatusCode, got)
	}
	privateURL.Store(true)
	res, err = http.Get(files[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 502 || privateReached.Load() {
		t.Fatal("a task result URL bypassed the gateway's private-address guard")
	}
	mu.Lock()
	defer mu.Unlock()
	for _, route := range []string{"POST /api/ai/tasks", "GET /api/ai/tasks/art", "GET /api/ai/tasks/motion"} {
		if !slices.Contains(seen, route) {
			t.Fatalf("missing %s in %s", route, strings.Join(seen, ", "))
		}
	}
}
