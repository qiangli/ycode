// genie adapts one SWE-bench instance to ycode and emits a prediction.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type request struct {
	InstanceID       string `json:"instance_id"`
	ProblemStatement string `json:"problem_statement"`
	RepoPath         string `json:"repo_path"`
	ModelName        string `json:"model_name_or_path"`
	ArtifactDir      string `json:"artifact_dir"`
	RunID            string `json:"run_id"`
}

type prediction struct {
	InstanceID string `json:"instance_id"`
	ModelName  string `json:"model_name_or_path"`
	ModelPatch string `json:"model_patch"`
}

func main() {
	config := flag.String("config", "agent.yaml", "path to ycode harness YAML")
	flag.Parse()
	if err := run(os.Stdin, os.Stdout, os.Stderr, *config); err != nil {
		fmt.Fprintln(os.Stderr, "genie:", err)
		os.Exit(1)
	}
}

func run(input io.Reader, output, diagnostic io.Writer, config string) (retErr error) {
	var req request
	dec := json.NewDecoder(io.LimitReader(input, 4<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	if req.InstanceID == "" || req.ProblemStatement == "" || req.RepoPath == "" || req.ArtifactDir == "" || req.RunID == "" || req.ModelName == "" {
		return errors.New("instance_id, problem_statement, repo_path, artifact_dir, run_id, and model_name_or_path are required")
	}
	repo, err := filepath.Abs(req.RepoPath)
	if err != nil {
		return fmt.Errorf("resolve repo_path: %w", err)
	}
	config, err = resolveConfig(config)
	if err != nil {
		return fmt.Errorf("resolve config: %w", err)
	}
	artifacts, err := filepath.Abs(req.ArtifactDir)
	if err != nil {
		return fmt.Errorf("resolve artifact_dir: %w", err)
	}
	sessionID := safePathComponent(req.RunID + "_" + req.InstanceID)
	instanceArtifacts := filepath.Join(artifacts, safePathComponent(req.RunID), safePathComponent(req.InstanceID))
	for _, dir := range []string{instanceArtifacts, filepath.Join(instanceArtifacts, "home"), filepath.Join(instanceArtifacts, "kb"), filepath.Join(instanceArtifacts, "bashy-home")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create artifact directory %s: %w", dir, err)
		}
	}
	requestBytes, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode request for manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(instanceArtifacts, "request.json"), append(requestBytes, '\n'), 0o600); err != nil {
		return fmt.Errorf("save request artifact: %w", err)
	}
	var response, diagnostics bytes.Buffer
	defer func() {
		_ = os.WriteFile(filepath.Join(instanceArtifacts, "response.txt"), response.Bytes(), 0o600)
		_ = os.WriteFile(filepath.Join(instanceArtifacts, "ycode.stderr.log"), diagnostics.Bytes(), 0o600)
		if retErr != nil {
			_ = os.WriteFile(filepath.Join(instanceArtifacts, "error.txt"), []byte(retErr.Error()+"\n"), 0o600)
		}
	}()
	ycode := os.Getenv("YCODE_BIN")
	if ycode == "" {
		ycode = adjacentBinary("ycode")
	}
	timeout := 30 * time.Minute
	if raw := os.Getenv("GENIE_TIMEOUT"); raw != "" {
		timeout, err = time.ParseDuration(raw)
		if err != nil || timeout <= 0 {
			return fmt.Errorf("invalid GENIE_TIMEOUT %q", raw)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	startedAt := time.Now().UTC()
	prompt := "Solve this SWE-bench issue in the current repository. Inspect, implement, and verify the fix.\n\nInstance: " + req.InstanceID + "\n\nIssue:\n" + req.ProblemStatement
	taskConfig, err := instanceConfig(config, repo, filepath.Join(instanceArtifacts, "config"))
	if err != nil {
		return fmt.Errorf("write instance config: %w", err)
	}
	cmd := exec.CommandContext(ctx, ycode, "--file", taskConfig, "--session", sessionID, prompt)
	cmd.Dir = repo
	cmd.Stderr = io.MultiWriter(diagnostic, &diagnostics)
	cmd.Env = isolatedEnvironment(os.Environ(), instanceArtifacts)
	cmd.Stdout = &response
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("ycode timed out after %s: %w", timeout, ctx.Err())
		}
		return fmt.Errorf("run ycode: %w", err)
	}
	patch, err := workspacePatch(ctx, repo)
	if err != nil {
		return err
	}
	if strings.TrimSpace(patch) == "" {
		return errors.New("ycode completed without producing a git diff")
	}
	model := req.ModelName
	pred := prediction{InstanceID: req.InstanceID, ModelName: model, ModelPatch: patch}
	encoded, err := json.MarshalIndent(pred, "", "  ")
	if err != nil {
		return fmt.Errorf("encode prediction: %w", err)
	}
	if err := os.WriteFile(filepath.Join(instanceArtifacts, "prediction.json"), append(encoded, '\n'), 0o600); err != nil {
		return fmt.Errorf("save prediction artifact: %w", err)
	}
	// The host-aware model pick (dag target pick-model) travels with the
	// run: its facts and reason are part of what made this prediction.
	choicePath := os.Getenv("GENIE_MODEL_CHOICE")
	if choicePath != "" {
		choice, err := os.ReadFile(choicePath)
		if err != nil {
			return fmt.Errorf("read GENIE_MODEL_CHOICE: %w", err)
		}
		if err := os.WriteFile(filepath.Join(instanceArtifacts, "model-choice.json"), choice, 0o600); err != nil {
			return fmt.Errorf("save model choice artifact: %w", err)
		}
	}
	configBytes, err := os.ReadFile(config)
	if err != nil {
		return fmt.Errorf("read config for manifest: %w", err)
	}
	manifest := map[string]any{
		"schema": "genie-run/v1", "run_id": req.RunID, "instance_id": req.InstanceID,
		"model_name_or_path": model, "config_path": config, "instance_config_path": taskConfig,
		"config_sha256":   fmt.Sprintf("%x", sha256.Sum256(configBytes)),
		"request_sha256":  fmt.Sprintf("%x", sha256.Sum256(requestBytes)),
		"started_at_utc":  startedAt.Format(time.RFC3339Nano),
		"finished_at_utc": time.Now().UTC().Format(time.RFC3339Nano),
		"duration_ms":     time.Since(startedAt).Milliseconds(),
		"session_id":      sessionID,
	}
	if choicePath != "" {
		manifest["model_choice"] = "model-choice.json"
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode run manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(instanceArtifacts, "run.json"), append(manifestBytes, '\n'), 0o600); err != nil {
		return fmt.Errorf("save run manifest: %w", err)
	}
	enc := json.NewEncoder(output)
	enc.SetEscapeHTML(false)
	return enc.Encode(pred)
}

