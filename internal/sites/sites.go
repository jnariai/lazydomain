package sites

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jnariai/lazydomain/internal/laravel"
	"github.com/jnariai/lazydomain/internal/names"
	"github.com/jnariai/lazydomain/internal/platform"
	"github.com/jnariai/lazydomain/internal/proxy"
	"github.com/jnariai/lazydomain/internal/reconcile"
	"github.com/jnariai/lazydomain/internal/state"
)

type AddPlan struct {
	Project  *laravel.Project
	Site     state.Site
	Warnings []string
	Next     *state.State
}

func PlanAdd(st *state.State, dir string) (*AddPlan, error) {
	p, err := laravel.Inspect(dir)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	if !p.Supported() {
		v := p.Version
		if v == "" {
			v = "unknown (run composer install)"
		}
		return nil, fmt.Errorf("%s uses Laravel %s; lazydomain needs Laravel %d+", p.Name(), v, laravel.MinMajor)
	}
	if p.EnvErr != nil {
		return nil, fmt.Errorf("read %s/.env: %w", dir, p.EnvErr)
	}
	label, err := names.Normalize(p.Name())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p.Name(), err)
	}

	site := state.Site{Label: label, Dir: dir, AppPort: p.AppPort, VitePort: p.VitePort, AddedAt: time.Now().UTC()}
	if other, ok := st.ByLabel(label); ok && other.Dir != dir {
		return nil, fmt.Errorf("%s is already used by %s", site.Domain(), other.Dir)
	}
	warnings, err := checkPorts(st, site)
	if err != nil {
		return nil, err
	}

	next := st.Clone()
	next.Upsert(site)
	return &AddPlan{Project: p, Site: site, Warnings: warnings, Next: next}, nil
}

func PlanRemove(st *state.State, site state.Site) *state.State {
	next := st.Clone()
	next.Remove(site.Label)
	return next
}

func Apply(ctx context.Context, plat platform.Info, prev, next *state.State) error {
	if _, err := proxy.NewClient().Config(ctx); err != nil {
		return fmt.Errorf("proxy is not reachable (%v); run `lazydomain proxy up`", err)
	}
	if _, err := reconcile.Apply(ctx, plat, prev, next); err != nil {
		return err
	}
	if err := next.Save(); err != nil {
		return fmt.Errorf("proxy and hosts were updated but saving state failed: %w (run add again)", err)
	}
	return nil
}

func checkPorts(st *state.State, site state.Site) (warnings []string, err error) {
	envPath := filepath.Join(site.Dir, ".env")
	for _, pp := range []struct {
		key  string
		port int
	}{{"APP_PORT", site.AppPort}, {"VITE_PORT", site.VitePort}} {
		if pp.port == 80 || pp.port == 443 {
			return nil, fmt.Errorf("%s is %d, which the proxy needs. Set %s=%d in %s and restart the app (sail down && sail up -d)",
				pp.key, pp.port, pp.key, SuggestPort(st), envPath)
		}
	}
	for _, other := range st.Sites {
		if other.Dir == site.Dir {
			continue
		}
		if other.AppPort == site.AppPort || other.VitePort == site.AppPort {
			return nil, fmt.Errorf("APP_PORT %d is already routed for %s. Set APP_PORT=%d in %s and restart the app",
				site.AppPort, other.Domain(), SuggestPort(st), envPath)
		}
		if other.VitePort == site.VitePort {
			warnings = append(warnings, fmt.Sprintf(
				"VITE_PORT %d is shared with %s: only one of them can run the Vite dev server at a time (set VITE_PORT in %s to avoid this)",
				site.VitePort, other.Domain(), envPath))
		}
	}
	return warnings, nil
}

func SuggestPort(st *state.State) int {
	used := map[int]bool{}
	for _, s := range st.Sites {
		used[s.AppPort], used[s.VitePort] = true, true
	}
	for port := 8080; port < 9000; port++ {
		if !used[port] && !Listening(port) {
			return port
		}
	}
	return 8080
}

func Listening(port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
