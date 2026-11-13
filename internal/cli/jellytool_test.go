//go:build jellytool

package cli

import (
	"os"
	"testing"

	"github.com/yetone/magpie/internal/appdir"
	"github.com/yetone/magpie/internal/testenv"
)

func TestJellytoolCLIAdmission(t *testing.T) {
	testenv.SetHome(t, t.TempDir())
	appdir.UseExecutable("")
	for _, args := range [][]string{{"provider", "add", "evil", "key"}, {"plugin", "add", "evil"}, {"serve"}, {"web"}, {"daemon", "restart"}, {"import", "magpie://import?x"}, {"accounts", "discovery", "on"}, {"codex", "jellytoken/model"}, {"agent-disconnect-dry-run", "codex"}} {
		if err := jellytoolMain(args); err == nil {
			t.Errorf("disabled command succeeded: %v", args)
		}
	}
	for _, arg := range []string{"help", "version", "licenses"} {
		if err := jellytoolMain([]string{arg}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(appdir.Config()); !os.IsNotExist(err) {
		t.Fatalf("information or rejected command created app state: %v", err)
	}
}
