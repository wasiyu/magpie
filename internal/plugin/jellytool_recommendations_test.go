package plugin

import (
	"encoding/json"
	"testing"
)

func TestJellytoolRecommendationReleaseData(t *testing.T) {
	recommendations, err := JellytoolRecommendations()
	if err != nil {
		t.Fatal(err)
	}
	if len(recommendations) != 0 {
		t.Fatal("recommendations need product review before entering the release")
	}
	// Synthetic records verify the contract without approving any real model.
	fixture := JellytoolRecommendation{ID: "vendor/literal.v1:exact", Category: "coding", Reason: "fixture only", Constraints: "not tool validated", Evidence: "local mock", Updated: "2026-11-03", Maintainer: "test fixture"}
	encode := func(r JellytoolRecommendation) []byte {
		b, _ := json.Marshal(jellytoolRecommendations{Version: 1, Recommendations: []JellytoolRecommendation{r}})
		return b
	}
	got, err := parseJellytoolRecommendations(encode(fixture))
	if err != nil || len(got) != 1 || got[0].ID != fixture.ID {
		t.Fatalf("literal ID lost: %+v %v", got, err)
	}
	for _, field := range []string{"id", "category", "reason", "constraints", "evidence", "updated", "maintainer"} {
		var data map[string]any
		json.Unmarshal(encode(fixture), &data)
		delete(data["recommendations"].([]any)[0].(map[string]any), field)
		b, _ := json.Marshal(data)
		if _, err := parseJellytoolRecommendations(b); err == nil {
			t.Errorf("accepted missing %s", field)
		}
	}
	for _, bad := range []string{`{"version":2,"recommendations":[]}`, `{"version":1}`, `{"version":1,"recommendations":null}`} {
		if _, err := parseJellytoolRecommendations([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}
