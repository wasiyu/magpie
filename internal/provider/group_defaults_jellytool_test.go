//go:build jellytool

package provider

import (
	"path/filepath"
	"testing"
)

func TestAutoGroupsDefaultOff(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	if AutoGroupsOn() {
		t.Fatal("found groups are on for a new Jellytool user")
	}
	if err := SetAutoGroups(true); err != nil {
		t.Fatal(err)
	}
	if !AutoGroupsOn() {
		t.Fatal("turning found groups on was not kept")
	}
	if err := SetAutoGroups(false); err != nil {
		t.Fatal(err)
	}
	if AutoGroupsOn() {
		t.Fatal("turning found groups off was not kept")
	}
}
