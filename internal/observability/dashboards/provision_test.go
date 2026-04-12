package dashboards

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProvision(t *testing.T) {
	dir := t.TempDir()
	promURL := "http://127.0.0.1:58080/prometheus"

	if err := Provision(dir, promURL); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	// Verify global datasource.
	dsPath := filepath.Join(dir, "globaldatasources", "prometheus.json")
	dsData, err := os.ReadFile(dsPath)
	if err != nil {
		t.Fatalf("expected datasource file: %v", err)
	}
	var ds map[string]any
	if err := json.Unmarshal(dsData, &ds); err != nil {
		t.Fatalf("unmarshal datasource: %v", err)
	}
	if ds["kind"] != "GlobalDatasource" {
		t.Errorf("expected kind GlobalDatasource, got %v", ds["kind"])
	}

	// Verify projects.
	for _, name := range []string{"ycode", "host-metrics", "ycode-node-collector"} {
		projPath := filepath.Join(dir, "projects", name+".json")
		data, err := os.ReadFile(projPath)
		if err != nil {
			t.Errorf("expected project file %s: %v", name, err)
			continue
		}
		var proj map[string]any
		if err := json.Unmarshal(data, &proj); err != nil {
			t.Errorf("unmarshal project %s: %v", name, err)
			continue
		}
		if proj["kind"] != "Project" {
			t.Errorf("project %s: expected kind Project, got %v", name, proj["kind"])
		}
	}

	// Verify dashboards exist for each project.
	expectedDashboards := map[string]int{
		"ycode":                6,
		"host-metrics":         5,
		"ycode-node-collector": 5,
	}
	for project, minCount := range expectedDashboards {
		dbDir := filepath.Join(dir, "dashboards", project)
		entries, err := os.ReadDir(dbDir)
		if err != nil {
			t.Errorf("read dashboard dir for %s: %v", project, err)
			continue
		}
		if len(entries) < minCount {
			t.Errorf("project %s: expected at least %d dashboards, got %d", project, minCount, len(entries))
		}

		// Verify each dashboard has panels and layouts.
		for _, e := range entries {
			data, err := os.ReadFile(filepath.Join(dbDir, e.Name()))
			if err != nil {
				t.Errorf("read dashboard %s/%s: %v", project, e.Name(), err)
				continue
			}
			var db map[string]any
			if err := json.Unmarshal(data, &db); err != nil {
				t.Errorf("unmarshal dashboard %s/%s: %v", project, e.Name(), err)
				continue
			}
			spec, ok := db["spec"].(map[string]any)
			if !ok {
				t.Errorf("dashboard %s/%s: missing spec", project, e.Name())
				continue
			}
			panels, ok := spec["panels"].(map[string]any)
			if !ok || len(panels) == 0 {
				t.Errorf("dashboard %s/%s: has no panels", project, e.Name())
			}
			layouts, ok := spec["layouts"].([]any)
			if !ok || len(layouts) == 0 {
				t.Errorf("dashboard %s/%s: has no layouts", project, e.Name())
			}
		}
	}
}

func TestProvisionIdempotent(t *testing.T) {
	dir := t.TempDir()
	promURL := "http://localhost:9090"

	// First provision.
	if err := Provision(dir, promURL); err != nil {
		t.Fatal(err)
	}

	// Read a dashboard file.
	data1, err := os.ReadFile(filepath.Join(dir, "projects", "ycode.json"))
	if err != nil {
		t.Fatal(err)
	}

	// Provision again — should not overwrite.
	if err := Provision(dir, promURL); err != nil {
		t.Fatal(err)
	}
	data2, err := os.ReadFile(filepath.Join(dir, "projects", "ycode.json"))
	if err != nil {
		t.Fatal(err)
	}

	if string(data1) != string(data2) {
		t.Error("Provision overwrote existing file — should be idempotent")
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"LLM Overview", "llm-overview"},
		{"host-metrics", "host-metrics"},
		{"Context & Compaction", "context-and-compaction"},
		{"Session & Turn Metrics", "session-and-turn-metrics"},
		{"CPU", "cpu"},
	}
	for _, tt := range tests {
		got := slugify(tt.input)
		if got != tt.want {
			t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestPanelKey(t *testing.T) {
	key := panelKey(0, "API Call Rate")
	for _, c := range key {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '.' || c == '-') {
			t.Errorf("panelKey contains invalid char %q in %q", string(c), key)
		}
	}
	if len(key) > 75 {
		t.Errorf("panelKey too long: %d", len(key))
	}
}
