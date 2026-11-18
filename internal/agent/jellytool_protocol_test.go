//go:build jellytool

package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/provider"
)

func jellytoolProtocolHome(t *testing.T, protocols ...string) string {
	t.Helper()
	home := t.TempDir()
	jellytoolWorkBuddyHome(t, home)
	for _, api := range protocols {
		base := "https://" + api + ".fixture.invalid/v1"
		p := provider.Provider{ID: api, Name: api, Key: "fixture-key", Models: []string{"literal-model"}}
		switch api {
		case "responses":
			p.Responses = base
		case "chat":
			p.Chat = base
		}
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
		if err := catalog.SaveLive(api, base, []catalog.Model{{ID: "literal-model", Efforts: []string{"low", "high"}}}); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func TestJellytoolOpenCodeNativeProtocol(t *testing.T) {
	jellytoolProtocolHome(t, "responses", "chat")
	for _, id := range []string{"opencode", "mimocode", "deveco", "air"} {
		// as the file holds it: the models are kept in order (orderedJSON)
		b, err := json.Marshal(magpieProviderJSONFor("opencode", id))
		if err != nil {
			t.Fatal(err)
		}
		var p map[string]any
		if err := json.Unmarshal(b, &p); err != nil {
			t.Fatal(err)
		}
		ms := p["models"].(map[string]any)
		response := ms["responses/literal-model"].(map[string]any)
		sdk, _ := response["provider"].(map[string]any)
		if sdk["npm"] != "@ai-sdk/openai" {
			t.Fatalf("%s still sends a Responses-native model through Chat: %v", id, response)
		}
		chat := ms["chat/literal-model"].(map[string]any)
		if chat["provider"] != nil || p["npm"] != "@ai-sdk/openai-compatible" {
			t.Fatalf("%s changed its Chat-only model: %v", id, p)
		}
	}
}

func TestJellytoolProviderProtocolDefaults(t *testing.T) {
	for _, tc := range []struct {
		name, pencilAPI, zcodeAPI string
		protocols                 []string
	}{
		{"responses only", "openai-responses", "openai-responses", []string{"responses"}},
		{"mixed", "openai-completions", "anthropic-messages", []string{"responses", "chat"}},
		{"chat only", "openai-completions", "anthropic-messages", []string{"chat"}},
		{"no models", "openai-completions", "anthropic-messages", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := jellytoolProtocolHome(t, tc.protocols...)
			if p := pencilProviderJSON("http://127.0.0.1:3435"); p["api"] != tc.pencilAPI {
				t.Fatalf("Pencil protocol = %v, want %s", p["api"], tc.pencilAPI)
			}
			dir := filepath.Join(home, ".zcode", "v2")
			if got := zcodeFormat(dir); got != tc.zcodeAPI {
				t.Fatalf("ZCode protocol = %s, want %s", got, tc.zcodeAPI)
			}
			if err := edit.SetJSON(filepath.Join(dir, "config.json"), edit.KV{Path: "provider.magpie.kind", Value: "openai-compatible"}); err != nil {
				t.Fatal(err)
			}
			if got := zcodeFormat(dir); got != "openai-chat-completions" {
				t.Fatalf("ZCode overwrote a saved protocol: %s", got)
			}
		})
	}
}

func TestJellytoolOpenCodeResponsesVariants(t *testing.T) {
	if got := openCodeResponsesVariant("high-fast"); !reflect.DeepEqual(got, map[string]any{"reasoningEffort": "high", "serviceTier": "priority"}) {
		t.Fatalf("Responses SDK options: %v", got)
	}
	home := jellytoolProtocolHome(t, "responses")
	path := filepath.Join(home, ".config", "opencode", "opencode.json")
	writeFile(t, path, `{"theme":"dark","provider":{"magpie":{"models":{"responses/literal-model":{"variants":{
	  "max-fast":{"reasoningEffort":"max","serviceTier":"priority"},
	  "max":{"reasoningEffort":"max"},
	  "high":{"reasoningEffort":"low","textVerbosity":"low"}
	}}}}}}`)
	a := opencode(home, filepath.Join(home, ".config"))
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Theme    string `json:"theme"`
		Provider map[string]struct {
			Models map[string]struct {
				Variants map[string]map[string]any `json:"variants"`
			} `json:"models"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	variants := cfg.Provider["magpie"].Models["responses/literal-model"].Variants
	if cfg.Theme != "dark" || variants["high"]["textVerbosity"] != "low" || variants["max-fast"] != nil || variants["max"] != nil {
		t.Fatalf("sync lost user settings or retained obsolete generated variants: %s", b)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(b) {
		t.Fatal("repeated sync changed the same Responses configuration")
	}
}
