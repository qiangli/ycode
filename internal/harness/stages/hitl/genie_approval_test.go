package hitl

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/harness/spec"
)

// Exercise the actual instance builder and compiled policy used by bashy genie.
func TestGenieApprovalMode(t *testing.T) {
	genieDir, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "examples", "genie"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		mode, want string
	}{
		{"prompt", "ask"},
		{"auto", "allow"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			workspace, instance := t.TempDir(), filepath.Join(t.TempDir(), "instance")
			cmd := exec.Command("go", "run", "./cmd/genie", "-config", filepath.Join(genieDir, "agent.yaml"), "-workspace", workspace, "-instance-dir", instance)
			cmd.Dir = genieDir
			cmd.Env = append(os.Environ(), "GOWORK=off", "GENIE_APPROVAL="+tc.mode)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("build genie config: %v\n%s", err, out)
			}
			doc, err := spec.Load(strings.TrimSpace(string(out)))
			if err != nil {
				t.Fatal(err)
			}
			policy := doc.Spec.Policies["workspace"]
			for _, check := range []struct {
				name   string
				report Preflight
				want   string
			}{
				{"workspace deletion", report("delete", true, []string{"write", "destroy"}, []string{"workspace"}), tc.want},
				{"outside deletion", report("outside", true, []string{"write", "destroy"}, []string{"outside"}), "deny"},
				{"unscoped deletion", report("unscoped", true, []string{"destroy"}, nil), "deny"},
				{"network deletion", report("network", true, []string{"destroy", "net"}, []string{"workspace"}), "ask"},
				{"incomplete", report("incomplete", false, []string{"destroy"}, []string{"workspace"}), "deny"},
			} {
				_, got, err := decide(policy, check.report)
				if err != nil || got != check.want {
					t.Errorf("%s: decision = %q, want %q (error %v)", check.name, got, check.want, err)
				}
			}
		})
	}
}
