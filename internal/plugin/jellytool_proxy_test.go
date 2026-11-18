package plugin

import (
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/settings"
	"github.com/yetone/magpie/internal/testenv"
)

// Jellytoken's requests go through the proxy magpie's other Bun processes
// would: the provider's choice, else Settings' global proxy, else direct.
// Before, the bundled host was told "direct" on every call, whatever the
// user had set.
func TestJellytoolProxyFollowsTheChoice(t *testing.T) {
	testenv.SetHome(t, t.TempDir())
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
		t.Setenv(k, "")
	}
	if got := jellytoolProxy("direct"); got != "direct" {
		t.Errorf("direct: %q", got)
	}
	if got := jellytoolProxy("http://127.0.0.1:8080"); got != "http://127.0.0.1:8080" {
		t.Errorf("the provider's own proxy: %q", got)
	}
	if err := settings.Save(settings.Settings{Proxy: "http://127.0.0.1:8081"}); err != nil {
		t.Fatal(err)
	}
	if got := jellytoolProxy(""); got != "http://127.0.0.1:8081" {
		t.Errorf("the app's proxy: %q", got)
	}
	if got := jellytoolProxy("direct"); got != "direct" {
		t.Errorf("direct over the app's proxy: %q", got)
	}
	was := ProxyFor
	t.Cleanup(func() { ProxyFor = was })
	ProxyFor = func(provider, key string) string {
		if provider != "jellytoken" || key != "jellytoken" {
			t.Errorf("asked for %s/%s", provider, key)
		}
		return "http://127.0.0.1:8082"
	}
	if got := jellytoolAccountProxy(); got != "http://127.0.0.1:8082" {
		t.Errorf("the account's proxy: %q", got)
	}
}

// The bundled host's environment holds no proxy or preload, and on Windows
// the system variables a process needs there to reach the network.
func TestJellytoolHostEnv(t *testing.T) {
	vars := map[string]string{"SystemRoot": `C:\Windows`, "TEMP": `C:\Temp`, "HTTPS_PROXY": "http://leak", "NODE_OPTIONS": "--require x"}
	get := func(k string) string { return vars[k] }
	has := func(env []string, want string) bool {
		for _, e := range env {
			if e == want {
				return true
			}
		}
		return false
	}
	win := jellytoolHostEnv("windows", `C:\app\res`, `C:\data`, get)
	for _, want := range []string{`SystemRoot=C:\Windows`, `TEMP=C:\Temp`, `PATH=C:\app\res;` + filepath.Join(`C:\Windows`, "System32")} {
		if !has(win, want) {
			t.Errorf("windows env lacks %s: %v", want, win)
		}
	}
	mac := jellytoolHostEnv("darwin", "/app/res", "/data", get)
	for _, env := range [][]string{win, mac} {
		for _, e := range env {
			if len(e) > 6 && (e[:6] == "HTTPS_" || e[:5] == "NODE_" && e != "NODE_USE_SYSTEM_CA=1") {
				t.Errorf("inherited %s", e)
			}
		}
	}
	if has(mac, `SystemRoot=C:\Windows`) {
		t.Error("a Mac host was given Windows' variables")
	}
}
