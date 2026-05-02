package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// TaskExecutor is a function that executes a task and returns the output.
type TaskExecutor func(ctx context.Context, message, taskContext string) (string, error)

// Handler serves A2A protocol requests by delegating to a local agent runtime.
type Handler struct {
	card     *AgentCard
	executor TaskExecutor
}

// NewHandler creates an A2A HTTP handler.
func NewHandler(card *AgentCard, executor TaskExecutor) *Handler {
	return &Handler{card: card, executor: executor}
}

// RegisterRoutes adds A2A routes to an HTTP mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /.well-known/agent-card.json", h.handleAgentCard)
	mux.HandleFunc("POST /a2a/tasks/send", h.handleTaskSend)
}

// handleAgentCard returns the agent card JSON.
func (h *Handler) handleAgentCard(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(h.card); err != nil {
		http.Error(w, fmt.Sprintf("encode card: %v", err), http.StatusInternalServerError)
	}
}

// handleTaskSend executes a task synchronously and returns the result.
func (h *Handler) handleTaskSend(w http.ResponseWriter, r *http.Request) {
	var req TaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.ID == "" {
		req.ID = uuid.New().String()
	}

	start := time.Now()
	output, err := h.executor(r.Context(), req.Message, req.Context)
	duration := time.Since(start)

	resp := TaskResponse{
		ID:       req.ID,
		Duration: duration,
	}

	if err != nil {
		resp.Status = TaskStatusFailed
		resp.Error = err.Error()
	} else {
		resp.Status = TaskStatusCompleted
		resp.Output = output
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
	}
}
