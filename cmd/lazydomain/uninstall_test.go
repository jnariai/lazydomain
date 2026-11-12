package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fakeUninstall(t *testing.T, wsl bool) (*uninstaller, *[]string, *bytes.Buffer) {
	t.Helper()
	root := t.TempDir()
	configDir := filepath.Join(root, "config", "lazydomain")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "state.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "bin", "lazydomain")
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	calls := &[]string{}
	out := &bytes.Buffer{}
	u := &uninstaller{
		removeHosts:  func() (bool, error) { *calls = append(*calls, "hosts"); return true, nil },
		untrustLocal: func() (bool, error) { *calls = append(*calls, "trust-wsl"); return true, nil },
		purgeProxy:   func() error { *calls = append(*calls, "proxy"); return nil },
		configDir:    configDir,
		binary:       binary,
		out:          out,
	}
	if wsl {
		u.untrustWindows = func() (int, error) { *calls = append(*calls, "trust-windows"); return 1, nil }
	}
	return u, calls, out
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestUninstallRemovesEverythingInOrder(t *testing.T) {
	u, calls, out := fakeUninstall(t, true)
	if !u.run() {
		t.Fatalf("run failed:\n%s", out)
	}
	want := []string{"hosts", "trust-windows", "trust-wsl", "proxy"}
	if !reflect.DeepEqual(*calls, want) {
		t.Errorf("calls = %v, want %v", *calls, want)
	}
	if exists(u.configDir) {
		t.Error("config dir still exists")
	}
	if exists(u.binary) {
		t.Error("binary still exists")
	}
	if !strings.Contains(out.String(), "lazydomain is uninstalled") {
		t.Errorf("missing success message:\n%s", out)
	}
}

func TestUninstallSkipsWindowsStepOutsideWSL(t *testing.T) {
	u, calls, out := fakeUninstall(t, false)
	if !u.run() {
		t.Fatalf("run failed:\n%s", out)
	}
	want := []string{"hosts", "trust-wsl", "proxy"}
	if !reflect.DeepEqual(*calls, want) {
		t.Errorf("calls = %v, want %v", *calls, want)
	}
	if strings.Contains(out.String(), "certificate (Windows)") {
		t.Errorf("Windows step shown outside WSL:\n%s", out)
	}
}

func TestUninstallKeepsConfigAndBinaryWhenAStepFails(t *testing.T) {
	for _, failing := range []string{"hosts", "trust-windows", "trust-wsl", "proxy"} {
		t.Run(failing, func(t *testing.T) {
			u, calls, out := fakeUninstall(t, true)
			boom := errors.New("boom")
			switch failing {
			case "hosts":
				u.removeHosts = func() (bool, error) { *calls = append(*calls, "hosts"); return false, boom }
			case "trust-windows":
				u.untrustWindows = func() (int, error) { *calls = append(*calls, "trust-windows"); return 0, boom }
			case "trust-wsl":
				u.untrustLocal = func() (bool, error) { *calls = append(*calls, "trust-wsl"); return false, boom }
			case "proxy":
				u.purgeProxy = func() error { *calls = append(*calls, "proxy"); return boom }
			}

			if u.run() {
				t.Fatal("run reported success")
			}
			if want := []string{"hosts", "trust-windows", "trust-wsl", "proxy"}; !reflect.DeepEqual(*calls, want) {
				t.Errorf("calls = %v, want %v", *calls, want)
			}
			if !exists(u.configDir) || !exists(u.binary) {
				t.Error("config or binary removed although a step failed")
			}
			if !strings.Contains(out.String(), "✗") || !strings.Contains(out.String(), "run `lazydomain uninstall` again") {
				t.Errorf("failure not reported:\n%s", out)
			}
		})
	}
}

func TestUninstallReportsNothingFound(t *testing.T) {
	u, _, out := fakeUninstall(t, true)
	u.removeHosts = func() (bool, error) { return false, nil }
	u.untrustWindows = func() (int, error) { return 0, nil }
	u.untrustLocal = func() (bool, error) { return false, nil }
	if !u.run() {
		t.Fatalf("run failed:\n%s", out)
	}
	if n := strings.Count(out.String(), "none found"); n != 3 {
		t.Errorf("want 3 'none found', got %d:\n%s", n, out)
	}
}

func TestUninstallRefusesUnexpectedConfigDir(t *testing.T) {
	u, _, out := fakeUninstall(t, true)
	wrong := filepath.Join(filepath.Dir(u.configDir), "other")
	if err := os.MkdirAll(wrong, 0o755); err != nil {
		t.Fatal(err)
	}
	u.configDir = wrong
	if u.run() {
		t.Fatal("run reported success")
	}
	if !exists(wrong) {
		t.Error("a directory not named lazydomain was removed")
	}
	if !strings.Contains(out.String(), "unexpected config dir") {
		t.Errorf("missing guard message:\n%s", out)
	}
}

func TestUninstallBinaryPermissionHint(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can delete from read-only directories")
	}
	u, _, out := fakeUninstall(t, true)
	dir := filepath.Dir(u.binary)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	if u.run() {
		t.Fatal("run reported success")
	}
	if !strings.Contains(out.String(), "sudo rm "+u.binary) {
		t.Errorf("missing sudo hint:\n%s", out)
	}
}

func TestUninstallUnknownBinaryPath(t *testing.T) {
	u, _, out := fakeUninstall(t, true)
	u.binaryErr = errors.New("running from a temporary go build")
	if u.run() {
		t.Fatal("run reported success")
	}
	if !strings.Contains(out.String(), "temporary go build") {
		t.Errorf("missing reason:\n%s", out)
	}
}

func TestConfirmUninstall(t *testing.T) {
	cases := []struct {
		name        string
		yes, tty    bool
		input       string
		wantOK      bool
		wantErr     error
		wantPrompts bool
	}{
		{name: "--yes skips the prompt", yes: true, wantOK: true},
		{name: "--yes works without a terminal", yes: true, tty: false, wantOK: true},
		{name: "no terminal and no --yes refuses", tty: false, input: "yes\n", wantErr: errNeedsConfirmation},
		{name: "typing yes proceeds", tty: true, input: "yes\n", wantOK: true, wantPrompts: true},
		{name: "surrounding spaces are fine", tty: true, input: "  yes  \n", wantOK: true, wantPrompts: true},
		{name: "y is not enough", tty: true, input: "y\n", wantPrompts: true},
		{name: "YES is not enough", tty: true, input: "YES\n", wantPrompts: true},
		{name: "no aborts", tty: true, input: "no\n", wantPrompts: true},
		{name: "EOF aborts", tty: true, input: "", wantPrompts: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			ok, err := confirmUninstall(c.yes, c.tty, strings.NewReader(c.input), &out)
			if !errors.Is(err, c.wantErr) || (err == nil) != (c.wantErr == nil) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if ok != c.wantOK {
				t.Errorf("ok = %v, want %v", ok, c.wantOK)
			}
			if prompted := strings.Contains(out.String(), "Type 'yes'"); prompted != c.wantPrompts {
				t.Errorf("prompted = %v, want %v", prompted, c.wantPrompts)
			}
		})
	}
}
