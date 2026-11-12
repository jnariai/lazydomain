package reconcile

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/jnariai/lazydomain/internal/hostsfile"
	"github.com/jnariai/lazydomain/internal/laravel"
	"github.com/jnariai/lazydomain/internal/platform"
	"github.com/jnariai/lazydomain/internal/proxy"
	"github.com/jnariai/lazydomain/internal/state"
)

// Proxy first: if the hosts update fails (e.g. UAC declined) the proxy is
// rolled back, so state.json can stay untouched.
func Apply(ctx context.Context, plat platform.Info, prev, next *state.State) (hostsChanged bool, err error) {
	c := proxy.NewClient()
	cfg, err := proxy.BuildConfig(next.Routes())
	if err != nil {
		return false, err
	}
	if err := c.Load(ctx, cfg); err != nil {
		return false, fmt.Errorf("update proxy: %w", err)
	}

	hostsChanged, err = platform.ApplyHosts(ctx, plat, next.HostsEntries())
	if err != nil {
		if prevCfg, e := proxy.BuildConfig(prev.Routes()); e == nil {
			_ = c.Load(ctx, prevCfg)
		}
		return false, fmt.Errorf("update hosts file: %w", err)
	}
	return hostsChanged, nil
}

type SiteStatus struct {
	Site     state.Site
	Project  *laravel.Project
	InProxy  bool
	InHosts  bool
	AppUp    bool
	Problems []string
}

func (s SiteStatus) OK() bool { return len(s.Problems) == 0 }

type Observed struct {
	ProxyHosts map[string]bool
	ProxyErr   error
	HostsFile  map[string]bool
	HostsErr   error
}

func Observe(ctx context.Context, plat platform.Info) Observed {
	var o Observed
	o.ProxyHosts, o.ProxyErr = proxy.NewClient().Hosts(ctx)

	path := platform.HostsPath(ctx, plat)
	raw, err := os.ReadFile(path)
	if err != nil {
		o.HostsErr = err
		return o
	}
	hosts, _, err := hostsfile.Managed(string(raw))
	if err != nil {
		o.HostsErr = err
		return o
	}
	o.HostsFile = map[string]bool{}
	for _, h := range hosts {
		o.HostsFile[h] = true
	}
	return o
}

func Check(site state.Site, o Observed) SiteStatus {
	st := SiteStatus{Site: site}

	if p, err := laravel.Inspect(site.Dir); err == nil {
		st.Project = p
		if p.AppPort != site.AppPort {
			st.Problems = append(st.Problems, fmt.Sprintf("APP_PORT is now %d (re-run add)", p.AppPort))
		}
		if p.VitePort != site.VitePort {
			st.Problems = append(st.Problems, fmt.Sprintf("VITE_PORT is now %d (re-run add)", p.VitePort))
		}
	} else {
		st.Problems = append(st.Problems, "project folder missing or no longer Laravel")
	}

	switch {
	case o.ProxyErr != nil:
		st.Problems = append(st.Problems, "proxy unreachable")
	default:
		st.InProxy = o.ProxyHosts[site.Domain()] && o.ProxyHosts[site.ViteDomain()]
		if !st.InProxy {
			st.Problems = append(st.Problems, "proxy route missing")
		}
	}

	switch {
	case o.HostsErr != nil:
		st.Problems = append(st.Problems, "hosts file unreadable")
	default:
		st.InHosts = o.HostsFile[site.Domain()] && o.HostsFile[site.ViteDomain()]
		if !st.InHosts {
			st.Problems = append(st.Problems, "hosts entry missing")
		}
	}

	st.AppUp = portOpen(site.AppPort)
	return st
}

func portOpen(port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
