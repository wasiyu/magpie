package gui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/yetone/magpie/internal/gateway"
	"github.com/yetone/magpie/internal/lazyre"
	"github.com/yetone/magpie/internal/plugin"
	"github.com/yetone/magpie/internal/product"
	"github.com/yetone/magpie/internal/provider"
)

type jellytoolPrice struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

// jellytoolPrices caches the per-model prices Jellytoken's site shows. The
// models page answers with what is kept and has it read again in the
// background once it is older than jellytoolPriceFresh, one read at a time;
// only the first ask waits for it, and for jellytoolPriceFirstWait at most.
// A read is the app's own, not the page request's: a page closed half way
// leaves no half-read prices kept as fresh.
var jellytoolPrices = struct {
	sync.Mutex
	at      time.Time
	data    map[string]jellytoolPrice
	reading chan struct{} // closed when the read in flight ends; nil when none is
}{data: map[string]jellytoolPrice{}}

var (
	jellytoolPriceURL       = "https://www.jellytoken.com/api/console/getModelDetail"
	jellytoolPriceFresh     = 10 * time.Minute
	jellytoolPriceFirstWait = 3 * time.Second
	jellytoolPriceClient    = &http.Client{Timeout: 10 * time.Second} // the app's transport, its proxy included
)

func jellytoolPriceCandidates(ps []plugin.Provider) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range ps {
		for _, m := range p.Models {
			if seen[m.ID] || len(m.Outputs) > 0 && !containsString(m.Outputs, "text") {
				continue
			}
			id := strings.ToLower(m.ID)
			if strings.HasPrefix(id, "claude-") || !(strings.HasPrefix(id, "gpt-") || strings.HasPrefix(id, "minimax-") || strings.HasPrefix(id, "kimi-") || strings.HasPrefix(id, "qwen") || strings.HasPrefix(id, "deepseek-") || strings.HasPrefix(id, "glm-") || strings.HasPrefix(id, "doubao-") || strings.HasPrefix(id, "gemini-")) {
				continue
			}
			seen[m.ID] = true
			out = append(out, m.ID)
		}
	}
	return out
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if strings.EqualFold(x, want) {
			return true
		}
	}
	return false
}

func jellytoolPriceData(ctx context.Context, ids []string) map[string]jellytoolPrice {
	jellytoolPrices.Lock()
	first := jellytoolPrices.at.IsZero()
	reading := jellytoolPrices.reading
	if reading == nil && time.Since(jellytoolPrices.at) >= jellytoolPriceFresh && len(ids) > 0 {
		reading = make(chan struct{})
		jellytoolPrices.reading = reading
		go jellytoolReadPrices(slices.Clone(ids), reading)
	}
	jellytoolPrices.Unlock()
	if first && reading != nil {
		select {
		case <-reading:
		case <-ctx.Done():
		case <-time.After(jellytoolPriceFirstWait):
		}
	}
	jellytoolPrices.Lock()
	defer jellytoolPrices.Unlock()
	return mapsClone(jellytoolPrices.data)
}

// jellytoolReadPrices reads ids' prices, six at a time, and keeps those it
// got; a model it couldn't read keeps the price read before.
func jellytoolReadPrices(ids []string, done chan struct{}) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	type priceRow struct {
		Usage string  `json:"usageField"`
		Unit  string  `json:"priceUnit"`
		Price float64 `json:"discountUnitPrice"`
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	got := map[string]jellytoolPrice{}
	sem := make(chan struct{}, 6)
	for _, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			body, _ := json.Marshal(map[string]string{"modelCode": id})
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, jellytoolPriceURL, bytes.NewReader(body))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Page-Pathname", "/models")
			res, err := jellytoolPriceClient.Do(req)
			if err != nil {
				return
			}
			defer res.Body.Close()
			var payload struct {
				Success bool `json:"success"`
				Data    struct {
					Prices []priceRow `json:"prices"`
				} `json:"data"`
			}
			if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, 256<<10)).Decode(&payload) != nil || !payload.Success {
				return
			}
			var p jellytoolPrice
			for _, row := range payload.Data.Prices {
				if row.Unit != "PER_1K_TOKENS" {
					continue
				}
				switch strings.ToLower(row.Usage) {
				case "inputtokens":
					p.Input = row.Price * 1000
				case "outputtokens":
					p.Output = row.Price * 1000
				}
			}
			if p.Input > 0 || p.Output > 0 {
				mu.Lock()
				got[id] = p
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	jellytoolPrices.Lock()
	for id, p := range got {
		jellytoolPrices.data[id] = p
	}
	jellytoolPrices.at = time.Now()
	jellytoolPrices.reading = nil
	jellytoolPrices.Unlock()
	close(done)
}

