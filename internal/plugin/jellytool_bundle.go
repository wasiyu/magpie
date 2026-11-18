package plugin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/yetone/magpie/internal/proc"
)

//go:embed jellytoken/index.js jellytoken/package.json jellytoken/recommendations.json
var jellytokenFiles embed.FS

const jellytokenVersion = "0.1.0"

type jellytoolRuntimeLock struct {
	Version string `json:"version"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	SHA256  string `json:"sha256"`
}

// PackJellytoolPlugins stages application resources from a locally supplied
// Bun. It never downloads or installs anything. The app packager owns and must
// protect these resources (including runtime-lock.json) with the app itself.
func PackJellytoolPlugins(ctx context.Context, bun, resources string) error {
	return packJellytoolPlugins(ctx, bun, resources, runtime.GOOS, runtime.GOARCH, true)
}

// PackJellytoolPluginsFor stages resources for a cross-compiled Jellytool.
// The supplied Bun is for the target platform and therefore cannot be run on
// the build host; the normal packer remains strict about the host's Bun.
func PackJellytoolPluginsFor(ctx context.Context, bun, resources, goos, goarch string) error {
	return packJellytoolPlugins(ctx, bun, resources, goos, goarch, false)
}

func packJellytoolPlugins(ctx context.Context, bun, resources, goos, goarch string, verifyBun bool) error {
	if !filepath.IsAbs(bun) || !filepath.IsAbs(resources) {
		return errors.New("Jellytool bundle paths must be absolute")
	}
	if goos != "darwin" && goos != "linux" && goos != "windows" || goarch != "amd64" && goarch != "arm64" {
		return errors.New("Jellytool target must be darwin, linux or windows with amd64 or arm64")
	}
	if verifyBun {
		out, err := proc.CommandContext(ctx, bun, "--version").Output()
		if err != nil || strings.TrimSpace(string(out)) != BunVersion {
			return fmt.Errorf("Jellytool requires bundled Bun %s", BunVersion)
		}
	}
	if err := os.MkdirAll(resources, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(resources)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return errors.New("Jellytool resource output must be empty")
	}
	for _, name := range []string{"index.js", "package.json", "recommendations.json"} {
		b, _ := jellytokenFiles.ReadFile("jellytoken/" + name)
		if err := os.WriteFile(filepath.Join(resources, name), b, 0o600); err != nil {
			return err
		}
	}
	src, err := os.Open(bun)
	if err != nil {
		return err
	}
	defer src.Close()
	bunName := "bun"
	if goos == "windows" {
		bunName = "bun.exe"
	}
	dst, err := os.OpenFile(filepath.Join(resources, bunName), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return err
	}
	sum, err := jellytoolFileSum(filepath.Join(resources, bunName))
	if err != nil {
		return err
	}
	lock, _ := json.MarshalIndent(jellytoolRuntimeLock{BunVersion, goos, goarch, sum}, "", "  ")
	return os.WriteFile(filepath.Join(resources, "runtime-lock.json"), append(lock, '\n'), 0o600)
}

// JellytoolPlugins is an isolated, owned plugin host for P0 and the later app
// composition. It has no arbitrary RPC, install, update or plugin-selection
// entry point. Close it after all response bodies have been closed.
type JellytoolPlugins struct {
	h         *host
	closeOnce sync.Once
}

// StartJellytoolPlugins verifies offline resources and registers just the
// embedded Jellytoken plugin in data/plugins. data must be Jellytool's own
// absolute data directory, never magpie's. This does not start the desktop app.
func StartJellytoolPlugins(ctx context.Context, resources, data string) (*JellytoolPlugins, error) {
	return startJellytoolPlugins(ctx, resources, data, nil)
}

func startJellytoolPlugins(ctx context.Context, resources, data string, testOptions map[string]any) (*JellytoolPlugins, error) {
	if !filepath.IsAbs(resources) || !filepath.IsAbs(data) {
		return nil, errors.New("Jellytool bundle paths must be absolute")
	}
	b, err := os.ReadFile(filepath.Join(resources, "runtime-lock.json"))
	if err != nil {
		return nil, err
	}
	var lock jellytoolRuntimeLock
	if json.Unmarshal(b, &lock) != nil || lock.Version != BunVersion || lock.OS != runtime.GOOS || lock.Arch != runtime.GOARCH || len(lock.SHA256) != 64 {
		return nil, errors.New("Jellytool runtime lock does not match this build")
	}
	bun := filepath.Join(resources, bunExe())
	sum, err := jellytoolFileSum(bun)
	if err != nil || sum != lock.SHA256 {
		return nil, errors.New("Jellytool bundled Bun failed integrity verification")
	}
	for _, name := range []string{"index.js", "package.json", "recommendations.json"} {
		want, _ := jellytokenFiles.ReadFile("jellytoken/" + name)
		got, err := os.ReadFile(filepath.Join(resources, name))
		if err != nil || !bytes.Equal(got, want) {
			return nil, fmt.Errorf("Jellytool bundled %s failed integrity verification", name)
		}
	}
	// The bundle content determines the generation. Existing files are verified,
	// never imported from plugins.json or replaced from npm/the plugin market.
	js, _ := jellytokenFiles.ReadFile("jellytoken/index.js")
	pkg, _ := jellytokenFiles.ReadFile("jellytoken/package.json")
	recommendations, _ := jellytokenFiles.ReadFile("jellytoken/recommendations.json")
	generation := sha256.New()
	for _, content := range [][]byte{js, pkg, hostJS, recommendations} {
		generation.Write(content)
	}
	dir := filepath.Join(data, "plugins", "jellytoken-"+jellytokenVersion+"-"+hex.EncodeToString(generation.Sum(nil)[:6]))
	if err := os.MkdirAll(data, 0o700); err != nil {
		return nil, err
	}
	for _, d := range []string{filepath.Join(data, "plugins"), dir} {
		if err := os.Mkdir(d, 0o700); err != nil && !os.IsExist(err) {
			return nil, err
		}
		info, err := os.Lstat(d)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("Jellytool plugin directory must be a real directory")
		}
	}
	for name, content := range map[string][]byte{"index.js": js, "host.js": hostJS, "recommendations.json": recommendations} {
		if err := jellytoolStageFile(filepath.Join(dir, name), content); err != nil {
			return nil, err
		}
	}
	if err := jellytoolStageFile(filepath.Join(dir, "package.json"), pkg); err != nil {
		return nil, err
	}
	cmd := proc.Command(bun, "--no-install", "run", filepath.Join(dir, "host.js"))
	cmd.Dir = data
	// Do not inherit BUN_OPTIONS/NODE_OPTIONS, preloads, proxies or magpie paths.
	cmd.Env = []string{"PATH=" + filepath.Dir(bun), "HOME=" + data, "USERPROFILE=" + data, "BUN_INSTALL_AUTO=disable", "NODE_USE_SYSTEM_CA=1"}
	if testOptions != nil {
		if os.Getenv("MAGPIE_TEST_SANDBOX") == "" {
			return nil, errors.New("Jellytool test options require a sandbox")
		}
		cmd.Env = append(cmd.Env, "MAGPIE_TEST_SANDBOX="+data)
	}
	h, err := launchHost(cmd, "", "", true)
	if err != nil {
		return nil, err
	}
	var result struct {
		Plugins []Loaded `json:"plugins"`
	}
	startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err = h.call(startCtx, "init", map[string]any{
		"authPath": filepath.Join(data, "plugin-auth.json"), "directory": data,
		"plugins": []map[string]any{{"spec": filepath.Join(dir, "index.js"), "target": filepath.Join(dir, "index.js"), "options": testOptions}},
	}, &result)
	if err == nil && (len(result.Plugins) != 1 || result.Plugins[0].Error != "") {
		err = errors.New("Jellytool bundled plugin did not load")
	}
	if err != nil {
		h.stop()
		<-h.dead
		return nil, err
	}
	return &JellytoolPlugins{h: h}, nil
}

func jellytoolFileSum(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("Jellytool resource must be a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func jellytoolStageFile(path string, content []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if os.IsExist(err) {
		if _, err := jellytoolFileSum(path); err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(b, content) {
			return errors.New("Jellytool staged plugin failed integrity verification")
		}
		return nil
	}
	if err != nil {
		return err
	}
	_, writeErr := f.Write(content)
	return errors.Join(writeErr, f.Close())
}

// Providers returns the plugin's contract in the shape the supplier page uses.
// It deliberately does not publish to magpie's provider cache.
func (p *JellytoolPlugins) Providers(ctx context.Context) ([]Provider, error) {
	var result []Provider
	err := p.h.call(ctx, "providers", nil, &result)
	return result, err
}

// APIKey validates the candidate's model directory before replacing the one
// Jellytoken sign-in. A refused/unreadable directory preserves the old key.
func (p *JellytoolPlugins) APIKey(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if key == "" || strings.ContainsAny(key, "\r\n") {
		return errors.New("Jellytoken requires an API key")
	}
	return p.h.call(ctx, "jellytool", map[string]any{"action": "save", "key": key}, nil)
}

func (p *JellytoolPlugins) Fetch(ctx context.Context, r FetchRequest) (*http.Response, error) {
	if r.Provider != "jellytoken" || r.Account != "" && r.Account != "jellytoken" || r.Proxy != "" {
		return nil, errors.New("Jellytool permits only its bundled Jellytoken provider")
	}
	r.Proxy = "direct"
	return p.h.fetch(ctx, r)
}

func (p *JellytoolPlugins) Close() {
	p.closeOnce.Do(func() {
		if jellytoolActive.CompareAndSwap(p, nil) {
			jellytoolCatalog.Lock()
			jellytoolCatalog.providers = nil
			jellytoolCatalog.Unlock()
		}
		p.h.stop()
		<-p.h.dead
	})
}

// Done closes when the private runtime exits. It is never automatically replaced.
func (p *JellytoolPlugins) Done() <-chan struct{} { return p.h.dead }
