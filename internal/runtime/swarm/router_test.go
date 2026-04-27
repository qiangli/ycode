package swarm

import (
	"fmt"
	"testing"
)

func TestRouter_Route_WithMock(t *testing.T) {
	router := NewRouter(func(prompt string) (string, error) {
		return "deploy", nil
	})

	workflows := []WorkflowInfo{
		{Name: "build", Description: "Build the project"},
		{Name: "deploy", Description: "Deploy to production"},
		{Name: "test", Description: "Run tests"},
	}

	result, err := router.Route("push to production", workflows, "build")
	if err != nil {
		t.Fatal(err)
	}
	if result != "deploy" {
		t.Errorf("expected deploy, got %s", result)
	}
}

func TestRouter_Route_FallbackOnError(t *testing.T) {
	router := NewRouter(func(prompt string) (string, error) {
		return "", fmt.Errorf("LLM error")
	})

	workflows := []WorkflowInfo{
		{Name: "build", Description: "Build the project"},
	}

	result, err := router.Route("test", workflows, "build")
	if err != nil {
		t.Fatal(err)
	}
	if result != "build" {
		t.Errorf("expected fallback to build, got %s", result)
	}
}

func TestRouter_Route_FallbackOnUnknown(t *testing.T) {
	router := NewRouter(func(prompt string) (string, error) {
		return "nonexistent", nil
	})

	workflows := []WorkflowInfo{
		{Name: "build", Description: "Build the project"},
	}

	result, err := router.Route("test", workflows, "build")
	if err != nil {
		t.Fatal(err)
	}
	if result != "build" {
		t.Errorf("expected fallback to build, got %s", result)
	}
}

func TestRouter_Route_NilRouteFunc(t *testing.T) {
	router := NewRouter(nil)
	result, err := router.Route("test", nil, "default")
	if err != nil {
		t.Fatal(err)
	}
	if result != "default" {
		t.Errorf("expected default, got %s", result)
	}
}

func TestRouter_Route_EmptyWorkflows(t *testing.T) {
	router := NewRouter(func(prompt string) (string, error) {
		return "something", nil
	})

	result, err := router.Route("test", nil, "fallback")
	if err != nil {
		t.Fatal(err)
	}
	if result != "fallback" {
		t.Errorf("expected fallback, got %s", result)
	}
}

func TestRouter_Route_CaseInsensitive(t *testing.T) {
	router := NewRouter(func(prompt string) (string, error) {
		return "DEPLOY", nil
	})

	workflows := []WorkflowInfo{
		{Name: "deploy", Description: "Deploy"},
	}

	result, err := router.Route("test", workflows, "build")
	if err != nil {
		t.Fatal(err)
	}
	if result != "deploy" {
		t.Errorf("expected deploy, got %s", result)
	}
}

func TestFormatRoutingPrompt(t *testing.T) {
	workflows := []WorkflowInfo{
		{Name: "build", Description: "Build the project"},
	}
	prompt := FormatRoutingPrompt("hello", workflows)
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
}
