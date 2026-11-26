//go:build jellytool

package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/filelock"
	"github.com/yetone/magpie/internal/gateway"
	"github.com/yetone/magpie/internal/product"
)

func TestCodexIsolationPeerOwnsConfig(t *testing.T) {
	peer := codexPeerID()
	for _, profile := range []bool{false, true} {
		t.Run(map[bool]string{false: "top", true: "profile"}[profile], func(t *testing.T) {
			cfg := "model = \"fake/m1\"\nmodel_provider = \"" + peer + "\"\n"
			if profile {
				cfg = "profile = \"other\"\nmodel = \"fake/m1\"\n[profiles.other]\nmodel_provider = \"" + peer + "\"\n"
			}
			cfg += "[model_providers." + peer + "]\nbase_url = \"http://127.0.0.1:9999/v1\"\n"
			home, read := codexHome(t, "", cfg)
			cx := codex(home)
			if cx.Wired() {
				t.Fatal("peer model mistaken for own wiring")
			}
			for _, op := range []func() error{cx.Sync, cx.Unwire, cx.Disconnect, cx.EndFailover} {
				if err := op(); err != nil {
					t.Fatal(err)
				}
				if read() != cfg {
					t.Fatalf("peer configuration changed:\n%s", read())
				}
			}
			if _, err := cx.Join(); err != nil {
				t.Fatal(err)
			}
			if read() != cfg {
				t.Fatal("Join changed peer configuration")
			}
		})
	}
}

func TestCodexIsolationExplicitSwitch(t *testing.T) {
	peer := codexPeerID()
	cfg := "model = \"fake/m1\"\nmodel_provider = \"" + peer + "\"\n[model_providers." + peer + "]\nname = \"peer\"\nbase_url = \"http://127.0.0.1:9999/v1\"\n"
	home, read := codexHome(t, "", cfg)
	cx := codex(home)
	if err := cx.Field("model").Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	p, _ := edit.GetTOMLTop(cx.Path, "model_provider")
	if p != codexProviderID {
		t.Fatalf("provider %s", p)
	}
	tb, _ := edit.GetTOMLTable(cx.Path, "model_providers."+peer)
	if tb["base_url"] != "http://127.0.0.1:9999/v1" {
		t.Fatal("peer table changed")
	}
	profile, err := edit.GetTOMLTable(cx.Path, "profiles."+codexProviderID)
	if err != nil || profile != nil {
		t.Fatalf("switch created a legacy profile: %v, %v", profile, err)
	}
	catalog, _ := edit.GetTOMLTop(cx.Path, "model_catalog_json")
	if _, err := os.Stat(catalog); err != nil {
		t.Fatal(err)
	}
	if err := cx.Unwire(); err != nil {
		t.Fatal(err)
	}
	if p, _ := edit.GetTOMLTop(cx.Path, "model_provider"); p != peer {
		t.Fatalf("peer not restored: %s", read())
	}
	if _, err := os.Stat(catalog); err != nil {
		t.Fatal("switch removed reusable catalog", err)
	}
	if m, _ := edit.GetTOMLTop(cx.Path, "model"); m != "fake/m1" {
		t.Fatal("peer model lost", read())
	}
	if tb, _ := edit.GetTOMLTable(cx.Path, "model_providers."+codexProviderID); tb == nil {
		t.Fatal("provider needed by history removed")
	}
}

