// Package update replaces the running lazydomain binary with a published
// GitHub release, verified against the release's checksums.txt. It follows
// the same asset naming and verification as install.sh.
package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// Must match the repo in install.sh and .goreleaser.yaml.
const DefaultReleasesURL = "https://github.com/jnariai/lazydomain/releases"

// maxDownload bounds every response body; the archive is a few MB.
const maxDownload = 100 << 20

type Updater struct {
	ReleasesURL string
	Client      *http.Client
	Arch        string // GOARCH of the asset to download
}

func New() *Updater {
	return &Updater{ReleasesURL: DefaultReleasesURL, Client: http.DefaultClient, Arch: runtime.GOARCH}
}

// NormalizeTag turns "0.2.0" or "v0.2.0" into "v0.2.0".
func NormalizeTag(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

// LatestTag reads the tag from the redirect of /releases/latest, like
// install.sh, which avoids the rate-limited GitHub API.
func (u *Updater) LatestTag(ctx context.Context) (string, error) {
	client := *u.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u.ReleasesURL+"/latest", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not reach GitHub to find the latest release: %w", err)
	}
	resp.Body.Close()

	loc := resp.Header.Get("Location")
	if resp.StatusCode < 300 || resp.StatusCode >= 400 || loc == "" {
		return "", fmt.Errorf("no published release found (%s/latest returned %s)", u.ReleasesURL, resp.Status)
	}
	tag := path.Base(loc)
	if !strings.HasPrefix(tag, "v") {
		return "", fmt.Errorf("unexpected latest release URL %q", loc)
	}
	return tag, nil
}

func (u *Updater) assetName(tag string) string {
	return fmt.Sprintf("lazydomain_%s_linux_%s.tar.gz", strings.TrimPrefix(tag, "v"), u.Arch)
}

// Download fetches the release archive for tag, verifies its checksum and
// returns the lazydomain binary inside it.
func (u *Updater) Download(ctx context.Context, tag string) ([]byte, error) {
	asset := u.assetName(tag)
	base := u.ReleasesURL + "/download/" + tag + "/"

	archive, err := u.get(ctx, base+asset)
	if err != nil {
		return nil, fmt.Errorf("download %s (does release %s exist?): %w", asset, tag, err)
	}
	sums, err := u.get(ctx, base+"checksums.txt")
	if err != nil {
		return nil, fmt.Errorf("download checksums.txt for %s: %w", tag, err)
	}

	want, err := checksumFor(sums, asset)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); got != want {
		return nil, fmt.Errorf("checksum mismatch for %s (expected %s, got %s)", asset, want, got)
	}
	return extractBinary(archive)
}

func (u *Updater) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxDownload {
		return nil, fmt.Errorf("GET %s: response larger than %d bytes", url, maxDownload)
	}
	return body, nil
}

func checksumFor(sums []byte, asset string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && f[1] == asset {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("%s is not listed in checksums.txt", asset)
}

func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("read archive: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("archive does not contain the lazydomain binary")
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		if h.Typeflag == tar.TypeReg && path.Clean(h.Name) == "lazydomain" {
			return io.ReadAll(io.LimitReader(tr, maxDownload))
		}
	}
}

// Replace atomically swaps the file at target for bin: it writes a temp file
// in the same directory and renames it over target. The running process keeps
// its old inode, so replacing the binary that is executing is safe.
func Replace(target string, bin []byte) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".lazydomain-update-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename

	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), target)
}
