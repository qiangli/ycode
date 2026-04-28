package containertool

import (
	"context"
	"testing"
	"time"
)

func TestToolAvailable_NilEngine(t *testing.T) {
	tool := &Tool{Name: "test", Engine: nil}
	if tool.Available() {
		t.Error("should not be available with nil engine")
	}
}

func TestToolEnsureImage_NoEngine(t *testing.T) {
	tool := &Tool{
		Name:       "test-tool",
		Image:      "ycode-test:never",
		Dockerfile: "FROM alpine\n",
		Sources:    map[string]string{},
		Engine:     nil,
	}

	err := tool.EnsureImage(context.Background())
	if err == nil {
		t.Error("expected error with nil engine")
	}
}

func TestToolEnsureImage_Idempotent(t *testing.T) {
	tool := &Tool{
		Name:       "idempotent-test",
		Image:      "ycode-idempotent-test:never",
		Dockerfile: "FROM nonexistent-base-image:latest\n",
		Sources:    map[string]string{},
		Engine:     nil,
	}

	err1 := tool.EnsureImage(context.Background())
	err2 := tool.EnsureImage(context.Background())

	if err1 == nil {
		t.Skip("unexpectedly succeeded")
	}
	if err1.Error() != err2.Error() {
		t.Errorf("EnsureImage not idempotent: first=%v, second=%v", err1, err2)
	}
}

func TestToolRun_NoEngine(t *testing.T) {
	tool := &Tool{
		Name:       "run-test",
		Image:      "ycode-test:never",
		Dockerfile: "FROM alpine\n",
		Engine:     nil,
	}

	_, err := tool.Run(context.Background(), []byte("{}"))
	if err == nil {
		t.Error("expected error with nil engine")
	}
}

func TestToolRunJSON_NoEngine(t *testing.T) {
	tool := &Tool{
		Name:       "json-test",
		Image:      "ycode-json-test:never",
		Dockerfile: "FROM alpine\n",
		Engine:     nil,
	}

	input := map[string]string{"hello": "world"}
	var output map[string]string
	err := tool.RunJSON(context.Background(), input, &output)
	if err == nil {
		t.Error("expected error with nil engine")
	}
}

func TestMountFormat(t *testing.T) {
	m := Mount{
		Source:   "/host/path",
		Target:   "/container/path",
		ReadOnly: true,
	}
	if m.Source != "/host/path" {
		t.Errorf("unexpected source: %s", m.Source)
	}
	if !m.ReadOnly {
		t.Error("expected read-only mount")
	}
}

func TestToolTimeout_Defaults(t *testing.T) {
	tool := &Tool{Name: "timeout-test", Image: "test:latest", Dockerfile: "FROM alpine\n"}
	if tool.BuildTimeout != 0 {
		t.Error("expected zero BuildTimeout as default")
	}
	if tool.RunTimeout != 0 {
		t.Error("expected zero RunTimeout as default")
	}
}

func TestToolSources(t *testing.T) {
	tool := &Tool{
		Name:       "source-test",
		Image:      "ycode-source-test:latest",
		Dockerfile: "FROM alpine\nRUN echo hello\n",
		Sources: map[string]string{
			"main.go": "package main\nfunc main() {}\n",
			"go.mod":  "module test\ngo 1.22\n",
		},
		BuildTimeout: 5 * time.Second,
	}

	if tool.Name != "source-test" {
		t.Error("unexpected name")
	}
	if len(tool.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(tool.Sources))
	}
}
