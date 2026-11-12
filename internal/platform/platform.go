package platform

import (
	"context"
	"os"
	"strings"
)

type Kind int

const (
	Linux Kind = iota
	WSL
)

func (k Kind) String() string {
	if k == WSL {
		return "WSL2"
	}
	return "Linux"
}

type Info struct {
	Kind   Kind
	Distro string
}

func Detect() Info {
	if isWSL() {
		return Info{Kind: WSL, Distro: os.Getenv("WSL_DISTRO_NAME")}
	}
	return Info{Kind: Linux}
}

func isWSL() bool {
	for _, p := range []string{
		"/proc/sys/fs/binfmt_misc/WSLInterop",
		"/proc/sys/fs/binfmt_misc/WSLInterop-late",
	} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		return true
	}
	b, err := os.ReadFile("/proc/sys/kernel/osrelease")
	return err == nil && strings.Contains(strings.ToLower(string(b)), "microsoft")
}

func SystemdIsInit() bool {
	b, err := os.ReadFile("/proc/1/comm")
	return err == nil && strings.TrimSpace(string(b)) == "systemd"
}

func HostsPath(ctx context.Context, info Info) string {
	if info.Kind != WSL {
		return "/etc/hosts"
	}
	if p, err := wslpathU(ctx, `C:\Windows\System32\drivers\etc\hosts`); err == nil && p != "" {
		return p
	}
	return "/mnt/c/Windows/System32/drivers/etc/hosts"
}

const LocalCAPath = "/usr/local/share/ca-certificates/lazydomain-root.crt"
