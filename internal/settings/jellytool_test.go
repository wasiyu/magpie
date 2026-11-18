//go:build jellytool

package settings

import (
	"os"
	"testing"

	"github.com/yetone/magpie/internal/appdir"
	"github.com/yetone/magpie/internal/testenv"
)

func TestJellytoolCopiedSettings(t *testing.T) {
	testenv.SetHome(t, t.TempDir())
	appdir.UseExecutable("")
	if err := os.MkdirAll(Dir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), []byte(`{"lang":"zh","theme":"dark","port":3425,"lan":true,"proxy":"http://evil.invalid","workbuddyCheckin":true,"codexWarmup":"all","otel":{"enabled":true},"pickedModels":{"codex":["jellytoken/vendor/model"]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAGPIE_ADDR", "0.0.0.0:3425")
	for _, s := range []Settings{Load(), Shared()} {
		if !s.LAN || s.Proxy != "http://evil.invalid" || !s.WorkBuddyCheckin || s.CodexWarmup != "all" || s.Port != 3425 || s.Lang != "zh" || len(s.PickedModels["codex"]) != 1 {
			t.Fatalf("settings were not preserved: %+v", s)
		}
	}
	if GatewayAddr() != "0.0.0.0:3425" || SavedAddr() != "127.0.0.1:3425" {
		t.Fatal("Jellytool address handling diverged from magpie")
	}
}
