package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jnariai/lazydomain/internal/platform"
	"github.com/jnariai/lazydomain/internal/proxy"
)

func runTrust(ctx context.Context) int {
	ca, err := proxy.LoadRootCA(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		fmt.Fprintln(os.Stderr, "Is the proxy running? Try `lazydomain proxy up`.")
		return 1
	}
	fmt.Printf("Root CA: %s\n  thumbprint %s\n\n", ca.Cert.Subject.CommonName, ca.Thumbprint)

	failed := false

	if platform.Detect().Kind == platform.WSL {
		ok, err := platform.WindowsRootTrusted(ctx, ca.Thumbprint)
		switch {
		case err != nil:
			fmt.Fprintln(os.Stderr, "✗ Windows:", err)
			failed = true
		case ok:
			fmt.Println("✓ Windows: already trusted")
		default:
			fmt.Println("→ Windows: adding to Cert:\\CurrentUser\\Root. Click \"Yes\" in the Windows security dialog…")
			if err := platform.TrustWindows(ctx, ca.PEM); err != nil {
				fmt.Fprintln(os.Stderr, "✗ Windows:", err)
				failed = true
			} else {
				fmt.Println("✓ Windows: trusted (Chrome, Edge and Firefox use this store; restart open browsers)")
			}
		}
	}

	ok, err := platform.LocalTrusted(ca.PEM)
	switch {
	case err != nil:
		fmt.Fprintln(os.Stderr, "✗ WSL:", err)
		failed = true
	case ok:
		fmt.Println("✓ WSL: already trusted")
	default:
		fmt.Println("→ WSL: installing into the system trust store (sudo)…")
		if err := platform.TrustLocal(ctx, ca.PEM); err != nil {
			fmt.Fprintln(os.Stderr, "✗ WSL:", err)
			failed = true
		} else {
			fmt.Println("✓ WSL: trusted")
		}
	}

	if failed {
		return 1
	}
	return 0
}