func mapsClone(in map[string]jellytoolPrice) map[string]jellytoolPrice {
	out := make(map[string]jellytoolPrice, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// jellytoolHandler retains the existing supplier editor and Agent API. Every
// request is admitted before the general mux, including assets and POST bodies.
func jellytoolHandler(w Windows, gw *gateway.Server) http.Handler {
	next := handler(w, gw)
	return noFramedAPI(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/api/jellytool/config" {
			jellytoolConnectionRoute(rw, r)
			return
		}
		if p == "/api/jellytool/key" {
			jellytoolConnectionRoute(rw, r)
			return
		}
		if p == "/api/jellytool/detect" {
			jellytoolConnectionRoute(rw, r)
			return
		}
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && (p == "/" || p == "/index.html") {
			if r.URL.Query().Get("mode") != "" {
				http.Redirect(rw, r, "/?view=providers", http.StatusSeeOther)
				return
			}
			rw.Header().Set("Content-Type", "text/html; charset=utf-8")
			page := versionedPage(jellytoolPage())
			sum := sha256.Sum256(page)
			rw.Header().Set("ETag", `"`+hex.EncodeToString(sum[:12])+`"`)
			rw.Header().Set("Cache-Control", "no-cache")
			http.ServeContent(rw, r, "index.html", time.Time{}, strings.NewReader(string(page)))
			return
		}
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && p == "/manifest.json" {
			b, _ := assets.ReadFile("assets/manifest.json")
			var manifest map[string]any
			_ = json.Unmarshal(b, &manifest)
			manifest["name"], manifest["short_name"] = product.Name, product.Name
			manifest["icons"] = []map[string]string{{"src": "icons/jellytoken.png", "sizes": "256x256", "type": "image/png"}}
			b, _ = json.Marshal(manifest)
			rw.Header().Set("Content-Type", "application/manifest+json")
			rw.Header().Set("Cache-Control", "no-cache")
			http.ServeContent(rw, r, "manifest.json", time.Time{}, strings.NewReader(string(b)))
			return
		}
		if r.Method == http.MethodGet && p == "/jellytool-model-config.js" {
			cfg, err := readJellytoolModelConfig()
			rw.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			rw.Header().Set("Cache-Control", "no-store")
			if err != nil {
				message, _ := json.Marshal(err.Error())
				_, _ = fmt.Fprintf(rw, "window.jellytoolModelConfigError = %s;", message)
			} else {
				data, _ := json.Marshal(cfg)
				_, _ = fmt.Fprintf(rw, "window.jellytoolModelConfig = %s;", data)
			}
			return
		}
		if r.Method == http.MethodGet && p == "/api/jellytool/models" {
			recommendations, err := plugin.JellytoolRecommendations()
			if err != nil {
				fail(rw, err)
				return
			}
			models := []map[string]any{}
			catalogError := false
			cached := plugin.Cached()
			prices := jellytoolPriceData(r.Context(), jellytoolPriceCandidates(cached))
			for _, pp := range cached {
				if pp.ID == "jellytoken" && pp.SignedIn {
					catalogError = pp.FellBack || pp.ListError != ""
					if !catalogError {
						for _, m := range pp.Models {
							item := map[string]any{"id": m.ID, "outputs": m.Outputs, "free": m.Free, "rate": m.Rate, "released": m.Released}
							if price, ok := prices[m.ID]; ok {
								item["cost"] = price
							}
							if m.Cost != nil {
								item["cost"] = map[string]any{"input": m.Cost.Input, "output": m.Cost.Output, "cacheRead": m.Cost.Cache.Read, "cacheWrite": m.Cost.Cache.Write}
							}
							models = append(models, item)
						}
					}
				}
			}
			writeJSON(rw, map[string]any{"models": models, "recommendations": recommendations, "catalogError": catalogError})
			return
		}
		next.ServeHTTP(rw, r)
	}))
}

