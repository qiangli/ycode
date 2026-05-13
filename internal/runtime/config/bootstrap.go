package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/projectid"
)

// BootstrapLoader assembles a four-tier Loader, computing the per-project
// tier directory from the logical project id. Resolution order for
// project id (mirrors origin.Resolve and projectid.ResolveFromCwd):
//
//  1. cfg.Project.ID declared in user-global settings.json
//  2. normalized git remote origin URL at cwd
//  3. cwd-hash:<sha8> fallback
//
// A Project.ID declared in any non-user-global tier is intentionally
// ignored when computing which per-project file to load — otherwise
// the file you're trying to find would determine its own location.
//
// homeAgents is typically ~/.agents/ycode; userDir is ~/.config/ycode;
// projectDir and localDir are usually both <cwd>/.agents/ycode/.
//
// Returns the configured Loader and the resolved project id (useful
// for callers that need to locate sibling state like backlog/foreman).
func BootstrapLoader(ctx context.Context, userDir, homeAgents, cwd, projectDir, localDir string) (*Loader, string) {
	userOverride := peekProjectIDFromFile(filepath.Join(userDir, "settings.json"))
	id := projectid.ResolveFromCwd(ctx, cwd, userOverride)
	perProjectDir := ""
	if homeAgents != "" && id != "" {
		perProjectDir = projectid.StateDir(homeAgents, id)
	}
	return NewLoaderWithPerProject(userDir, perProjectDir, projectDir, localDir), id
}

// peekProjectIDFromFile reads only the project.id field from a settings
// file without fully parsing or merging. Returns "" if the file is
// absent, unparseable, or the field is unset.
func peekProjectIDFromFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var peek struct {
		Project *ProjectConfig `json:"project,omitempty"`
	}
	if err := json.Unmarshal(data, &peek); err != nil {
		return ""
	}
	if peek.Project == nil {
		return ""
	}
	return strings.TrimSpace(peek.Project.ID)
}
