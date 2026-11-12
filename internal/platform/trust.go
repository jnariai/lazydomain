package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func LocalTrusted(pem []byte) (bool, error) {
	installed, err := os.ReadFile(LocalCAPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return bytes.Equal(bytes.TrimSpace(installed), bytes.TrimSpace(pem)), nil
}

func TrustLocal(ctx context.Context, pem []byte) error {
	if _, err := exec.LookPath("update-ca-certificates"); err != nil {
		return errors.New("update-ca-certificates not found (only Debian/Ubuntu are supported for now)")
	}
	f, err := os.CreateTemp("", "lazydomain-root-*.crt")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(pem); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	for _, args := range [][]string{
		{"install", "-m", "0644", f.Name(), LocalCAPath},
		{"update-ca-certificates"},
	} {
		cmd := exec.CommandContext(ctx, "sudo", args...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("sudo %s: %w", strings.Join(args, " "), err)
		}
	}
	return nil
}

func TrustWindows(ctx context.Context, pem []byte) error {
	winTemp, linuxTemp, err := windowsTemp(ctx)
	if err != nil {
		return err
	}

	const name = "lazydomain-root.crt"
	local := filepath.Join(linuxTemp, name)
	if err := os.WriteFile(local, pem, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", local, err)
	}
	defer os.Remove(local)

	script := fmt.Sprintf(`certutil.exe -user -addstore Root '%s' | Out-Null
if ($LASTEXITCODE -ne 0) { throw "certutil exited with code $LASTEXITCODE (was the dialog declined?)" }`,
		psQuote(winTemp+name))
	_, err = PowerShell(ctx, script)
	return err
}

func UntrustLocal(ctx context.Context) (removed bool, err error) {
	if _, err := os.Stat(LocalCAPath); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	for _, args := range [][]string{
		{"rm", "-f", LocalCAPath},
		{"update-ca-certificates", "--fresh"},
	} {
		cmd := exec.CommandContext(ctx, "sudo", args...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return false, fmt.Errorf("sudo %s: %w", strings.Join(args, " "), err)
		}
	}
	return true, nil
}

// Matched by subject, not thumbprint, so it still works after the proxy's CA
// volume is gone.
func UntrustWindows(ctx context.Context, caName string) (removed int, err error) {
	script := fmt.Sprintf(`$certs = @(Get-ChildItem Cert:\CurrentUser\Root | Where-Object { $_.Subject -like '*%s*' })
foreach ($c in $certs) {
  certutil.exe -user -delstore Root $c.Thumbprint | Out-Null
  if ($LASTEXITCODE -ne 0) { throw "certutil exited with code $LASTEXITCODE (was the dialog declined?)" }
}
$certs.Count`, psQuote(caName))
	out, err := PowerShell(ctx, script)
	if err != nil {
		return 0, err
	}
	if _, err := fmt.Sscan(out, &removed); err != nil {
		return 0, fmt.Errorf("unexpected output from certificate removal: %q", out)
	}
	return removed, nil
}

func wslpathU(ctx context.Context, winPath string) (string, error) {
	out, err := exec.CommandContext(ctx, "wslpath", "-u", winPath).Output()
	if err != nil {
		return "", fmt.Errorf("wslpath -u %q: %w", winPath, err)
	}
	return strings.TrimSpace(string(out)), nil
}
