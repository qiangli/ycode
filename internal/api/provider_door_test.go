package api

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/yoke/pkg/broker/door"
)

func testDoor(t *testing.T) (string, func()) {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" || r.Header.Get("Authorization") != "Bearer test-owner-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"schema_version":"bashy-broker-health-v1"}`)
	}))
	t.Cleanup(s.Close)
	_, port, err := net.SplitHostPort(strings.TrimPrefix(s.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(door.PortEnv, port)
	t.Setenv("BASHY_HOME", t.TempDir())
	path, err := door.TokenPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("test-owner-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return door.BaseURL(), s.Close
}

func TestDetectProvider_DoorUp(t *testing.T) {
	clearEnv(t)
	base, _ := testDoor(t)
	cfg, err := DetectProvider("door-example")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Kind != ProviderOpenAI || cfg.BaseURL != base+"/v1" || cfg.APIKey != "test-owner-token" {
		t.Fatalf("door config: kind=%q base=%q key match=%v", cfg.Kind, cfg.BaseURL, cfg.APIKey == "test-owner-token")
	}
}

func TestDetectProvider_AnthropicDoorUp(t *testing.T) {
	clearEnv(t)
	base, _ := testDoor(t)
	cfg, err := DetectProvider("claude-example")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Kind != ProviderAnthropic || cfg.BaseURL != base+"/anthropic" || cfg.APIKey != "test-owner-token" {
		t.Fatalf("anthropic door config: kind=%q base=%q key match=%v", cfg.Kind, cfg.BaseURL, cfg.APIKey == "test-owner-token")
	}
}

func TestDetectProvider_ExplicitEnvBeatsDoor(t *testing.T) {
	clearEnv(t)
	testDoor(t)
	t.Setenv("OPENAI_BASE_URL", "http://localhost:9999/v1")
	t.Setenv("OPENAI_API_KEY", "explicit-key")
	cfg, err := DetectProvider("door-example")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "http://localhost:9999/v1" || cfg.APIKey != "explicit-key" {
		t.Fatalf("explicit config lost: base=%q key match=%v", cfg.BaseURL, cfg.APIKey == "explicit-key")
	}
}

func TestDetectProvider_DoorDownFailsFast(t *testing.T) {
	clearEnv(t)
	_, closeDoor := testDoor(t)
	closeDoor()
	start := time.Now()
	_, err := DetectProvider("L4")
	if err == nil || !strings.Contains(err.Error(), "model door is not running; start it with: bashy llm up") {
		t.Fatalf("door-down error = %v", err)
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("door-down detection took %s", d)
	}
}
