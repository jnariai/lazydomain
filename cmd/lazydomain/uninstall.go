package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/jnariai/lazydomain/internal/docker"
	"github.com/jnariai/lazydomain/internal/platform"
	"github.com/jnariai/lazydomain/internal/proxy"
	"github.com/jnariai/lazydomain/internal/state"
)

type uninstaller struct {
	removeHosts    func() (changed bool, err error)
	untrustWindows func() (removed int, err error)
	untrustLocal   func() (removed bool, err error)
	purgeProxy     func() error

	configDir string
	binary    string
	binaryErr error

	out io.Writer
}

func newUninstaller(ctx context.Context, plat platform.Info) *uninstaller {
	u := &uninstaller{
		removeHosts:  func() (bool, error) { return platform.ApplyHosts(ctx, plat, nil) },
		untrustLocal: func() (bool, error) { return platform.UntrustLocal(ctx) },
		purgeProxy: func() error {
			if _, err := docker.ServerVersion(ctx); err != nil {
				return fmt.Errorf("docker unreachable (%v)", err)
			}
			return proxy.Purge(ctx)
		},
		out: os.Stdout,
	}
	if plat.Kind == platform.WSL {
		u.untrustWindows = func() (int, error) { return platform.UntrustWindows(ctx, proxy.CAName) }
	}
	if p, err := state.DefaultPath(); err == nil {
		u.configDir = filepath.Dir(p)
	}
	u.binary, u.binaryErr = executablePath()
	return u
}

func (u *uninstaller) run() bool {
	failed := false
	step := func(name string, fn func() (string, error)) {
		detail, err := fn()
		switch {
		case err != nil:
			failed = true
			fmt.Fprintf(u.out, "✗ %s: %v\n", name, err)
		case detail != "":
			fmt.Fprintf(u.out, "✓ %s: %s\n", name, detail)
		default:
			fmt.Fprintf(u.out, "✓ %s\n", name)
		}
	}

	step("hosts entries", func() (string, error) {
		changed, err := u.removeHosts()
		if err != nil {
			return "", err
		}
		if !changed {
			return "none found", nil
		}
		return "removed", nil
	})
	if u.untrustWindows != nil {
		step("certificate (Windows)", func() (string, error) {
			n, err := u.untrustWindows()
			if err != nil {
				return "", err
			}
			if n == 0 {
				return "none found", nil
			}
			return fmt.Sprintf("removed %d", n), nil
		})
	}
	step("certificate (WSL)", func() (string, error) {
		removed, err := u.untrustLocal()
		if err != nil {
			return "", err
		}
		if !removed {
			return "none found", nil
		}
		return "removed " + platform.LocalCAPath, nil
	})
	step("proxy container", func() (string, error) {
		if err := u.purgeProxy(); err != nil {
			return "", err
		}
		return "container and volumes removed", nil
	})

	if failed {
		fmt.Fprintln(u.out, "\nSome steps failed, so the config and the binary were kept. Fix the errors above and run `lazydomain uninstall` again.")
		return false
	}

	step("config", func() (string, error) {
		if u.configDir == "" || filepath.Base(u.configDir) != "lazydomain" {
			return "", fmt.Errorf("unexpected config dir %q, not removing it", u.configDir)
		}
		if err := os.RemoveAll(u.configDir); err != nil {
			return "", err
		}
		return "removed " + tildify(u.configDir), nil
	})
	step("binary", func() (string, error) {
		if u.binaryErr != nil {
			return "", u.binaryErr
		}
		err := os.Remove(u.binary)
		if errors.Is(err, fs.ErrPermission) {
			return "", fmt.Errorf("no permission; remove it with: sudo rm %s", u.binary)
		}
		if err != nil {
			return "", err
		}
		return "removed " + tildify(u.binary), nil
	})

	if failed {
		return false
	}
	fmt.Fprintln(u.out, "\nlazydomain is uninstalled.")
	return true
}

var errNeedsConfirmation = errors.New("refusing to uninstall without confirmation; re-run with --yes")

func confirmUninstall(yes, interactive bool, in io.Reader, out io.Writer) (bool, error) {
	if yes {
		return true, nil
	}
	if !interactive {
		return false, errNeedsConfirmation
	}
	fmt.Fprint(out, "Type 'yes' to continue: ")
	answer, _ := bufio.NewReader(in).ReadString('\n')
	return strings.TrimSpace(answer) == "yes", nil
}

func runUninstall(ctx context.Context, args []string) int {
	yes := false
	for _, a := range args {
		switch a {
		case "--yes", "-y":
			yes = true
		default:
			fmt.Fprintf(os.Stderr, "unknown argument %q\n\n%s", a, usage)
			return 2
		}
	}

	plat := platform.Detect()
	u := newUninstaller(ctx, plat)

	fmt.Println("This removes lazydomain from this machine:")
	if plat.Kind == platform.WSL {
		fmt.Println("  • every *.test entry lazydomain added to the Windows hosts file (UAC prompt)")
		fmt.Println("  • its root certificate from Windows (confirmation dialog) and from WSL (sudo)")
	} else {
		fmt.Println("  • every *.test entry lazydomain added to /etc/hosts (sudo)")
		fmt.Println("  • its root certificate from the system trust store (sudo)")
	}
	fmt.Println("  • the proxy container and its volumes (" + proxy.ContainerName + ")")
	if u.configDir != "" {
		fmt.Println("  • " + tildify(u.configDir))
	}
	if u.binaryErr == nil {
		fmt.Println("  • the binary " + tildify(u.binary))
	}
	fmt.Println("Your projects are not modified; revert APP_URL and the Vite server block yourself if you want.")
	fmt.Println()

	ok, err := confirmUninstall(yes, isTerminal(os.Stdin), os.Stdin, os.Stdout)
	if err != nil {
		return fail("%v", err)
	}
	if !ok {
		fmt.Println("Aborted. Nothing was changed.")
		return 1
	}
	if !yes {
		fmt.Println()
	}

	if !u.run() {
		return 1
	}
	return 0
}

func executablePath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	if p, err = filepath.EvalSymlinks(p); err != nil {
		return "", err
	}
	if strings.Contains(p, string(filepath.Separator)+"go-build") {
		return "", fmt.Errorf("running from a temporary go build (%s); nothing to remove", p)
	}
	return p, nil
}
