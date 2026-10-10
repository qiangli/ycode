package hitl

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/harness/spec"
)

func TestGenieYoloProfile(t *testing.T) {
	t.Setenv("BASHY_HOME", t.TempDir())
	path := filepath.Join("..", "..", "..", "..", "examples", "genie", "agent-yolo.yaml")
	doc, err := spec.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Spec.Bashy.Execution.PermissionCeiling != "danger-full-access" || doc.Spec.Agents["coder"].PermissionCeiling != "danger-full-access" {
		t.Fatal("YOLO permission ceilings not compiled")
	}
	if !strings.Contains(doc.Spec.Sources["identity"].Resolved, "explicit YOLO harness profile") {
		t.Fatal("YOLO identity prompt did not compile")
	}
	for _, effect := range []string{"read", "write", "exec", "net", "destroy", "priv", "persist", "cred", "remote", "spend"} {
		_, got, err := decide(doc.Spec.Policies["workspace"], report("yolo", true, []string{effect}, []string{"workspace"}))
		if err != nil || got != "allow" {
			t.Errorf("%s: %s, %v", effect, got, err)
		}
	}
	_, outside, err := decide(doc.Spec.Policies["workspace"], report("outside", true, []string{"write", "destroy"}, []string{"userland"}))
	if err != nil || outside != "allow" {
		t.Fatalf("complete outside-workspace preflight: %s, %v", outside, err)
	}
	for _, preflight := range []Preflight{report("incomplete", false, []string{"exec"}, []string{"workspace"}), report("unscoped", true, []string{"destroy"}, nil)} {
		_, got, err := decide(doc.Spec.Policies["workspace"], preflight)
		if err != nil || got != "deny" {
			t.Errorf("mandatory bound: %s, %v", got, err)
		}
	}
	ordinary, err := spec.Load(filepath.Join(filepath.Dir(path), "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	_, got, err := decide(ordinary.Spec.Policies["workspace"], report("normal", true, []string{"net"}, []string{"workspace"}))
	if err != nil || got != "ask" {
		t.Fatalf("default policy changed: %s, %v", got, err)
	}
}