func TestCodexIsolationSwitchRoundTrip(t *testing.T) {
	const auth = `{"OPENAI_API_KEY":"fixture"}`
	const peerCatalog = `{"models":[],"custom":"keep"}`
	const base = "http://127.0.0.1:3425/backend-api/codex"
	cfg := "model_provider = \"magpie\"\nmodel = \"fake/m1\"\nmodel_catalog_json = \"magpie-models.json\"\nopenai_base_url = " + quoteTOML(base) + "\n" +
		"[model_providers.magpie]\nname = \"Magpie\"\nbase_url = \"http://127.0.0.1:3425/v1\"\nexperimental_bearer_token = \"peer-key\"\n" +
		"[model_providers.magpie.http_headers]\nX-Custom = \"keep\"\n"
	home, read := codexHome(t, auth, cfg)
	dir := filepath.Join(home, ".codex")
	if err := os.WriteFile(filepath.Join(dir, "magpie-models.json"), []byte(peerCatalog), 0600); err != nil {
		t.Fatal(err)
	}
	cx := codex(home)
	for round := 0; round < 3; round++ {
		if _, err := cx.ConnectHow(); err != nil {
			t.Fatal(err)
		}
		if p, _ := edit.GetTOMLTop(cx.Path, "model_provider"); p != "jellytool" {
			t.Fatalf("round %d: did not select Jellytool: %s", round, read())
		}
		if err := cx.Sync(); err != nil {
			t.Fatal(err)
		}
		if err := cx.Disconnect(); err != nil {
			t.Fatal(err)
		}
		// magpie's own keys are magpie's to put back: Jellytool's connect
		// took them over, its disconnect clears its own and restores none
		// (magpie's base URL, which a sign-in by API key never takes, stays,
		// and with it Codex is magpie's again: the model is left as it is)
		for key, want := range map[string]string{"model_provider": "", "model": "fake/m1", "model_catalog_json": "", "openai_base_url": base} {
			if got, _ := edit.GetTOMLTop(cx.Path, key); got != want {
				t.Fatalf("round %d: %s = %q, want %q", round, key, got, want)
			}
		}
		peer, _ := edit.GetTOMLTable(cx.Path, "model_providers.magpie")
		headers, _ := edit.GetTOMLTable(cx.Path, "model_providers.magpie.http_headers")
		if peer["base_url"] != "http://127.0.0.1:3425/v1" || peer["experimental_bearer_token"] != "peer-key" || headers["X-Custom"] != "keep" {
			t.Fatalf("round %d: peer connection changed: %s", round, read())
		}
		before := read()
		if err := cx.Sync(); err != nil {
			t.Fatal(err)
		}
		if read() != before || cx.Wired() {
			t.Fatalf("round %d: reclaimed Magpie's connection", round)
		}
		for file, want := range map[string]string{"auth.json": auth, "magpie-models.json": peerCatalog} {
			if got, err := os.ReadFile(filepath.Join(dir, file)); err != nil || string(got) != want {
				t.Fatalf("round %d: %s changed: %v", round, file, err)
			}
		}
	}
}

func TestCodexIsolationJellytoolWithChatGPT(t *testing.T) {
	if !product.Jellytool {
		t.Skip("Jellytool build")
	}
	home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, "model = \"gpt-5.5\"\n")
	cx := codex(home)
	if joined, err := cx.Join(); err != nil || !joined {
		t.Fatalf("join beside subscription: %v, %v", joined, err)
	}
	if err := cx.Field("model").Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(), `model_provider = "jellytool"`) || !strings.Contains(read(), codexGatewayURL()) {
		t.Fatal(read())
	}
	tb, _ := edit.GetTOMLTable(cx.Path, "model_providers.jellytool")
	if tb["requires_openai_auth"] != "true" {
		t.Fatal("ChatGPT sign-in lost")
	}
	if cx.Field("login").Get() == "api" {
		t.Fatal("incorrect sign-in display")
	}
	if err := cx.Field("model").Set("gpt-5.5"); err != nil || !cx.Wired() {
		t.Fatalf("subscription pick lost connection: %v, %s", err, read())
	}
}

func TestCodexIsolationUpgradeSubscription(t *testing.T) {
	home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, "model_provider = \"jellytool\"\nmodel = \"fake/m1\"\nmodel_catalog_json = \"jellytool-models.json\"\n[model_providers.jellytool]\nbase_url = "+quoteTOML(gateway.URL()+"/v1")+"\n")
	cx := codex(home)
	if err := edit.SetTOMLTop(cx.Path, edit.KV{Path: "openai_base_url", Value: codexGatewayURL()}); err != nil {
		t.Fatal(err)
	}
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if !cx.Beside() || !strings.Contains(read(), `model = "fake/m1"`) {
		t.Fatal(read())
	}
}

func TestCodexIsolationLegacyCatalogPreserved(t *testing.T) {
	if !product.Jellytool {
		t.Skip("Jellytool build")
	}
	home, _ := codexHome(t, "", "model = \"fake/m1\"\nmodel_provider = \"magpie\"\n[model_providers.magpie]\nbase_url = \""+gateway.URL()+"/v1\"\n")
	old := filepath.Join(home, ".codex", "magpie-models.json")
	os.WriteFile(old, []byte(`{"models":[],"custom":true}`), 0600)
	cx := codex(home)
	if err := cx.Field("model").Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(old)
	if err != nil || string(b) != `{"models":[],"custom":true}` {
		t.Fatal("legacy catalog modified", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "jellytool-models.json")); err != nil {
		t.Fatal(err)
	}
}

func TestCodexIsolationKeepsUserProfile(t *testing.T) {
	home, _ := codexHome(t, "", "model = \"gpt-5.5\"\n[profiles."+codexProviderID+"]\nmodel_provider = \"mine\"\nmodel = \"custom\"\n")
	cx := codex(home)
	if err := cx.Field("model").Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	tb, _ := edit.GetTOMLTable(cx.Path, "profiles."+codexProviderID)
	if tb["model_provider"] != "mine" || tb["model"] != "custom" {
		t.Fatalf("user profile replaced: %v", tb)
	}
}

