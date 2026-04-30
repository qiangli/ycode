package memory

import "time"

// Persona represents a rich user model inferred from behavior.
// Unlike the flat UserProfile, it tracks knowledge, communication style,
// behavioral patterns, and session context — all confidence-scored.
type Persona struct {
	// ID is a stable identifier derived from environment signals hash.
	ID string `json:"id"`
	// DisplayHint is a human-readable hint (e.g., git username). Not identity proof.
	DisplayHint string `json:"display_hint,omitempty"`
	// Confidence is the overall match confidence for this persona (0.0-1.0).
	// Used to scale how much persona context is injected into the prompt.
	Confidence float64   `json:"confidence"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`

	Knowledge     *KnowledgeMap       `json:"knowledge"`
	Communication *CommunicationStyle `json:"communication"`
	Behavior      *BehaviorProfile    `json:"behavior"`
	Interactions  *InteractionSummary `json:"interactions"`
	Environment   *EnvironmentSignals `json:"environment"`

	// SessionContext is ephemeral per-session state. Not persisted.
	SessionContext *SessionContext `json:"-"`
}

// KnowledgeMap tracks what the user knows across domains.
type KnowledgeMap struct {
	Domains []KnowledgeDomain `json:"domains,omitempty"`
}

// KnowledgeDomain represents the user's knowledge in a specific area.
type KnowledgeDomain struct {
	Name             string    `json:"name"`
	Level            string    `json:"level"`             // "novice", "intermediate", "advanced", "expert"
	Confidence       float64   `json:"confidence"`        // 0.0-1.0
	LastDemonstrated time.Time `json:"last_demonstrated"` // when expertise was last observed
	EvidenceCount    int       `json:"evidence_count"`    // number of signals contributing to this assessment
	ProjectScoped    bool      `json:"project_scoped"`    // true if expertise only observed in specific project
}

// KnowledgeLevels are the recognized expertise tiers.
const (
	LevelNovice       = "novice"
	LevelIntermediate = "intermediate"
	LevelAdvanced     = "advanced"
	LevelExpert       = "expert"
)

// CommunicationStyle captures how the user prefers to interact.
// All float64 fields are in [0.0, 1.0].
type CommunicationStyle struct {
	Verbosity   float64 `json:"verbosity"`    // 0.0=terse, 1.0=verbose
	Formality   float64 `json:"formality"`    // 0.0=casual, 1.0=formal
	JustDoIt    bool    `json:"just_do_it"`   // user prefers results over explanation
	AsksClarify bool    `json:"asks_clarify"` // user frequently asks follow-up questions
	Confidence  float64 `json:"confidence"`   // overall confidence in communication model
}

// BehaviorProfile captures workflow patterns.
// All float64 fields are in [0.0, 1.0].
type BehaviorProfile struct {
	ReviewsDiffs      float64 `json:"reviews_diffs"`       // 0.0=trusts agent, 1.0=always reviews
	PrefersTDD        float64 `json:"prefers_tdd"`         // 0.0=write-first, 1.0=test-first
	ToolApprovalRate  float64 `json:"tool_approval_rate"`  // fraction of tool calls auto-approved
	CorrectionFreq    float64 `json:"correction_freq"`     // how often user corrects agent output
	QuestionToCommand float64 `json:"question_to_command"` // 0.0=all commands, 1.0=all questions
	AvgSessionMinutes float64 `json:"avg_session_minutes"`
	TopicBreadth      float64 `json:"topic_breadth"` // 0.0=single-focus, 1.0=multi-topic sessions
}

// SessionContext holds ephemeral per-session state, rebuilt each session.
type SessionContext struct {
	DetectedRole     string          `json:"detected_role"` // "learning", "debugging", "architecting", "reviewing"
	DetectedMood     string          `json:"detected_mood"` // "focused", "exploratory", "frustrated"
	TurnsSinceSwitch int             `json:"turns_since_switch"`
	SignalHistory    []SessionSignal `json:"signal_history"` // ring buffer, max signalHistorySize
}

const signalHistorySize = 20

// Update adds a signal to the ring buffer and recomputes role/mood every 3 turns.
func (sc *SessionContext) Update(sig SessionSignal) {
	if len(sc.SignalHistory) >= signalHistorySize {
		sc.SignalHistory = sc.SignalHistory[1:]
	}
	sc.SignalHistory = append(sc.SignalHistory, sig)
	sc.TurnsSinceSwitch++

	// Recompute role/mood every 3 turns.
	if len(sc.SignalHistory) >= 3 && len(sc.SignalHistory)%3 == 0 {
		sc.recomputeRole()
	}
}

// recomputeRole uses majority vote over the last N signals to detect role.
func (sc *SessionContext) recomputeRole() {
	counts := make(map[string]int)
	// Vote over the most recent signals (up to 9 for stability).
	window := sc.SignalHistory
	if len(window) > 9 {
		window = window[len(window)-9:]
	}
	for _, sig := range window {
		if sig.DetectedIntent != "" {
			counts[sig.DetectedIntent]++
		}
	}

	bestRole := ""
	bestCount := 0
	for role, count := range counts {
		if count > bestCount {
			bestRole = role
			bestCount = count
		}
	}

	if bestRole != "" && bestRole != sc.DetectedRole {
		sc.DetectedRole = bestRole
		sc.TurnsSinceSwitch = 0
	}

	// Mood detection: frustrated if correction rate is high in recent signals.
	recentCorrections := 0
	for _, sig := range window {
		recentCorrections += sig.Corrections
	}
	if len(window) > 0 && float64(recentCorrections)/float64(len(window)) > 0.5 {
		sc.DetectedMood = "frustrated"
	} else if bestRole == "learning" {
		sc.DetectedMood = "exploratory"
	} else {
		sc.DetectedMood = "focused"
	}
}

