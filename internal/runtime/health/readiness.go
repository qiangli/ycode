package health

import (
	"fmt"
	"log/slog"
	"strings"
)

// ReadinessStatus indicates a subsystem's state.
type ReadinessStatus string

const (
	StatusReady   ReadinessStatus = "ready"
	StatusWarning ReadinessStatus = "warning"
	StatusBlocked ReadinessStatus = "blocked"
)

// SubsystemCheck is the readiness result for one subsystem.
type SubsystemCheck struct {
	Name    string
	Status  ReadinessStatus
	Message string
}

// ReadinessReport is the result of a dry-run readiness check.
type ReadinessReport struct {
	Overall    ReadinessStatus
	Subsystems []SubsystemCheck
}

// NewReadinessReport creates an empty report.
func NewReadinessReport() *ReadinessReport {
	return &ReadinessReport{Overall: StatusReady}
}

// Add records a subsystem check result.
func (r *ReadinessReport) Add(name string, status ReadinessStatus, message string) {
	slog.Info("health.readiness.check",
		"subsystem", name,
		"status", string(status),
		"message", message,
	)
	r.Subsystems = append(r.Subsystems, SubsystemCheck{
		Name: name, Status: status, Message: message,
	})
	// Overall is the worst status.
	if status == StatusBlocked {
		r.Overall = StatusBlocked
	} else if status == StatusWarning && r.Overall != StatusBlocked {
		r.Overall = StatusWarning
	}
}

// Format returns a human-readable report.
func (r *ReadinessReport) Format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Readiness: %s\n", r.Overall)
	b.WriteString(strings.Repeat("-", 40) + "\n")
	for _, s := range r.Subsystems {
		icon := "+"
		if s.Status == StatusWarning {
			icon = "!"
		} else if s.Status == StatusBlocked {
			icon = "x"
		}
		fmt.Fprintf(&b, "%s %-20s %s\n", icon, s.Name, s.Message)
	}
	return b.String()
}
