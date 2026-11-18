//go:build nogui

package cli

import (
	"errors"
	"github.com/yetone/magpie/internal/product"
)

const hasGUI = false

func runGUI(bool, string) error {
	return errors.New(product.Text("this build has no GUI; run `magpie tui`"))
}

func runPanel() error { return runGUI(false, "") }

func runWindow(string) error { return runGUI(true, "") }

func runUninstallWindow() error { return runGUI(true, "") }
