package proxy

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jnariai/lazydomain/internal/docker"
)

//go:embed assets/compose.yaml
var composeYAML []byte

const (
	// ContainerName must match container_name in assets/compose.yaml.
	ContainerName = "lazydomain-caddy"
	RootCertPath  = "/data/caddy/pki/authorities/local/root.crt"

	intermediateCertPath = "/data/caddy/pki/authorities/local/intermediate.crt"
	intermediateKeyPath  = "/data/caddy/pki/authorities/local/intermediate.key"
	leafCertsDir         = "/data/caddy/certificates/local"
)

func ComposeFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "lazydomain", "compose.yaml"), nil
}

func writeCompose() (string, error) {
	path, err := ComposeFile()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, composeYAML, 0o644)
}

func Up(ctx context.Context, routes []Route) error {
	path, err := writeCompose()
	if err != nil {
		return fmt.Errorf("write compose file: %w", err)
	}
	if err := docker.Compose(ctx, path, "up", "-d"); err != nil {
		return fmt.Errorf("docker compose up: %w", err)
	}

	c := NewClient()
	wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := c.WaitReady(wctx); err != nil {
		return fmt.Errorf("caddy admin API on %s did not come up: %w", AdminAddr, err)
	}

	// Always push the full config so routes from state.json and newer
	// settings (e.g. certificate lifetimes) reach a proxy restored by --resume.
	cfg, err := BuildConfig(routes)
	if err != nil {
		return err
	}
	if err := c.Load(ctx, cfg); err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	inter, err := loadCert(ctx, intermediateCertPath)
	if err != nil {
		return fmt.Errorf("read intermediate certificate: %w", err)
	}
	if !IntermediateTooShort(inter) {
		return nil
	}
	fmt.Println("Regenerating the proxy's intermediate certificate with a longer lifetime (root CA unchanged)...")
	if err := regenerateIntermediate(ctx); err != nil {
		return err
	}
	wctx2, cancel2 := context.WithTimeout(ctx, 30*time.Second)
	defer cancel2()
	if err := c.WaitReady(wctx2); err != nil {
		return fmt.Errorf("caddy admin API on %s did not come back: %w", AdminAddr, err)
	}
	return nil
}

func Purge(ctx context.Context) error {
	path, err := writeCompose()
	if err != nil {
		return err
	}
	return docker.Compose(ctx, path, "down", "--volumes", "--remove-orphans")
}

func Down(ctx context.Context) error {
	path, err := writeCompose()
	if err != nil {
		return err
	}
	return docker.Compose(ctx, path, "down")
}
