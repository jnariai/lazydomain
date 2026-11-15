package proxy

import (
	"encoding/json"
	"sort"
	"time"
)

type Route struct {
	Host     string
	Upstream string
}

const CAName = "lazydomain Local CA"

// Caddy's defaults (12h leaves, 7d intermediate) assume a server that never
// sleeps. Under WSL2 the VM is paused while Windows sleeps, Caddy's renewal
// timer stops with it, and after resume the browser gets a certificate that
// expired during the night (ERR_CERT_DATE_INVALID). Long lifetimes keep the
// renewal window (last third of the lifetime) far wider than any sleep.
// Leaves chained to a user-installed root are exempt from the 398-day limit.
const (
	IntermediateLifetime = 365 * 24 * time.Hour
	LeafLifetime         = 30 * 24 * time.Hour // renewed ~10 days before expiry
)

// pki is always present so Caddy generates the root CA before any domain
// exists. http is only added with routes, since it makes Caddy bind :443/:80.
func BuildConfig(routes []Route) ([]byte, error) {
	apps := map[string]any{
		"pki": map[string]any{
			"certificate_authorities": map[string]any{
				"local": map[string]any{
					"name":                  CAName,
					"install_trust":         false,
					"intermediate_lifetime": IntermediateLifetime.String(),
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
					"issuers": []any{map[string]any{
						"module":   "internal",
						"ca":       "local",
						"lifetime": LeafLifetime.String(),
					}},
				}},
			},
		}
	}

	return json.MarshalIndent(map[string]any{
		"admin": map[string]any{"listen": AdminAddr},
		"apps":  apps,
	}, "", "  ")
}
