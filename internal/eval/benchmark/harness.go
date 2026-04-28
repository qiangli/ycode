// Package benchmark provides a containerized E2E benchmark that runs each
// agentic tool's /init command against test repos and compares the quality
// of generated AGENTS.md files.
//
// It uses ycode's internal container.Engine (Podman REST API) and
// inference.OllamaComponent for a self-contained benchmark.
//
// Run with: go test -tags benchmark -v -timeout 35m ./internal/eval/benchmark/...
package benchmark

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/qiangli/ycode/internal/container"
	"github.com/qiangli/ycode/internal/eval/agentsmd"
)

// Config holds configuration for a benchmark run.
// All fields can be overridden by environment variables (see ConfigFromEnv).
type Config struct {
	ProjectRoot string        // ycode repo root
	Model       string        // Ollama model (empty = auto-detect from RAM)
	TestRepos   []TestRepo    // repos to benchmark against
	Timeout     time.Duration // total timeout (default 30m)
	OllamaURL   string        // Ollama HTTP endpoint (empty = http://localhost:11434)
	PodmanURL   string        // Podman socket URL (empty = auto-detect; ssh://user@host/... for remote)
}

// ConfigFromEnv populates a Config from environment variables.
// Explicit fields in cfg take precedence over env vars.
func ConfigFromEnv(cfg Config) Config {
	if cfg.OllamaURL == "" {
		if v := os.Getenv("OLLAMA_URL"); v != "" {
			cfg.OllamaURL = v
		} else if host := os.Getenv("HOST"); host != "" && host != "localhost" && host != "127.0.0.1" {
			cfg.OllamaURL = "http://" + host + ":11434"
		}
	}
	if cfg.PodmanURL == "" {
		if v := os.Getenv("PODMAN_URL"); v != "" {
			cfg.PodmanURL = v
		}
	}
	if cfg.Model == "" {
		if v := os.Getenv("BENCH_MODEL"); v != "" {
			cfg.Model = v
		}
	}
	if cfg.Timeout == 0 {
		if v := os.Getenv("BENCH_TIMEOUT"); v != "" {
			if d, err := time.ParseDuration(v); err == nil {
				cfg.Timeout = d
			}
		}
	}
	return cfg
}

// TestRepo identifies a repo to run /init against.
type TestRepo struct {
	Name      string // display name (e.g., "opencode")
	SourceDir string // absolute path to the repo source
}

// Result holds all results for one benchmark run.
type Result struct {
	Model       string
	HostRAMGB   int
	ToolResults []ToolResult
	Comparison  string // FormatComparison output
}

// ToolResult holds one tool's result on one repo.
type ToolResult struct {
	ToolName    string
	RepoName    string
	GeneratedMD string           // raw AGENTS.md content
	Report      *agentsmd.Report // scored
	Duration    time.Duration
	Error       string
	Skipped     bool
	SkipReason  string
}

// Run executes the full benchmark. It:
// 1. Auto-selects the best Ollama model for host RAM
// 2. Connects to the container engine
// 3. Builds tool images from embedded Dockerfiles
// 4. Runs each tool's /init against each test repo
// 5. Collects and scores all generated AGENTS.md files
// 6. Returns side-by-side comparison
func Run(ctx context.Context, cfg Config) (*Result, error) {
	// Detect host RAM and select model.
	ramGB, err := DetectHostRAM()
	if err != nil {
		slog.Warn("benchmark: could not detect RAM, defaulting to 16GB", "error", err)
		ramGB = 16
	}

	model := cfg.Model
	if model == "" {
		model = SelectModel(ramGB)
	}
	slog.Info("benchmark: configuration", "model", model, "ram_gb", ramGB, "repos", len(cfg.TestRepos))

	// Apply environment variable overrides.
	cfg = ConfigFromEnv(cfg)

	// Detect Ollama URL.
	ollamaURL := cfg.OllamaURL
	if ollamaURL == "" {
		ollamaURL = "http://localhost:11434"
	}

	// Connect to container engine (local or remote socket).
	engineCfg := &container.EngineConfig{}
	if cfg.PodmanURL != "" {
		engineCfg.SocketPath = cfg.PodmanURL
		slog.Info("benchmark: using remote podman", "url", cfg.PodmanURL)
	}
	engine, err := container.NewEngine(ctx, engineCfg)
	if err != nil {
		return nil, fmt.Errorf("container engine: %w", err)
	}
	defer engine.Close(ctx)

	slog.Info("benchmark: container engine ready")

	drivers := DefaultDrivers(cfg.ProjectRoot, ollamaURL, model)
	var allResults []ToolResult

	for _, driver := range drivers {
		if driver.Skip {
			allResults = append(allResults, ToolResult{
				ToolName:   driver.Name,
				Skipped:    true,
				SkipReason: driver.SkipReason,
			})
			continue
		}

		// Build image.
		slog.Info("benchmark: building image", "tool", driver.Name, "image", driver.ImageName)
		if err := buildToolImage(ctx, engine, driver); err != nil {
			slog.Warn("benchmark: image build failed", "tool", driver.Name, "error", err)
			for _, repo := range cfg.TestRepos {
				allResults = append(allResults, ToolResult{
					ToolName: driver.Name,
					RepoName: repo.Name,
					Error:    fmt.Sprintf("image build failed: %v", err),
				})
			}
			continue
		}

		// Run against each test repo.
		for _, repo := range cfg.TestRepos {
			slog.Info("benchmark: running", "tool", driver.Name, "repo", repo.Name)
			result := runToolOnRepo(ctx, engine, driver, repo)
			allResults = append(allResults, result)
		}
	}

	// Score all results and build comparison.
	comparison := scoreAndCompare(allResults, cfg.TestRepos)

	return &Result{
		Model:       model,
		HostRAMGB:   ramGB,
		ToolResults: allResults,
		Comparison:  comparison,
	}, nil
}

