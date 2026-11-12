package state

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingIsEmpty(t *testing.T) {
	s, err := LoadFrom(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Sites) != 0 {
		t.Errorf("sites = %v", s.Sites)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")
	s, _ := LoadFrom(path)
	added := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	s.Upsert(Site{Label: "b", Dir: "/p/b", AppPort: 8081, VitePort: 5174, AddedAt: added})
	s.Upsert(Site{Label: "a", Dir: "/p/a", AppPort: 8080, VitePort: 5173, AddedAt: added})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	got, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sites) != 2 || got.Sites[0].Label != "a" {
		t.Fatalf("sites = %+v", got.Sites)
	}
	if !got.Sites[0].AddedAt.Equal(added) {
		t.Errorf("AddedAt = %v", got.Sites[0].AddedAt)
	}
}

func TestUpsertKeepsAddedAtAndReplacesByDir(t *testing.T) {
	s := &State{}
	first := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.Upsert(Site{Label: "a", Dir: "/p/a", AppPort: 8080, AddedAt: first})
	s.Upsert(Site{Label: "a", Dir: "/p/a", AppPort: 9090, AddedAt: time.Now()})
	if len(s.Sites) != 1 {
		t.Fatalf("sites = %d", len(s.Sites))
	}
	if s.Sites[0].AppPort != 9090 || !s.Sites[0].AddedAt.Equal(first) {
		t.Errorf("site = %+v", s.Sites[0])
	}
}

func TestDerivedRoutesAndHosts(t *testing.T) {
	s := &State{Sites: []Site{{Label: "shop", Dir: "/p/shop", AppPort: 8080, VitePort: 5173}}}

	routes := s.Routes()
	if len(routes) != 2 ||
		routes[0].Host != "shop.test" || routes[0].Upstream != "127.0.0.1:8080" ||
		routes[1].Host != "vite.shop.test" || routes[1].Upstream != "127.0.0.1:5173" {
		t.Errorf("routes = %+v", routes)
	}

	entries := s.HostsEntries()
	if len(entries) != 1 || entries[0].IP != "127.0.0.1" || len(entries[0].Hosts) != 2 {
		t.Errorf("entries = %+v", entries)
	}

	clone := s.Clone()
	clone.Remove("shop")
	if len(s.Sites) != 1 {
		t.Error("Clone shares the Sites slice")
	}
}
