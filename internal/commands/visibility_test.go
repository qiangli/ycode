package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/features"
)

// hiddenCommands is the inventory of commands withheld from the user-facing
// listings, and WHY each one is withheld.
//
// This map is the point. Hiding a command is a product decision, so making it
// (or un-making it) has to be a visible, one-line diff here — not a silent
// consequence of editing a handler. A command that quietly appears or vanishes
// from this table fails the test below.
var hiddenCommands = map[string]features.Tier{
	// Nothing behind them at all — the handler returns a canned string.
	"plugin": features.TierWIP, // internal/plugins is never instantiated
	"loop":   features.TierWIP, // no timer, no goroutine, nothing to stop
	"team":   features.TierWIP, // registry is built and discarded at app.go:319
	"cron":   features.TierWIP, // same
	"clear":  features.TierWIP, // internal/runtime/session has no clear/truncate API

	// Real work exists to do, and it is small — but not done yet.
	"doctor": features.TierExperimental, // real checks live in the cobra `ycode doctor`
	"skills": features.TierExperimental, // skillengine discovery is unwired; install-bundled FAKES success
}

func TestTieredCommandInventory(t *testing.T) {
	r := newTestRegistry(t)

	for _, spec := range r.ListAll() {
		want, shouldHide := hiddenCommands[spec.Name]
		switch {
		case shouldHide && !spec.Hidden():
			t.Errorf("/%s is listed as hidden but its Tier is %q — it would be advertised to users", spec.Name, spec.Tier)
		case shouldHide && spec.Tier != want:
			t.Errorf("/%s tier = %q, want %q", spec.Name, spec.Tier, want)
		case !shouldHide && spec.Hidden():
			t.Errorf("/%s is hidden (tier %q) but is not in hiddenCommands — "+
				"if that is deliberate, add it there with a reason; if not, drop the Tier line",
				spec.Name, spec.Tier)
		}
	}

	// Every name in the table must exist, or the table is describing commands
	// that were renamed or deleted and is silently hiding nothing.
	for name := range hiddenCommands {
		if _, ok := r.Get(name); !ok {
			t.Errorf("hiddenCommands names /%s, which is not registered", name)
		}
	}
}

// A hidden command must vanish from the listings but still RUN when typed.
// Losing either half defeats the design: the first would advertise stubs, the
// second would delete work in progress.
func TestHiddenCommandsAreListedNowhereButStillDispatch(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()

	visible := map[string]bool{}
	for _, spec := range r.List() {
		visible[spec.Name] = true
	}

	for name := range hiddenCommands {
		if visible[name] {
			t.Errorf("/%s is hidden but appears in List()", name)
		}
		if _, ok := r.Get(name); !ok {
			t.Errorf("/%s is hidden and also unreachable — Get() must still find it", name)
		}
		if _, err := r.Execute(ctx, name, ""); err != nil {
			t.Errorf("/%s is hidden but no longer runs: %v", name, err)
		}
	}
}

func TestHelpListsVisibleCommands(t *testing.T) {
	r := newTestRegistry(t)

	output, err := r.Execute(context.Background(), "help", "")
	if err != nil {
		t.Fatalf("/help error: %v", err)
	}
	for _, spec := range r.List() {
		if !strings.Contains(output, "/"+spec.Name) {
			t.Errorf("/help omits the visible command /%s", spec.Name)
		}
	}
	for _, name := range []string{"/quit", "/exit"} {
		if !strings.Contains(output, name) {
			t.Errorf("/help omits %s", name)
		}
	}
}

// The regression guard that actually matters: /help must not name a stub.
func TestHelpOmitsHiddenCommands(t *testing.T) {
	r := newTestRegistry(t)

	output, err := r.Execute(context.Background(), "help", "")
	if err != nil {
		t.Fatalf("/help error: %v", err)
	}
	for name := range hiddenCommands {
		if strings.Contains(output, "/"+name) {
			t.Errorf("/help advertises /%s, which is hidden", name)
		}
	}
}

// Ranging a map made this output differ between runs, which breaks diffing and
// any golden test built on it.
func TestHelpOutputIsDeterministic(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()

	first, err := r.Execute(ctx, "help", "")
	if err != nil {
		t.Fatalf("/help error: %v", err)
	}
	for i := 0; i < 20; i++ {
		got, err := r.Execute(ctx, "help", "")
		if err != nil {
			t.Fatalf("/help error on run %d: %v", i, err)
		}
		if got != first {
			t.Fatalf("/help output is not stable across runs\nfirst:\n%s\nrun %d:\n%s", first, i, got)
		}
	}
}

// The one thing that keeps "one chokepoint" true a year from now.
//
// ListAll() exists for inventory and diagnostics. The moment a user-facing
// listing calls it, hidden commands are advertised again and every test above
// still passes — because they assert on the registry, not on the caller. So
// assert on the callers directly.
func TestPresentationSurfacesDoNotUseListAll(t *testing.T) {
	surfaces := []string{
		"../cli/completion.go",
		"../cli/commandpalette.go",
		"../cli/skillrouter.go",
		"../shell/manifest.go",
		"../shell/dispatcher.go",
		"../shell/completion.go",
	}
	for _, path := range surfaces {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Base(path), err)
		}
		if strings.Contains(string(src), ".ListAll(") {
			t.Errorf("%s calls ListAll() — user-facing surfaces must use List(), "+
				"or hidden commands get advertised", path)
		}
	}
}
