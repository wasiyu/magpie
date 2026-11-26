package product

import "testing"

func TestDisplayText(t *testing.T) {
	for _, tc := range []struct{ text, jellytool string }{
		{"Open magpie", "Open Jellytoken助手"},
		{"magpie's models. Restart magpie.", "Jellytoken助手's models. Restart Jellytoken助手."},
		{"magpie web --help", "jellytool web --help"},
		{"Run magpie in a terminal", "Run jellytool in a terminal"},
		{"  magpie snow <model>", "  jellytool snow <model>"},
		{"X-Magpie-Model /mcp/magpie/tools ~/.config/magpie/providers.json sk-magpie-key @magpie-community/test magpie://import .magpie-backup", "X-Magpie-Model /mcp/magpie/tools ~/.config/magpie/providers.json sk-magpie-key @magpie-community/test magpie://import .magpie-backup"},
		{"Remote magpie", "Remote magpie"},
		{`C:\magpie\providers.json`, `C:\magpie\providers.json`},
		{"another computer's magpie, shared on its network", "another computer's magpie, shared on its network"},
		{"Written for magpie by its community", "Written for magpie by its community"},
	} {
		want := tc.text
		if Jellytool {
			want = tc.jellytool
		}
		if got := Text(tc.text); got != want {
			t.Errorf("Text(%q) = %q, want %q", tc.text, got, want)
		}
	}
}
