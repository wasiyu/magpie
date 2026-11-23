//go:build jellytool

package gui

import (
	"bytes"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestJellytoolModelConfigMigratesOldRecommendations(t *testing.T) {
	h := jellytoolFixture(t)
	if err := ensureJellytoolModelConfig(); err != nil {
		t.Fatal(err)
	}
	path := jellytoolModelConfigPath()
	initial, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"gpt-6.2-astra", "gpt-6-luna"} {
		if !bytes.Contains(initial, []byte(`"id": "`+id+`"`)) {
			t.Fatalf("default lacks %s", id)
		}
	}

	// A readable version 1 file follows the new release, and is kept aside.
	old := []byte(`{"version":1,"tiers":{"daily":[{"id":"own-model","recommended":true,"alwaysShow":true,"name":"My model"}],"simple":[]}}`)
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	if err := ensureJellytoolModelConfig(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, initial) {
		t.Fatalf("version 1 configuration was not migrated: %s", got)
	}
	if got, _ := os.ReadFile(path + ".v1.bak"); !bytes.Equal(got, old) {
		t.Fatalf("version 1 configuration was not kept aside: %s", got)
	}
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/jellytool-model-config.js", nil))
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"id":"gpt-6.2-astra","latest":"astra"`) || r.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("config script: %d %s", r.Code, r.Body.String())
	}

	// Edits to a version 2 file survive the next start and are served.
	custom := []byte(`{"version":2,"tiers":{"daily":[{"id":"own-model","recommended":true,"alwaysShow":true,"name":"My model"}],"simple":[]}}`)
	if err := os.WriteFile(path, custom, 0600); err != nil {
		t.Fatal(err)
	}
	if err := ensureJellytoolModelConfig(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, custom) {
		t.Fatal("startup overwrote a version 2 configuration")
	}
	r = httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/jellytool-model-config.js", nil))
	if !strings.Contains(r.Body.String(), `"id":"own-model"`) {
		t.Fatalf("edited configuration not served: %s", r.Body.String())
	}

	// Malformed JSON is reported, and never replaced, whatever it claims.
	for _, broken := range [][]byte{[]byte(`{"version":2,"tiers":`), []byte(`{"version":1,"tiers":`)} {
		if err := os.WriteFile(path, broken, 0600); err != nil {
			t.Fatal(err)
		}
		r = httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", "/jellytool-model-config.js", nil))
		if !strings.Contains(r.Body.String(), "jellytoolModelConfigError") || strings.Contains(r.Body.String(), "gpt-6.2-astra") {
			t.Fatalf("bad user JSON silently replaced: %s", r.Body.String())
		}
		if err := ensureJellytoolModelConfig(); err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(path); !bytes.Equal(got, broken) {
			t.Fatalf("invalid configuration %s was overwritten", broken)
		}
	}
}

func TestJellytoolModelConfigRejectsInvalidRecords(t *testing.T) {
	jellytoolFixture(t)
	if err := ensureJellytoolModelConfig(); err != nil {
		t.Fatal(err)
	}
	// The cases below differ from this accepted record in one field each, so
	// each is rejected by its own check, not by the version check.
	if err := os.WriteFile(jellytoolModelConfigPath(), []byte(`{"version":2,"tiers":{"daily":[{"id":"a"},{"id":"gpt-6.2-sol","latest":"sol"}],"simple":[{"id":"b"},{"id":"GPT-6-luna","latest":"luna"}]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readJellytoolModelConfig(); err != nil {
		t.Fatalf("rejected a valid record: %v", err)
	}
	for _, body := range []string{
		`{"version":1,"tiers":{}}`,
		`{"version":3,"tiers":{}}`,
		`{"version":2}`,
		`{"version":2,"tiers":{"unknown":[]}}`,
		`{"version":2,"tiers":{"complex":null}}`,
		`{"version":2,"tiers":{"daily":[{"id":" model"}]}}`,
		`{"version":2,"tiers":{"daily":[{"id":"a"}],"simple":[{"id":"a"}]}}`,
		`{"version":2,"tiers":{},"typo":true}`,
		`{"version":2,"tiers":{}} {}`,
		`{"version":2,"tiers":{"daily":[{"id":"gpt-6.2-sol","latest":"nova"}]}}`,
		`{"version":2,"tiers":{"daily":[{"id":"gpt-6.2-astra","latest":"sol"}]}}`,
		`{"version":2,"tiers":{"daily":[{"id":"glm-5.3","latest":"sol"}]}}`,
		`{"version":2,"tiers":{"daily":[{"id":"gpt--sol","latest":"sol"}]}}`,
		`{"version":2,"tiers":{"daily":[{"id":"gpt-6.2-sol","latest":"sol"}],"simple":[{"id":"gpt-6.1-sol","latest":"sol"}]}}`,
	} {
		if err := os.WriteFile(jellytoolModelConfigPath(), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readJellytoolModelConfig(); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}
