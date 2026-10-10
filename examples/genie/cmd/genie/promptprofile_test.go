package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPromptProfileSelectionDefaults(t *testing.T) {
	t.Setenv("GENIE_PROMPT_PROFILE", "")
	got, err := promptProfile("")
	if err != nil || got != "general" {
		t.Fatalf("promptProfile(\"\") = %q, %v; want \"general\", nil", got, err)
	}
	got, err = promptProfile("swe")
	if err != nil || got != "swe" {
		t.Fatalf("promptProfile(\"swe\") = %q, %v; want \"swe\", nil", got, err)
	}
	got, err = promptProfile("general")
	if err != nil || got != "general" {
		t.Fatalf("promptProfile(\"general\") = %q, %v; want \"general\", nil", got, err)
	}
}

func TestPromptProfileSelectionValues(t *testing.T) {
	for _, profile := range []string{"general", "swe", "terminal"} {
		t.Run(profile, func(t *testing.T) {
			t.Setenv("GENIE_PROMPT_PROFILE", profile)
			got, err := promptProfile("swe")
			if err != nil || got != profile {
				t.Fatalf("promptProfile(\"swe\") with env %q = %q, %v; want %q, nil", profile, got, err, profile)
			}
		})
	}
}

func TestPromptProfileUnknownFailsClosed(t *testing.T) {
	for _, invalid := range []string{"unknown", "foo", "SWE", "General", "invalid"} {
		t.Run(invalid, func(t *testing.T) {
			t.Setenv("GENIE_PROMPT_PROFILE", invalid)
			got, err := promptProfile("general")
			if err == nil {
				t.Fatalf("promptProfile(\"general\") with env %q unexpectedly succeeded with %q", invalid, got)
			}
			if !strings.Contains(err.Error(), "unknown GENIE_PROMPT_PROFILE") {
				t.Fatalf("promptProfile(\"general\") with env %q returned error %v; want error containing 'unknown GENIE_PROMPT_PROFILE'", invalid, err)
			}
		})
	}
}

func TestPromptFilesUnderByteLimit(t *testing.T) {
	for _, name := range []string{"system-general.md", "system-swe.md", "system-terminal.md"} {
		path := filepath.Join("..", "..", "prompts", name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if info.Size() >= 4096 {
			t.Fatalf("%s size %d >= maxBytes 4096", name, info.Size())
		}
	}
}

func TestInstanceConfigCopiesSelectedPromptProfile(t *testing.T) {
	workspace := t.TempDir()
	sourceDir := filepath.Join("..", "..")

	for _, profile := range []string{"general", "swe", "terminal"} {
		t.Run(profile, func(t *testing.T) {
			t.Setenv("GENIE_PROMPT_PROFILE", profile)
			dir := filepath.Join(t.TempDir(), "instance-"+profile)
			cfgPath, err := chatConfig(filepath.Join(sourceDir, "agent.yaml"), workspace, dir)
			if err != nil {
				t.Fatalf("chatConfig error: %v", err)
			}
			if _, err := os.Stat(cfgPath); err != nil {
				t.Fatalf("config file missing: %v", err)
			}
			systemPromptPath := filepath.Join(dir, "prompts", "system.md")
			systemData, err := os.ReadFile(systemPromptPath)
			if err != nil {
				t.Fatalf("read %s: %v", systemPromptPath, err)
			}
			expectedPath := filepath.Join(sourceDir, "prompts", "system-"+profile+".md")
			expectedData, err := os.ReadFile(expectedPath)
			if err != nil {
				t.Fatalf("read %s: %v", expectedPath, err)
			}
			if string(systemData) != string(expectedData) {
				t.Fatalf("system.md in instance does not match %s", expectedPath)
			}
		})
	}
}

func TestInstanceConfigDefaultProfiles(t *testing.T) {
	workspace := t.TempDir()
	sourceDir := filepath.Join("..", "..")
	t.Setenv("GENIE_PROMPT_PROFILE", "")

	// chatConfig defaults to "general"
	chatDir := filepath.Join(t.TempDir(), "chat-default")
	_, err := chatConfig(filepath.Join(sourceDir, "agent.yaml"), workspace, chatDir)
	if err != nil {
		t.Fatalf("chatConfig error: %v", err)
	}
	chatSystemData, err := os.ReadFile(filepath.Join(chatDir, "prompts", "system.md"))
	if err != nil {
		t.Fatalf("read chat system.md: %v", err)
	}
	generalData, err := os.ReadFile(filepath.Join(sourceDir, "prompts", "system-general.md"))
	if err != nil {
		t.Fatalf("read system-general.md: %v", err)
	}
	if string(chatSystemData) != string(generalData) {
		t.Fatal("chatConfig did not default to system-general.md")
	}

	// instanceConfig for SWE solve path defaults to "swe"
	solveDir := filepath.Join(t.TempDir(), "solve-default")
	_, err = instanceConfig(filepath.Join(sourceDir, "agent.yaml"), workspace, solveDir, PromptProfileSWE)
	if err != nil {
		t.Fatalf("instanceConfig error: %v", err)
	}
	solveSystemData, err := os.ReadFile(filepath.Join(solveDir, "prompts", "system.md"))
	if err != nil {
		t.Fatalf("read solve system.md: %v", err)
	}
	sweData, err := os.ReadFile(filepath.Join(sourceDir, "prompts", "system-swe.md"))
	if err != nil {
		t.Fatalf("read system-swe.md: %v", err)
	}
	if string(solveSystemData) != string(sweData) {
		t.Fatal("instanceConfig with PromptProfileSWE did not default to system-swe.md")
	}
}

func TestSolvePathUnknownProfileFailsClosed(t *testing.T) {
	repo := initGitRepo(t, map[string]string{"stats.py": "MEAN=1\n"})
	artifacts := t.TempDir()
	fake := filepath.Join(t.TempDir(), "ycode")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YCODE_BIN", fake)
	t.Setenv("GENIE_MODEL_CHOICE", "")
	t.Setenv("GENIE_PROMPT_PROFILE", "invalid")
	req := `{"instance_id":"invalid-profile","problem_statement":"fix","repo_path":` +
		strconv.Quote(repo) + `,"artifact_dir":` + strconv.Quote(artifacts) +
		`,"run_id":"run1","model_name_or_path":"m"}` + "\n"
	var output, diagnostic strings.Builder
	err := run(strings.NewReader(req), &output, &diagnostic, filepath.Join("..", "..", "agent.yaml"))
	if err == nil {
		t.Fatal("run with invalid GENIE_PROMPT_PROFILE unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "unknown GENIE_PROMPT_PROFILE") {
		t.Fatalf("expected unknown GENIE_PROMPT_PROFILE error, got: %v", err)
	}
}

