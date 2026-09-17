package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/qiangli/ycode/examples"
	harnesscli "github.com/qiangli/ycode/internal/harness/cli"
	"github.com/qiangli/ycode/internal/harness/spec"
	"gopkg.in/yaml.v3"
)

func isolatedCLICwd(t *testing.T) string {
	t.Helper()
	var seed spec.Document
	if err := yaml.Unmarshal(examples.Agent(), &seed); err != nil {
		t.Fatal(err)
	}
	if seed.Spec.Interfaces.CLI == nil {
		t.Fatal("embedded harness has no CLI")
	}
	if env := seed.Spec.Interfaces.CLI.Bootstrap.Env; env != "" {
		t.Setenv(env, "")
	}
	dir := t.TempDir()
	t.Chdir(dir)
	return filepath.Join(dir, seed.Spec.Interfaces.CLI.Bootstrap.DefaultFile)
}

func TestCLIDiscoveryUsesEmbeddedOfflineHelpWithoutProject(t *testing.T) {
	wantSource := isolatedCLICwd(t)
	doc, err := discoverCLI([]string{"--help"})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Source != wantSource {
		t.Fatalf("source = %q, want %q", doc.Source, wantSource)
	}
	if _, err := os.Stat(wantSource); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("offline discovery created a project file: %v", err)
	}
	root, err := harnesscli.New(doc, dispatchCLI, harnesscli.Options{
		Version: version, Commit: commit, IsTerminal: false, LookupEnv: os.LookupEnv,
	})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output.Bytes(), []byte("validate")) || !bytes.Contains(output.Bytes(), []byte("docs")) {
		t.Fatalf("offline help omitted configured commands: %q", output.String())
	}
}

func TestCLIDiscoveryExplicitMissingPathFailsClosed(t *testing.T) {
	isolatedCLICwd(t)
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	for _, args := range [][]string{{"--file", missing}, {"--file=" + missing}, {"-f", missing}, {"-f" + missing}} {
		if _, err := discoverCLI(args); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("discoverCLI(%q) = %v, want missing-file error", args, err)
		}
	}
}

func TestCLIDiscoveryAcceptsConfigFlagSpellings(t *testing.T) {
	fixture := harnessFixture(t)
	isolatedCLICwd(t)
	for _, args := range [][]string{{"--file", fixture}, {"--file=" + fixture}, {"-f", fixture}, {"-f" + fixture}} {
		doc, err := discoverCLI(args)
		if err != nil {
			t.Fatalf("discoverCLI(%q): %v", args, err)
		}
		if doc.Source != fixture {
			t.Fatalf("discoverCLI(%q) source = %q, want %q", args, doc.Source, fixture)
		}
	}
}

func TestCLIDiscoveryIgnoresConfigFlagsAfterSeparator(t *testing.T) {
	wantSource := isolatedCLICwd(t)
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	doc, err := discoverCLI([]string{"prompt", "--", "--file", missing, "-f" + missing})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Source != wantSource {
		t.Fatalf("source = %q, want default %q", doc.Source, wantSource)
	}
}

func TestCLIDiscoveryRepeatedConfigFlagsUseLastPath(t *testing.T) {
	fixture := harnessFixture(t)
	isolatedCLICwd(t)
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	doc, err := discoverCLI([]string{"--file", missing, "-f" + fixture})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Source != fixture {
		t.Fatalf("source = %q, want last path %q", doc.Source, fixture)
	}
	if _, err := discoverCLI([]string{"-f" + fixture, "--file=" + missing}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("last missing path = %v, want missing-file error", err)
	}
}

func TestCLIDiscoveryIgnoresConfigSpellingsInOtherFlagValues(t *testing.T) {
	wantSource := isolatedCLICwd(t)
	for _, args := range [][]string{{"docs", "--search", "-filter"}, {"docs", "--search", "--file=topic"}} {
		doc, err := discoverCLI(args)
		if err != nil {
			t.Fatalf("discoverCLI(%q): %v", args, err)
		}
		if doc.Source != wantSource {
			t.Fatalf("discoverCLI(%q) source = %q, want default %q", args, doc.Source, wantSource)
		}
	}
}
