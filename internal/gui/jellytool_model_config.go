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

// Seed once under the application's instance lock. Updates never replace an
// existing configuration, including one with invalid or incomplete JSON.
func ensureJellytoolModelConfig() error {
	path := jellytoolModelConfigPath()
	if _, err := os.Lstat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
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
	if cfg.Version != 1 || cfg.Tiers == nil {
		return cfg, fmt.Errorf("expected version 1 and a tiers object")
	}
	seen := map[string]bool{}
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
		}
	}
	return cfg, nil
}