func TestCodexIsolationLockContention(t *testing.T) {
	home, read := codexHome(t, "", "model = \"gpt-5.5\"\n")
	cx := codex(home)
	before := read()
	unlock, held := filelock.Lock(cx.Path+".provider.lock", 0)
	if !held {
		t.Fatal("lock unavailable")
	}
	defer unlock()
	if err := cx.Field("model").Set("fake/m1"); err == nil {
		t.Fatal("wrote without acquiring lock")
	}
	if read() != before {
		t.Fatal("lock failure changed configuration")
	}
}

func TestCodexIsolationUserProfileKeepsCatalog(t *testing.T) {
	home, _ := codexHome(t, `{"OPENAI_API_KEY":"fixture"}`, "model = \"gpt-5.5\"\n[profiles.mywork]\nmodel_provider = \""+codexProviderID+"\"\nmodel_catalog_json = \"./"+codexProviderID+"-models.json\"\n")
	cx := codex(home)
	if err := cx.Field("model").Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	if err := cx.Unwire(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", codexProviderID+"-models.json")); err != nil {
		t.Fatal("user profile lost catalog", err)
	}
}

func TestCodexIsolationModernProfileKeepsCatalog(t *testing.T) {
	// An existing unrelated legacy profile prevents the old implementation
	// from creating its own profile and accidentally keeping the catalog.
	home, _ := codexHome(t, `{"OPENAI_API_KEY":"fixture"}`, "model = \"gpt-5.5\"\n[profiles.jellytool]\nmodel_provider = \"mine\"\n")
	dir := filepath.Join(home, ".codex")
	path := filepath.Join(dir, "work.config.toml")
	profile := "model_provider = \"jellytool\"\nmodel = \"fake/m1\"\nmodel_catalog_json = \"./jellytool-models.json\"\n"
	if err := os.WriteFile(path, []byte(profile), 0600); err != nil {
		t.Fatal(err)
	}
	cx := codex(home)
	if _, err := cx.ConnectHow(); err != nil {
		t.Fatal(err)
	}
	if err := cx.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != profile {
		t.Fatal("user's profile changed", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "jellytool-models.json")); err != nil {
		t.Fatal("modern profile lost its catalog", err)
	}
}

func TestCodexIsolationCustomProviderStands(t *testing.T) {
	cfg := "model = \"fake/m1\"\nmodel_provider = \"mine\"\n[model_providers.mine]\nbase_url = \"https://relay.example/v1\"\n[model_providers." + codexPeerID() + "]\nbase_url = \"http://127.0.0.1:9999/v1\"\n"
	home, read := codexHome(t, "", cfg)
	cx := codex(home)
	for _, op := range []func() error{cx.Sync, cx.Unwire, cx.Disconnect} {
		if err := op(); err != nil {
			t.Fatal(err)
		}
		if read() != cfg {
			t.Fatal("custom provider configuration changed", read())
		}
	}
}