// SessionSignal captures behavioral signals from a single conversation turn.
type SessionSignal struct {
	TurnNumber       int       `json:"turn_number"`
	MessageLength    int       `json:"message_length"`    // word count
	QuestionCount    int       `json:"question_count"`    // number of questions detected
	TechnicalDensity float64   `json:"technical_density"` // ratio of technical terms to total words
	ToolApprovals    int       `json:"tool_approvals"`
	ToolDenials      int       `json:"tool_denials"`
	Corrections      int       `json:"corrections"`
	DetectedIntent   string    `json:"detected_intent"` // "debugging", "learning", "architecting", "reviewing", ""
	Timestamp        time.Time `json:"timestamp"`
}

// InteractionSummary holds distilled cross-session patterns.
type InteractionSummary struct {
	TotalSessions    int                  `json:"total_sessions"`
	TotalTurns       int                  `json:"total_turns"`
	FirstInteraction time.Time            `json:"first_interaction"`
	Observations     []PersonaObservation `json:"observations,omitempty"` // max 20
}

// MaxObservations is the cap on persona observations.
const MaxObservations = 20

// PersonaObservation is a distilled insight about the user.
type PersonaObservation struct {
	Text       string    `json:"text"`       // e.g., "Prefers Go table-driven tests over subtests"
	Category   string    `json:"category"`   // "preference", "expertise", "workflow", "communication"
	Confidence float64   `json:"confidence"` // 0.0-1.0
	ObservedAt time.Time `json:"observed_at"`
	Source     string    `json:"source"` // "explicit" (user stated) or "inferred" (from behavior)
}

// EnvironmentSignals are soft identity hints.
type EnvironmentSignals struct {
	Platform    string `json:"platform"`      // runtime.GOOS
	Shell       string `json:"shell"`         // e.g., "zsh", "bash"
	GitUserName string `json:"git_user_name"` // from git config user.name
	GitEmail    string `json:"git_email"`     // from git config user.email
	HomeDir     string `json:"home_dir"`
	Hostname    string `json:"hostname"`
}

// NewPersona creates a new persona with all behavioral scores at 0.5 (maximum uncertainty).
func NewPersona(id string, env *EnvironmentSignals) *Persona {
	now := time.Now()
	return &Persona{
		ID:          id,
		DisplayHint: env.GitUserName,
		Confidence:  0.5,
		CreatedAt:   now,
		LastSeenAt:  now,
		Knowledge:   &KnowledgeMap{},
		Communication: &CommunicationStyle{
			Verbosity:  0.5,
			Formality:  0.5,
			Confidence: 0.0, // no confidence yet
		},
		Behavior: &BehaviorProfile{
			ReviewsDiffs:      0.5,
			PrefersTDD:        0.5,
			ToolApprovalRate:  0.5,
			CorrectionFreq:    0.5,
			QuestionToCommand: 0.5,
			TopicBreadth:      0.5,
		},
		Interactions: &InteractionSummary{
			FirstInteraction: now,
		},
		Environment:    env,
		SessionContext: &SessionContext{},
	}
}

// NewSessionContext creates an empty session context.
func NewSessionContext() *SessionContext {
	return &SessionContext{}
}

// AddObservation adds an observation, respecting the MaxObservations cap.
// If at capacity, the lowest-confidence oldest observation is evicted.
func (is *InteractionSummary) AddObservation(obs PersonaObservation) {
	if len(is.Observations) < MaxObservations {
		is.Observations = append(is.Observations, obs)
		return
	}

	// Evict the lowest-confidence observation, breaking ties by oldest.
	evictIdx := 0
	for i := 1; i < len(is.Observations); i++ {
		curr := is.Observations[i]
		evict := is.Observations[evictIdx]
		if curr.Confidence < evict.Confidence ||
			(curr.Confidence == evict.Confidence && curr.ObservedAt.Before(evict.ObservedAt)) {
			evictIdx = i
		}
	}
	is.Observations[evictIdx] = obs
}

// FindDomain returns the knowledge domain by name, or nil if not found.
func (km *KnowledgeMap) FindDomain(name string) *KnowledgeDomain {
	for i := range km.Domains {
		if km.Domains[i].Name == name {
			return &km.Domains[i]
		}
	}
	return nil
}

// AddOrUpdateDomain adds or updates a knowledge domain.
func (km *KnowledgeMap) AddOrUpdateDomain(name, level string, confidence float64) {
	if d := km.FindDomain(name); d != nil {
		d.Level = level
		d.Confidence = confidence
		d.LastDemonstrated = time.Now()
		d.EvidenceCount++
		return
	}
	km.Domains = append(km.Domains, KnowledgeDomain{
		Name:             name,
		Level:            level,
		Confidence:       confidence,
		LastDemonstrated: time.Now(),
		EvidenceCount:    1,
	})
}
