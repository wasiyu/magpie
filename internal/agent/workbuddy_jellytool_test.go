//go:build jellytool

package agent

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/appdir"
	"github.com/yetone/magpie/internal/plugin"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/testenv"
)

func jellytoolWorkBuddyHome(t *testing.T, home string) {
	t.Helper()
	testenv.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	appdir.UseExecutable("")
}

func jellytoolWorkBuddyProvider(t *testing.T) {
	t.Helper()
	plugin.UseCached([]plugin.Provider{{
		ID: "jellytoken", Name: "Jellytoken", API: "https://aiservice.jellytoken.com/v1",
		SignedIn: true, Single: true,
		Methods:  []plugin.Method{{Type: "api", Label: "API key"}},
		Accounts: []plugin.Account{{Key: "jellytoken", Type: "api"}},
		Models: []plugin.Model{{ID: "vendor/text.v1:exact", Name: "vendor/text.v1:exact", NPM: "@ai-sdk/openai-compatible", Outputs: []string{"text"}},
			{ID: "vendor/other.v2:exact", Name: "vendor/other.v2:exact", NPM: "@ai-sdk/openai-compatible", Outputs: []string{"text"}}},
	}})
	t.Cleanup(func() { plugin.UseCached(nil); provider.Changed() })
	if err := provider.Save(provider.Provider{ID: "jellytoken", Name: "Jellytoken", Chat: "https://aiservice.jellytoken.com/v1", Key: "fixture-key", Models: []string{"vendor/text.v1:exact"}}); err != nil {
		t.Fatal(err)
	}
}

