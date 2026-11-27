//go:build !nogui && !windows

package gui

// jellytoolInstanceHeld: only Windows hands a second launch over.
func jellytoolInstanceHeld(string) bool { return false }
