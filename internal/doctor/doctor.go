package doctor

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/jnariai/lazydomain/internal/docker"
	"github.com/jnariai/lazydomain/internal/hostsfile"
	"github.com/jnariai/lazydomain/internal/platform"
	"github.com/jnariai/lazydomain/internal/proxy"
)

type Status int

const (
	OK Status = iota
	Warn
	Fail
	Skip
)

func (s Status) symbol() string {
	switch s {
	case OK:
		return "✓"
	case Warn:
		return "!"
	case Fail:
		return "✗"
	default:
		return "-"
	}
}

type Result struct {
	Name   string
	Status Status
	Detail string
	Hint   string
}

type env struct {
	plat         platform.Info
	dockerOK     bool
	caddyRunning bool
	port443Open  bool
	rootPEM      []byte
	thumbprint   string
}

type check func(context.Context, *env) Result

func Run(ctx context.Context) []Result {
	e := &env{plat: platform.Detect()}

	checks := []check{
		checkPlatform, checkSystemd, checkDocker, checkCompose,
		checkCaddyContainer, checkCaddyAdmin, checkPort(80), checkPort(443),
	}
	if e.plat.Kind == platform.WSL {
		checks = append(checks, checkNetworkingMode, checkWindowsReach)
	}
	checks = append(checks, checkRootCA, checkLocalTrust)
	if e.plat.Kind == platform.WSL {
		checks = append(checks, checkWindowsTrust)
	}
	checks = append(checks, checkHosts)

	results := make([]Result, 0, len(checks))
	for _, c := range checks {
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		results = append(results, c(cctx, e))
		cancel()
	}
	return results
}

func Print(w io.Writer, results []Result) (failed bool) {
	for _, r := range results {
		fmt.Fprintf(w, "%s %-20s %s\n", r.Status.symbol(), r.Name, r.Detail)
		if r.Hint != "" && (r.Status == Warn || r.Status == Fail) {
			for _, line := range strings.Split(r.Hint, "\n") {
				fmt.Fprintf(w, "    %s\n", line)
			}
		}
		if r.Status == Fail {
			failed = true
		}
	}
	return failed
}

func checkPlatform(_ context.Context, e *env) Result {
	r := Result{Name: "Platform"}
	if e.plat.Kind == platform.WSL {
		r.Status, r.Detail = OK, fmt.Sprintf("WSL2 (%s)", e.plat.Distro)
		return r
	}
	r.Status, r.Detail = Warn, "native Linux"
	r.Hint = "Only WSL2 is supported in the MVP; native Linux DNS/trust setup comes later."
	return r
}

func checkSystemd(_ context.Context, e *env) Result {
	r := Result{Name: "systemd"}
	if platform.SystemdIsInit() {
		r.Status, r.Detail = OK, "PID 1 is systemd"
		return r
	}
	r.Status, r.Detail = Fail, "PID 1 is not systemd; docker and the proxy will not start on boot"
	if e.plat.Kind == platform.WSL {
		r.Hint = "Add to /etc/wsl.conf:\n  [boot]\n  systemd=true\nthen run `wsl --shutdown` from Windows."
	}
	return r
}

func checkDocker(ctx context.Context, e *env) Result {
	r := Result{Name: "Docker engine"}
	v, err := docker.ServerVersion(ctx)
	if err != nil {
		r.Status, r.Detail = Fail, err.Error()
		r.Hint = "Start it with `sudo systemctl enable --now docker` and make sure your user is in the docker group."
		return r
	}
	e.dockerOK = true
	r.Status, r.Detail = OK, "server "+v
	return r
}

func checkCompose(ctx context.Context, e *env) Result {
	r := Result{Name: "Docker compose"}
	if !e.dockerOK {
		r.Status, r.Detail = Skip, "docker unavailable"
		return r
	}
	v, err := docker.ComposeVersion(ctx)
	if err != nil {
		r.Status, r.Detail = Fail, err.Error()
		r.Hint = "Install the compose plugin: `sudo apt install docker-compose-plugin`."
		return r
	}
	r.Status, r.Detail = OK, v
	return r
}

func checkCaddyContainer(ctx context.Context, e *env) Result {
	r := Result{Name: "Proxy container"}
	if !e.dockerOK {
		r.Status, r.Detail = Skip, "docker unavailable"
		return r
	}
	exists, running, err := docker.ContainerState(ctx, proxy.ContainerName)
	switch {
	case err != nil:
		r.Status, r.Detail = Fail, err.Error()
	case !exists:
		r.Status, r.Detail, r.Hint = Warn, proxy.ContainerName+" not created", "Run `lazydomain proxy up`."
	case !running:
		r.Status, r.Detail, r.Hint = Fail, proxy.ContainerName+" exists but is stopped", "Run `lazydomain proxy up`, or check `docker logs "+proxy.ContainerName+"`."
	default:
		e.caddyRunning = true
		r.Status, r.Detail = OK, proxy.ContainerName+" running"
	}
	return r
}

func checkCaddyAdmin(ctx context.Context, e *env) Result {
	r := Result{Name: "Proxy admin API"}
	if !e.caddyRunning {
		r.Status, r.Detail = Skip, "proxy not running"
		return r
	}
	if _, err := proxy.NewClient().Config(ctx); err != nil {
		r.Status, r.Detail = Fail, err.Error()
		r.Hint = "Check `docker logs " + proxy.ContainerName + "`; another process may hold " + proxy.AdminAddr + "."
		return r
	}
	r.Status, r.Detail = OK, "reachable on "+proxy.AdminAddr
	return r
}

