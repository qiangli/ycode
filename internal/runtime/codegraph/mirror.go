package codegraph

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/qiangli/gfy/pkg/graph"
)

// graphMirror is the minimal interface internal/runtime/codegraph needs from
// a memex queryable graph store. *pkg/memex/graph.Graph satisfies it. By
// keeping the type structural, codegraph avoids importing pkg/memex/graph
// (which would forbid memex from importing codegraph in the future).
type graphMirror interface {
	Mutate(ctx context.Context, nquads []byte) (map[string]string, error)
	Upsert(ctx context.Context, queryDQL string, nquads []byte) error
	MirrorTarget()
}

// MirrorTo writes the gfy code-knowledge graph into a memex queryable store
// using the canonical code.* schema declared in pkg/memex/graph/schema.go.
//
// Public entry: callers can mirror manually after Build/Rebuild. The
// Manager.SetGraphTwin path also calls into this asynchronously after each
// successful Set.
func (gc *GraphContext) MirrorTo(ctx context.Context, mg graphMirror) error {
	return gc.mirrorTo(ctx, mg)
}

// mirrorTo is the unexported worker shared by Manager.mirrorAsync.
func (gc *GraphContext) mirrorTo(ctx context.Context, mg graphMirror) error {
	if gc == nil || gc.Graph == nil {
		return fmt.Errorf("codegraph.MirrorTo: nil graph context")
	}
	if mg == nil {
		return fmt.Errorf("codegraph.MirrorTo: nil memex graph")
	}

	communityOf := make(map[string]int, gc.Graph.NodeCount())
	for cid, members := range gc.Communities {
		for _, m := range members {
			communityOf[m] = cid
		}
	}

	const batchSize = 500
	var nodeBatch strings.Builder
	count := 0
	for _, id := range gc.Graph.Nodes() {
		writeNodeNQuads(&nodeBatch, id, gc.Graph.NodeAttrs(id), communityOf[id])
		count++
		if count%batchSize == 0 {
			if _, err := mg.Mutate(ctx, []byte(nodeBatch.String())); err != nil {
				return fmt.Errorf("codegraph mirror: nodes batch: %w", err)
			}
			nodeBatch.Reset()
		}
	}
	if nodeBatch.Len() > 0 {
		if _, err := mg.Mutate(ctx, []byte(nodeBatch.String())); err != nil {
			return fmt.Errorf("codegraph mirror: nodes final: %w", err)
		}
	}

	for _, e := range gc.Graph.Edges() {
		writeEdgeUpsert(ctx, mg, e)
	}

	slog.Debug("codegraph: mirrored to bonsai",
		"nodes", gc.Graph.NodeCount(), "edges", gc.Graph.EdgeCount())
	return nil
}

func writeNodeNQuads(b *strings.Builder, id string, attrs map[string]any, community int) {
	label := nquadEscape(id)
	kind := attrStr(attrs, "kind")
	if kind == "" {
		kind = attrStr(attrs, "type")
	}
	file := attrStr(attrs, "file")
	if file == "" {
		file = attrStr(attrs, "path")
	}
	tag := "_:n" + sanitizeBlankNode(id)
	fmt.Fprintf(b, "%s <code.label> %q .\n", tag, label)
	fmt.Fprintf(b, "%s <dgraph.type> \"CodeNode\" .\n", tag)
	if kind != "" {
		fmt.Fprintf(b, "%s <code.kind> %q .\n", tag, nquadEscape(kind))
	}
	if file != "" {
		fmt.Fprintf(b, "%s <code.file> %q .\n", tag, nquadEscape(file))
	}
	if community >= 0 {
		fmt.Fprintf(b, "%s <code.community> \"%d\"^^<xs:int> .\n", tag, community)
	}
}

func writeEdgeUpsert(ctx context.Context, mg graphMirror, e graph.EdgeData) {
	pred := edgeKindToPredicate(attrStr(e.Attrs, "type"))
	q := fmt.Sprintf(`{
  src(func: eq(code.label, %q)) { v as uid }
  dst(func: eq(code.label, %q)) { w as uid }
}`, e.Source, e.Target)
	mut := fmt.Sprintf("uid(v) <%s> uid(w) .\n", pred)
	if err := mg.Upsert(ctx, q, []byte(mut)); err != nil {
		slog.Debug("codegraph mirror: edge upsert", "src", e.Source, "tgt", e.Target, "err", err)
	}
}

func edgeKindToPredicate(kind string) string {
	switch strings.ToLower(kind) {
	case "uses", "type", "type_use":
		return "code.uses"
	default:
		return "code.calls"
	}
}

func nquadEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`)
	return r.Replace(s)
}

func sanitizeBlankNode(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
