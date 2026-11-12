package docker

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
)

func run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func ServerVersion(ctx context.Context) (string, error) {
	return run(ctx, "version", "--format", "{{.Server.Version}}")
}

func ComposeVersion(ctx context.Context) (string, error) {
	return run(ctx, "compose", "version", "--short")
}

func ContainerState(ctx context.Context, name string) (exists, running bool, err error) {
	out, err := run(ctx, "inspect", "--type", "container", "--format", "{{.State.Running}}", name)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such") {
			return false, false, nil
		}
		return false, false, err
	}
	return true, out == "true", nil
}

func ReadFile(ctx context.Context, container, path string) ([]byte, error) {
	out, err := run(ctx, "exec", container, "cat", path)
	return []byte(out), err
}

func Compose(ctx context.Context, file string, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose", "-f", file}, args...)...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}
