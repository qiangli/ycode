package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/qiangli/ycode/internal/runtime/codegraph"
)

// RegisterGraphHandlers registers code graph query tool handlers.
// The manager provides thread-safe access to the live code knowledge graph.
// Tools trigger a background rebuild if the graph is stale from code changes.
func RegisterGraphHandlers(r *Registry, mgr *codegraph.Manager) {
	noGraph := "No code graph available. Run /init to build one, or the graph will be built automatically as you edit code."

	registerGraphTool(r, "query_graph", mgr, noGraph, func(gc *codegraph.GraphContext, input json.RawMessage) (string, error) {
		var params struct {
			Question string `json:"question"`
			Depth    int    `json:"depth"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse query_graph input: %w", err)
		}
		return gc.QueryGraph(params.Question, params.Depth), nil
	})

	registerGraphTool(r, "get_node", mgr, noGraph, func(gc *codegraph.GraphContext, input json.RawMessage) (string, error) {
		var params struct {
			Label string `json:"label"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse get_node input: %w", err)
		}
		return gc.GetNode(params.Label), nil
	})

	registerGraphTool(r, "get_neighbors", mgr, noGraph, func(gc *codegraph.GraphContext, input json.RawMessage) (string, error) {
		var params struct {
			Label          string `json:"label"`
			RelationFilter string `json:"relation_filter"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse get_neighbors input: %w", err)
		}
		return gc.GetNeighbors(params.Label, params.RelationFilter), nil
	})

	registerGraphTool(r, "get_community", mgr, noGraph, func(gc *codegraph.GraphContext, input json.RawMessage) (string, error) {
		var params struct {
			CommunityID int `json:"community_id"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse get_community input: %w", err)
		}
		return gc.GetCommunity(params.CommunityID), nil
	})

	registerGraphTool(r, "god_nodes", mgr, noGraph, func(gc *codegraph.GraphContext, input json.RawMessage) (string, error) {
		var params struct {
			TopN int `json:"top_n"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse god_nodes input: %w", err)
		}
		return gc.GetGodNodes(params.TopN), nil
	})

	registerGraphTool(r, "graph_stats", mgr, noGraph, func(gc *codegraph.GraphContext, input json.RawMessage) (string, error) {
		return gc.GetGraphStats(), nil
	})

	registerGraphTool(r, "shortest_path", mgr, noGraph, func(gc *codegraph.GraphContext, input json.RawMessage) (string, error) {
		var params struct {
			Source  string `json:"source"`
			Target  string `json:"target"`
			MaxHops int    `json:"max_hops"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse shortest_path input: %w", err)
		}
		return gc.ShortestPath(params.Source, params.Target, params.MaxHops), nil
	})
}

type graphToolFunc func(gc *codegraph.GraphContext, input json.RawMessage) (string, error)

func registerGraphTool(r *Registry, name string, mgr *codegraph.Manager, noGraphMsg string, fn graphToolFunc) {
	spec, ok := r.Get(name)
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		// Trigger background rebuild if graph is stale from code changes.
		mgr.RebuildIfDirty(ctx)

		gc := mgr.Get()
		if gc == nil {
			return noGraphMsg, nil
		}
		return fn(gc, input)
	}
}
