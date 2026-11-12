package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/jnariai/lazydomain/internal/hostsfile"
	"github.com/jnariai/lazydomain/internal/names"
	"github.com/jnariai/lazydomain/internal/platform"
	"github.com/jnariai/lazydomain/internal/sites"
	"github.com/jnariai/lazydomain/internal/state"
)

func splitArgs(args []string) (dryRun bool, pos []string, err error) {
	for _, a := range args {
		switch {
		case a == "--dry-run" || a == "-n":
			dryRun = true
		case strings.HasPrefix(a, "-"):
			return false, nil, fmt.Errorf("unknown flag %q", a)
		default:
			pos = append(pos, a)
		}
	}
	return dryRun, pos, nil
}

func fail(format string, a ...any) int {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", a...)
	return 1
}

func resolveDir(st *state.State, pos []string) (string, error) {
	if len(pos) == 0 {
		return os.Getwd()
	}
	arg := pos[0]
	if filepath.IsAbs(arg) || strings.HasPrefix(arg, ".") || strings.Contains(arg, "/") {
		return filepath.Abs(arg)
	}
	return filepath.Join(st.Root(), arg), nil
}

func applyMessage(plat platform.Info) string {
	if plat.Kind == platform.WSL {
		return "→ Updating the proxy and the Windows hosts file. Accept the UAC prompt if Windows shows one…"
	}
	return "→ Updating the proxy and /etc/hosts (sudo)…"
}

func runAdd(ctx context.Context, args []string) int {
	dryRun, pos, err := splitArgs(args)
	if err != nil || len(pos) > 1 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	st, err := state.Load()
	if err != nil {
		return fail("%v", err)
	}
	dir, err := resolveDir(st, pos)
	if err != nil {
		return fail("%v", err)
	}
	plan, err := sites.PlanAdd(st, dir)
	if err != nil {
		return fail("%v", err)
	}
	for _, w := range plan.Warnings {
		fmt.Println("! " + w)
	}

	if dryRun {
		printPlan(plan.Next)
		printNextSteps(plan.Project, plan.Site)
		return 0
	}

	plat := platform.Detect()
	fmt.Println(applyMessage(plat))
	if err := sites.Apply(ctx, plat, st, plan.Next); err != nil {
		return fail("%v", err)
	}
	fmt.Printf("✓ https://%s → 127.0.0.1:%d\n", plan.Site.Domain(), plan.Site.AppPort)
	fmt.Printf("✓ https://%s → 127.0.0.1:%d (Vite)\n", plan.Site.ViteDomain(), plan.Site.VitePort)
	printNextSteps(plan.Project, plan.Site)
	return 0
}

func printPlan(next *state.State) {
	fmt.Println("Dry run: nothing was changed.")
	fmt.Println("\nProxy routes:")
	for _, r := range next.Routes() {
		fmt.Printf("  https://%-28s → %s\n", r.Host, r.Upstream)
	}
	fmt.Println("\nHosts file block:")
	block, _ := hostsfile.Replace("", next.HostsEntries())
	if block == "" {
		fmt.Println("  (removed)")
	}
	for _, line := range strings.Split(strings.TrimRight(block, "\n"), "\n") {
		if line != "" {
			fmt.Println("  " + line)
		}
	}
	fmt.Println()
}

func runRemove(ctx context.Context, args []string) int {
	dryRun, pos, err := splitArgs(args)
	if err != nil || len(pos) > 1 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	st, err := state.Load()
	if err != nil {
		return fail("%v", err)
	}
	site, ok := findSite(st, pos)
	if !ok {
		what := "the current directory"
		if len(pos) == 1 {
			what = strconv.Quote(pos[0])
		}
		return fail("no managed site for %s; see `lazydomain list`", what)
	}
	next := sites.PlanRemove(st, site)

	if dryRun {
		printPlan(next)
		return 0
	}
	plat := platform.Detect()
	fmt.Println(applyMessage(plat))
	if err := sites.Apply(ctx, plat, st, next); err != nil {
		return fail("%v", err)
	}
	fmt.Printf("✓ removed https://%s and https://%s\n", site.Domain(), site.ViteDomain())
	fmt.Printf("  If APP_URL in %s/.env points to https://%s, change it back.\n", tildify(site.Dir), site.Domain())
	return 0
}

func findSite(st *state.State, pos []string) (state.Site, bool) {
	if len(pos) == 0 {
		if wd, err := os.Getwd(); err == nil {
			return st.ByDir(wd)
		}
		return state.Site{}, false
	}
	arg := pos[0]
	if s, ok := st.ByLabel(strings.TrimSuffix(arg, "."+names.TLD)); ok {
		return s, true
	}
	if label, err := names.Normalize(arg); err == nil {
		if s, ok := st.ByLabel(label); ok {
			return s, true
		}
	}
	if dir, err := resolveDir(st, pos); err == nil {
		return st.ByDir(dir)
	}
	return state.Site{}, false
}

func runList(ctx context.Context, args []string) int {
	if len(args) != 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	st, err := state.Load()
	if err != nil {
		return fail("%v", err)
	}
	snap := sites.Collect(ctx, platform.Detect(), st)

	if len(snap.Entries) == 0 {
		fmt.Printf("No Laravel projects found in %s.\n", tildify(snap.Root))
	} else {
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tDOMAIN\tSTATUS\tAPP\tLARAVEL\tPATH")
		drift := false
		for _, e := range snap.Entries {
			domain := "-"
			if e.Kind == sites.Managed {
				domain = "https://" + e.Domain()
				drift = drift || !e.Status.OK()
			}
			app := fmt.Sprintf(":%d", e.AppPort)
			if e.Kind == sites.Managed {
				app = map[bool]string{true: "up ", false: "down "}[e.AppUp] + app
			}
			version := "-"
			if e.Project != nil && e.Project.Version != "" {
				version = e.Project.Version
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", e.Name, domain, e.StatusText(), app, version, tildify(e.Dir))
		}
		tw.Flush()
		if drift {
			fmt.Println("\nRun `lazydomain add <name>` to re-apply a site that drifted.")
		}
	}
	if snap.ScanErr != nil {
		fmt.Fprintf(os.Stderr, "\n! could not scan %s: %v\n", snap.Root, snap.ScanErr)
	}
	if snap.ProxyErr != nil {
		fmt.Println("\n! Proxy unreachable. Run `lazydomain proxy up`.")
	}
	return 0
}

func tildify(p string) string {
	if home, err := os.UserHomeDir(); err == nil && (p == home || strings.HasPrefix(p, home+"/")) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
