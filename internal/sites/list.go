package sites

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/jnariai/lazydomain/internal/laravel"
	"github.com/jnariai/lazydomain/internal/platform"
	"github.com/jnariai/lazydomain/internal/reconcile"
	"github.com/jnariai/lazydomain/internal/state"
)

type Kind int

const (
	Managed Kind = iota
	Available
	Unsupported
)

type Entry struct {
	Name    string
	Kind    Kind
	Dir     string
	AppPort int
	AppUp   bool
	Project *laravel.Project

	Site   *state.Site
	Status *reconcile.SiteStatus

	Plan    *AddPlan
	PlanErr error
	Reason  string
}

func (e Entry) Domain() string {
	switch {
	case e.Site != nil:
		return e.Site.Domain()
	case e.Plan != nil:
		return e.Plan.Site.Domain()
	}
	return ""
}

func (e Entry) StatusText() string {
	switch e.Kind {
	case Managed:
		if e.Status.OK() {
			return "✓ active"
		}
		return "! " + strings.Join(e.Status.Problems, ", ")
	case Available:
		if e.PlanErr != nil {
			return "available (needs changes)"
		}
		return "available"
	default:
		return e.Reason
	}
}

type Snapshot struct {
	Root     string
	Entries  []Entry
	ProxyErr error
	ScanErr  error
	State    *state.State
}

func Collect(ctx context.Context, plat platform.Info, st *state.State) Snapshot {
	snap := Snapshot{Root: st.Root(), State: st}
	projects, err := laravel.Discover(st.Root())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		snap.ScanErr = err
	}
	obs := reconcile.Observe(ctx, plat)
	snap.ProxyErr = obs.ProxyErr

	managed := map[string]bool{}
	for i := range st.Sites {
		site := st.Sites[i]
		managed[site.Dir] = true
		status := reconcile.Check(site, obs)
		snap.Entries = append(snap.Entries, Entry{
			Name: site.Label, Kind: Managed, Dir: site.Dir,
			AppPort: site.AppPort, AppUp: status.AppUp, Project: status.Project,
			Site: &site, Status: &status,
		})
	}
	for _, p := range projects {
		if managed[p.Dir] {
			continue
		}
		e := Entry{Name: p.Name(), Dir: p.Dir, AppPort: p.AppPort, AppUp: Listening(p.AppPort), Project: p}
		switch {
		case p.Supported():
			e.Kind = Available
			e.Plan, e.PlanErr = PlanAdd(st, p.Dir)
		case p.Major == 0:
			e.Kind, e.Reason = Unsupported, "unsupported (version unknown)"
		default:
			e.Kind, e.Reason = Unsupported, fmt.Sprintf("unsupported (Laravel %d)", p.Major)
		}
		snap.Entries = append(snap.Entries, e)
	}
	sort.SliceStable(snap.Entries, func(i, j int) bool { return snap.Entries[i].Name < snap.Entries[j].Name })
	return snap
}
