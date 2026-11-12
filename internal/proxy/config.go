package proxy

import (
	"encoding/json"
	"sort"
)

type Route struct {
	Host     string
	Upstream string
}

const CAName = "lazydomain Local CA"

// pki is always present so Caddy generates the root CA before any domain
// exists. http is only added with routes, since it makes Caddy bind :443/:80.
func BuildConfig(routes []Route) ([]byte, error) {
	apps := map[string]any{
		"pki": map[string]any{
			"certificate_authorities": map[string]any{
				"local": map[string]any{
					"name":          CAName,
					"install_trust": false,
				},
			},
		},
	}

	if len(routes) > 0 {
		sorted := append([]Route(nil), routes...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Host < sorted[j].Host })

		hosts := make([]string, 0, len(sorted))
		caddyRoutes := make([]any, 0, len(sorted))
		for _, r := range sorted {
			hosts = append(hosts, r.Host)
			caddyRoutes = append(caddyRoutes, map[string]any{
				"match": []any{map[string]any{"host": []string{r.Host}}},
				"handle": []any{map[string]any{
					"handler":   "reverse_proxy",
					"upstreams": []any{map[string]any{"dial": r.Upstream}},
				}},
				"terminal": true,
			})
		}

		apps["http"] = map[string]any{
			"servers": map[string]any{
				"lazydomain": map[string]any{
					"listen": []string{":443"},
					"routes": caddyRoutes,
				},
			},
		}
		apps["tls"] = map[string]any{
			"automation": map[string]any{
				"policies": []any{map[string]any{
					"subjects": hosts,
					"issuers":  []any{map[string]any{"module": "internal", "ca": "local"}},
				}},
			},
		}
	}

	return json.MarshalIndent(map[string]any{
		"admin": map[string]any{"listen": AdminAddr},
		"apps":  apps,
	}, "", "  ")
}
