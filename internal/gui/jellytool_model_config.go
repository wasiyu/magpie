package gui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/yetone/magpie/internal/appdir"
	"github.com/yetone/magpie/internal/edit"
)

type jellytoolModelPick struct {
	ID          string `json:"id"`
	Latest      string `json:"latest,omitempty"`
	Name        string `json:"name,omitempty"`
	Recommended bool   `json:"recommended,omitempty"`
	AlwaysShow  bool   `json:"alwaysShow,omitempty"`
}

type jellytoolModelConfig struct {
	Version int                             `json:"version"`
	Tiers   map[string][]jellytoolModelPick `json:"tiers"`
}

func jellytoolModelConfigPath() string {
	return filepath.Join(appdir.Config(), "model-recommendations.json")
}

// Seed or migrate the bundled recommendations under the application's
// instance lock. Only a readable version 1 file is replaced, so its models
// follow the installed application; it is first saved beside the new file as
// model-recommendations.v1.bak. Any other existing file, malformed JSON
// included, is left for the page to report rather than replaced.
func ensureJellytoolModelConfig() error {
	path := jellytoolModelConfigPath()
	old, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
			return nil // a dangling link is the user's, not ours to replace
		}
	case err != nil:
		return err
	default:
		var cfg struct {
			Version int `json:"version"`
		}
		if json.Unmarshal(old, &cfg) != nil || cfg.Version != 1 {
			return nil
		}
		if err := edit.WriteAtomic(path+".v1.bak", old); err != nil {
			return err
		}
	}
	data, err := assets.ReadFile("assets/jellytool-models.json")
	if err != nil {
		return err
	}
	return edit.WriteAtomic(path, data)
}

func readJellytoolModelConfig() (jellytoolModelConfig, error) {
	var cfg jellytoolModelConfig
	path := jellytoolModelConfigPath()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if _, statErr := os.Lstat(path); os.IsNotExist(statErr) {
			data, err = assets.ReadFile("assets/jellytool-models.json")
		}
	}
	if err != nil {
		return cfg, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&cfg); err != nil {
		return cfg, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return cfg, fmt.Errorf("expected one JSON object")
	}
	if cfg.Version != 2 || cfg.Tiers == nil {
		return cfg, fmt.Errorf("expected version 2 and a tiers object")
	}
	seen, latest := map[string]bool{}, map[string]bool{}
	for tier, picks := range cfg.Tiers {
		if tier != "complex" && tier != "daily" && tier != "simple" {
			return cfg, fmt.Errorf("unknown tier %q", tier)
		}
		if picks == nil {
			return cfg, fmt.Errorf("%s must be an array; omit the tier to calculate it automatically", tier)
		}
		for _, pick := range picks {
			if pick.ID == "" || strings.TrimSpace(pick.ID) != pick.ID || strings.ContainsAny(pick.ID, "\r\n\t") {
				return cfg, fmt.Errorf("invalid model ID in %s", tier)
			}
			if seen[pick.ID] {
				return cfg, fmt.Errorf("duplicate model ID %q", pick.ID)
			}
			seen[pick.ID] = true
			if pick.Latest != "" {
				if !jellytoolGPTRole(pick.ID, pick.Latest) {
					return cfg, fmt.Errorf("latest %q needs astra, sol, terra or luna, and an id of that role such as gpt-6.2-%s", pick.Latest, pick.Latest)
				}
				if latest[pick.Latest] {
					return cfg, fmt.Errorf("duplicate latest %q", pick.Latest)
				}
				latest[pick.Latest] = true
			}
		}
	}
	return cfg, nil
}

// jellytoolGPTRole is whether role is a GPT role a pick can follow, and id is
// a GPT model of that role (gpt-<version>-<role>), shown when the key lists
// none of them.
func jellytoolGPTRole(id, role string) bool {
	switch role {
	case "astra", "sol", "terra", "luna":
	default:
		return false
	}
	v, ok := strings.CutPrefix(strings.ToLower(id), "gpt-")
	if v, ok = strings.CutSuffix(v, "-"+role); !ok || v == "" || v[0] == '.' || v[len(v)-1] == '.' || strings.Contains(v, "..") {
		return false
	}
	return strings.Trim(v, "0123456789.") == ""
}
