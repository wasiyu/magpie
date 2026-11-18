//go:build !jellytool

package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/edit"
)

// A Jellytool table must not opt the upstream build into new wiring behavior.
func TestCodexMagpieKeepsOriginalWiringBesideJellytool(t *testing.T) {
	home, _ := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`,
		"model = \"gpt-5.5\"\n[model_providers.jellytool]\nbase_url = \"http://127.0.0.1:3435/v1\"\n")
	cx := codex(home)
	if ok, err := cx.Join(); err != nil || !ok {
		t.Fatalf("original ChatGPT join: %v, %v", ok, err)
	}
	if p, _ := edit.GetTOMLTop(cx.Path, "model_provider"); p != "" {
		t.Fatalf("unexpected separate provider: %s", p)
	}
	if base, _ := edit.GetTOMLTop(cx.Path, "openai_base_url"); base != codexGatewayURL() {
		t.Fatalf("original base URL: %s", base)
	}
	if err := cx.Field("login").Set("api"); err != nil {
		t.Fatal(err)
	}
	if err := cx.Field("model").Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	if p, _ := edit.GetTOMLTop(cx.Path, "model_provider"); p != "magpie" {
		t.Fatalf("provider: %s", p)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "magpie-models.json")); err != nil {
		t.Fatal(err)
	}
	if profile, err := edit.GetTOMLTable(cx.Path, "profiles.magpie"); err != nil || profile != nil {
		t.Fatalf("new profile rule leaked into Magpie: %v, %v", profile, err)
	}
	if _, err := os.Stat(cx.Path + ".provider.lock"); !os.IsNotExist(err) {
		t.Fatalf("new lock rule leaked into Magpie: %v", err)
	}
}
