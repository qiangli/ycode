//go:build benchmark

package benchmark

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBenchmarkInit(t *testing.T) {
	root := findProjectRoot(t)

	// Verify priorart repos exist.
	repos := []TestRepo{
		{Name: "opencode", SourceDir: filepath.Join(root, "priorart/opencode")},
		{Name: "clawcode", SourceDir: filepath.Join(root, "priorart/clawcode")},
	}

	var available []TestRepo
	for _, repo := range repos {
		if _, err := os.Stat(repo.SourceDir); err == nil {
			available = append(available, repo)
		} else {
			t.Logf("skip repo %s: %v", repo.Name, err)
		}
	}

	if len(available) == 0 {
		t.Fatal("no test repos available in priorart/")
	}

	cfg := ConfigFromEnv(Config{
		ProjectRoot: root,
		TestRepos:   available,
		Timeout:     30 * time.Minute,
	})

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	result, err := Run(ctx, cfg)
	if err != nil {
		t.Fatalf("benchmark failed: %v", err)
	}

	t.Logf("Model: %s, Host RAM: %dGB\n", result.Model, result.HostRAMGB)
	t.Log("\n" + result.Comparison)

	// Report per-tool results.
	for _, r := range result.ToolResults {
		if r.Skipped {
			t.Logf("SKIP %s: %s", r.ToolName, r.SkipReason)
			continue
		}
		if r.Error != "" {
			t.Logf("FAIL %s/%s: %s (%s)", r.ToolName, r.RepoName, r.Error, r.Duration)
			continue
		}
		if r.Report != nil {
			t.Logf("OK   %s/%s: %.1f/10, %d lines (%s)",
				r.ToolName, r.RepoName, r.Report.Score*10, r.Report.TotalLines, r.Duration)
		} else {
			t.Logf("OK   %s/%s: no report (%s)", r.ToolName, r.RepoName, r.Duration)
		}
	}
}

func TestModelSelection(t *testing.T) {
	tests := []struct {
		ram   int
		model string
	}{
		{8, "qwen2.5-coder:3b"},
		{16, "qwen2.5-coder:7b"},
		{24, "qwen2.5-coder:14b"},
		{32, "qwen2.5-coder:32b"},
		{64, "qwen2.5-coder:32b"},
	}
	for _, tt := range tests {
		got := SelectModel(tt.ram)
		if got != tt.model {
			t.Errorf("SelectModel(%d) = %s, want %s", tt.ram, got, tt.model)
		}
	}
}

func TestDetectHostRAM(t *testing.T) {
	ram, err := DetectHostRAM()
	if err != nil {
		t.Fatalf("DetectHostRAM: %v", err)
	}
	if ram < 1 || ram > 1024 {
		t.Errorf("RAM %dGB seems unreasonable", ram)
	}
	t.Logf("detected %dGB RAM → model: %s", ram, SelectModel(ram))
}

func TestDefaultDrivers(t *testing.T) {
	drivers := DefaultDrivers("/tmp/fake", "http://localhost:11434", "test-model")
	if len(drivers) == 0 {
		t.Fatal("no drivers returned")
	}

	var names []string
	for _, d := range drivers {
		names = append(names, d.Name)
		if !d.Skip && len(d.Dockerfile) == 0 {
			t.Errorf("driver %s has empty Dockerfile", d.Name)
		}
		if !d.Skip && d.InitCommand == "" {
			t.Errorf("driver %s has empty InitCommand", d.Name)
		}
	}
	t.Logf("drivers: %v", names)
}

func findProjectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root")
		}
		dir = parent
	}
}
