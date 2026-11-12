// Command lazydomain manages trusted https://<project>.test domains for
// Dockerized Laravel apps running in WSL2.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/charmbracelet/x/term"

	"github.com/jnariai/lazydomain/internal/doctor"
	"github.com/jnariai/lazydomain/internal/proxy"
	"github.com/jnariai/lazydomain/internal/tui"
)

// isTerminal reports whether f is an interactive terminal (a TTY, not just
// any character device such as /dev/null).
func isTerminal(f *os.File) bool { return term.IsTerminal(f.Fd()) }

var version = "dev" // overridden with -ldflags "-X main.version=..."

const usage = `lazydomain: trusted https://<project>.test domains for Laravel apps

Usage:
  lazydomain                       open the interactive TUI (same as: lazydomain tui)
  lazydomain list                 show Laravel projects and their domains
  lazydomain add [name|path]       serve a project at https://<name>.test (default: current dir)
  lazydomain remove [name|path]    stop serving a project's domain
  lazydomain doctor                check docker, proxy, ports, CA trust and hosts file
  lazydomain proxy up              start the shared Caddy proxy (idempotent)
  lazydomain proxy down            stop the proxy (keeps the CA and routes)
  lazydomain trust                 trust the proxy's root CA in Windows and WSL
  lazydomain uninstall [--yes]     remove hosts entries, certificates, proxy, config and the binary
  lazydomain version

add and remove accept --dry-run (-n) to print the plan without changing anything.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:]))
}

func run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		if !isTerminal(os.Stdout) {
			fmt.Fprint(os.Stderr, usage)
			return 2
		}
		args = []string{"tui"}
	}
	switch args[0] {
	case "tui":
		if err := tui.Run(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		return 0
	case "doctor":
		if doctor.Print(os.Stdout, doctor.Run(ctx)) {
			return 1
		}
		return 0
	case "proxy":
		return runProxy(ctx, args[1:])
	case "trust":
		return runTrust(ctx)
	case "list", "ls":
		return runList(ctx, args[1:])
	case "add":
		return runAdd(ctx, args[1:])
	case "remove", "rm":
		return runRemove(ctx, args[1:])
	case "uninstall":
		return runUninstall(ctx, args[1:])
	case "version", "--version":
		fmt.Println(version)
		return 0
	case "help", "-h", "--help":
		fmt.Print(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func runProxy(ctx context.Context, args []string) int {
	if len(args) != 1 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	var err error
	switch args[0] {
	case "up":
		err = proxy.Up(ctx)
	case "down":
		err = proxy.Down(ctx)
	default:
		fmt.Fprintf(os.Stderr, "unknown proxy command %q\n\n%s", args[0], usage)
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}
