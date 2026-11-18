package plugin

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// JellytoolRecommendation is reviewed release content, independent of a key's
// catalog and of protocol/tool acceptance. Evidence is plain text, never HTML.
type JellytoolRecommendation struct {
	ID          string `json:"id"`
	Category    string `json:"category"`
	Reason      string `json:"reason"`
	Constraints string `json:"constraints"`
	Evidence    string `json:"evidence"`
	Updated     string `json:"updated"`
	Maintainer  string `json:"maintainer"`
}

type jellytoolRecommendations struct {
	Version         int                       `json:"version"`
	Recommendations []JellytoolRecommendation `json:"recommendations"`
}

// JellytoolRecommendations reads only the embedded, versioned plugin resource.
// An empty list is deliberate until specific model evidence has been reviewed.
func JellytoolRecommendations() ([]JellytoolRecommendation, error) {
	b, err := jellytokenFiles.ReadFile("jellytoken/recommendations.json")
	if err != nil {
		return nil, err
	}
	return parseJellytoolRecommendations(b)
}

func parseJellytoolRecommendations(b []byte) ([]JellytoolRecommendation, error) {
	var data jellytoolRecommendations
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, err
	}
	if data.Version != 1 || data.Recommendations == nil {
		return nil, fmt.Errorf("invalid Jellytool recommendation version or list")
	}
	seen := map[string]bool{}
	for _, r := range data.Recommendations {
		if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Reason) == "" || strings.TrimSpace(r.Constraints) == "" || strings.TrimSpace(r.Evidence) == "" || strings.TrimSpace(r.Maintainer) == "" {
			return nil, fmt.Errorf("incomplete Jellytool recommendation")
		}
		switch r.Category {
		case "coding", "reasoning", "writing", "research", "cost", "speed", "tools":
		default:
			return nil, fmt.Errorf("unknown Jellytool recommendation category")
		}
		if _, err := time.Parse(time.DateOnly, r.Updated); err != nil {
			return nil, fmt.Errorf("invalid Jellytool recommendation date")
		}
		key := r.Category + "\x00" + r.ID
		if seen[key] {
			return nil, fmt.Errorf("duplicate Jellytool recommendation")
		}
		seen[key] = true
	}
	return data.Recommendations, nil
}
