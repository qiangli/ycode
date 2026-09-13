// Package memory implements explicit YAML harness memory and compaction stages.
package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/message"
	"github.com/qiangli/ycode/internal/harness/spec"
	memexmemory "github.com/qiangli/ycode/pkg/memex/memory"
)

const SchemaVersion = "ycode.memory-stage/v1"

type Facade interface {
	Recall(context.Context, RecallQuery) ([]RecallHit, error)
	Write(context.Context, *memexmemory.Memory) error
}

type RecallQuery struct {
	Query      string
	Scopes     []string
	Ranking    string
	MaxResults int
	AgentID    string
}

type RecallHit struct {
	Memory *memexmemory.Memory
	Score  float64
	Source string
}

type TokenCounter interface {
	CountMessages([]message.Message) (int, error)
}

type Summarizer interface {
	Summarize(context.Context, string, string, []message.Message, string) (string, error)
}

type Config struct {
	Document *spec.Document
	Events   *event.Store
	Payloads *event.PayloadStore
	Facade   Facade
	Tokens   TokenCounter
	Summary  Summarizer
}

type Engine struct {
	memories map[string]spec.Memory
	routes   map[string]spec.Route
	models   map[string]spec.Model
	sources  map[string]spec.Source
	events   *event.Store
	payloads *event.PayloadStore
	facade   Facade
	tokens   TokenCounter
	summary  Summarizer
}

func New(config Config) (*Engine, error) {
	if config.Document == nil || config.Events == nil || config.Payloads == nil || config.Facade == nil || config.Tokens == nil {
		return nil, errors.New("memory stages require compiled document, stores, facade and token counter")
	}
	return &Engine{memories: cloneMemories(config.Document.Spec.Memories), routes: cloneRoutes(config.Document.Spec.Routes), models: cloneModels(config.Document.Spec.Models), sources: cloneSources(config.Document.Spec.Sources), events: config.Events, payloads: config.Payloads, facade: config.Facade, tokens: config.Tokens, summary: config.Summary}, nil
}

func (e *Engine) BindSummary(summary Summarizer) {
	e.summary = summary
}

type Meta struct {
	SessionID    string
	RunID        string
	StageID      string
	ConfigDigest string
}

func (m Meta) validate() error {
	if m.SessionID == "" || m.RunID == "" || m.StageID == "" || m.ConfigDigest == "" {
		return errors.New("memory stage requires session, run, stage and config digest")
	}
	return nil
}

type RecalledItem struct {
	Name       string  `json:"name"`
	Scope      string  `json:"scope"`
	Score      float64 `json:"score"`
	Source     string  `json:"source"`
	PayloadRef string  `json:"payload_ref"`
	Tokens     int     `json:"tokens"`
}

type RecallResult struct {
	SchemaVersion string         `json:"schema_version"`
	MemoryRef     string         `json:"memory_ref"`
	QueryRef      string         `json:"query_ref"`
	Items         []RecalledItem `json:"items"`
	Tokens        int            `json:"tokens"`
}

func (e *Engine) Recall(ctx context.Context, meta Meta, memoryRef, query, agentID string) (RecallResult, error) {
	if err := meta.validate(); err != nil {
		return RecallResult{}, err
	}
	configured, err := e.memory(memoryRef)
	if err != nil {
		return RecallResult{}, err
	}
	if strings.TrimSpace(query) == "" || len(configured.Recall.Scopes) == 0 || configured.Recall.Ranking != "hybrid" || configured.Recall.MaxItems <= 0 || configured.Recall.MaxTokens <= 0 {
		return RecallResult{}, errors.New("memory recall: incomplete or unsupported compiled policy")
	}
	if err := validateScopes(configured.Recall.Scopes); err != nil {
		return RecallResult{}, err
	}
	queryRef, err := e.payloads.Put([]byte(query))
	if err != nil {
		return RecallResult{}, err
	}
	hits, err := e.facade.Recall(ctx, RecallQuery{Query: query, Scopes: append([]string(nil), configured.Recall.Scopes...), Ranking: configured.Recall.Ranking, MaxResults: configured.Recall.MaxItems, AgentID: agentID})
	if err != nil {
		_, _ = e.append(meta, "memory.recall.failed", map[string]any{"schema_version": SchemaVersion, "memory_ref": memoryRef, "query_ref": queryRef, "error": err.Error()})
		return RecallResult{}, err
	}
	result := RecallResult{SchemaVersion: SchemaVersion, MemoryRef: memoryRef, QueryRef: queryRef}
	for _, hit := range hits {
		if hit.Memory == nil || len(result.Items) == configured.Recall.MaxItems {
			break
		}
		encoded, err := json.Marshal(hit.Memory)
		if err != nil {
			return RecallResult{}, err
		}
		count, err := e.tokens.CountMessages([]message.Message{{Role: message.RoleSystem, Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: hit.Memory.Content}}}})
		if err != nil || count < 0 {
			return RecallResult{}, fmt.Errorf("memory recall: token count for %q failed", hit.Memory.Name)
		}
		if result.Tokens+count > configured.Recall.MaxTokens {
			continue
		}
		payloadRef, err := e.payloads.Put(encoded)
		if err != nil {
			return RecallResult{}, err
		}
		result.Tokens += count
		result.Items = append(result.Items, RecalledItem{Name: hit.Memory.Name, Scope: string(hit.Memory.EffectiveScope()), Score: hit.Score, Source: hit.Source, PayloadRef: payloadRef, Tokens: count})
	}
	_, err = e.append(meta, "memory.recalled", result)
	return result, err
}

