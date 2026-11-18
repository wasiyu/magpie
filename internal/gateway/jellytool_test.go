//go:build jellytool

package gateway

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestJellytoolGatewaySurface(t *testing.T) {
	s := New()
	h := s.JellytoolFront()
	for _, p := range []string{"/", "/v1/magpie/status", "/v1/magpie/heap", "/v1/magpie/goroutines", "/mcp/magpie/web-search", "/api/hello", "/responses", "/v1beta/models", "/v1/responses/anything", "/_magpie/claude-mcp/token"} {
		for _, m := range []string{"GET", "POST"} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(m, p, nil))
			if w.Code != 404 {
				t.Errorf("%s %s: %d", m, p, w.Code)
			}
		}
	}
	// Real server stack, dynamic loopback port; WhileServing must not run.
	started := make(chan struct{}, 1)
	old := WhileServing
	WhileServing = []func(context.Context){func(context.Context) { started <- struct{}{} }}
	defer func() { WhileServing = old }()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.ServeJellytool(ctx, ln) }()
	client := http.Client{Timeout: 5 * time.Second}
	res, err := client.Get("http://" + ln.Addr().String() + "/v1/magpie/status")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 404 {
		t.Fatal(res.Status)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("gateway leaked after cancellation")
	}
	select {
	case <-started:
		t.Fatal("legacy background tasks started")
	default:
	}
}
