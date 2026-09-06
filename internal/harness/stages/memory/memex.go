package memory

import (
	"context"
	"errors"
	"fmt"

	memex "github.com/qiangli/ycode/pkg/memex"
	memexmemory "github.com/qiangli/ycode/pkg/memex/memory"
)

// MemexFacade adapts the existing facade without granting it scheduling or
// fallback authority. The harness supplies every scope, rank and bound.
type MemexFacade struct {
	memex *memex.Memex
}

func NewMemexFacade(handle *memex.Memex) (*MemexFacade, error) {
	if handle == nil || handle.Memory() == nil {
		return nil, errors.New("memory stages: nil memex facade")
	}
	return &MemexFacade{memex: handle}, nil
}

func (m *MemexFacade) Recall(ctx context.Context, query RecallQuery) ([]RecallHit, error) {
	if query.Ranking != "hybrid" {
		return nil, fmt.Errorf("memex recall: unsupported ranking %q", query.Ranking)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	all, err := m.memex.Memory().All()
	if err != nil {
		return nil, err
	}
	// Ask for the complete fused ranking before applying harness scopes; a
	// smaller pre-filter limit could let out-of-scope hits hide valid results.
	results, err := m.memex.Memory().Recall(query.Query, len(all))
	if err != nil {
		return nil, err
	}
	hits := make([]RecallHit, 0, query.MaxResults)
	for _, result := range results {
		if result.Memory == nil || !scopeAllowed(result.Memory, query.Scopes, query.AgentID) {
			continue
		}
		hits = append(hits, RecallHit{Memory: cloneMemory(result.Memory), Score: result.Score, Source: result.Source})
		if len(hits) == query.MaxResults {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return hits, nil
}

func (m *MemexFacade) Write(ctx context.Context, item *memexmemory.Memory) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.memex.Memory().Save(item); err != nil {
		return err
	}
	return nil
}

func scopeAllowed(item *memexmemory.Memory, scopes []string, agentID string) bool {
	for _, scope := range scopes {
		switch scope {
		case "workspace":
			if item.EffectiveScope() == memexmemory.ScopeProject {
				return true
			}
		case "user":
			if item.EffectiveScope() == memexmemory.ScopeUser {
				return true
			}
		case "team":
			if item.EffectiveScope() == memexmemory.ScopeTeam {
				return true
			}
		case "global":
			if item.EffectiveScope() == memexmemory.ScopeGlobal {
				return true
			}
		case "agent":
			if agentID != "" && item.Origin != nil && item.Origin.AgentTool == agentID {
				return true
			}
		}
	}
	return false
}
