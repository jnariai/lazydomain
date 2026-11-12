package sites

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jnariai/lazydomain/internal/laravel"
	"github.com/jnariai/lazydomain/internal/state"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func project(t *testing.T, root, name, env string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	write(t, filepath.Join(dir, "artisan"), "")
	write(t, filepath.Join(dir, "composer.json"), `{"require":{"laravel/framework":"^13"}}`)
	write(t, filepath.Join(dir, "composer.lock"), `{"packages":[{"name":"laravel/framework","version":"v13.1.0"}]}`)
	write(t, filepath.Join(dir, ".env"), env)
	return dir
}

func titles(steps []Step) string {
	var s []string
	for _, st := range steps {
		s = append(s, st.Title)
	}
	return strings.Join(s, " | ")
}

func TestPlanAddRejectsProxyPorts(t *testing.T) {
	root := t.TempDir()
	dir := project(t, root, "shop", "APP_PORT=80\n")
	if _, err := PlanAdd(&state.State{}, dir); err == nil || !strings.Contains(err.Error(), "APP_PORT is 80") {
		t.Errorf("err = %v", err)
	}
}

func TestPlanAddRejectsTakenPortAndWarnsSharedVite(t *testing.T) {
	root := t.TempDir()
	st := &state.State{Sites: []state.Site{{Label: "a", Dir: "/elsewhere/a", AppPort: 8080, VitePort: 5173}}}

	dir := project(t, root, "b", "APP_PORT=8080\n")
	if _, err := PlanAdd(st, dir); err == nil || !strings.Contains(err.Error(), "already routed") {
		t.Errorf("err = %v", err)
	}

	write(t, filepath.Join(dir, ".env"), "APP_PORT=8081\n")
	plan, err := PlanAdd(st, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Warnings) != 1 || !strings.Contains(plan.Warnings[0], "VITE_PORT 5173") {
		t.Errorf("warnings = %v", plan.Warnings)
	}
	if len(plan.Next.Sites) != 2 || len(st.Sites) != 1 {
		t.Error("plan must not modify the input state")
	}
}

func TestNextSteps(t *testing.T) {
	root := t.TempDir()
	dir := project(t, root, "shop", "APP_URL=http://localhost\n")
	write(t, filepath.Join(dir, "vite.config.js"), "export default defineConfig({})\n")
	p, err := laravel.Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	site := state.Site{Label: "shop", Dir: dir, AppPort: 8080, VitePort: 5173}

	if got := titles(NextSteps(p, site)); !strings.Contains(got, ".env") || !strings.Contains(got, "bootstrap") || !strings.Contains(got, "vite.config.js") {
		t.Errorf("all steps expected, got %q", got)
	}

	write(t, filepath.Join(dir, ".env"), "APP_URL=https://shop.test\nVITE_APP_URL=https://vite.shop.test\n")
	write(t, filepath.Join(dir, "bootstrap", "app.php"), "$middleware->trustProxies(at: '*');")
	write(t, filepath.Join(dir, "vite.config.js"), "const env = loadEnv(mode, process.cwd(), '')\n")
	p, _ = laravel.Inspect(dir)
	if steps := NextSteps(p, site); len(steps) != 0 {
		t.Errorf("expected no steps, got %q", titles(steps))
	}

	write(t, filepath.Join(dir, ".env"), "APP_URL=https://shop.test\n")
	p, _ = laravel.Inspect(dir)
	if got := titles(NextSteps(p, site)); got != "vite.config.js, inside defineConfig({ … })" {
		t.Errorf("expected only the vite step, got %q", got)
	}
}
