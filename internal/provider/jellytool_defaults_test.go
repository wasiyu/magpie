package provider

import (
	"testing"

	"github.com/yetone/magpie/internal/plugin"
	"github.com/yetone/magpie/internal/product"
)

func TestJellytoolSubscriptionDefaults(t *testing.T) {
	signIn(t)
	for _, id := range accountIDs {
		p := withPicks(Provider{ID: id, Account: &Account{}}, Provider{})
		if p.Off != product.Jellytool {
			t.Errorf("%s default off = %v, want %v", id, p.Off, product.Jellytool)
		}
	}
	for _, id := range []string{"codex", "copilot"} {
		p, err := Find(id)
		if err != nil || p.Off != product.Jellytool {
			t.Fatalf("%s default: %+v, %v", id, p, err)
		}
		if product.Jellytool && p.On() {
			t.Fatalf("%s serves models before it is enabled", id)
		}
		for _, off := range []bool{false, true, false} {
			if err := SetOff(id, off); err != nil {
				t.Fatal(err)
			}
			Changed()
			p, err = Find(id)
			if err != nil || p.Off != off {
				t.Fatalf("%s saved off = %v: %+v, %v", id, off, p, err)
			}
		}
	}
	for _, id := range []string{"jellytoken", "another-plugin"} {
		p := withPicks(Provider{ID: id, Account: &Account{}}, Provider{})
		if p.Off {
			t.Errorf("plugin-only provider %s starts off", id)
		}
	}
	if got := withPicks(Provider{ID: "jellytoken", Account: &Account{}}, Provider{}); got.NoModels {
		t.Fatalf("Jellytoken changed an existing account's vendor-list default: %+v", got)
	}
	for _, none := range []bool{false, true} {
		got := withPicks(Provider{ID: "jellytoken", Account: &Account{}}, Provider{ID: "jellytoken", NoModels: none})
		if got.NoModels != none {
			t.Fatalf("Jellytoken default overrode saved model selection: %+v", got)
		}
	}
	if product.Jellytool {
		if got := PluginIcon(plugin.Provider{ID: "jellytoken"}); got != "jellytoken" {
			t.Errorf("Jellytoken icon = %q, want the Jellytool icon", got)
		}
	}
}
