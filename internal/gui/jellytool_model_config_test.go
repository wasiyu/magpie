//go:build jellytool

package gui

import (
	"bytes"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestJellytoolModelConfigPreservesEditsAndReloads(t *testing.T) {
	h := jellytoolFixture(t)
	if err := ensureJellytoolModelConfig(); err != nil {
		t.Fatal(err)
	}
	path := jellytoolModelConfigPath()
	initial, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"gpt-6-astra", "gpt-6-luna"} {
		if !bytes.Contains(initial, []byte(`"id": "`+id+`"`)) {
			t.Fatalf("default lacks %s", id)
		}
	}
	custom := []byte(`{"version":1,"tiers":{"daily":[{"id":"own-model","recommended":true,"alwaysShow":true,"name":"My model"}],"simple":[]}}`)
	if err := os.WriteFile(path, custom, 0600); err != nil {
		t.Fatal(err)
	}
	if err := ensureJellytoolModelConfig(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, custom) {
		t.Fatal("startup overwrote user configuration")
	}
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/jellytool-model-config.js", nil))
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"id":"own-model"`) || r.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("config script: %d %s", r.Code, r.Body.String())
	}
	broken := []byte(`{"version":1,"tiers":`)
	if err := os.WriteFile(path, broken, 0600); err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/jellytool-model-config.js", nil))
	if !strings.Contains(r.Body.String(), "jellytoolModelConfigError") || strings.Contains(r.Body.String(), "gpt-6-astra") {
		t.Fatalf("bad user JSON silently replaced: %s", r.Body.String())
	}
	if err := ensureJellytoolModelConfig(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, broken) {
		t.Fatal("invalid configuration was overwritten")
	}
}

func TestJellytoolModelConfigRejectsInvalidRecords(t *testing.T) {
	jellytoolFixture(t)
	if err := ensureJellytoolModelConfig(); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"version":2,"tiers":{}}`,
		`{"version":1,"tiers":{"unknown":[]}}`,
		`{"version":1,"tiers":{"complex":null}}`,
		`{"version":1,"tiers":{"daily":[{"id":" model"}]}}`,
		`{"version":1,"tiers":{"daily":[{"id":"a"}],"simple":[{"id":"a"}]}}`,
		`{"version":1,"tiers":{},"typo":true}`,
		`{"version":1,"tiers":{}} {}`,
	} {
		if err := os.WriteFile(jellytoolModelConfigPath(), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readJellytoolModelConfig(); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}