// buildToolImage builds the container image for a tool via REST API.
func buildToolImage(ctx context.Context, engine *container.Engine, driver ToolDriver) error {
	if engine.ImageExists(ctx, driver.ImageName) {
		slog.Info("benchmark: image already exists", "image", driver.ImageName)
		return nil
	}

	return engine.BuildImage(ctx, driver.ImageName, driver.Dockerfile)
}

// runToolOnRepo runs a single tool's /init on a single test repo.
func runToolOnRepo(ctx context.Context, engine *container.Engine, driver ToolDriver, repo TestRepo) ToolResult {
	start := time.Now()
	result := ToolResult{
		ToolName: driver.Name,
		RepoName: repo.Name,
	}

	toolCtx, cancel := context.WithTimeout(ctx, driver.Timeout)
	defer cancel()

	// Create container.
	ctrCfg := &container.ContainerConfig{
		Name:    fmt.Sprintf("bench-%s-%s-%d", driver.Name, repo.Name, time.Now().UnixMilli()),
		Image:   driver.ImageName,
		Env:     driver.Env,
		WorkDir: "/workspace",
		Labels: map[string]string{
			"ycode.benchmark": "true",
			"ycode.tool":      driver.Name,
			"ycode.repo":      repo.Name,
		},
	}

	ctr, err := engine.CreateContainer(toolCtx, ctrCfg)
	if err != nil {
		result.Error = fmt.Sprintf("create container: %v", err)
		result.Duration = time.Since(start)
		return result
	}
	defer func() {
		ctr.Remove(ctx, true) //nolint:errcheck
	}()

	// Start container.
	if err := ctr.Start(toolCtx); err != nil {
		result.Error = fmt.Sprintf("start container: %v", err)
		result.Duration = time.Since(start)
		return result
	}

	// Copy test repo into container workspace.
	if err := ctr.CopyTo(toolCtx, repo.SourceDir+"/.", "/workspace"); err != nil {
		result.Error = fmt.Sprintf("copy repo: %v", err)
		result.Duration = time.Since(start)
		return result
	}

	// Remove existing instruction files so each tool generates from scratch.
	ctr.Exec(toolCtx, "rm -f /workspace/AGENTS.md /workspace/CLAUDE.md /workspace/GEMINI.md", "") //nolint:errcheck

	// Run the tool's init command.
	slog.Info("benchmark: executing init", "tool", driver.Name, "repo", repo.Name, "command", driver.InitCommand)
	execResult, err := ctr.Exec(toolCtx, driver.InitCommand, "/workspace")
	if err != nil {
		result.Error = fmt.Sprintf("exec init: %v", err)
		result.Duration = time.Since(start)
		return result
	}

	if execResult.ExitCode != 0 {
		slog.Warn("benchmark: init exited non-zero", "tool", driver.Name, "exit", execResult.ExitCode)
	}

	// Collect generated file.
	tmpDir, err := os.MkdirTemp("", "bench-output-*")
	if err != nil {
		result.Error = fmt.Sprintf("create temp dir: %v", err)
		result.Duration = time.Since(start)
		return result
	}
	defer os.RemoveAll(tmpDir)

	var collected bool
	for _, outputFile := range driver.OutputFiles {
		hostPath := filepath.Join(tmpDir, outputFile)
		if err := ctr.CopyFrom(toolCtx, "/workspace/"+outputFile, hostPath); err == nil {
			if content, err := os.ReadFile(hostPath); err == nil && len(content) > 0 {
				result.GeneratedMD = string(content)
				collected = true
				slog.Info("benchmark: collected output", "tool", driver.Name, "file", outputFile, "bytes", len(content))
				break
			}
		}
	}

	if !collected {
		result.Error = "no AGENTS.md generated"
	}

	// Score the generated content.
	if result.GeneratedMD != "" {
		result.Report = agentsmd.Analyze(result.GeneratedMD, agentsmd.Options{
			ProjectRoot: repo.SourceDir,
		})
	}

	result.Duration = time.Since(start)
	return result
}

// scoreAndCompare builds a comparison table from all results.
func scoreAndCompare(results []ToolResult, repos []TestRepo) string {
	// Group by tool, pick best result per tool (across repos).
	best := map[string]*ToolResult{}
	for i := range results {
		r := &results[i]
		if r.Skipped || r.Report == nil {
			continue
		}
		existing, ok := best[r.ToolName]
		if !ok || r.Report.Score > existing.Report.Score {
			best[r.ToolName] = r
		}
	}

	var entries []agentsmd.ComparisonEntry
	for _, r := range results {
		if r.Skipped {
			continue
		}
		b, ok := best[r.ToolName]
		if !ok || b != &results[0] {
			// Only include best result per tool.
		}
		_ = b
	}

	// Simpler: include all non-skipped results with reports.
	seen := map[string]bool{}
	for _, r := range results {
		if r.Skipped || r.Report == nil || seen[r.ToolName] {
			continue
		}
		seen[r.ToolName] = true
		entries = append(entries, agentsmd.ComparisonEntry{
			Name:   r.ToolName + "/" + r.RepoName,
			Report: r.Report,
		})
	}

	return agentsmd.FormatComparison(entries)
}
