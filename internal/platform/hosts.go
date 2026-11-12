package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jnariai/lazydomain/internal/hostsfile"
)

func ApplyHosts(ctx context.Context, info Info, entries []hostsfile.Entry) (changed bool, err error) {
	path := HostsPath(ctx, info)
	current, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	next, err := hostsfile.Replace(string(current), entries)
	if err != nil {
		return false, err
	}
	if next == string(current) {
		return false, nil
	}

	if info.Kind == WSL {
		err = writeWindowsHosts(ctx, current, []byte(next))
	} else {
		err = writeLinuxHosts(ctx, []byte(next))
	}
	if err != nil {
		return false, err
	}

	written, err := os.ReadFile(path)
	if err != nil {
		return true, fmt.Errorf("verify %s: %w", path, err)
	}
	if string(written) != next {
		return true, fmt.Errorf("%s does not contain the expected content after writing (antivirus or another tool may have reverted it)", path)
	}
	return true, nil
}

// The elevated script re-checks the SHA-256 of what we read, so a concurrent
// edit of the hosts file is never overwritten.
func writeWindowsHosts(ctx context.Context, original, next []byte) error {
	winTemp, linuxTemp, err := windowsTemp(ctx)
	if err != nil {
		return err
	}

	stagedName, scriptName := "lazydomain-hosts.txt", "lazydomain-hosts.ps1"
	stagedWin, scriptWin := winTemp+stagedName, winTemp+scriptName

	sum := sha256.Sum256(original)
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$hosts = Join-Path $env:SystemRoot 'System32\drivers\etc\hosts'
if ((Get-FileHash -Algorithm SHA256 -LiteralPath $hosts).Hash -ne '%s') { exit 3 }
Copy-Item -LiteralPath '%s' -Destination $hosts -Force
ipconfig /flushdns | Out-Null
exit 0
`, strings.ToUpper(hex.EncodeToString(sum[:])), psQuote(stagedWin))

	for name, content := range map[string][]byte{stagedName: next, scriptName: []byte(script)} {
		p := filepath.Join(linuxTemp, name)
		if err := os.WriteFile(p, content, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", p, err)
		}
		defer os.Remove(p)
	}

	out, err := PowerShell(ctx, fmt.Sprintf(
		`$p = Start-Process -FilePath powershell.exe -Verb RunAs -Wait -PassThru -WindowStyle Hidden -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','"%s"'
$p.ExitCode`, psQuote(scriptWin)))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "canceled by the user") {
			return fmt.Errorf("the Windows UAC prompt was declined; the hosts file was not changed")
		}
		return err
	}
	switch strings.TrimSpace(out) {
	case "0":
		return nil
	case "3":
		return fmt.Errorf("the Windows hosts file changed while lazydomain was updating it; nothing was written, run the command again")
	default:
		return fmt.Errorf("elevated hosts update failed (exit code %s)", out)
	}
}

func writeLinuxHosts(ctx context.Context, next []byte) error {
	f, err := os.CreateTemp("", "lazydomain-hosts-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(next); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// cp (not install/mv) keeps the inode, owner and mode of /etc/hosts.
	cmd := exec.CommandContext(ctx, "sudo", "cp", f.Name(), "/etc/hosts")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sudo cp to /etc/hosts: %w", err)
	}
	return nil
}

func windowsTemp(ctx context.Context) (win, linux string, err error) {
	win, err = PowerShell(ctx, "[IO.Path]::GetTempPath()")
	if err != nil {
		return "", "", err
	}
	win = strings.TrimRight(win, `\`) + `\`
	linux, err = wslpathU(ctx, win)
	return win, linux, err
}

func psQuote(s string) string { return strings.ReplaceAll(s, "'", "''") }
