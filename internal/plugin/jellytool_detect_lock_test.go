//go:build jellytool

package plugin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/testenv"
)

// A detect is up to 120 probes over 40s and saves nothing: while one waits
// on the vendor, a catalog refresh (and with it the provider list) still
// answers at once. Before, JellytoolConnection held jellytoolAuthMu for the
// whole detect, and every refresh waited for it.
func TestJellytoolDetectHoldsNoCatalogLock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	resources, data := t.TempDir(), t.TempDir()
	if err := PackJellytoolPlugins(ctx, testenv.Bun(t), resources); err != nil {
		t.Fatal(err)
	}
	probing, release := make(chan struct{}, 8), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"data":[{"id":"m1","type":"TEXT"}]}`)
			return
		}
		// every probe of the detect waits here until the test lets go
		select {
		case probing <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(503)
	}))
	defer srv.Close()
	defer close(release)
	p, err := startJellytoolPlugins(ctx, resources, data, map[string]any{"testOrigin": srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if !jellytoolActive.CompareAndSwap(nil, p) {
		t.Fatal("already active")
	}
	defer p.Close()
	if _, err := jellytoolAPIKey(ctx, "jellytoken", 0, "fixture-key"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := JellytoolConnection(ctx, map[string]any{"action": "detect", "models": []string{"m1"}})
		done <- err
	}()
	select {
	case <-probing:
	case err := <-done:
		t.Fatalf("detect ended before probing: %v", err)
	case <-ctx.Done():
		t.Fatal("detect never probed")
	}
	rctx, rcancel := context.WithTimeout(ctx, 10*time.Second)
	defer rcancel()
	start := time.Now()
	ps, err := refreshJellytool(rctx)
	if err != nil {
		t.Fatalf("refresh during a detect: %v (after %v)", err, time.Since(start))
	}
	if len(ps) != 1 || len(ps[0].Models) != 1 || !strings.EqualFold(ps[0].Models[0].ID, "m1") {
		t.Fatalf("catalog during a detect: %+v", ps)
	}
	select {
	case err := <-done:
		t.Fatalf("detect ended before the refresh was checked: %v", err)
	default:
	}
}
