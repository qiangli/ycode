package health

import (
	"strings"
	"testing"
)

func TestReadinessReport_AllReady(t *testing.T) {
	r := NewReadinessReport()
	r.Add("provider", StatusReady, "API key set")
	r.Add("tools", StatusReady, "12 tools loaded")

	if r.Overall != StatusReady {
		t.Errorf("expected ready, got %s", r.Overall)
	}
	if len(r.Subsystems) != 2 {
		t.Errorf("expected 2 subsystems, got %d", len(r.Subsystems))
	}
}

func TestReadinessReport_WarningPropagation(t *testing.T) {
	r := NewReadinessReport()
	r.Add("provider", StatusReady, "ok")
	r.Add("tools", StatusWarning, "some tools missing")

	if r.Overall != StatusWarning {
		t.Errorf("expected warning, got %s", r.Overall)
	}
}

func TestReadinessReport_BlockedOverridesWarning(t *testing.T) {
	r := NewReadinessReport()
	r.Add("provider", StatusWarning, "slow")
	r.Add("tools", StatusBlocked, "no tools")

	if r.Overall != StatusBlocked {
		t.Errorf("expected blocked, got %s", r.Overall)
	}
}

func TestReadinessReport_BlockedStaysBlocked(t *testing.T) {
	r := NewReadinessReport()
	r.Add("provider", StatusBlocked, "no key")
	r.Add("tools", StatusReady, "ok")

	if r.Overall != StatusBlocked {
		t.Errorf("expected blocked to persist, got %s", r.Overall)
	}
}

func TestReadinessReport_Format(t *testing.T) {
	r := NewReadinessReport()
	r.Add("provider", StatusReady, "ok")
	r.Add("tools", StatusWarning, "partial")
	r.Add("model", StatusBlocked, "missing")

	output := r.Format()
	if !strings.Contains(output, "Readiness: blocked") {
		t.Errorf("expected blocked in output, got: %s", output)
	}
	if !strings.Contains(output, "provider") {
		t.Error("expected provider in output")
	}
	if !strings.Contains(output, "tools") {
		t.Error("expected tools in output")
	}
}

func TestReadinessReport_Empty(t *testing.T) {
	r := NewReadinessReport()
	if r.Overall != StatusReady {
		t.Errorf("empty report should be ready, got %s", r.Overall)
	}
	output := r.Format()
	if !strings.Contains(output, "Readiness: ready") {
		t.Errorf("expected ready in output, got: %s", output)
	}
}
