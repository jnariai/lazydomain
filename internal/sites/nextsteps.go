package sites

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jnariai/lazydomain/internal/laravel"
	"github.com/jnariai/lazydomain/internal/state"
)

type Step struct {
	Title string
	Body  string
}

func NextSteps(p *laravel.Project, site state.Site) []Step {
	var out []Step

	appURL := "https://" + site.Domain()
	if p.Env["APP_URL"] != appURL {
		out = append(out, Step{Title: ".env", Body: "APP_URL=" + appURL})
	}

	if !fileContains(filepath.Join(p.Dir, "bootstrap", "app.php"), "trustProxies") {
		out = append(out, Step{
			Title: "bootstrap/app.php, inside ->withMiddleware(function (Middleware $middleware) { … })",
			Body:  "$middleware->trustProxies(at: '*');",
		})
	}

	if cfg := viteConfig(p.Dir); cfg != "" && !viteConfigured(cfg, p.Env, site) {
		out = append(out, Step{
			Title: filepath.Base(cfg) + ", inside defineConfig({ … })",
			Body: fmt.Sprintf(`server: {
    host: '0.0.0.0',
    port: %d,
    strictPort: true,
    origin: 'https://%s',
    allowedHosts: ['%s'],
    cors: { origin: 'https://%s' },
    hmr: { host: '%s', protocol: 'wss', clientPort: 443 },
},`, site.VitePort, site.ViteDomain(), site.ViteDomain(), site.Domain(), site.ViteDomain()),
		})
	}
	return out
}

// Vite does not put .env into process.env, so a config without the literal
// domain can only see the Vite URL through loadEnv.
func viteConfigured(cfgPath string, env map[string]string, site state.Site) bool {
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		return false
	}
	cfg := string(raw)
	if strings.Contains(cfg, site.ViteDomain()) {
		return true
	}
	if !strings.Contains(cfg, "loadEnv") {
		return false
	}
	want := "https://" + site.ViteDomain()
	for _, v := range env {
		if strings.TrimRight(v, "/") == want {
			return true
		}
	}
	return false
}

func viteConfig(dir string) string {
	for _, name := range []string{"vite.config.js", "vite.config.ts", "vite.config.mjs", "vite.config.mts"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func fileContains(path, needle string) bool {
	b, err := os.ReadFile(path)
	return err == nil && strings.Contains(string(b), needle)
}
