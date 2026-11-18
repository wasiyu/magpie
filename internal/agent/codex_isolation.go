//go:build jellytool

package agent

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/filelock"
	"github.com/yetone/magpie/internal/filememo"
	"github.com/yetone/magpie/internal/gateway"
	"github.com/yetone/magpie/internal/product"
)

const codexProviderID = product.ID

func codexPeerID() string { return "magpie" }

// The provider is authoritative; a model name can occur in both catalogs.
// Older Jellytool builds used magpie's ID, so an exact endpoint match is
// needed before treating that entry as an upgrade rather than a peer.
func codexPeerActive(path, ownURL string) bool {
	p, _ := edit.GetTOMLTop(path, "model_provider")
	if profile, _ := edit.GetTOMLTop(path, "profile"); profile != "" {
		t, _ := edit.GetTOMLTable(path, "profiles."+profile)
		if t["model_provider"] != "" {
			p = t["model_provider"]
		}
	}
	top, _ := edit.GetTOMLTop(path, "model_provider")
	if top != "" && top != "openai" && top != codexProviderID {
		p = top
	}
	if p == codexPeerID() {
		t, _ := edit.GetTOMLTable(path, "model_providers."+p)
		return t["base_url"] != ownURL
	}
	if p != "" && p != "openai" {
		if p == codexProviderID {
			// Magpie can redirect other provider tables for existing threads
			// without changing the selected provider ID. Only an explicit
			// reconnect should reclaim an entry redirected away from us.
			t, _ := edit.GetTOMLTable(path, "model_providers."+p)
			return t["base_url"] != "" && strings.TrimSuffix(t["base_url"], "/") != strings.TrimSuffix(ownURL, "/")
		}
		return true
	}
	// Old beside-sign-in connections have no provider ID. Protect the peer's
	// catalog and default port rather than claiming every loopback gateway.
	c, _ := edit.GetTOMLTop(path, "model_catalog_json")
	if filepath.Base(c) == codexPeerID()+"-desktop-models.json" {
		return true
	}
	u, _ := edit.GetTOMLTop(path, "openai_base_url")
	peer, _ := edit.GetTOMLTable(path, "model_providers."+codexPeerID())
	if base := strings.TrimSuffix(peer["base_url"], "/v1"); base != "" && strings.TrimSuffix(u, "/") == base+gateway.CodexPath {
		return true
	}
	port := ":3425/"
	return strings.Contains(u, port) && strings.HasSuffix(strings.TrimSuffix(u, "/"), gateway.CodexPath)
}

// Jellytool writers share a lock outside the atomically/rollback boundary.
// A timeout fails closed; otherwise rollback could erase another writer.
func codexIsolated(a *Agent, ownURL string) *Agent {
	run := func(fn func() error) error {
		unlock, held := filelock.Lock(a.Path+".provider.lock", time.Second)
		if !held {
			return fmt.Errorf("Codex configuration is busy: %s", a.Path)
		}
		defer unlock()
		filememo.Forget()
		raw, err := edit.Read(a.Path)
		if err != nil {
			return err
		}
		var cfg map[string]any
		if len(raw) > 0 {
			if err := toml.Unmarshal(raw, &cfg); err != nil {
				return fmt.Errorf("%s: %w", a.Path, err)
			}
		}
		return edit.Atomically(fn, a.Path, filepath.Join(filepath.Dir(a.Path), codexProviderID+"-models.json"), filepath.Join(filepath.Dir(a.Path), codexProviderID+"-desktop-models.json"))
	}
	for i := range a.Fields {
		f := &a.Fields[i]
		if set := f.Set; set != nil {
			key := f.Key
			f.Set = func(v string) error {
				return run(func() error {
					if codexPeerActive(a.Path, ownURL) {
						if v == "" {
							return nil
						}
						if key != "model" {
							return fmt.Errorf("Codex is using another provider; select a %s model first", product.Name)
						}
					}
					if profile, _ := edit.GetTOMLTop(a.Path, "profile"); profile != "" {
						t, err := edit.GetTOMLTable(a.Path, "profiles."+profile)
						if err != nil {
							return err
						}
						if t["model_provider"] != "" && t["model_provider"] != codexProviderID {
							return fmt.Errorf("Codex profile %s selects %s; change the selected profile before connecting %s", profile, t["model_provider"], product.Name)
						}
					}
					return set(v)
				})
			}
		}
	}
	for _, hook := range []*func() error{&a.Sync, &a.Unwire, &a.EndFailover} {
		if fn := *hook; fn != nil {
			*hook = func() error { return run(fn) }
		}
	}
	if join := a.Join; join != nil {
		a.Join = func() (ok bool, err error) {
			err = run(func() error {
				if codexPeerActive(a.Path, ownURL) {
					return nil
				}
				var e error
				ok, e = join()
				return e
			})
			return
		}
	}
	if move := a.move; move != nil {
		a.move = func(from, to string) error {
			return run(func() error {
				if codexPeerActive(a.Path, ownURL) {
					return nil
				}
				return move(from, to)
			})
		}
	}
	return a
}
