package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yetone/magpie/internal/product"
)

var jellytoolActive atomic.Pointer[JellytoolPlugins]

// Serialize catalog reads with credential replacement/sign-out. Cached readers
// never take this lock while a network request is running.
var jellytoolAuthMu sync.Mutex
var jellytoolCatalog struct {
	sync.RWMutex
	providers []Provider
}

// ActivateJellytool connects the existing provider adapter to the verified
// private host. It never installs the host as magpie's general plugin host.
func ActivateJellytool(ctx context.Context, resources, data string) (*JellytoolPlugins, error) {
	if !product.Jellytool {
		return nil, errors.New("Jellytool requires a jellytool build")
	}
	p, err := StartJellytoolPlugins(ctx, resources, data)
	if err != nil {
		return nil, err
	}
	if !jellytoolActive.CompareAndSwap(nil, p) {
		p.Close()
		return nil, errors.New("Jellytool plugin host is already active")
	}
	generation.Add(1)
	if _, err = refreshJellytool(ctx); err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}

func jellytoolHost() (*host, error) {
	p := jellytoolActive.Load()
	if p == nil || !p.h.alive() {
		return nil, errors.New("Jellytool bundled plugin is unavailable; restart Jellytool")
	}
	return p.h, nil
}

// JellytoolConnection manages only the bundled provider. Its private hook
// validates before committing one auth/config record; probes save nothing.
func JellytoolConnection(ctx context.Context, in map[string]any) (map[string]any, error) {
	jellytoolAuthMu.Lock()
	defer jellytoolAuthMu.Unlock()
	h, err := jellytoolHost()
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := h.call(ctx, "jellytool", in, &result); err != nil {
		return nil, err
	}
	if in["action"] == "save" {
		generation.Add(1)
		changed()
		_, _ = refreshJellytoolLocked(ctx)
	}
	return result, nil
}

func jellytoolCached() []Provider {
	jellytoolCatalog.RLock()
	defer jellytoolCatalog.RUnlock()
	// Provider contains nested maps and slices; never lend a caller the cache.
	b, _ := json.Marshal(jellytoolCatalog.providers)
	var out []Provider
	_ = json.Unmarshal(b, &out)
	return out
}

func refreshJellytool(ctx context.Context) ([]Provider, error) {
	jellytoolAuthMu.Lock()
	defer jellytoolAuthMu.Unlock()
	return refreshJellytoolLocked(ctx)
}

func refreshJellytoolLocked(ctx context.Context) ([]Provider, error) {
	p := jellytoolActive.Load()
	if p == nil {
		return nil, errHostGone
	}
	ps, err := p.Providers(ctx)
	if p != jellytoolActive.Load() {
		return nil, errHostGone
	}
	if err != nil {
		// A failed authorization read must not lend the previous key's catalog
		// to the supplier picker or its save boundary.
		jellytoolCatalog.Lock()
		jellytoolCatalog.providers = nil
		jellytoolCatalog.Unlock()
		listed()
		return nil, err
	}
	if len(ps) != 1 || ps[0].ID != "jellytoken" {
		return nil, errors.New("unexpected Jellytool provider")
	}
	if ps[0].FellBack || ps[0].ListError != "" {
		ps[0].Models = nil // A fallback list is not a key authorization result.
	}
	jellytoolCatalog.Lock()
	jellytoolCatalog.providers = ps
	jellytoolCatalog.Unlock()
	listed()
	return jellytoolCached(), nil
}

func jellytoolAPIKey(ctx context.Context, provider string, method int, key string) (Saved, error) {
	if provider != "jellytoken" || method != 0 {
		return Saved{}, errors.New("Jellytool permits only Jellytoken API key sign-in")
	}
	jellytoolAuthMu.Lock()
	defer jellytoolAuthMu.Unlock()
	p := jellytoolActive.Load()
	if p == nil {
		return Saved{}, errHostGone
	}
	if err := p.APIKey(ctx, key); err != nil {
		return Saved{}, err
	}
	generation.Add(1) // LoaderOptions must not retain a replaced key's loader.
	changed()
	if _, err := refreshJellytoolLocked(ctx); err != nil {
		return Saved{}, err
	}
	return Saved{Provider: "jellytoken", Account: "jellytoken"}, nil
}

func jellytoolCall(ctx context.Context, method string, params, out any) error {
	h, err := jellytoolHost()
	if err != nil {
		return err
	}
	b, err := json.Marshal(params)
	if err != nil {
		return err
	}
	var in map[string]any
	if err := json.Unmarshal(b, &in); err != nil {
		return err
	}
	switch method {
	case "load", "prompt", "validate", "signOut":
		if in["provider"] != "jellytoken" {
			return errors.New("Jellytool permits only Jellytoken")
		}
		if account, _ := in["account"].(string); account != "" && account != "jellytoken" {
			return errors.New("unexpected Jellytoken account")
		}
	default:
		return errors.New("this plugin operation is disabled in Jellytool")
	}
	if method == "signOut" {
		jellytoolAuthMu.Lock()
		defer jellytoolAuthMu.Unlock()
	}
	in["proxy"] = "direct"
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := h.call(ctx, method, in, out); err != nil {
		return err
	}
	if method == "signOut" {
		generation.Add(1)
		changed()
		_, err = refreshJellytoolLocked(ctx)
	}
	return err
}
