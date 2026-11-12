package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const fallbackPowerShell = "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe"

func powerShellPath() (string, error) {
	if p, err := exec.LookPath("powershell.exe"); err == nil {
		return p, nil
	}
	if _, err := os.Stat(fallbackPowerShell); err == nil {
		return fallbackPowerShell, nil
	}
	return "", errors.New("powershell.exe not found: is WSL interop enabled? ([interop] enabled=true in /etc/wsl.conf)")
}

func PowerShell(ctx context.Context, script string) (string, error) {
	exe, err := powerShellPath()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, exe, "-NoProfile", "-NonInteractive", "-Command", script)
	// Start from a Windows directory: a \\wsl$ UNC working directory makes
	// Windows tools print warnings or fall back to C:\Windows anyway.
	cmd.Dir = filepath.Dir(exe)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("powershell: %s", msg)
	}
	return strings.TrimSpace(strings.ReplaceAll(stdout.String(), "\r\n", "\n")), nil
}

func NetworkingMode(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "wslinfo", "--networking-mode").Output()
	if err != nil {
		return "", fmt.Errorf("wslinfo: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func WindowsTCPOpen(ctx context.Context, port int) (bool, error) {
	script := fmt.Sprintf(`$c = New-Object Net.Sockets.TcpClient
try { if ($c.ConnectAsync('127.0.0.1', %d).Wait(2000) -and $c.Connected) { 'open' } else { 'closed' } }
catch { 'closed' } finally { $c.Dispose() }`, port)
	out, err := PowerShell(ctx, script)
	if err != nil {
		return false, err
	}
	return out == "open", nil
}

func OpenURL(ctx context.Context, info Info, url string) error {
	if !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("refusing to open non-https URL %q", url)
	}
	if info.Kind == WSL {
		_, err := PowerShell(ctx, fmt.Sprintf("Start-Process '%s'", psQuote(url)))
		return err
	}
	return exec.CommandContext(ctx, "xdg-open", url).Start()
}

var thumbprintRE = regexp.MustCompile(`^[0-9A-F]{40}$`)

func WindowsRootTrusted(ctx context.Context, thumbprint string) (bool, error) {
	if !thumbprintRE.MatchString(thumbprint) {
		return false, fmt.Errorf("invalid thumbprint %q", thumbprint)
	}
	out, err := PowerShell(ctx, fmt.Sprintf(`Test-Path 'Cert:\CurrentUser\Root\%s'`, thumbprint))
	if err != nil {
		return false, err
	}
	return strings.EqualFold(out, "True"), nil
}
