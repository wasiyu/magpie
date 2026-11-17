package agent

import (
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/product"
	"github.com/yetone/magpie/internal/provider"
)

func TestBrandProviderDisplayNames(t *testing.T) {
	home, read := codexHome(t, `{"OPENAI_API_KEY":"sk-fixture"}`, "model = \"gpt-5.5\"\n")
	if err := provider.Save(provider.Provider{ID: "fake", Name: "my-magpie-provider", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	cx := codex(home)
	if err := cx.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	config := read()
	for _, want := range []string{`model_provider = "` + codexProviderID + `"`, `[model_providers.` + codexProviderID + `]`, `name = "` + product.Name + `"`} {
		if !strings.Contains(config, want) {
			t.Fatalf("missing %s in %s", want, config)
		}
	}
	openCode := magpieProviderJSON("opencode").(map[string]any)
	if openCode["name"] != product.Name {
		t.Fatalf("OpenCode display name = %v", openCode["name"])
	}
	if p, _ := provider.Find("fake"); p.Name != "my-magpie-provider" {
		t.Fatal("display branding changed the user's provider name")
	}
}
