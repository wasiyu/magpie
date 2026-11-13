//go:build jellytool

package agent

import (
	"slices"
	"testing"

	"github.com/yetone/magpie/internal/testenv"
)

// Jellytool keeps magpie's complete local agent catalog. An empty sandbox also
// proves detection does not scan WSL or import a user's existing state.
func TestJellytoolLocalAgentSet(t *testing.T) {
	home := t.TempDir()
	testenv.SetHome(t, home)
	agents := All()
	got := make([]string, 0, len(agents))
	for _, a := range agents {
		got = append(got, a.ID)
		if a.WSL != "" {
			t.Fatalf("Jellytool exposed WSL agent %q", a.ID)
		}
	}
	slices.Sort(got)
	for _, want := range []string{"claude", "codex", "opencode", "workbuddy"} {
		if !slices.Contains(got, want) {
			t.Fatalf("Jellytool agents omitted %q: %v", want, got)
		}
	}
}
