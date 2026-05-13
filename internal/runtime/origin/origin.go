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
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/projectid"
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
//
// The ProjectID branch delegates to the projectid package so that
// OTEL attribution and on-disk per-project state (backlog, foreman,
// per-project settings) stay keyed by the same identifier.
func Resolve(ctx context.Context, cwd string, cfg *config.Config) Origin {
	o := Origin{ProjectRoot: absOrSame(cwd)}

	override := ""
	if cfg != nil && cfg.Project != nil {
		override = strings.TrimSpace(cfg.Project.ID)
	}
	remote, remoteOK := projectid.ReadGitRemote(ctx, cwd)
	normalized := ""
	if remoteOK {
		normalized = projectid.NormalizeRemote(remote)
	}
	o.ProjectID = projectid.Resolve(override, normalized, o.ProjectRoot)

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

// projectNameFromRemote returns the last path segment of a remote
// URL — `github.com/foo/bar` → `bar`. Falls back to "" if it can't
// extract anything sensible.
func projectNameFromRemote(raw string) string {
	id := projectid.NormalizeRemote(raw)
	if id == "" {
		return ""
	}
	if i := strings.LastIndex(id, "/"); i >= 0 && i < len(id)-1 {
		return id[i+1:]
	}
	return id
}

func absOrSame(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}
