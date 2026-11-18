package gateway

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/yetone/magpie/internal/product"
)

// JellytoolFront admits only local inference and model listing. In particular,
// none of magpie's owner, profiling, administration or MCP routes is served.
func (s *Server) JellytoolFront() http.Handler {
	next := s.Front()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed := r.Method == "GET" && r.URL.Path == "/v1/models"
		if r.Method == "POST" {
			switch r.URL.Path {
			case "/v1/chat/completions", "/v1/responses", "/v1/messages":
				allowed = true
			}
		}
		if !allowed {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ServeJellytool serves a listener already acquired by the independent app.
// There are no socket handovers, background tasks, migrations or updaters.
func (s *Server) ServeJellytool(ctx context.Context, ln net.Listener) error {
	if !product.Jellytool {
		return errors.New("Jellytool requires a jellytool build")
	}
	srv := &http.Server{Handler: s.JellytoolFront(), ReadHeaderTimeout: 15 * time.Second, IdleTimeout: time.Minute}
	finished := make(chan struct{})
	stopped := make(chan struct{})
	defer func() { close(finished); <-stopped }()
	go func() {
		defer close(stopped)
		select {
		case <-ctx.Done():
		case <-finished:
			return
		}
		drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(drain); err != nil {
			_ = srv.Close()
		}
	}()
	err := srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		<-stopped
		return nil
	}
	return err
}
