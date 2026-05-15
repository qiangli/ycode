package cli

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/qiangli/ycode/internal/runtime/origin"
	"github.com/qiangli/ycode/pkg/memex/memory"
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
