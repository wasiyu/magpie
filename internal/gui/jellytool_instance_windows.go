//go:build !nogui && windows

package gui

import "golang.org/x/sys/windows"

// jellytoolInstanceHeld is whether a running Jellytool has made Wails'
// single-instance mutex for id (wails-app-<id>-sim, its
// single_instance_windows.go), opened without making it.
func jellytoolInstanceHeld(id string) bool {
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr("wails-app-"+id+"-sim"))
	if err != nil {
		return false
	}
	windows.CloseHandle(h)
	return true
}
