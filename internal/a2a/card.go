// Package a2a implements the Agent-to-Agent protocol for remote agent
// discovery and task delegation. Compatible with the Google/CrewAI A2A spec.
//
// Agents advertise their capabilities via agent cards served at
// /.well-known/agent-card.json. Tasks are delegated over HTTP.
package a2a

import "time"

// AgentCard describes an agent's capabilities for remote discovery.
type AgentCard struct {
	Name            string       `json:"name"`
	Description     string       `json:"description"`
	URL             string       `json:"url"`
	Version         string       `json:"version"`
	Skills          []AgentSkill `json:"skills,omitempty"`
	Capabilities    Capabilities `json:"capabilities"`
	InputModes      []string     `json:"default_input_modes"`
	OutputModes     []string     `json:"default_output_modes"`
	ProtocolVersion string       `json:"protocol_version"`
}

// AgentSkill describes one capability of an agent.
type AgentSkill struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	InputModes  []string `json:"input_modes,omitempty"`
	OutputModes []string `json:"output_modes,omitempty"`
}

// Capabilities describes what the agent supports.
type Capabilities struct {
	Streaming         bool `json:"streaming"`
	PushNotifications bool `json:"push_notifications"`
	StateTransitions  bool `json:"state_transitions"`
}

// TaskRequest is a request to execute a task on a remote agent.
type TaskRequest struct {
	ID      string `json:"id"`
	Message string `json:"message"`
	Context string `json:"context,omitempty"`
}

// TaskResponse is the result of a remote task execution.
type TaskResponse struct {
	ID       string        `json:"id"`
	Status   TaskStatus    `json:"status"`
	Output   string        `json:"output,omitempty"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration,omitempty"`
}

// TaskStatus represents the state of a remote task.
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
)

// AuthConfig holds authentication configuration for A2A connections.
type AuthConfig struct {
	Type   string `yaml:"type,omitempty" json:"type,omitempty"` // bearer, api_key
	Token  string `yaml:"token,omitempty" json:"token,omitempty"`
	Header string `yaml:"header,omitempty" json:"header,omitempty"` // custom header name
}
