package proxy

import (
	"bytes"
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

func Up(ctx context.Context) error {
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

	current, err := c.Config(ctx)
	if err != nil {
		return err
	}
	// Only seed an empty proxy; never wipe routes restored by --resume.
	if t := bytes.TrimSpace(current); len(t) == 0 || bytes.Equal(t, []byte("null")) {
		cfg, err := BuildConfig(nil)
		if err != nil {
			return err
		}
		if err := c.Load(ctx, cfg); err != nil {
			return fmt.Errorf("load base config: %w", err)
		}
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
