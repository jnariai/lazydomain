package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/jnariai/lazydomain/internal/hostsfile"
	"github.com/jnariai/lazydomain/internal/names"
	"github.com/jnariai/lazydomain/internal/proxy"
)

const currentVersion = 1

type Site struct {
	Label    string    `json:"label"`
	Dir      string    `json:"dir"`
	AppPort  int       `json:"app_port"`
	VitePort int       `json:"vite_port"`
	AddedAt  time.Time `json:"added_at"`
}

func (s Site) Domain() string     { return names.Domain(s.Label) }
func (s Site) ViteDomain() string { return names.ViteDomain(s.Label) }

type State struct {
	Version     int    `json:"version"`
	ProjectsDir string `json:"projects_dir,omitempty"`
	Sites       []Site `json:"sites"`

	path string
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "lazydomain", "state.json"), nil
}

func Load() (*State, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return LoadFrom(path)
}

func LoadFrom(path string) (*State, error) {
	s := &State{Version: currentVersion, path: path}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if s.Version > currentVersion {
		return nil, fmt.Errorf("%s was written by a newer lazydomain (version %d)", path, s.Version)
	}
	s.path = path
	return s, nil
}

func (s *State) Save() error {
	if s.path == "" {
		return errors.New("state has no path")
	}
	s.Version = currentVersion
	sort.Slice(s.Sites, func(i, j int) bool { return s.Sites[i].Label < s.Sites[j].Label })
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".state-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

func (s *State) Root() string {
	if s.ProjectsDir != "" {
		return s.ProjectsDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "Projects"
	}
	return filepath.Join(home, "Projects")
}

func (s *State) Clone() *State {
	c := *s
	c.Sites = append([]Site(nil), s.Sites...)
	return &c
}

func (s *State) ByLabel(label string) (Site, bool) {
	for _, site := range s.Sites {
		if site.Label == label {
			return site, true
		}
	}
	return Site{}, false
}

func (s *State) ByDir(dir string) (Site, bool) {
	for _, site := range s.Sites {
		if site.Dir == dir {
			return site, true
		}
	}
	return Site{}, false
}

func (s *State) Upsert(site Site) {
	for i := range s.Sites {
		if s.Sites[i].Dir == site.Dir {
			if !s.Sites[i].AddedAt.IsZero() {
				site.AddedAt = s.Sites[i].AddedAt
			}
			s.Sites[i] = site
			return
		}
	}
	s.Sites = append(s.Sites, site)
}

func (s *State) Remove(label string) bool {
	for i, site := range s.Sites {
		if site.Label == label {
			s.Sites = append(s.Sites[:i], s.Sites[i+1:]...)
			return true
		}
	}
	return false
}

func (s *State) Routes() []proxy.Route {
	routes := make([]proxy.Route, 0, 2*len(s.Sites))
	for _, site := range s.Sites {
		routes = append(routes,
			proxy.Route{Host: site.Domain(), Upstream: upstream(site.AppPort)},
			proxy.Route{Host: site.ViteDomain(), Upstream: upstream(site.VitePort)},
		)
	}
	return routes
}

func (s *State) HostsEntries() []hostsfile.Entry {
	entries := make([]hostsfile.Entry, 0, len(s.Sites))
	for _, site := range s.Sites {
		entries = append(entries, hostsfile.Entry{IP: "127.0.0.1", Hosts: []string{site.Domain(), site.ViteDomain()}})
	}
	return entries
}

func upstream(port int) string { return "127.0.0.1:" + strconv.Itoa(port) }
