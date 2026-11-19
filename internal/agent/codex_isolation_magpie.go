//go:build !jellytool

package agent

import (
	"path/filepath"
	"strings"

	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/gateway"
)

// The upstream build retains its original provider identity and wiring.
const codexProviderID = magpieID

func codexPeerID() string { return "jellytool" }

func codexPeerActive(path, ownURL string) bool {
	selected, _ := edit.GetTOMLTop(path, "model_provider")
	if profile, _ := edit.GetTOMLTop(path, "profile"); profile != "" {
		settings, _ := edit.GetTOMLTable(path, "profiles."+profile)
		if settings["model_provider"] != "" {
			selected = settings["model_provider"]
		}
	}
	peer, _ := edit.GetTOMLTable(path, "model_providers.jellytool")
	if selected == "jellytool" {
		return peer["base_url"] != ownURL
	}
	if selected != "" && selected != "openai" {
		return false
	}
	catalog, _ := edit.GetTOMLTop(path, "model_catalog_json")
	base, _ := edit.GetTOMLTop(path, "openai_base_url")
	endpoint := strings.TrimSuffix(peer["base_url"], "/v1")
	return filepath.Base(catalog) == "jellytool-desktop-models.json" || endpoint != "" && strings.TrimSuffix(base, "/") == endpoint+gateway.CodexPath
}

func codexIsolated(a *Agent, _ string) *Agent { return a }
