//go:build unix

package bashy

// Sprint: #531; Story: #57; Story-ID: f7ce5d2016a9

import (
	"encoding/json"
	"strings"
	"testing"
)

// A governed delegate call runs as bashy's own delegate, uncontained: no
// `bashy contain` wrapper, so no podman and no network namespace. The allowed
// target names no launch template, so delegate fails closed (vendor) before
// any launch — after its guards admitted the genie caller, not denied it.
func TestGovernedDelegateRunsUncontained(t *testing.T) {
	executor, _ := genieDelegateHarness(t, "nosuchtool:m")
	result := runCall(t, executor, "bashy delegate --json --agent nosuchtool:m --timeout 30s 'review'")
	if result.Protocol != "ok" {
		t.Fatalf("delegate protocol = %q/%q", result.Protocol, result.Outcome)
	}
	if result.Intent == nil || !result.Intent.Complete {
		t.Fatalf("intent = %#v", result.Intent)
	}
	for _, command := range result.Intent.Commands {
		if command.Name != "delegate" {
			t.Fatalf("intent commands = %+v, want delegate alone", result.Intent.Commands)
		}
	}
	stdout := strings.TrimSpace(string(outputBytes(t, result)))
	var envelope struct {
		SchemaVersion string `json:"schema_version"`
		Error         *struct {
			Kind    string `json:"kind"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout[strings.LastIndex(stdout, "\n")+1:]), &envelope); err != nil {
		t.Fatalf("delegate stdout = %q: %v", stdout, err)
	}
	if envelope.SchemaVersion != "bashy-chat-v1" {
		t.Fatalf("delegate envelope = %+v (stdout %q)", envelope, stdout)
	}
	if envelope.Error != nil && (envelope.Error.Kind == "denied" || strings.Contains(envelope.Error.Message, "contain")) {
		t.Fatalf("delegate error = %+v, want the call admitted and uncontained", envelope.Error)
	}
}
