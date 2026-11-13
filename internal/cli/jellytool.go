package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yetone/magpie/internal/agent"
	"github.com/yetone/magpie/internal/notices"
)

// jellytoolMain is entered before magpie's URL handling, network setup,
// migrations or command dispatch. Commands outside this list have no effects.
func jellytoolMain(args []string) error {
	if len(args) == 2 && args[0] == agent.DryRunArg {
		home, _ := os.UserHomeDir()
		sandbox := os.Getenv("MAGPIE_TEST_SANDBOX")
		if filepath.IsAbs(sandbox) && filepath.Clean(sandbox) == filepath.Clean(home) && strings.HasPrefix(filepath.Base(home), "magpie-preview-") && os.Getenv("MAGPIE_DRY_PROVIDERS") == filepath.Join(home, ".magpie-dry-providers") {
			return agent.DryRun(args[1])
		}
		return errors.New("Agent preview requires an isolated sandbox")
	}
	if len(args) == 0 || len(args) == 1 && args[0] == "app" {
		return runJellytoolApp()
	}
	if len(args) == 1 {
		switch args[0] {
		case "help", "-h", "--help":
			fmt.Println("Jellytool — Jellytoken for your coding agents\n\nCommands: app, help, version, licenses")
			return nil
		case "version", "-v", "--version":
			fmt.Println("Jellytool", version)
			return nil
		case "licenses":
			fmt.Print(notices.Text)
			return nil
		}
	}
	return errors.New("this command is disabled in Jellytool; use app, help, version or licenses")
}
