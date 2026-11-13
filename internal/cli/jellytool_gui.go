//go:build !nogui

package cli

import "github.com/yetone/magpie/internal/gui"

func runJellytoolApp() error { return gui.RunJellytool(version) }