// A running Jellytool must observe rewrites made after it connected, including
// Magpie redirecting a retained Jellytool provider table for old threads.
func TestCodexIsolationExternalRewriteAndReconnect(t *testing.T) {
	for _, mode := range []string{"provider", "openai-base", "redirected-table"} {
		t.Run(mode, func(t *testing.T) {
			home, read := codexHome(t, "", "model = \"original\"\n")
			cx := codex(home)
			if _, err := cx.ConnectHow(); err != nil {
				t.Fatal(err)
			}
			catalog := filepath.Join(home, ".codex", "magpie-desktop-models.json")
			if err := os.WriteFile(catalog, []byte(`{"models":[{"slug":"fake/m1"}]}`), 0600); err != nil {
				t.Fatal(err)
			}
			rewrite := "model = \"fake/m1\"\nmodel_reasoning_effort = \"high\"\nmodel_catalog_json = " + quoteTOML(catalog) + "\n"
			switch mode {
			case "provider":
				rewrite += "model_provider = \"magpie\"\n"
			case "openai-base":
				rewrite += "openai_base_url = \"http://127.0.0.1:3425" + gateway.CodexPath + "\"\n"
			case "redirected-table":
				rewrite += "model_provider = \"jellytool\"\n[model_providers.jellytool]\nbase_url = \"http://127.0.0.1:3425/v1\"\n"
			}
			rewrite += "[model_providers.magpie]\nname = \"Magpie\"\nbase_url = \"http://127.0.0.1:3425/v1\"\n"
			if err := os.WriteFile(cx.Path, []byte(rewrite), 0600); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if err := cx.Sync(); err != nil {
					t.Fatal(err)
				}
				if cx.Wired() || cx.Routed() {
					t.Fatal("external route mistaken for Jellytool")
				}
				if err := cx.Disconnect(); err != nil {
					t.Fatal(err)
				}
				if read() != rewrite {
					t.Fatalf("background work changed external config:\n%s", read())
				}
			}
			// Deleted generated files must be recreated by the next explicit connect.
			if err := os.Remove(filepath.Join(home, ".codex", "jellytool-models.json")); err != nil {
				t.Fatal(err)
			}
			connection, err := cx.ConnectHow()
			if err != nil {
				t.Fatal(err)
			}
			if connection.How == "kept" {
				t.Fatal("reconnect incorrectly skipped")
			}
			tb, _ := edit.GetTOMLTable(cx.Path, "model_providers.jellytool")
			if tb["base_url"] != gateway.URL()+"/v1" {
				t.Fatal("own endpoint not repaired", tb)
			}
			if !cx.Wired() || !cx.Routed() {
				t.Fatal("not connected after explicit reconnect")
			}
			if _, err := os.Stat(filepath.Join(home, ".codex", "jellytool-models.json")); err != nil {
				t.Fatal(err)
			}
			peer, _ := edit.GetTOMLTable(cx.Path, "model_providers.magpie")
			if peer["base_url"] != "http://127.0.0.1:3425/v1" {
				t.Fatal("Magpie table changed")
			}
			if mode == "provider" {
				if err := cx.Unwire(); err != nil {
					t.Fatal(err)
				}
				// what magpie had written is not put back (magpie manages it)
				if p, _ := edit.GetTOMLTop(cx.Path, "model_provider"); p != "" {
					t.Fatal("restored magpie's provider", read())
				}
				if m, _ := edit.GetTOMLTop(cx.Path, "model"); m != "" {
					t.Fatal("restored magpie's model", read())
				}
			}
		})
	}
}

func TestCodexIsolationIncompleteExternalRewrite(t *testing.T) {
	home, read := codexHome(t, "", "model = \"original\"\n")
	cx := codex(home)
	if _, err := cx.ConnectHow(); err != nil {
		t.Fatal(err)
	}
	incomplete := "model_provider = \"magpie\"\n[model_providers."
	if err := os.WriteFile(cx.Path, []byte(incomplete), 0600); err != nil {
		t.Fatal(err)
	}
	if err := cx.Sync(); err == nil {
		t.Fatal("incomplete TOML accepted")
	}
	if read() != incomplete {
		t.Fatal("incomplete external write overwritten")
	}
	complete := "model_provider = \"magpie\"\nmodel = \"fake/m1\"\n[model_providers.magpie]\nbase_url = \"http://127.0.0.1:3425/v1\"\n"
	if err := os.WriteFile(cx.Path, []byte(complete), 0600); err != nil {
		t.Fatal(err)
	}
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if read() != complete || cx.Wired() {
		t.Fatal("did not recover after external write completed")
	}
}

