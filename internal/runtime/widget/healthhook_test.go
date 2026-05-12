package widget

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/bus"
)

func TestHealthHook_BootstrapEmitsSchemaPlusData(t *testing.T) {
	b := bus.NewMemoryBus()
	ch, unsub := b.Subscribe(bus.EventStateUpdate)
	defer unsub()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hook := NewHealthHook(b, "sess", func(_ context.Context) HealthData {
		return HealthData{
			YcodeVersion: "test-v",
			AlertsFiring: 3,
			Sessions:     1,
			Incidents:    []HealthRow{{Primary: "BadDeploy", Secondary: "auth p99 700ms"}},
			Deploys:      []HealthRow{{Primary: "abc1234", Secondary: "fix login", Caption: "5m ago"}},
		}
	})
	// Skip the periodic refresh in tests.
	hook.SetRefreshInterval(time.Hour)
	hook.Start(ctx)

	select {
	case ev := <-ch:
		if ev.SessionID != "sess" {
			t.Errorf("session = %q want sess", ev.SessionID)
		}
		// payload is the iframe/a2ui-discriminated wrapper; the body
		// is the {"a2ui_operations":[...]} container.
		var p struct {
			Format string          `json:"format"`
			Body   json.RawMessage `json:"body"`
			Origin string          `json:"origin"`
		}
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			t.Fatal(err)
		}
		if p.Format != "a2ui" {
			t.Errorf("format = %q want a2ui", p.Format)
		}
		if p.Origin != "ycode-health" {
			t.Errorf("origin = %q want ycode-health", p.Origin)
		}
		var wrap struct {
			Ops []map[string]any `json:"a2ui_operations"`
		}
		if err := json.Unmarshal(p.Body, &wrap); err != nil {
			t.Fatal(err)
		}
		// Bootstrap should emit 3 ops: createSurface + updateComponents + updateDataModel.
		if len(wrap.Ops) != 3 {
			t.Fatalf("bootstrap op count = %d want 3 (createSurface + updateComponents + updateDataModel)", len(wrap.Ops))
		}
		if _, ok := wrap.Ops[0]["createSurface"]; !ok {
			t.Errorf("first bootstrap op should be createSurface, got %v", wrap.Ops[0])
		}
		if _, ok := wrap.Ops[1]["updateComponents"]; !ok {
			t.Errorf("second bootstrap op should be updateComponents, got %v", wrap.Ops[1])
		}
		dm, ok := wrap.Ops[2]["updateDataModel"].(map[string]any)
		if !ok {
			t.Errorf("third bootstrap op should be updateDataModel, got %v", wrap.Ops[2])
		}
		// Spot-check the data model carried the provider's payload.
		s := string(p.Body)
		for _, want := range []string{"test-v", "BadDeploy", "abc1234", "fix login"} {
			if !strings.Contains(s, want) {
				t.Errorf("data model body missing %q", want)
			}
		}
		_ = dm
	case <-time.After(2 * time.Second):
		t.Fatal("no bootstrap event published")
	}
}

func TestHealthHook_NilProviderEmitsEmptyData(t *testing.T) {
	// Nil dataProvider is the smoke/demo case — the surface should
	// still bootstrap with the schema and an empty payload.
	b := bus.NewMemoryBus()
	ch, unsub := b.Subscribe(bus.EventStateUpdate)
	defer unsub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hook := NewHealthHook(b, "s", nil)
	hook.SetRefreshInterval(time.Hour)
	hook.Start(ctx)

	select {
	case ev := <-ch:
		var p struct {
			Body json.RawMessage `json:"body"`
		}
		_ = json.Unmarshal(ev.Data, &p)
		// Empty data → no "BadDeploy" / no numbers but no crash either.
		if !strings.Contains(string(p.Body), `"updateDataModel"`) {
			t.Errorf("expected updateDataModel op even with nil provider; got %s", p.Body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event published")
	}
}

func TestHealthHook_NilBusIsNoOp(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil-bus Start panicked: %v", r)
		}
	}()
	NewHealthHook(nil, "x", nil).Start(context.Background())
}

func TestHealthComponents_IsValidJSON(t *testing.T) {
	// Surface schema is a literal JSON blob in source — a parse failure
	// would surface as a runtime panic inside Start(). Compile-test it
	// at package init by calling the function once.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("healthComponents() panicked: %v", r)
		}
	}()
	comps := healthComponents()
	if len(comps) < 10 {
		t.Errorf("expected ≥10 components in health schema, got %d", len(comps))
	}
	// First component must be id=root — the renderer entry point.
	var first map[string]any
	if err := json.Unmarshal(comps[0], &first); err != nil {
		t.Fatal(err)
	}
	if first["id"] != "root" {
		t.Errorf("first component should be id=root, got %v", first["id"])
	}
}
