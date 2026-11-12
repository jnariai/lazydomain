package laravel

import (
	"os"
	"path/filepath"
	"testing"
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

func makeProject(t *testing.T, root, name, version, env string) {
	t.Helper()
	dir := filepath.Join(root, name)
	write(t, filepath.Join(dir, "artisan"), "#!/usr/bin/env php\n")
	write(t, filepath.Join(dir, "composer.json"), `{"require":{"laravel/framework":"^`+version+`"}}`)
	write(t, filepath.Join(dir, "composer.lock"), `{"packages":[{"name":"other/pkg","version":"1.0"},{"name":"laravel/framework","version":"v`+version+`.2.0"}]}`)
	if env != "" {
		write(t, filepath.Join(dir, ".env"), env)
	}
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "shop", "13", "APP_PORT=8080\nVITE_PORT=5174\n")
	makeProject(t, root, "legacy", "10", "")
	write(t, filepath.Join(root, "notes", "README.md"), "not laravel")
	makeProject(t, root, ".hidden", "12", "")

	projects, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 {
		t.Fatalf("got %d projects, want 2", len(projects))
	}

	legacy, shop := projects[0], projects[1]
	if legacy.Name() != "legacy" || shop.Name() != "shop" {
		t.Fatalf("unexpected order: %s, %s", legacy.Name(), shop.Name())
	}
	if legacy.Supported() || legacy.Major != 10 {
		t.Errorf("legacy: major %d supported %v", legacy.Major, legacy.Supported())
	}
	if legacy.AppPort != 80 || legacy.VitePort != 5173 {
		t.Errorf("legacy defaults: %d/%d", legacy.AppPort, legacy.VitePort)
	}
	if !shop.Supported() || shop.Version != "13.2.0" {
		t.Errorf("shop: version %q supported %v", shop.Version, shop.Supported())
	}
	if shop.AppPort != 8080 || shop.VitePort != 5174 {
		t.Errorf("shop ports: %d/%d", shop.AppPort, shop.VitePort)
	}
}

func TestInspectNotLaravel(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "artisan"), "")
	write(t, filepath.Join(dir, "composer.json"), `{"require":{"symfony/console":"^7"}}`)
	if _, err := Inspect(dir); err != ErrNotLaravel {
		t.Errorf("err = %v, want ErrNotLaravel", err)
	}
}

func TestParseMajor(t *testing.T) {
	for in, want := range map[string]int{"v13.40.0": 13, "11.0.1": 11, "13.x-dev": 13, "dev-main": 0, "": 0} {
		if got := ParseMajor(in); got != want {
			t.Errorf("ParseMajor(%q) = %d, want %d", in, got, want)
		}
	}
}