// jellytoolConnectionRoute shares the bundled plugin's validation, probe and
// commit path. The key is never returned to the page or used by a generic
// provider transport.
var jellytoolManageConnection = plugin.JellytoolConnection

func jellytoolConnectionRoute(rw http.ResponseWriter, r *http.Request) {
	in := map[string]any{}
	if r.Method == http.MethodGet && r.URL.Path == "/api/jellytool/config" {
		in["action"] = "config"
	} else if r.Method == http.MethodPost {
		var body struct {
			Key     string   `json:"key"`
			BaseURL *string  `json:"baseURL"`
			API     *string  `json:"api"`
			Models  []string `json:"models"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&body); err != nil {
			fail(rw, err)
			return
		}
		if body.Key != "" {
			in["key"] = body.Key
		}
		if body.BaseURL != nil {
			in["baseURL"] = *body.BaseURL
		}
		if body.API != nil {
			in["api"] = *body.API
		}
		in["action"] = "save"
		if r.URL.Path == "/api/jellytool/detect" {
			in["action"] = "detect"
			in["models"] = body.Models
		}
	} else {
		rw.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	hasSavedSelection := false
	if in["action"] == "save" {
		stored, err := provider.Stored()
		if err != nil {
			fail(rw, err)
			return
		}
		for _, p := range stored {
			if p.ID == "jellytoken" {
				hasSavedSelection = true
				break
			}
		}
	}
	result, err := jellytoolManageConnection(ctx, in)
	if err != nil {
		fail(rw, err)
		return
	}
	if in["action"] == "save" {
		provider.PluginSignedIn(plugin.Saved{Provider: "jellytoken", Account: "jellytoken"})
		provider.Changed()
		// Only the first key entered through this guide starts without model
		// picks. Existing accounts, including old vendor-list defaults, retain
		// their configured behavior when upgrading or replacing a key.
		if result["first"] == true && !hasSavedSelection {
			p, err := provider.Find("jellytoken")
			if err == nil {
				p.NoModels = true
				err = provider.Save(*p)
			}
			if err != nil {
				fail(rw, err)
				return
			}
		}
	}
	writeJSON(rw, result)
}

// Only the existing shell, provider editor and Agent scripts enter this app.
// Library's static stylesheet also supplies the shared switch controls.
func jellytoolAppIcon() []byte {
	b, _ := assets.ReadFile("assets/icons/jellytool-app.png")
	return b
}

func jellytoolIcon() []byte {
	b, _ := assets.ReadFile("assets/icons/jellytoken.png")
	return b
}

var jellytoolBrandLogo = lazyre.New(`(?s)<span class="logo"[^>]*>.*?</span>`)

func jellytoolPage() []byte {
	b, _ := assets.ReadFile("assets/index.html")
	b = jellytoolBrandLogo.ReplaceAll(b, []byte(`<span class="logo" aria-hidden="true"><img src="icons/jellytoken.png" width="22" height="22" alt=""></span>`))
	s := strings.ReplaceAll(string(b), "<title>magpie</title>", "<title>"+product.Name+"</title>")
	s = strings.ReplaceAll(s, `href="icons/favicon.svg" type="image/svg+xml"`, `href="icons/jellytoken.png" type="image/png"`)
	s = strings.ReplaceAll(s, "<span>magpie</span>", "<span>"+product.Name+"</span>")
	s = strings.ReplaceAll(s, `content="magpie"`, `content="`+product.Name+`"`)
	s = strings.ReplaceAll(s, `href="icons/apple-touch-icon.png"`, `href="icons/jellytoken.png"`)
	s = strings.Replace(s, "</head>", `<link rel="stylesheet" href="jellytool.css"></head>`, 1)
	s = strings.Replace(s, `<script src="start.js"></script>`, `<script src="jellytool-model-config.js"></script><script src="jellytool.js"></script><script src="start.js"></script>`, 1)
	return []byte(s)
}