func checkPort(port int) check {
	return func(_ context.Context, e *env) Result {
		r := Result{Name: fmt.Sprintf("Port %d (WSL)", port)}
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
		open := err == nil
		if open {
			conn.Close()
		}
		if port == 443 {
			e.port443Open = open
		}
		switch {
		case e.caddyRunning && open:
			r.Status, r.Detail = OK, "served by the proxy"
		case e.caddyRunning:
			r.Status, r.Detail = OK, "not bound yet (proxy binds it once a domain is added)"
		case open:
			r.Status, r.Detail = Fail, "in use by another process; the proxy cannot bind it"
			r.Hint = fmt.Sprintf("Find it with `sudo ss -ltnp 'sport = :%d'`. A Sail app with APP_PORT=%d is a common cause.", port, port)
		default:
			r.Status, r.Detail = OK, "free"
		}
		return r
	}
}

func checkNetworkingMode(ctx context.Context, _ *env) Result {
	r := Result{Name: "WSL networking"}
	mode, err := platform.NetworkingMode(ctx)
	switch {
	case err != nil:
		r.Status, r.Detail, r.Hint = Warn, "could not detect", "Update WSL from Windows: `wsl --update`."
	case mode == "nat":
		r.Status, r.Detail = OK, "nat (Windows reaches WSL via localhost forwarding)"
	case mode == "mirrored":
		r.Status, r.Detail = OK, "mirrored (Windows and WSL share 127.0.0.1)"
	default:
		r.Status, r.Detail = Warn, mode+" (unknown mode; the Windows → :443 check below is what matters)"
	}
	return r
}

func checkWindowsReach(ctx context.Context, e *env) Result {
	r := Result{Name: "Windows → :443"}
	if !e.port443Open {
		r.Status, r.Detail = Skip, "nothing listening on 443 yet"
		return r
	}
	open, err := platform.WindowsTCPOpen(ctx, 443)
	switch {
	case err != nil:
		r.Status, r.Detail = Fail, err.Error()
	case open:
		r.Status, r.Detail = OK, "Windows reaches the proxy on 127.0.0.1:443"
	default:
		r.Status, r.Detail = Fail, "Windows cannot reach 127.0.0.1:443"
		r.Hint = "Check that %UserProfile%\\.wslconfig does not set localhostForwarding=false,\nand that nothing on Windows listens on 443 (`netstat -ano | findstr :443`)."
	}
	return r
}

func checkRootCA(ctx context.Context, e *env) Result {
	r := Result{Name: "Local root CA"}
	if !e.caddyRunning {
		r.Status, r.Detail = Skip, "proxy not running"
		return r
	}
	ca, err := proxy.LoadRootCA(ctx)
	if err != nil {
		r.Status, r.Detail, r.Hint = Warn, "not generated yet", "Run `lazydomain proxy up` to seed the proxy config."
		return r
	}
	e.rootPEM, e.thumbprint = ca.PEM, ca.Thumbprint
	r.Status = OK
	r.Detail = fmt.Sprintf("%q, thumbprint %s…, expires %s", ca.Cert.Subject.CommonName, ca.Thumbprint[:12], ca.Cert.NotAfter.Format("2006-01-02"))
	return r
}

const trustHint = "Run `lazydomain trust`."

func checkLocalTrust(_ context.Context, e *env) Result {
	r := Result{Name: "CA trusted (WSL)"}
	if e.rootPEM == nil {
		r.Status, r.Detail = Skip, "no root CA yet"
		return r
	}
	_, statErr := os.Stat(platform.LocalCAPath)
	ok, err := platform.LocalTrusted(e.rootPEM)
	switch {
	case err != nil:
		r.Status, r.Detail = Fail, err.Error()
	case ok:
		r.Status, r.Detail = OK, platform.LocalCAPath
	case statErr == nil:
		r.Status, r.Detail, r.Hint = Fail, "installed CA differs from the proxy's (volume recreated?)", trustHint
	default:
		r.Status, r.Detail, r.Hint = Warn, "not installed (curl/PHP in WSL will reject *.test)", trustHint
	}
	return r
}

func checkWindowsTrust(ctx context.Context, e *env) Result {
	r := Result{Name: "CA trusted (Windows)"}
	if e.thumbprint == "" {
		r.Status, r.Detail = Skip, "no root CA yet"
		return r
	}
	ok, err := platform.WindowsRootTrusted(ctx, e.thumbprint)
	switch {
	case err != nil:
		r.Status, r.Detail = Fail, err.Error()
	case ok:
		r.Status, r.Detail = OK, `Cert:\CurrentUser\Root`
	default:
		r.Status, r.Detail, r.Hint = Fail, "browsers on Windows will show certificate errors", trustHint
	}
	return r
}

func checkHosts(ctx context.Context, e *env) Result {
	path := platform.HostsPath(ctx, e.plat)
	r := Result{Name: "Hosts file"}
	content, err := os.ReadFile(path)
	if err != nil {
		r.Status, r.Detail = Fail, err.Error()
		return r
	}
	hosts, found, err := hostsfile.Managed(string(content))
	switch {
	case err != nil:
		r.Status, r.Detail, r.Hint = Fail, err.Error(), "Fix the lazydomain block by hand in "+path+"."
	case !found:
		r.Status, r.Detail = OK, "no managed block yet (no domains added)"
	default:
		r.Status, r.Detail = OK, fmt.Sprintf("%d managed host(s): %s", len(hosts), strings.Join(hosts, " "))
	}
	return r
}