type WriteResult struct {
	SchemaVersion string   `json:"schema_version"`
	MemoryRef     string   `json:"memory_ref"`
	PayloadRefs   []string `json:"payload_refs"`
}

func (e *Engine) Write(ctx context.Context, meta Meta, memoryRef string, items []*memexmemory.Memory) (WriteResult, error) {
	if err := meta.validate(); err != nil {
		return WriteResult{}, err
	}
	configured, err := e.memory(memoryRef)
	if err != nil {
		return WriteResult{}, err
	}
	if configured.Write.MaxItems <= 0 || configured.Write.MaxBytes <= 0 || len(items) == 0 || len(items) > configured.Write.MaxItems {
		return WriteResult{}, errors.New("memory write: item bound violated")
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return WriteResult{}, err
	}
	if len(encoded) > configured.Write.MaxBytes {
		return WriteResult{}, errors.New("memory write: byte bound violated")
	}
	result := WriteResult{SchemaVersion: SchemaVersion, MemoryRef: memoryRef}
	for position, item := range items {
		if item == nil || item.Name == "" || item.Content == "" {
			return WriteResult{}, errors.New("memory write: incomplete item")
		}
		if !writeScopeAllowed(item, configured.Recall.Scopes) {
			return WriteResult{}, fmt.Errorf("memory write: scope %q is outside compiled memory scopes", item.EffectiveScope())
		}
		data, err := json.Marshal(item)
		if err != nil {
			return WriteResult{}, err
		}
		ref, err := e.payloads.Put(data)
		if err != nil {
			return WriteResult{}, err
		}
		if _, err = e.append(meta, "memory.write.requested", map[string]any{"schema_version": SchemaVersion, "memory_ref": memoryRef, "position": position, "payload_ref": ref}); err != nil {
			return WriteResult{}, err
		}
		if err = e.facade.Write(ctx, cloneMemory(item)); err != nil {
			_, _ = e.append(meta, "memory.write.failed", map[string]any{"schema_version": SchemaVersion, "memory_ref": memoryRef, "position": position, "payload_ref": ref, "error": err.Error()})
			return WriteResult{}, err
		}
		if _, err = e.append(meta, "memory.write.completed", map[string]any{"schema_version": SchemaVersion, "memory_ref": memoryRef, "position": position, "payload_ref": ref}); err != nil {
			return WriteResult{}, err
		}
		result.PayloadRefs = append(result.PayloadRefs, ref)
	}
	return result, nil
}

type Measurement struct {
	SchemaVersion  string                `json:"schema_version"`
	PayloadRef     string                `json:"payload_ref"`
	Tokens         int                   `json:"tokens"`
	ContextBudget  int                   `json:"context_budget"`
	TruncateBudget int                   `json:"truncate_budget"`
	Measured       MeasurementProvenance `json:"measured"`
}

type MeasurementProvenance struct {
	Source         string  `json:"source"`
	ProviderTokens int     `json:"provider_tokens"`
	EstimatedTail  int     `json:"estimated_tail"`
	Margin         float64 `json:"margin"`
}

type MeasureRequest struct {
	MemoryRef    string
	RouteRef     string
	SafetyMargin float64
	Messages     []message.Message
}

