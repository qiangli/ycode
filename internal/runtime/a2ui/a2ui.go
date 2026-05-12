// Package a2ui builds A2UI v0.9 operations for ycode's generative-UI
// surfaces. Mirrors the shape of priorart/CopilotKit/sdk-python/copilotkit/
// a2ui.py so the wire format is interchangeable with CopilotKit-speaking
// agents and the @a2ui/web_core / @a2ui/lit reference renderers.
//
// The three primitive ops — CreateSurface, UpdateComponents,
// UpdateDataModel — are addressed by a surface ID. Components are emitted
// once per surface; data is streamed independently. That separation is
// what makes A2UI a clean fit for bidirectional shared state: the agent
// updates the data model, the user mutates it back, both addressed by
// the same surface + path.
//
// Build the ops directly (NewCreateSurface, etc.) and flow them onto
// ycode's bus via bus.EventStateUpdate with format="a2ui".
package a2ui

import (
	"encoding/json"
	"fmt"
	"sync"
)

// Version is the A2UI protocol revision this package targets.
const Version = "v0.9"

// BasicCatalogID is the v0.9 component catalog default. Surfaces that
// don't specify their own catalog use this one.
const BasicCatalogID = "https://a2ui.org/specification/v0_9/basic_catalog.json"

// OperationsKey is the JSON container key that wraps a batch of ops on
// the wire. Renderers explicitly look for this to disambiguate A2UI
// payloads from other JSON the agent may emit.
const OperationsKey = "a2ui_operations"

// Op is one A2UI v0.9 operation. Exactly one of CreateSurface,
// UpdateComponents, UpdateDataModel is non-nil — the rest of the
// fields stay omitted in the marshaled JSON.
type Op struct {
	Version          string            `json:"version"`
	CreateSurface    *CreateSurface    `json:"createSurface,omitempty"`
	UpdateComponents *UpdateComponents `json:"updateComponents,omitempty"`
	UpdateDataModel  *UpdateDataModel  `json:"updateDataModel,omitempty"`
}

// CreateSurface registers a new addressable UI region with the renderer.
// Subsequent UpdateComponents / UpdateDataModel ops target it by SurfaceID.
type CreateSurface struct {
	SurfaceID string `json:"surfaceId"`
	CatalogID string `json:"catalogId"`
}

// UpdateComponents replaces the surface's component tree. Components is
// kept as raw JSON because the v0.9 schema is large and evolving — and
// because in many cases the schema is loaded from a static JSON file or
// emitted by the model. Validate against the catalog renderer-side.
type UpdateComponents struct {
	SurfaceID  string            `json:"surfaceId"`
	Components []json.RawMessage `json:"components"`
}

// UpdateDataModel patches the data the surface's components bind to.
// Path is a JSON-pointer-ish string ("/" for the root, "/items/0/name"
// for a specific value). Value is any JSON-serializable Go value.
type UpdateDataModel struct {
	SurfaceID string `json:"surfaceId"`
	Path      string `json:"path"`
	Value     any    `json:"value"`
}

// NewCreateSurface returns a CreateSurface op using the basic catalog.
// Most call-sites want this — pass a custom catalog only when targeting
// a non-default component library.
func NewCreateSurface(surfaceID string) Op {
	return NewCreateSurfaceWithCatalog(surfaceID, BasicCatalogID)
}

// NewCreateSurfaceWithCatalog is the explicit-catalog form of
// NewCreateSurface. Use when a surface needs components from a custom
// catalog (e.g. a ycode-specific component set hosted on a known URL).
func NewCreateSurfaceWithCatalog(surfaceID, catalogID string) Op {
	return Op{
		Version: Version,
		CreateSurface: &CreateSurface{
			SurfaceID: surfaceID,
			CatalogID: catalogID,
		},
	}
}

// NewUpdateComponents returns an UpdateComponents op. The components
// slice is kept as raw JSON — caller is responsible for shape.
func NewUpdateComponents(surfaceID string, components []json.RawMessage) Op {
	return Op{
		Version: Version,
		UpdateComponents: &UpdateComponents{
			SurfaceID:  surfaceID,
			Components: components,
		},
	}
}

// NewUpdateDataModel returns an UpdateDataModel op. Path defaults to "/"
// when empty — the most common case where the caller is replacing the
// whole root value.
func NewUpdateDataModel(surfaceID, path string, value any) Op {
	if path == "" {
		path = "/"
	}
	return Op{
		Version: Version,
		UpdateDataModel: &UpdateDataModel{
			SurfaceID: surfaceID,
			Path:      path,
			Value:     value,
		},
	}
}

// Render wraps a batch of ops in the OperationsKey container and marshals
// to JSON. Mirrors priorart/CopilotKit/sdk-python/copilotkit/a2ui.py's
// render() so renderers see the exact same shape coming off either source.
func Render(ops []Op) ([]byte, error) {
	if ops == nil {
		ops = []Op{}
	}
	return json.Marshal(map[string]any{OperationsKey: ops})
}

// Surface describes a first-class ycode-published A2UI surface — one
// the runtime announces (createSurface + updateComponents) on session
// start so foreign agents and the canvas client can address it without
// having to ship the schema themselves.
//
// Components is kept as raw JSON so schemas can live as static files
// embedded via go:embed without round-tripping through Go types.
type Surface struct {
	ID         string
	CatalogID  string
	Components []json.RawMessage
}

// Bootstrap returns the ops that announce this surface: CreateSurface
// followed by UpdateComponents. Most call-sites that publish a Surface
// emit these two atomically.
func (s *Surface) Bootstrap() []Op {
	catalog := s.CatalogID
	if catalog == "" {
		catalog = BasicCatalogID
	}
	return []Op{
		NewCreateSurfaceWithCatalog(s.ID, catalog),
		NewUpdateComponents(s.ID, s.Components),
	}
}

// Registry holds the first-class surfaces ycode publishes. Empty by
// default — surfaces are added as their tracks ship (e.g. v1
// observability adds "health"; v2 memos adds "memos"; later tracks
// add "kanban", "lanes", etc.).
type Registry struct {
	mu       sync.RWMutex
	surfaces map[string]*Surface
}

// NewRegistry returns an empty surface registry. Use Add to populate.
func NewRegistry() *Registry {
	return &Registry{surfaces: make(map[string]*Surface)}
}

// Add registers a surface. Replaces any prior surface with the same ID
// — most call-sites add surfaces once at startup; replacement is for
// hot-reload scenarios.
func (r *Registry) Add(s *Surface) error {
	if s == nil || s.ID == "" {
		return fmt.Errorf("a2ui: surface must have a non-empty ID")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.surfaces[s.ID] = s
	return nil
}

// Get returns the surface with the given ID, if registered.
func (r *Registry) Get(id string) (*Surface, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.surfaces[id]
	return s, ok
}

// All returns a snapshot of all registered surfaces, in arbitrary order.
// Used by the canvas-bootstrap path to emit createSurface+updateComponents
// for every first-class surface on session start.
func (r *Registry) All() []*Surface {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Surface, 0, len(r.surfaces))
	for _, s := range r.surfaces {
		out = append(out, s)
	}
	return out
}