// Jellytool leaves cc-switch's tables alone (takeTables returns while
// isolated), so one pointed at magpie's gateway is no drift of Jellytool's:
// flagged, Reconnect rewrote nothing and the row stayed "config changed".
func TestCodexIsolationCCSwitchTableIsNoDrift(t *testing.T) {
	home, read := codexHome(t, "", "model = \"original\"\n")
	cx := codex(home)
	if _, err := cx.ConnectHow(); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(cx.Path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("\n[model_providers.custom]\nname = \"custom\"\nbase_url = \"http://127.0.0.1:3425/v1\"\nwire_api = \"responses\"\n")
	f.Close()
	if !cx.Wired() {
		t.Fatal("not connected")
	}
	if msg := cx.Check(); msg != "" {
		t.Fatalf("a cc-switch table Jellytool never takes reads as drift: %s", msg)
	}
	if err := cx.Reapply(); err != nil {
		t.Fatal(err)
	}
	if tb, _ := edit.GetTOMLTable(cx.Path, "model_providers.custom"); tb["base_url"] != "http://127.0.0.1:3425/v1" {
		t.Fatal("Reconnect took the cc-switch table", read())
	}
}

// magpieBeside is Codex as magpie leaves it beside a ChatGPT sign-in, a CC
// Switch table taken over by magpie with it (the user's config, 11-26).
func magpieBeside(t *testing.T) (*Agent, func() string, string) {
	t.Helper()
	cfg := "model = \"fake/m1\"\nopenai_base_url = \"http://127.0.0.1:3425" + gateway.CodexPath + "\"\nmodel_catalog_json = \"magpie-desktop-models.json\"\n" +
		"[model_providers.magpie]\nname = \"magpie\"\nbase_url = \"http://127.0.0.1:3425/v1\"\nexperimental_bearer_token = \"magpie\"\n" +
		"[model_providers.custom]\nname = \"custom\"\nbase_url = \"http://127.0.0.1:3425/v1\"\nwire_api = \"responses\"\n"
	home, read := codexHome(t, chatGPTAuth, cfg)
	if err := os.WriteFile(filepath.Join(home, ".codex", "magpie-desktop-models.json"), []byte(`{"models":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	return codex(home), read, cfg
}

// magpie's keys untouched: its table, the CC Switch table it points at itself
func magpieTablesStand(t *testing.T, cx *Agent, read func() string) {
	t.Helper()
	for _, id := range []string{"magpie", "custom"} {
		if tb, _ := edit.GetTOMLTable(cx.Path, "model_providers."+id); tb["base_url"] != "http://127.0.0.1:3425/v1" {
			t.Fatalf("[model_providers.%s] changed:\n%s", id, read())
		}
	}
}

// besideSignIn: Jellytool beside the ChatGPT sign-in, the subscription's
// models in the app's list with its own
func besideSignIn(t *testing.T, cx *Agent, read func() string) {
	t.Helper()
	if u, _ := edit.GetTOMLTop(cx.Path, "openai_base_url"); u != gateway.URL()+gateway.CodexPath {
		t.Fatalf("openai_base_url not Jellytool's: %s", read())
	}
	if p, _ := edit.GetTOMLTop(cx.Path, "model_provider"); p != "" {
		t.Fatalf("model_provider %q left: Codex's own models out of its list\n%s", p, read())
	}
	if c, _ := edit.GetTOMLTop(cx.Path, "model_catalog_json"); filepath.Base(c) == "magpie-desktop-models.json" || filepath.Base(c) == "jellytool-models.json" {
		t.Fatalf("catalog %s, not the list beside the sign-in:\n%s", c, read())
	}
	if !cx.Wired() || cx.PeerHeld() {
		t.Fatalf("not Jellytool's after the takeover:\n%s", read())
	}
	if d := cx.Drift(); d != nil {
		t.Fatalf("drift after the takeover: %+v", d)
	}
}

// Jellytool beside magpie: magpie's routing keys are taken over by the
// user's connect only, never by a Sync, and a disconnect clears
// Jellytool's own without putting magpie's back.
func TestCodexIsolationConnectTakesMagpieBesideSignIn(t *testing.T) {
	cx, read, cfg := magpieBeside(t)
	if cx.Wired() || !cx.PeerHeld() {
		t.Fatal("magpie's connection not told as magpie's")
	}
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if read() != cfg {
		t.Fatalf("a Sync took magpie's connection:\n%s", read())
	}
	if _, err := cx.ConnectHow(); err != nil {
		t.Fatal(err)
	}
	besideSignIn(t, cx, read)
	magpieTablesStand(t, cx, read)
	if err := cx.Disconnect(); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"openai_base_url", "model_catalog_json", "model_provider"} {
		if v, _ := edit.GetTOMLTop(cx.Path, k); v != "" {
			t.Fatalf("%s = %q after Disconnect: magpie's put back, or Jellytool's left\n%s", k, v, read())
		}
	}
	magpieTablesStand(t, cx, read)
}

// Jellytool made Codex's provider while magpie held the base URL (a model
// set without the user's connect, as before this fix): the subscription's
// models were out of the app's list. That is drift of its own, and
// Reconnect puts Jellytool beside the sign-in.
func TestCodexIsolationReconnectTakesBackSignIn(t *testing.T) {
	cx, read, _ := magpieBeside(t)
	if err := cx.Field("model").Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	if p, _ := edit.GetTOMLTop(cx.Path, "model_provider"); p != codexProviderID {
		t.Fatalf("a set outside a connect took magpie's base URL:\n%s", read())
	}
	if !cx.Wired() || !cx.PeerHeld() {
		t.Fatalf("held by magpie not told:\n%s", read())
	}
	if d := cx.Drift(); d == nil || d.Kind != "peer" {
		t.Fatalf("drift %+v, want peer", d)
	}
	before := read()
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if read() != before {
		t.Fatalf("a Sync took magpie's base URL:\n%s", read())
	}
	if err := cx.Reapply(); err != nil {
		t.Fatal(err)
	}
	besideSignIn(t, cx, read)
	magpieTablesStand(t, cx, read)
}