func (e *Engine) Measure(_ context.Context, meta Meta, request MeasureRequest) (Measurement, error) {
	if err := meta.validate(); err != nil {
		return Measurement{}, err
	}
	if request.SafetyMargin <= 0 {
		return Measurement{}, errors.New("context measure: safety margin must be positive")
	}
	memoryConfig, err := e.memory(request.MemoryRef)
	if err != nil {
		return Measurement{}, err
	}
	route, ok := e.routes[request.RouteRef]
	if !ok || len(route.Attempts) == 0 {
		return Measurement{}, fmt.Errorf("context measure: undeclared route %q", request.RouteRef)
	}
	model, ok := e.models[route.Attempts[0].ModelRef]
	if !ok {
		return Measurement{}, fmt.Errorf("context measure: undeclared model %q", route.Attempts[0].ModelRef)
	}
	encoded, err := json.Marshal(request.Messages)
	if err != nil {
		return Measurement{}, err
	}
	ref, err := e.payloads.Put(encoded)
	if err != nil {
		return Measurement{}, err
	}
	count, measured, err := e.measureMessages(request.Messages, request.SafetyMargin)
	if err != nil {
		return Measurement{}, errors.New("context measure: token counter failed")
	}
	result := Measurement{SchemaVersion: SchemaVersion, PayloadRef: ref, Tokens: count, ContextBudget: model.Limits.ContextTokens - route.Budget.MaxOutputTokens - memoryConfig.Compaction.ReserveTokens, Measured: measured}
	if result.ContextBudget < 0 {
		return Measurement{}, errors.New("context measure: computed context budget is negative")
	}
	result.TruncateBudget = result.ContextBudget - memoryConfig.Compaction.ReserveTokens
	if result.TruncateBudget < 0 {
		result.TruncateBudget = 0
	}
	_, err = e.append(meta, "context.measured", result)
	return result, err
}

func (e *Engine) measureMessages(messages []message.Message, margin float64) (int, MeasurementProvenance, error) {
	lastUsage := -1
	providerTokens := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == message.RoleAssistant && messages[i].Usage != nil {
			lastUsage = i
			providerTokens = usageTotal(*messages[i].Usage)
			break
		}
	}
	if lastUsage < 0 {
		estimated, err := e.tokens.CountMessages(messages)
		if err != nil || estimated < 0 {
			return 0, MeasurementProvenance{}, err
		}
		total := applyMargin(estimated, margin)
		return total, MeasurementProvenance{Source: "estimate", EstimatedTail: estimated, Margin: margin}, nil
	}
	tail, err := e.tokens.CountMessages(messages[lastUsage+1:])
	if err != nil || tail < 0 {
		return 0, MeasurementProvenance{}, err
	}
	total := providerTokens + applyMargin(tail, margin)
	return total, MeasurementProvenance{Source: "provider", ProviderTokens: providerTokens, EstimatedTail: tail, Margin: margin}, nil
}

func usageTotal(usage message.TokenUsage) int {
	return usage.InputTokens + usage.CacheReadInput + usage.CacheCreationInput + usage.OutputTokens
}

func applyMargin(tokens int, margin float64) int {
	value := float64(tokens) * margin
	if value == float64(int(value)) {
		return int(value)
	}
	return int(value) + 1
}

func (e *Engine) memory(ref string) (spec.Memory, error) {
	configured, ok := e.memories[ref]
	if !ok || configured.Provider != "memex" {
		return spec.Memory{}, fmt.Errorf("memory stage: invalid compiled memory %q", ref)
	}
	return configured, nil
}

func (e *Engine) append(meta Meta, kind string, data any) (event.Event, error) {
	return e.events.Append(event.Draft{SessionID: meta.SessionID, RunID: meta.RunID, StageID: meta.StageID, Type: kind, ConfigDigest: meta.ConfigDigest, Data: data})
}

func cloneMemory(value *memexmemory.Memory) *memexmemory.Memory {
	data, _ := json.Marshal(value)
	var cloned memexmemory.Memory
	_ = json.Unmarshal(data, &cloned)
	return &cloned
}

func validateScopes(scopes []string) error {
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		switch scope {
		case "workspace", "agent", "user", "team", "global":
		default:
			return fmt.Errorf("memory stage: unsupported scope %q", scope)
		}
		if _, duplicate := seen[scope]; duplicate {
			return fmt.Errorf("memory stage: duplicate scope %q", scope)
		}
		seen[scope] = struct{}{}
	}
	return nil
}

func writeScopeAllowed(item *memexmemory.Memory, scopes []string) bool {
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
			if item.Origin != nil && item.Origin.AgentTool != "" {
				return true
			}
		}
	}
	return false
}

func cloneMemories(in map[string]spec.Memory) map[string]spec.Memory {
	out := make(map[string]spec.Memory, len(in))
	for key, value := range in {
		value.Recall.Scopes = append([]string(nil), value.Recall.Scopes...)
		out[key] = value
	}
	return out
}

func cloneRoutes(in map[string]spec.Route) map[string]spec.Route {
	out := make(map[string]spec.Route, len(in))
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out[key] = in[key]
	}
	return out
}

func cloneModels(in map[string]spec.Model) map[string]spec.Model {
	out := make(map[string]spec.Model, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneSources(in map[string]spec.Source) map[string]spec.Source {
	out := make(map[string]spec.Source, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
