package spec

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type capabilityMap struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Contract         string `yaml:"contract"`
		CanonicalFixture string `yaml:"canonicalFixture"`
		SoleModelTool    string `yaml:"soleModelTool"`
	} `yaml:"metadata"`
	Capabilities []struct {
		ID           string   `yaml:"id"`
		Owner        string   `yaml:"owner"`
		ModelVisible bool     `yaml:"modelVisible"`
		Entrypoints  []string `yaml:"entrypoints"`
	} `yaml:"capabilities"`
	StageCatalog              map[string]string `yaml:"stageCatalog"`
	RunForms                  map[string]string `yaml:"runForms"`
	Frontends                 map[string]string `yaml:"frontends"`
	ForbiddenKernelReferences []string          `yaml:"forbiddenKernelReferences"`
}

func TestFrozenCapabilityMapOwnsLiveSurface(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	data, err := os.ReadFile(filepath.Join(root, "docs", "harness-capability-map.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var inventory capabilityMap
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&inventory); err != nil {
		t.Fatalf("decode capability map: %v", err)
	}
	if inventory.APIVersion != APIVersion || inventory.Kind != "HarnessCapabilityMap" || inventory.Metadata.Contract != APIVersion {
		t.Fatalf("capability map does not freeze %s", APIVersion)
	}

	owners := []string{"resource:", "mechanism:", "invariant:", "frontend:", "sibling-api:", "deletion:"}
	seen, visible := map[string]bool{}, []string{}
	for _, capability := range inventory.Capabilities {
		if capability.ID == "" || seen[capability.ID] {
			t.Fatalf("empty or duplicate capability %q", capability.ID)
		}
		seen[capability.ID] = true
		owned := false
		for _, prefix := range owners {
			owned = owned || strings.HasPrefix(capability.Owner, prefix)
		}
		if !owned || strings.Contains(capability.Owner, "host-policy") {
			t.Fatalf("capability %q has invalid owner %q", capability.ID, capability.Owner)
		}
		if len(capability.Entrypoints) == 0 {
			t.Fatalf("capability %q has no live entrypoint", capability.ID)
		}
		for _, entrypoint := range capability.Entrypoints {
			assertLiveEntrypoint(t, root, capability.ID, entrypoint)
		}
		if capability.ModelVisible {
			visible = append(visible, capability.ID)
		}
	}
	if inventory.Metadata.SoleModelTool != "bashy" || !equalStrings(visible, []string{"sole-model-visible-tool"}) {
		t.Fatalf("model-visible capability surface = %v, tool = %q", visible, inventory.Metadata.SoleModelTool)
	}

	if len(inventory.StageCatalog) != len(knownStageCatalog) {
		t.Fatalf("owned stages=%d compiled stages=%d", len(inventory.StageCatalog), len(knownStageCatalog))
	}
	for stage := range knownStageCatalog {
		if inventory.StageCatalog[stage] == "" {
			t.Fatalf("compiled stage %q has no YAML owner", stage)
		}
	}
	wantForms := []string{"fallback", "forEach", "hook.invoke", "pipelineRef", "repeat", "stage", "switch"}
	if got := sortedKeys(inventory.RunForms); !equalStrings(got, wantForms) {
		t.Fatalf("run forms = %v, want %v", got, wantForms)
	}

	doc, err := Load(filepath.Join(root, inventory.Metadata.CanonicalFixture))
	if err != nil {
		t.Fatalf("canonical fixture: %v", err)
	}
	for _, frontend := range doc.Spec.Frontends {
		if inventory.Frontends[frontend.Kind] == "" {
			t.Fatalf("fixture frontend kind %q is unowned", frontend.Kind)
		}
	}
	assertNoForbiddenKernelReferences(t, root, inventory.ForbiddenKernelReferences)
}

func assertLiveEntrypoint(t *testing.T, root, capability, entrypoint string) {
	t.Helper()
	parts := strings.SplitN(entrypoint, "#", 2)
	if len(parts) != 2 {
		t.Fatalf("capability %q invalid entrypoint %q", capability, entrypoint)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(parts[0])))
	if err != nil {
		t.Fatalf("capability %q entrypoint %q: %v", capability, entrypoint, err)
	}
	if !bytes.Contains(data, []byte(parts[1])) {
		t.Fatalf("capability %q entrypoint %q is stale", capability, entrypoint)
	}
}

func assertNoForbiddenKernelReferences(t *testing.T, root string, forbidden []string) {
	t.Helper()
	roots := []string{"pipeline", "event", "provider", "bashy", "stages"}
	for _, name := range roots {
		dir := filepath.Join(root, "internal", "harness", name)
		err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, value := range forbidden {
				if bytes.Contains(data, []byte(value)) {
					return fmt.Errorf("%s contains forbidden kernel reference %q", path, value)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func sortedKeys[V any](values map[string]V) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
