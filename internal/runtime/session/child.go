package session

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/qiangli/ycode/internal/runtime/fileops"
)

// ChildSession is a subagent session linked to a parent.
type ChildSession struct {
	*Session
	ParentID  string `json:"parent_id"`
	AgentType string `json:"agent_type,omitempty"`
	Depth     int    `json:"depth"`
}

// NewChildSession creates an isolated session for a subagent.
// Messages are stored in a subdirectory of the parent session:
//
//	{parentDir}/subagents/{childID}/messages.jsonl
func NewChildSession(parentDir string, parentID string, agentType string, depth int) (*ChildSession, error) {
	childID := uuid.New().String()
	childDir := filepath.Join(parentDir, "subagents", childID)
	if err := os.MkdirAll(childDir, 0o755); err != nil {
		return nil, fmt.Errorf("create child session dir: %w", err)
	}

	sess := &Session{
		ID:        childID,
		CreatedAt: time.Now(),
		Dir:       childDir,
	}

	child := &ChildSession{
		Session:   sess,
		ParentID:  parentID,
		AgentType: agentType,
		Depth:     depth,
	}

	// Write a metadata file linking back to parent.
	if err := child.writeMetadata(); err != nil {
		return nil, err
	}

	return child, nil
}

// writeMetadata writes the parent link to a metadata file in the child dir.
func (cs *ChildSession) writeMetadata() error {
	path := filepath.Join(cs.Dir, "metadata.json")
	data := fmt.Sprintf(`{"parent_id":%q,"agent_type":%q,"depth":%d,"created_at":%q}`+"\n",
		cs.ParentID, cs.AgentType, cs.Depth, cs.CreatedAt.Format(time.RFC3339))
	return fileops.AtomicWriteFile(path, []byte(data), 0o644)
}

// ListChildSessions returns metadata for all child sessions under a parent.
func ListChildSessions(parentDir string) ([]ChildSessionInfo, error) {
	subagentDir := filepath.Join(parentDir, "subagents")
	entries, err := os.ReadDir(subagentDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list child sessions: %w", err)
	}

	var children []ChildSessionInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info := ChildSessionInfo{
			ID:  entry.Name(),
			Dir: filepath.Join(subagentDir, entry.Name()),
		}
		// Try to read metadata.
		metaPath := filepath.Join(info.Dir, "metadata.json")
		if data, err := os.ReadFile(metaPath); err == nil {
			info.RawMetadata = data
		}
		children = append(children, info)
	}
	return children, nil
}

// ChildSessionInfo is a summary of a child session for listing.
type ChildSessionInfo struct {
	ID          string `json:"id"`
	Dir         string `json:"dir"`
	RawMetadata []byte `json:"metadata,omitempty"`
}
