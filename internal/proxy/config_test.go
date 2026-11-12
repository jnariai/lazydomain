package proxy

import (
	"encoding/json"
	"testing"
)

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	return m
}

func TestBuildConfigEmptyHasOnlyPKI(t *testing.T) {
	b, err := BuildConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	apps := decode(t, b)["apps"].(map[string]any)
	if _, ok := apps["pki"]; !ok {
		t.Error("pki app missing")
	}
	if _, ok := apps["http"]; ok {
		t.Error("http app present without routes")
	}
}

func TestBuildConfigRoutes(t *testing.T) {
	b, err := BuildConfig([]Route{
		{Host: "vite.b.test", Upstream: "127.0.0.1:5174"},
		{Host: "a.test", Upstream: "127.0.0.1:8080"},
	})
	if err != nil {
		t.Fatal(err)
	}
	apps := decode(t, b)["apps"].(map[string]any)

	srv := apps["http"].(map[string]any)["servers"].(map[string]any)["lazydomain"].(map[string]any)
	routes := srv["routes"].([]any)
	if len(routes) != 2 {
		t.Fatalf("got %d routes", len(routes))
	}
	first := routes[0].(map[string]any)
	host := first["match"].([]any)[0].(map[string]any)["host"].([]any)[0]
	if host != "a.test" {
		t.Errorf("routes not sorted: first host %v", host)
	}
	dial := first["handle"].([]any)[0].(map[string]any)["upstreams"].([]any)[0].(map[string]any)["dial"]
	if dial != "127.0.0.1:8080" {
		t.Errorf("dial = %v", dial)
	}

	pol := apps["tls"].(map[string]any)["automation"].(map[string]any)["policies"].([]any)[0].(map[string]any)
	if n := len(pol["subjects"].([]any)); n != 2 {
		t.Errorf("tls subjects = %d, want 2", n)
	}
}
