//go:build jellytool

package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestJellytoolFeedIdentity(t *testing.T) {
	rel := Release{Product: "jellytool", Version: "0.2.0", URL: "https://downloads.example/jellytool"}
	var release atomic.Value
	release.Store(rel)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(release.Load().(Release))
	}))
	defer srv.Close()
	t.Setenv("JELLYTOOL_UPDATE_FEED", srv.URL)
	t.Setenv("MAGPIE_UPDATE_FEED", "http://127.0.0.1:1/not-jellytool")
	if r, err := Latest(context.Background()); err != nil || r.Version != "0.2.0" {
		t.Fatal(r, err)
	}
	for _, tc := range []struct{ product, version, url string }{
		{"", "0.2.0", rel.URL},
		{"magpie", "0.2.0", rel.URL},
		{"jellytool", "dev", rel.URL},
		{"jellytool", "0.2.0", ""},
		{"jellytool", "0.2.0", "http://downloads.example/app"},
		{"jellytool", "0.2.0", "javascript:alert(1)"},
		{"jellytool", "0.2.0", "https://user:password@downloads.example/app"},
	} {
		rel.Product, rel.Version, rel.URL = tc.product, tc.version, tc.url
		release.Store(rel)
		if _, err := Latest(context.Background()); err == nil {
			t.Errorf("invalid Jellytool release accepted: %+v", tc)
		}
	}
}
