//go:build jellytool

package plugin_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/appdir"
	"github.com/yetone/magpie/internal/gateway"
	"github.com/yetone/magpie/internal/plugin"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/testenv"
)

func TestJellytoolGatewayThroughPrivateHost(t *testing.T) {
	home := t.TempDir()
	testenv.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Cleanup(plugin.Settle)
	appdir.UseExecutable("")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Error("the gateway token reached the upstream instead of the plugin key")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"data":[{"id":"vendor/literal.v1:exact","type":"TEXT"}]}`)
			return
		}
		var body struct {
			Model   string
			Stream  bool
			Input   json.RawMessage
			Include []string
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Model != "vendor/literal.v1:exact" || !body.Stream || r.URL.Path != "/v1/responses" || len(body.Input) == 0 || len(body.Include) != 1 || body.Include[0] != "reasoning.encrypted_content" {
			t.Errorf("upstream request lost its literal model or path: %+v %s %v", body, r.URL.Path, err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: response.created\ndata: "+`{"type":"response.created","response":{"id":"resp_mock","object":"response","status":"in_progress","output":[]}}`+"\n\n")
		fmt.Fprint(w, "event: response.output_text.delta\ndata: "+`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"mock answer"}`+"\n\n")
		fmt.Fprint(w, "event: response.completed\ndata: "+`{"type":"response.completed","response":{"id":"resp_mock","object":"response","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`+"\n\n")
	}))
	defer upstream.Close()
	resources := t.TempDir()
	if err := plugin.PackJellytoolPlugins(ctx, testenv.Bun(t), resources); err != nil {
		t.Fatal(err)
	}
	private, err := plugin.ActivateJellytoolMockForTest(ctx, resources, appdir.Config(), upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer private.Close()
	defer provider.Changed()
	if _, err := plugin.APIKey(ctx, "jellytoken", 0, nil, "fixture-key", plugin.NewAccount); err != nil {
		t.Fatal(err)
	}
	provider.Changed()
	if err := provider.SetModels("jellytoken", []string{"vendor/literal.v1:exact"}); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := gateway.New()
	done := make(chan error, 1)
	go func() { done <- server.ServeJellytool(ctx, ln) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	req, err := http.NewRequestWithContext(ctx, "POST", "http://"+ln.Addr().String()+"/v1/responses", strings.NewReader(`{"model":"jellytoken/vendor/literal.v1:exact","input":[{"role":"user","content":"fixture"}],"stream":true,"include":["reasoning.encrypted_content"]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+gateway.Token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode != http.StatusOK || !strings.Contains(string(body), "mock answer") {
		t.Fatalf("private-host gateway response: %d %s %v", res.StatusCode, body, err)
	}
}