// instanceConfig writes a copy of config whose workspace is the task
// checkout. ycode resolves runtime.workspace and the roots against the config
// file's directory, so the shared config alone would put the agent in the
// config directory instead of the repository. The copy lives in dir with the
// prompts beside it, and dir is a readable root so the prompts load.
func instanceConfig(config, repo, dir string) (string, error) {
	source, err := os.ReadFile(config)
	if err != nil {
		return "", err
	}
	text := string(source)
	for _, edit := range [][2]string{
		{"\n    workspace: .\n", "\n    workspace: " + strconv.Quote(repo) + "\n"},
		{"\n    readableRoots: [.]\n", "\n    readableRoots: [" + strconv.Quote(repo) + ", " + strconv.Quote(dir) + "]\n"},
		{"\n    writableRoots: [.]\n", "\n    writableRoots: [" + strconv.Quote(repo) + "]\n"},
	} {
		if strings.Count(text, edit[0]) != 1 {
			return "", fmt.Errorf("%s: expected exactly one %q", config, strings.TrimSpace(edit[0]))
		}
		text = strings.Replace(text, edit[0], edit[1], 1)
	}
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o700); err != nil {
		return "", err
	}
	prompts, err := filepath.Glob(filepath.Join(filepath.Dir(config), "prompts", "*"))
	if err != nil {
		return "", err
	}
	for _, prompt := range prompts {
		data, err := os.ReadFile(prompt)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(dir, "prompts", filepath.Base(prompt)), data, 0o600); err != nil {
			return "", err
		}
	}
	target := filepath.Join(dir, "agent.yaml")
	return target, os.WriteFile(target, []byte(text), 0o600)
}

func resolveConfig(config string) (string, error) {
	if filepath.IsAbs(config) {
		return config, nil
	}
	if abs, err := filepath.Abs(config); err == nil {
		if _, statErr := os.Stat(abs); statErr == nil {
			return abs, nil
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), "..", config), nil
}

func adjacentBinary(name string) string {
	exe, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exe), name)
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate
		}
	}
	return name
}

func safePathComponent(value string) string {
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	sum := sha256.Sum256([]byte(value))
	name := b.String()
	if name == "" || name == "." || name == ".." {
		name = "instance"
	}
	if len(name) > 120 {
		name = name[:120]
	}
	return fmt.Sprintf("%s-%x", name, sum[:4])
}

func isolatedEnvironment(source []string, artifacts string) []string {
	values := make(map[string]string, len(source)+4)
	for _, item := range source {
		if key, value, ok := strings.Cut(item, "="); ok {
			values[key] = value
		}
	}
	values["HOME"] = filepath.Join(artifacts, "home")
	values["BASHY_KB_DIR"] = filepath.Join(artifacts, "kb")
	values["BASHY_HOME"] = filepath.Join(artifacts, "bashy-home")
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}

func workspacePatch(ctx context.Context, repo string) (string, error) {
	tracked := exec.CommandContext(ctx, "git", "diff", "--binary", "HEAD", "--")
	tracked.Dir = repo
	diff, err := tracked.Output()
	if err != nil {
		return "", fmt.Errorf("collect tracked changes: %w", err)
	}
	untracked := exec.CommandContext(ctx, "git", "ls-files", "--others", "--exclude-standard", "-z")
	untracked.Dir = repo
	paths, err := untracked.Output()
	if err != nil {
		return "", fmt.Errorf("list untracked changes: %w", err)
	}
	var patch bytes.Buffer
	patch.Write(diff)
	for _, path := range bytes.Split(paths, []byte{0}) {
		if len(path) == 0 {
			continue
		}
		cmd := exec.CommandContext(ctx, "git", "diff", "--binary", "--no-index", "--", "/dev/null", string(path))
		cmd.Dir = repo
		out, runErr := cmd.Output()
		if runErr != nil {
			var exitErr *exec.ExitError
			if !errors.As(runErr, &exitErr) || exitErr.ExitCode() != 1 {
				return "", fmt.Errorf("diff untracked file %q: %w", path, runErr)
			}
		}
		patch.Write(out)
	}
	return patch.String(), nil
}
