package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yetone/magpie/internal/netproxy"
	"github.com/yetone/magpie/internal/product"
)

var jellytoolActive atomic.Pointer[JellytoolPlugins]

// Serialize catalog reads with credential replacement/sign-out. Cached readers
// never take this lock while a network request is running.
var jellytoolAuthMu sync.Mutex
var jellytoolCatalog struct {
	sync.RWMutex
	providers []Provider
	// key is jellytoolKey when providers was read: a catalog read under
	// another key (replaced, or signed out) is never lent to this one
	key uint64
}

// jellytoolKey counts the bundled account's key changes: a save, an API
// key or a sign-out. Each refresh after one starts from no catalog.
var jellytoolKey atomic.Uint64

// jellytoolListRefused says a failed list was the vendor's answer about
// the key (HTTP 401 or 403, or a business error in the reply), not a
// failure to ask: a timeout, a dropped connection, a 429 or a 5xx says
// nothing about what the key may use. The plugin words its errors as
// catalog() in jellytoken/index.js does.
func jellytoolListRefused(said string) bool {
	return strings.Contains(said, "HTTP 401") || strings.Contains(said, "HTTP 403") || strings.Contains(said, "business error")
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
//
// Only a save takes jellytoolAuthMu: it replaces the account and the
// catalog read from it. A detect sends up to five probes for each of up to
// 24 models (over 40s) and saves nothing, and config reads only; held for
// them, every catalog refresh and with it the provider list waited.
func JellytoolConnection(ctx context.Context, in map[string]any) (map[string]any, error) {
	if in["action"] == "save" {
		jellytoolAuthMu.Lock()
		defer jellytoolAuthMu.Unlock()
	}
	h, err := jellytoolHost()
	if err != nil {
		return nil, err
	}
	in["proxy"] = jellytoolAccountProxy()
	var result map[string]any
	if err := h.call(ctx, "jellytool", in, &result); err != nil {
		return nil, err
	}
	if in["action"] == "save" {
		jellytoolKey.Add(1)
		generation.Add(1)
		changed()
		_, _ = refreshJellytoolLocked(ctx)
	}
	return result, nil
}

// jellytoolProxy is the proxy the bundled host sends a request through for
// choice (netproxy.With's: "" the app's own, "direct", a URL or a saved
// proxy's name), as magpie's other Bun processes take it (EnvWith): the
// provider's proxy, else Settings' global one, else the system's. The host
// is started with no proxy in its environment, so the choice goes with each
// call; before, every Jellytoken request went direct whatever was set.
func jellytoolProxy(choice string) string {
	env := netproxy.EnvWith(choice, nil)
	for _, want := range []string{"HTTPS_PROXY", "ALL_PROXY", "HTTP_PROXY"} {
		for _, e := range env {
			k, v, _ := strings.Cut(e, "=")
			if strings.EqualFold(k, want) && strings.TrimSpace(v) != "" {
				return forHost(strings.TrimSpace(v))
			}
		}
	}
	return "direct"
}

// jellytoolAccountProxy is jellytoolProxy for the bundled account's own
// proxy setting (the provider's, as ProxyFor gives it).
func jellytoolAccountProxy() string {
	choice := ""
	if ProxyFor != nil {
		choice = ProxyFor("jellytoken", "jellytoken")
	}
	return jellytoolProxy(choice)
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
	key := jellytoolKey.Load()
	ps, err := p.Providers(ctx)
	if p != jellytoolActive.Load() {
		return nil, errHostGone
	}
	if err == nil && (len(ps) != 1 || ps[0].ID != "jellytoken") {
		err = errors.New("unexpected Jellytool provider")
	}
	if err != nil {
		// The host itself failed to answer: what the key may use is
		// unknown. The last good catalog read under this same key is kept
		// (the vendor still refuses whatever the key may not use); one
		// read under another key is never lent to this one.
		jellytoolCatalog.Lock()
		if jellytoolCatalog.key != key {
			jellytoolCatalog.providers = nil
		}
		jellytoolCatalog.Unlock()
		listed()
		return nil, err
	}
	if ps[0].FellBack || ps[0].ListError != "" {
		// A fallback list is not a key authorization result. A refusal
		// (401/403) takes the catalog away; a list that could not be read
		// (timeout, 5xx) keeps the last good one of this key, so a moment's
		// outage doesn't turn every agent's request into an unknown model.
		jellytoolCatalog.Lock()
		kept := jellytoolCatalog.key == key && !jellytoolListRefused(ps[0].ListError) &&
			len(jellytoolCatalog.providers) == 1 && len(jellytoolCatalog.providers[0].Models) > 0
		if !kept {
			ps[0].Models = nil
			jellytoolCatalog.providers, jellytoolCatalog.key = ps, key
		}
		jellytoolCatalog.Unlock()
		listed()
		return jellytoolCached(), nil
	}
	jellytoolCatalog.Lock()
	jellytoolCatalog.providers, jellytoolCatalog.key = ps, key
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
	jellytoolKey.Add(1)
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
	in["proxy"] = jellytoolAccountProxy()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := h.call(ctx, method, in, out); err != nil {
		return err
	}
	if method == "signOut" {
		jellytoolKey.Add(1)
		generation.Add(1)
		changed()
		_, err = refreshJellytoolLocked(ctx)
	}
	return err
}
