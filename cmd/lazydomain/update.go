package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/jnariai/lazydomain/internal/docker"
	"github.com/jnariai/lazydomain/internal/proxy"
	"github.com/jnariai/lazydomain/internal/update"
)

// runUpdate replaces this binary with a release (latest by default) and then
// runs the new binary's `proxy up`, so proxy config changes shipped in the
// release (routes format, certificate settings) take effect right away.
func runUpdate(ctx context.Context, args []string) int {
	if len(args) > 1 || (len(args) == 1 && strings.HasPrefix(args[0], "-")) {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	fail := func(err error) int {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	target, err := executablePath()
	if err != nil {
		return fail(err)
	}

	u := update.New()
	tag := ""
	if len(args) == 1 {
		tag = update.NormalizeTag(args[0])
	} else if tag, err = u.LatestTag(ctx); err != nil {
		return fail(err)
	}

	current := update.NormalizeTag(version)
	if current == tag {
		fmt.Printf("lazydomain %s is already installed.\n", tag)
		return 0
	}

	fmt.Printf("Updating lazydomain %s → %s…\n", version, tag)
	bin, err := u.Download(ctx, tag)
	if err != nil {
		return fail(err)
	}
	if err := update.Replace(target, bin); err != nil {
		return fail(fmt.Errorf("%w\nre-run the installer with LAZYDOMAIN_INSTALL_DIR set to a directory you can write, or use sudo", err))
	}
	fmt.Printf("✓ Installed %s to %s\n", tag, target)

	// Only refresh a proxy that is already running; never start one the user
	// stopped or has not set up yet.
	_, running, err := docker.ContainerState(ctx, proxy.ContainerName)
	if err != nil || !running {
		return 0
	}
	fmt.Println("Applying the new version to the running proxy…")
	cmd := exec.CommandContext(ctx, target, "proxy", "up")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fail(fmt.Errorf("lazydomain was updated, but `lazydomain proxy up` failed: %w", err))
	}
	return 0
}
