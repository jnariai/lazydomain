package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// fakeReleases serves /latest, the archive and checksums.txt for v1.2.3.
func fakeReleases(t *testing.T, archive []byte, checksum string) *Updater {
	t.Helper()
	asset := "lazydomain_1.2.3_linux_amd64.tar.gz"
	if checksum == "" {
		sum := sha256.Sum256(archive)
		checksum = hex.EncodeToString(sum[:])
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/tag/v1.2.3", http.StatusFound)
	})
	mux.HandleFunc("/releases/download/v1.2.3/"+asset, func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	})
	mux.HandleFunc("/releases/download/v1.2.3/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "deadbeef  lazydomain_1.2.3_linux_arm64.tar.gz\n%s  %s\n", checksum, asset)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &Updater{ReleasesURL: srv.URL + "/releases", Client: srv.Client(), Arch: "amd64"}
}

func TestLatestTagAndDownload(t *testing.T) {
	u := fakeReleases(t, tarGz(t, map[string]string{"README.md": "hi", "lazydomain": "NEWBIN"}), "")
	ctx := context.Background()

	tag, err := u.LatestTag(ctx)
	if err != nil || tag != "v1.2.3" {
		t.Fatalf("LatestTag = %q, %v", tag, err)
	}
	bin, err := u.Download(ctx, tag)
	if err != nil {
		t.Fatal(err)
	}
	if string(bin) != "NEWBIN" {
		t.Errorf("binary = %q", bin)
	}
}

func TestDownloadRejectsChecksumMismatch(t *testing.T) {
	u := fakeReleases(t, tarGz(t, map[string]string{"lazydomain": "X"}), strings.Repeat("0", 64))
	_, err := u.Download(context.Background(), "v1.2.3")
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v", err)
	}
}

func TestDownloadMissingBinary(t *testing.T) {
	u := fakeReleases(t, tarGz(t, map[string]string{"README.md": "hi"}), "")
	if _, err := u.Download(context.Background(), "v1.2.3"); err == nil {
		t.Fatal("expected error for archive without binary")
	}
}

func TestReplace(t *testing.T) {
	target := filepath.Join(t.TempDir(), "lazydomain")
	if err := os.WriteFile(target, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Replace(target, []byte("NEW")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "NEW" {
		t.Errorf("content = %q", got)
	}
	if fi, _ := os.Stat(target); fi.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v", fi.Mode())
	}
	entries, _ := os.ReadDir(filepath.Dir(target))
	if len(entries) != 1 {
		t.Errorf("temp file left behind: %v", entries)
	}
}

func TestNormalizeTag(t *testing.T) {
	for in, want := range map[string]string{"0.2.0": "v0.2.0", "v0.2.0": "v0.2.0", "": ""} {
		if got := NormalizeTag(in); got != want {
			t.Errorf("NormalizeTag(%q) = %q, want %q", in, got, want)
		}
	}
}
