//go:build jellytool && !nogui

package gui

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/yetone/magpie/internal/appdir"
	"github.com/yetone/magpie/internal/plugin"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/testenv"
)

func TestJellytoolFailedStartupPreservesLegacyProvider(t *testing.T) {
	home := t.TempDir()
	testenv.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	appdir.UseExecutable("")
	plugin.UseCached(nil)
	t.Cleanup(provider.Changed)
	legacy := provider.Provider{
		ID: "jellytoken", Name: "My Jellytoken", Key: "synthetic-legacy-key",
		Chat: "https://aiservice.jellytoken.com/v1", Models: []string{"vendor/text:exact"},
		Keys: []provider.KeyAccount{{Name: "Second key", Key: "synthetic-second-key"}},
	}
	if _, _, err := provider.Restore([]provider.Provider{legacy}); err != nil {
		t.Fatal(err)
	}
	before, err := provider.Stored()
	if err != nil {
		t.Fatal(err)
	}
	// A test binary has no packaged plugin resources beside it. Exercise the
	// real startup failure before AppKit can run, with only isolated state.
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(exe), "jellytool-resources")); !os.IsNotExist(err) {
		t.Fatal("startup fixture must have no bundled plugin resources")
	}
	if err := RunJellytool("dev"); err == nil {
		t.Fatal("startup without bundled resources succeeded")
	}
	after, err := provider.Stored()
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("startup changed legacy credentials or model choices: %v", err)
	}
}
