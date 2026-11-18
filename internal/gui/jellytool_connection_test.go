//go:build jellytool

package gui

import (
	"context"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

// New onboarding waits for explicit model selection; upgrading an existing
// key must retain both implicit vendor-list mode and saved model choices.
func TestJellytoolKeyPreservesExistingModelChoices(t *testing.T) {
	for _, tc := range []struct {
		name         string
		first, saved bool
		models       []string
		wantNone     bool
	}{
		{"first key", true, false, nil, true},
		{"legacy key without provider row", false, false, nil, false},
		{"explicit vendor list on reconnect", true, true, nil, false},
		{"saved picks on replacement", false, true, []string{"vendor/text.v1:exact"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := jellytoolFixture(t)
			if tc.saved {
				p, err := provider.Find("jellytoken")
				if err != nil {
					t.Fatal(err)
				}
				p.Models, p.NoModels = tc.models, false
				if err := provider.Save(*p); err != nil {
					t.Fatal(err)
				}
			}
			was := jellytoolManageConnection
			t.Cleanup(func() { jellytoolManageConnection = was })
			jellytoolManageConnection = func(_ context.Context, in map[string]any) (map[string]any, error) {
				if in["action"] != "save" {
					t.Fatalf("unexpected operation: %v", in["action"])
				}
				return map[string]any{"first": tc.first, "baseURL": "https://fixture.invalid/v1", "api": "openai"}, nil
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("POST", "/api/jellytool/key", strings.NewReader(`{"key":"fixture-key"}`)))
			if w.Code != 200 {
				t.Fatalf("%d: %s", w.Code, w.Body.String())
			}
			p, err := provider.Find("jellytoken")
			if err != nil || p.NoModels != tc.wantNone || !reflect.DeepEqual(p.Models, tc.models) {
				t.Fatalf("model choice changed: models=%v noModels=%v, err=%v", p.Models, p.NoModels, err)
			}
		})
	}
}
