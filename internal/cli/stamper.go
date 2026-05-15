package cli

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/qiangli/ycode/internal/runtime/origin"
	"github.com/qiangli/ycode/pkg/memex/memory"
	"github.com/qiangli/ycode/pkg/memex/qacache"
)

// appStamper populates Memory.Origin on save with provenance from the
// surrounding App: persona, host, project, session, and agent-tool. Read
// at stamp time, so a Stamper attached early in startup will pick up the
// persona once resolvePersona finishes.
type appStamper struct {
	app *App

	hostOnce sync.Once
	host     string

	originOnce sync.Once
	projectID  string
}

func newAppStamper(a *App) *appStamper {
	return &appStamper{app: a}
}

func (s *appStamper) Stamp(mem *memory.Memory) {
	if mem == nil {
		return
	}
	if mem.Origin == nil {
		mem.Origin = &memory.Origin{}
	}
	if mem.Origin.PersonaID == "" {
		if p := s.app.currentPersona; p != nil {
			mem.Origin.PersonaID = p.ID
		}
	}
	if mem.Origin.Host == "" {
		mem.Origin.Host = s.cachedHost()
	}
	if mem.Origin.ProjectID == "" {
		mem.Origin.ProjectID = s.cachedProjectID()
	}
	if mem.Origin.SessionID == "" {
		mem.Origin.SessionID = s.app.SessionID()
	}
	if mem.Origin.AgentTool == "" {
		mem.Origin.AgentTool = origin.CurrentAgentTool()
	}
}

func (s *appStamper) cachedHost() string {
	s.hostOnce.Do(func() {
		if h, err := os.Hostname(); err == nil {
			s.host = h
		}
	})
	return s.host
}

func (s *appStamper) cachedProjectID() string {
	s.originOnce.Do(func() {
		// Resolve project identity once; it does not change within a session.
		org := origin.Resolve(context.Background(), s.app.workDir, s.app.config)
		s.projectID = org.ProjectID
	})
	return s.projectID
}

// userMemoryDir returns the on-disk path for user-scope memories, given
// the persona ID. Empty personaID means "no user scope available". Format
// is ~/.agents/ycode/users/<personaID>/memory/ per the memory plan.
func userMemoryDir(personaID string) string {
	if personaID == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".agents", "ycode", "users", personaID, "memory")
}

// attachOriginPlumbing wires the Stamper and the user-scope store onto
// the memory manager. Idempotent and safe to call multiple times.
func (a *App) attachOriginPlumbing() {
	if a.memoryManager == nil {
		return
	}
	a.memoryManager.SetStamper(newAppStamper(a))

	if a.currentPersona == nil {
		return
	}
	if a.memoryManager.UserStore() != nil {
		return
	}
	dir := userMemoryDir(a.currentPersona.ID)
	if dir == "" {
		return
	}
	store, err := memory.NewStore(dir)
	if err != nil {
		slog.Warn("user memory store init failed", "dir", dir, "error", err)
		return
	}
	a.memoryManager.SetUserStore(store)
}

// projectQACacheDir returns the directory where the project-scoped Q→A
// cache lives. Empty when workDir is unknown.
func (a *App) projectQACacheDir() string {
	if a.workDir == "" {
		return ""
	}
	return filepath.Join(a.workDir, ".agents", "ycode", "qacache")
}

// ensureQAInjector constructs the project-scoped Q→A cache and injector
// once per App and kicks off the background promotion loop. Subsequent
// calls return the same injector. Returns nil only when the cache
// directory cannot be created — in which case the runtime treats the
// nil injector as a no-op.
func (a *App) ensureQAInjector() *qacache.Injector {
	a.qaInjectorOnce.Do(func() {
		dir := a.projectQACacheDir()
		if dir == "" {
			return
		}
		cache, err := qacache.New(dir)
		if err != nil {
			slog.Warn("qacache init failed", "dir", dir, "error", err)
			return
		}
		a.qaInjector = qacache.NewInjector(cache)

		// Promotion goroutine: walks ≥2-ask / ≥1-day candidates and
		// persists them as TypeReference memories so future recall hits
		// the memex (not just the cache). Cancelled on App.Close.
		if a.memoryManager != nil {
			saver := makeQAPromotionSaver(a.memoryManager)
			promoter := qacache.NewPromoter(cache, saver)
			ctx, cancel := context.WithCancel(context.Background())
			go promoter.Start(ctx, 30*time.Minute)
			a.RegisterCleanup(cancel)
		}
	})
	return a.qaInjector
}

// makeQAPromotionSaver returns a PromotionSaver that persists a cached
// entry as a TypeReference memory. The memory's SourceQ field carries
// the normalized question so later semantic-similarity recall finds it.
func makeQAPromotionSaver(mgr *memory.Manager) qacache.PromotionSaver {
	return func(_ context.Context, e *qacache.Entry) error {
		now := time.Now().UTC()
		mem := &memory.Memory{
			Name:        qacache.PromotedMemoryName(e),
			Description: shortDescription(e.Question),
			Type:        memoryTypeForClass(e.Class),
			Scope:       memory.ScopeProject,
			Content:     e.Answer,
			Importance:  0.5,
			SourceQ:     e.Canonical,
			Entities:    e.Entities,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		return mgr.Save(mem)
	}
}

// memoryTypeForClass maps a qacache QuestionClass to a memex Type.
// Time-relative answers become TypeEpisodic (they're about events);
// everything else maps to TypeReference (durable how-to knowledge).
func memoryTypeForClass(class qacache.QuestionClass) memory.Type {
	if class == qacache.ClassTimeRelative {
		return memory.TypeEpisodic
	}
	return memory.TypeReference
}

// shortDescription returns a truncated question suitable for the
// memex Description field, which is shown in index dumps.
func shortDescription(q string) string {
	const maxLen = 80
	q = strings.TrimSpace(q)
	if len(q) > maxLen {
		return q[:maxLen] + "…"
	}
	return q
}
