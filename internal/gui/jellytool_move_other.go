//go:build !nogui && !darwin

package gui

import (
	"path/filepath"

	"github.com/yetone/magpie/internal/appdir"
)

// offerJellytoolMove is the Mac's move into Applications; elsewhere there is
// no disk image to run from.
func offerJellytoolMove(string) bool { return false }

func jellytoolInstanceLock() string { return filepath.Join(appdir.Config(), "jellytool-instance.lock") }
