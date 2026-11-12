package laravel

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jnariai/lazydomain/internal/envfile"
)

const MinMajor = 11

const (
	defaultAppPort  = 80
	defaultVitePort = 5173
)

var ErrNotLaravel = errors.New("not a Laravel project (needs an artisan file and laravel/framework in composer.json)")

type Project struct {
	Dir      string
	Version  string
	Major    int
	Env      map[string]string
	EnvErr   error
	AppPort  int
	VitePort int
}

func (p *Project) Name() string { return filepath.Base(p.Dir) }

func (p *Project) Supported() bool { return p.Major >= MinMajor }

func Inspect(dir string) (*Project, error) {
	if _, err := os.Stat(filepath.Join(dir, "artisan")); err != nil {
		return nil, ErrNotLaravel
	}
	raw, err := os.ReadFile(filepath.Join(dir, "composer.json"))
	if err != nil {
		return nil, ErrNotLaravel
	}
	var composer struct {
		Require map[string]string `json:"require"`
	}
	if err := json.Unmarshal(raw, &composer); err != nil {
		return nil, ErrNotLaravel
	}
	if _, ok := composer.Require["laravel/framework"]; !ok {
		return nil, ErrNotLaravel
	}

	p := &Project{Dir: dir}
	p.Version = lockedVersion(filepath.Join(dir, "composer.lock"))
	p.Major = ParseMajor(p.Version)

	env, err := envfile.Load(filepath.Join(dir, ".env"))
	switch {
	case err == nil:
		p.Env = env
	case errors.Is(err, os.ErrNotExist):
		p.Env = map[string]string{}
	default:
		p.Env, p.EnvErr = map[string]string{}, err
	}
	p.AppPort = intOr(p.Env["APP_PORT"], defaultAppPort)
	p.VitePort = intOr(p.Env["VITE_PORT"], defaultVitePort)
	return p, nil
}

func Discover(root string) ([]*Project, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var projects []*Project
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if p, err := Inspect(filepath.Join(root, e.Name())); err == nil {
			projects = append(projects, p)
		}
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].Name() < projects[j].Name() })
	return projects, nil
}

func ParseMajor(version string) int {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	end := 0
	for end < len(v) && v[end] >= '0' && v[end] <= '9' {
		end++
	}
	n, _ := strconv.Atoi(v[:end])
	return n
}

func lockedVersion(lockPath string) string {
	raw, err := os.ReadFile(lockPath)
	if err != nil {
		return ""
	}
	var lock struct {
		Packages []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"packages"`
	}
	if json.Unmarshal(raw, &lock) != nil {
		return ""
	}
	for _, pkg := range lock.Packages {
		if pkg.Name == "laravel/framework" {
			return strings.TrimPrefix(pkg.Version, "v")
		}
	}
	return ""
}

func intOr(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n > 0 {
		return n
	}
	return def
}
