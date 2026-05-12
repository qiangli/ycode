// Package origin resolves the project identity and the entry-point
// agent tool that drove a ycode process, so OTEL signal can be
// attributed back to a project / repo / tool / foreign client.
//
// Resolution order:
//
//	ProjectID:    cfg.Project.ID  →  normalize(git remote origin)
//	                              →  "cwd-hash:<sha8 of abs cwd>"
//	ProjectName:  cfg.Project.Name → $YCODE_PROJECT_NAME
//	                               → basename of remote path
//	                               → basename of cwd
//	AgentTool:    $YCODE_AGENT_TOOL  →  currentAgentTool (set by
//	                                    cobra command before
//	                                    newApp()) →  "cli-other"
//	Personality:  cfg.Personality (passthrough)
//
// AgentClient is intentionally NOT resolved here — it's per-MCP-
// connection and set on the request context by the MCP server.
package origin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/git"
)

// Origin captures the static-per-process attribution attributes for
// OTEL signal. Resolved once at startup and carried into the OTEL
// provider as resource attributes.
type Origin struct {
	ProjectID   string
	ProjectName string
	ProjectRoot string
	AgentTool   string
	Personality string
}

// Bounded enum for AgentTool — matches the labels dashboards expect.
const (
	ToolTUI        = "tui"
	ToolPrompt     = "prompt"
	ToolServe      = "serve"
	ToolMCPServe   = "mcp-serve"
	ToolShell      = "shell"
	ToolWrap       = "wrap"
	ToolShellTrace = "shell-trace"
	ToolCLIOther   = "cli-other"
)

// currentAgentTool is set by each cobra subcommand's RunE before
// instantiating an App. Defaults to "cli-other" so subcommands that
// don't explicitly mark themselves stay categorized.
var (
	currentMu   sync.RWMutex
	currentTool = ToolCLIOther
)

// SetAgentTool is called by cobra command initializers (main.go,
// shell_cmd.go, serve.go, mcp.go, etc.) at process startup to
// declare which entry point is running. Subsequent Resolve() calls
// see this value unless YCODE_AGENT_TOOL is set.
func SetAgentTool(tool string) {
	currentMu.Lock()
	currentTool = tool
	currentMu.Unlock()
}

// CurrentAgentTool returns whatever was last set via SetAgentTool.
// Used by tests and by Resolve.
func CurrentAgentTool() string {
	currentMu.RLock()
	defer currentMu.RUnlock()
	return currentTool
}

// Resolve gathers the Origin from env + cwd + git + cfg. Cheap: at
// most one git invocation for the remote URL. Idempotent — callers
// can resolve once and pass the struct around.
func Resolve(ctx context.Context, cwd string, cfg *config.Config) Origin {
	o := Origin{ProjectRoot: absOrSame(cwd)}

	// project.id: explicit override > git remote > cwd-hash
	override := ""
	if cfg != nil && cfg.Project != nil {
		override = strings.TrimSpace(cfg.Project.ID)
	}
	remote, remoteOK := readGitRemote(ctx, cwd)
	switch {
	case override != "":
		o.ProjectID = override
	case remoteOK:
		o.ProjectID = NormalizeRemote(remote)
	default:
		o.ProjectID = cwdHashID(o.ProjectRoot)
	}

	// project.name: cfg > env > remote path basename > cwd basename
	switch {
	case cfg != nil && cfg.Project != nil && strings.TrimSpace(cfg.Project.Name) != "":
		o.ProjectName = strings.TrimSpace(cfg.Project.Name)
	case os.Getenv("YCODE_PROJECT_NAME") != "":
		o.ProjectName = os.Getenv("YCODE_PROJECT_NAME")
	case remoteOK:
		o.ProjectName = projectNameFromRemote(remote)
	default:
		o.ProjectName = filepath.Base(o.ProjectRoot)
	}

	// agent.tool: env override > currentAgentTool (cobra-set) > cli-other
	if v := strings.TrimSpace(os.Getenv("YCODE_AGENT_TOOL")); v != "" {
		o.AgentTool = v
	} else {
		o.AgentTool = CurrentAgentTool()
	}

	if cfg != nil {
		o.Personality = cfg.Personality
	}
	return o
}

// NormalizeRemote turns a git remote URL into a stable, secret-free
// identifier of the form "<host>/<path>". Both https and ssh shapes
// converge:
//
//	https://user:tok@github.com/foo/bar.git  →  github.com/foo/bar
//	git@github.com:foo/bar.git               →  github.com/foo/bar
//	ssh://git@gitlab.example.com:2222/x/y    →  gitlab.example.com/x/y
//	/local/path/repo                         →  /local/path/repo (unchanged)
func NormalizeRemote(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// SSH shorthand: git@host:path
	if strings.HasPrefix(raw, "git@") || (strings.HasPrefix(raw, "ssh://") && !strings.Contains(raw, "://")) {
		// only the strict shorthand (git@host:path) — fully-qualified ssh URLs go through url.Parse below
	}
	if strings.Contains(raw, "@") && !strings.Contains(raw, "://") {
		// git@github.com:foo/bar.git
		// Strip the user@ prefix and convert the colon to slash.
		atIdx := strings.Index(raw, "@")
		colonIdx := strings.Index(raw[atIdx:], ":")
		if colonIdx > 0 {
			host := raw[atIdx+1 : atIdx+colonIdx]
			path := raw[atIdx+colonIdx+1:]
			return strings.ToLower(host) + "/" + trimDotGit(path)
		}
	}
	// Anything that parses as a URL: take host + path; strip user info.
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return strings.ToLower(u.Host) + trimDotGit(u.Path)
	}
	// File path or unknown shape — return as-is, lower-cased only when
	// it doesn't look like a path.
	if strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, ".") {
		return trimDotGit(raw)
	}
	return strings.ToLower(trimDotGit(raw))
}

func trimDotGit(s string) string {
	return strings.TrimSuffix(s, ".git")
}

// projectNameFromRemote returns the last path segment of a remote
// URL — `github.com/foo/bar` → `bar`. Falls back to "" if it can't
// extract anything sensible.
func projectNameFromRemote(raw string) string {
	id := NormalizeRemote(raw)
	if id == "" {
		return ""
	}
	if i := strings.LastIndex(id, "/"); i >= 0 && i < len(id)-1 {
		return id[i+1:]
	}
	return id
}

// cwdHashID hashes the absolute cwd to a stable 8-hex-char identifier.
// Two checkouts of the same code at different paths get different
// IDs — that's the point (different working trees).
func cwdHashID(abs string) string {
	sum := sha256.Sum256([]byte(abs))
	return "cwd-hash:" + hex.EncodeToString(sum[:4])
}

func absOrSame(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// readGitRemote returns the `origin` remote URL of the repo at dir,
// or ok=false if not a git repo / no origin configured. Best-effort:
// errors are swallowed (origin tagging is non-critical).
func readGitRemote(ctx context.Context, dir string) (string, bool) {
	if dir == "" {
		return "", false
	}
	ge := git.NewGitExec(nil)
	out, err := ge.RunOutput(ctx, dir, "config", "--get", "remote.origin.url")
	if err != nil {
		return "", false
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "", false
	}
	return out, true
}
