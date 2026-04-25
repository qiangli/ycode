//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// TestPersesDashboards verifies that the expected Perses projects, global
// datasource, and dashboards are provisioned and accessible via the API.
func TestPersesDashboards(t *testing.T) {
	requireConnectivity(t)

	persesBase := baseURL(t) + "/dashboard"

	t.Run("GlobalDatasource", func(t *testing.T) {
		url := persesBase + "/api/v1/globaldatasources/prometheus"
		status, body := httpGet(t, url)
		if status != 200 {
			t.Fatalf("GET %s returned %d, want 200 (body: %s)", url, status, body)
		}
		var ds map[string]any
		if err := json.Unmarshal([]byte(body), &ds); err != nil {
			t.Fatalf("unmarshal datasource: %v", err)
		}
		if ds["kind"] != "GlobalDatasource" {
			t.Errorf("expected kind GlobalDatasource, got %v", ds["kind"])
		}
	})

	// Expected projects and their minimum dashboard counts.
	expectedProjects := []struct {
		name          string
		minDashboards int
	}{
		{"ycode", 6},
		{"host-metrics", 5},
		{"ycode-node-collector", 5},
	}

	t.Run("Projects", func(t *testing.T) {
		url := persesBase + "/api/v1/projects"
		status, body := httpGet(t, url)
		if status != 200 {
			t.Fatalf("GET %s returned %d, want 200", url, status)
		}
		var projects []map[string]any
		if err := json.Unmarshal([]byte(body), &projects); err != nil {
			t.Fatalf("unmarshal projects: %v", err)
		}

		// Build a set of project names.
		found := make(map[string]bool)
		for _, p := range projects {
			meta, ok := p["metadata"].(map[string]any)
			if !ok {
				continue
			}
			if name, ok := meta["name"].(string); ok {
				found[name] = true
			}
		}

		for _, ep := range expectedProjects {
			if !found[ep.name] {
				t.Errorf("project %q not found in Perses (found: %v)", ep.name, found)
			}
		}
	})

	for _, ep := range expectedProjects {
		ep := ep
		t.Run(fmt.Sprintf("Dashboards/%s", ep.name), func(t *testing.T) {
			t.Parallel()
			url := fmt.Sprintf("%s/api/v1/projects/%s/dashboards", persesBase, ep.name)
			status, body := httpGet(t, url)
			if status != 200 {
				t.Fatalf("GET %s returned %d, want 200 (body: %.200s)", url, status, body)
			}
			var dashboards []map[string]any
			if err := json.Unmarshal([]byte(body), &dashboards); err != nil {
				t.Fatalf("unmarshal dashboards: %v", err)
			}
			if len(dashboards) < ep.minDashboards {
				t.Errorf("project %q has %d dashboards, want at least %d",
					ep.name, len(dashboards), ep.minDashboards)
				for _, d := range dashboards {
					if meta, ok := d["metadata"].(map[string]any); ok {
						t.Logf("  dashboard: %v", meta["name"])
					}
				}
			} else {
				t.Logf("project %q: %d dashboards OK", ep.name, len(dashboards))
				for _, d := range dashboards {
					if meta, ok := d["metadata"].(map[string]any); ok {
						t.Logf("  - %v", meta["name"])
					}
				}
			}

			// Verify each dashboard has panels and layouts.
			for _, d := range dashboards {
				meta, _ := d["metadata"].(map[string]any)
				name, _ := meta["name"].(string)
				spec, ok := d["spec"].(map[string]any)
				if !ok {
					t.Errorf("dashboard %q: missing spec", name)
					continue
				}
				if panels, ok := spec["panels"].(map[string]any); !ok || len(panels) == 0 {
					t.Errorf("dashboard %q: has no panels", name)
				}
				if layouts, ok := spec["layouts"].([]any); !ok || len(layouts) == 0 {
					t.Errorf("dashboard %q: has no layouts", name)
				}
			}
		})
	}
}

// TestPersesDashboardLinks verifies that every dashboard is accessible via
// both its API endpoint and UI route. Fetches the full dashboard list from
// the API and tests each one — no hardcoded names.
func TestPersesDashboardLinks(t *testing.T) {
	requireConnectivity(t)

	persesBase := baseURL(t) + "/dashboard"
	projects := []string{"ycode", "host-metrics", "ycode-node-collector"}

	for _, project := range projects {
		project := project
		t.Run(project, func(t *testing.T) {
			// Fetch all dashboards for this project.
			listURL := fmt.Sprintf("%s/api/v1/projects/%s/dashboards", persesBase, project)
			status, body := httpGet(t, listURL)
			if status != 200 {
				t.Fatalf("GET %s returned %d", listURL, status)
			}
			var dashboards []map[string]any
			if err := json.Unmarshal([]byte(body), &dashboards); err != nil {
				t.Fatalf("unmarshal dashboards: %v", err)
			}
			if len(dashboards) == 0 {
				t.Fatalf("project %q has no dashboards", project)
			}

			for _, d := range dashboards {
				meta, _ := d["metadata"].(map[string]any)
				dbName, _ := meta["name"].(string)
				if dbName == "" {
					continue
				}
				name := dbName
				t.Run(name, func(t *testing.T) {
					t.Parallel()

					// 1. API endpoint: GET individual dashboard by name.
					apiURL := fmt.Sprintf("%s/api/v1/projects/%s/dashboards/%s",
						persesBase, project, name)
					apiStatus, apiBody := httpGet(t, apiURL)
					if apiStatus != 200 {
						t.Errorf("API GET %s returned %d (body: %.200s)", apiURL, apiStatus, apiBody)
					} else {
						// Verify response is valid JSON with correct kind.
						var db map[string]any
						if err := json.Unmarshal([]byte(apiBody), &db); err != nil {
							t.Errorf("API response for %s is not valid JSON: %v", name, err)
						} else if db["kind"] != "Dashboard" {
							t.Errorf("API response kind = %v, want Dashboard", db["kind"])
						}
					}

					// 2. UI route: the Perses SPA serves index.html for all UI routes.
					uiURL := fmt.Sprintf("%s/projects/%s/dashboards/%s",
						persesBase, project, name)
					resp, err := httpClient().Get(uiURL)
					if err != nil {
						t.Errorf("UI GET %s: %v", uiURL, err)
						return
					}
					resp.Body.Close()
					// Perses SPA returns 200 for all valid UI routes (index.html fallback).
					if resp.StatusCode != http.StatusOK {
						t.Errorf("UI GET %s returned %d, want 200", uiURL, resp.StatusCode)
					}
				})
			}
		})
	}
}

// TestPersesPluginsLoaded verifies that the Perses plugins API is reachable.
// In embedded mode, Perses renders dashboards via built-in React components
// and may return an empty plugin list — this is expected.
func TestPersesPluginsLoaded(t *testing.T) {
	requireConnectivity(t)

	url := baseURL(t) + "/dashboard/api/v1/plugins"
	status, body := httpGet(t, url)
	if status != 200 {
		t.Fatalf("GET %s returned %d, want 200", url, status)
	}
	var modules []map[string]any
	if err := json.Unmarshal([]byte(body), &modules); err != nil {
		t.Fatalf("unmarshal plugins: %v", err)
	}
	t.Logf("%d plugins loaded", len(modules))
}