func TestJellytoolWorkBuddyUsesUpstreamProtocol(t *testing.T) {
	for _, tc := range []struct{ name, npm, api, endpoint, remote string }{
		{"responses", "@ai-sdk/openai", "openai-responses", "/responses", ""},
		{"chat", "@ai-sdk/openai-compatible", "", "/chat/completions", ""},
		{"remote chat", "@ai-sdk/openai", "", "/chat/completions", "http://192.168.1.20:3435/v1"},
		{"remote responses", "@ai-sdk/openai", "openai-responses", "/responses", "http://192.168.1.20:3435/v1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			jellytoolWorkBuddyHome(t, home)
			jellytoolWorkBuddyProvider(t)
			plugin.UseCached([]plugin.Provider{{
				ID: "jellytoken", Name: "Jellytoken", SignedIn: true,
				Accounts: []plugin.Account{{Key: "jellytoken", Type: "api"}},
				Models:   []plugin.Model{{ID: "vendor/text.v1:exact", NPM: tc.npm, Outputs: []string{"text"}}},
			}})
			provider.Changed()
			a := workbuddy(home)
			if tc.remote != "" {
				b, err := json.Marshal([]map[string]any{{"id": "jellytoken/vendor/text.v1:exact", "vendor": "jellytool", "url": tc.remote + tc.endpoint, "api": tc.api, "apiKey": "remote-key"}})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(a.Path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(a.Path, b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := a.Field("provider").Set(magpieID); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(a.Path)
			if err != nil {
				t.Fatal(err)
			}
			var models []map[string]any
			if err := json.Unmarshal(b, &models); err != nil || len(models) != 1 {
				t.Fatalf("WorkBuddy models: %s, %v", b, err)
			}
			m := models[0]
			api, _ := m["api"].(string)
			base := gatewayV1()
			if tc.remote != "" {
				base = tc.remote
				if m["apiKey"] != "remote-key" {
					t.Fatalf("remote key changed: %s", b)
				}
			}
			if m["url"] != base+tc.endpoint || api != tc.api {
				t.Fatalf("%s protocol was not written for WorkBuddy: %s", tc.name, b)
			}
			if m["id"] != "jellytoken/vendor/text.v1:exact" || m["vendor"] != "jellytool" {
				t.Fatalf("model identity changed: %s", b)
			}
		})
	}
}

func TestJellytoolWorkBuddyConnectSelectDisconnectRestore(t *testing.T) {
	home := t.TempDir()
	jellytoolWorkBuddyHome(t, home)
	t.Setenv("WORKBUDDY_CONFIG_DIR", "")
	notOnMac(t)

	// This represents a separately installed magpie configuration. Jellytool
	// must not read or rewrite it while it owns its own provider/agent state.
	oldMagpie := []byte("{\"provider\":\"legacy\",\"model\":\"legacy/model\"}\n")
	magpiePath := filepath.Join(home, ".config", "magpie", "providers.json")
	if err := os.MkdirAll(filepath.Dir(magpiePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(magpiePath, oldMagpie, 0o600); err != nil {
		t.Fatal(err)
	}

	original := []byte(`[{"id":"local-model","local":true,"url":"http://127.0.0.1:8080/v1/chat/completions"}]`)
	path := filepath.Join(home, ".workbuddy", "models.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	jellytoolWorkBuddyProvider(t)

	a := workbuddy(home)
	if !a.Detected() {
		t.Fatal("WorkBuddy was not detected")
	}
	if how, err := a.ConnectHow(); err != nil || how.How != "magpie" {
		t.Fatalf("connect: %+v %v", how, err)
	}
	connected, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(connected, original) || !bytes.Contains(connected, []byte("vendor/text.v1:exact")) {
		t.Fatalf("WorkBuddy selection was not written: %s", connected)
	}
	var models []map[string]any
	if err := json.Unmarshal(connected, &models); err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0]["id"] != "local-model" {
		t.Fatalf("local WorkBuddy model was not preserved: %s", connected)
	}
	if got := a.Field("provider").Get(); got != magpieID {
		t.Fatalf("provider after connect = %q", got)
	}

	// A provider selection change is reflected by the next sync, still without
	// touching the separately installed magpie config.
	if err := provider.Save(provider.Provider{ID: "jellytoken", Name: "Jellytoken", Chat: "https://aiservice.jellytoken.com/v1", Key: "fixture-key", Models: []string{"vendor/other.v2:exact"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	synced, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(synced, []byte("vendor/other.v2:exact")) || bytes.Contains(synced, []byte("vendor/text.v1:exact")) {
		t.Fatalf("selection was not refreshed: %s", synced)
	}

	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored, original) {
		t.Fatalf("WorkBuddy config was not restored byte-for-byte:\nwant %s\ngot  %s", original, restored)
	}
	if got := a.Field("provider").Get(); got != "" {
		t.Fatalf("provider after disconnect = %q", got)
	}
	magpieAfter, err := os.ReadFile(magpiePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(magpieAfter, oldMagpie) {
		t.Fatalf("existing magpie config changed: %s", magpieAfter)
	}

	if how, err := a.ConnectHow(); err != nil || how.How != "again" {
		t.Fatalf("reconnect: %+v %v", how, err)
	}
	reconnected, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(reconnected, []byte("vendor/other.v2:exact")) {
		t.Fatalf("reconnect did not use current selection: %s", reconnected)
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
}

func TestJellytoolWorkBuddyRestoresMissingAndObjectFiles(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		home := t.TempDir()
		jellytoolWorkBuddyHome(t, home)
		t.Setenv("WORKBUDDY_CONFIG_DIR", "")
		notOnMac(t)
		if err := os.MkdirAll(filepath.Join(home, ".workbuddy"), 0o700); err != nil {
			t.Fatal(err)
		}
		jellytoolWorkBuddyProvider(t)
		a := workbuddy(home)
		if _, err := a.ConnectHow(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(a.Path); err != nil {
			t.Fatalf("connect did not create models.json: %v", err)
		}
		if err := a.Disconnect(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(a.Path); !os.IsNotExist(err) {
			t.Fatalf("disconnect restored missing file as %v", err)
		}
	})

	t.Run("object", func(t *testing.T) {
		home := t.TempDir()
		jellytoolWorkBuddyHome(t, home)
		t.Setenv("WORKBUDDY_CONFIG_DIR", "")
		notOnMac(t)
		path := filepath.Join(home, ".workbuddy", "models.json")
		original := []byte("{\"models\":[{\"id\":\"local\",\"local\":true}],\"availableModels\":[\"local\"],\"other\":{\"keep\":1}}\n")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, original, 0o600); err != nil {
			t.Fatal(err)
		}
		jellytoolWorkBuddyProvider(t)
		a := workbuddy(home)
		if _, err := a.ConnectHow(); err != nil {
			t.Fatal(err)
		}
		if err := a.Disconnect(); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, original) {
			t.Fatalf("object file was not restored byte-for-byte:\nwant %s\ngot  %s", original, got)
		}
	})
}

func TestJellytoolWorkBuddyRestoresExistingMagpieWiring(t *testing.T) {
	home := t.TempDir()
	jellytoolWorkBuddyHome(t, home)
	t.Setenv("WORKBUDDY_CONFIG_DIR", "")
	notOnMac(t)
	path := filepath.Join(home, ".workbuddy", "models.json")
	original := []byte(`[{"id":"legacy/model","vendor":"magpie","apiKey":"magpie-workbuddy","url":"http://127.0.0.1:3425/v1/chat/completions"}]`)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	jellytoolWorkBuddyProvider(t)
	a := workbuddy(home)
	if how, err := a.ConnectHow(); err != nil || how.How != "magpie" {
		t.Fatalf("existing magpie wiring: %+v %v", how, err)
	}
	if err := provider.Save(provider.Provider{ID: "jellytoken", Name: "Jellytoken", Chat: "https://aiservice.jellytoken.com/v1", Key: "fixture-key", Models: []string{"vendor/other.v2:exact"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	changed, err := os.ReadFile(path)
	if err != nil || bytes.Equal(changed, original) || !bytes.Contains(changed, []byte("vendor/other.v2:exact")) || !bytes.Contains(changed, []byte("legacy/model")) || !bytes.Contains(changed, []byte("127.0.0.1:3435")) {
		t.Fatalf("existing wiring was not refreshed: %s %v", changed, err)
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored, original) {
		t.Fatalf("existing magpie wiring was not restored:\nwant %s\ngot  %s", original, restored)
	}
}

func TestJellytoolWorkBuddyBoundaries(t *testing.T) {
	for _, kind := range []string{"empty", "no-models", "edits", "backup-failure", "restore-failure", "collision"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			jellytoolWorkBuddyHome(t, home)
			t.Setenv("WORKBUDDY_CONFIG_DIR", "")
			path := filepath.Join(home, ".workbuddy", "models.json")
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			original := []byte(`[{"id":"local","local":true}]`)
			if kind == "empty" {
				original = nil
			}
			if kind == "collision" {
				original = []byte(`[{"id":"jellytoken/vendor/text.v1:exact","vendor":"magpie"}]`)
			}
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			jellytoolWorkBuddyProvider(t)
			a := workbuddy(home)
			// Merely constructing/reading the adapter never creates a restore point.
			_ = a.Values()
			if stashLoad()[jellytoolBuddyKey(path, a.ID)] != "" {
				t.Fatal("status read created a restore point")
			}
			if kind == "collision" {
				if _, err := a.ConnectHow(); err == nil {
					t.Fatal("duplicate WorkBuddy ID accepted")
				}
				got, _ := os.ReadFile(path)
				if a.Wired() || !bytes.Equal(got, original) {
					t.Fatal("failed collision check adopted or rewrote existing entry")
				}
				return
			}
			if kind == "backup-failure" {
				if err := os.MkdirAll(stashPath(), 0700); err != nil {
					t.Fatal(err)
				}
				if err := a.Field("provider").Set(magpieID); err == nil {
					t.Fatal("write ignored backup failure")
				}
				got, _ := os.ReadFile(path)
				if !bytes.Equal(got, original) {
					t.Fatal("failed backup changed WorkBuddy")
				}
				return
			}
			if _, err := a.ConnectHow(); err != nil {
				t.Fatal(err)
			}
			if kind == "no-models" {
				if err := provider.Save(provider.Provider{ID: "jellytoken", NoModels: true}); err != nil {
					t.Fatal(err)
				}
				if err := a.Sync(); err != nil {
					t.Fatal(err)
				}
				if !a.Wired() {
					t.Fatal("empty selection lost the disconnect path")
				}
			}
			if kind == "edits" {
				var entries []map[string]any
				b, _ := os.ReadFile(path)
				if err := json.Unmarshal(b, &entries); err != nil {
					t.Fatal(err)
				}
				entries = append(entries, map[string]any{"id": "new-user-model", "local": true})
				b, _ = json.Marshal(entries)
				if err := os.WriteFile(path, b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "restore-failure" {
				connected, _ := os.ReadFile(path)
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
				if err := a.Disconnect(); err == nil {
					t.Fatal("unreadable picker accepted")
				}
				if stashLoad()[jellytoolBuddyKey(path, a.ID)] == "" {
					t.Fatal("failed restore consumed backup")
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, connected, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := a.Disconnect(); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "edits" {
				if !bytes.Contains(got, []byte("new-user-model")) || bytes.Contains(got, []byte(`"vendor": "jellytool"`)) {
					t.Fatalf("disconnect lost user edits: %s", got)
				}
			} else if !bytes.Equal(got, original) {
				t.Fatalf("restore mismatch: %s", got)
			}
			if stashLoad()[jellytoolBuddyKey(path, a.ID)] != "" {
				t.Fatal("successful restore retained a stale backup")
			}
		})
	}
}

func TestJellytoolWorkBuddyPreview(t *testing.T) {
	home := t.TempDir()
	jellytoolWorkBuddyHome(t, home)
	t.Setenv("WORKBUDDY_CONFIG_DIR", "")
	jellytoolWorkBuddyProvider(t)
	a := workbuddy(home)
	if _, err := a.ConnectHow(); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(a.Path)
	snapshot, _ := os.ReadFile(stashPath())
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	changes, err := DisconnectPreview(a, exe)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || !changes[0].Removed {
		t.Fatalf("missing-file restore preview: %+v", changes)
	}
	after, _ := os.ReadFile(a.Path)
	remaining, _ := os.ReadFile(stashPath())
	if !bytes.Equal(before, after) || !bytes.Equal(snapshot, remaining) {
		t.Fatal("preview changed source config or stash")
	}
}
