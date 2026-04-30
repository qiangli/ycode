package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/runtime/fileops"
)

const personaFilePrefix = "_persona_"

// personaFilename returns the filename for a persona with the given ID.
func personaFilename(id string) string {
	return personaFilePrefix + sanitizeFilename(id) + ".md"
}

// SavePersona persists a persona as a markdown file with YAML frontmatter.
func SavePersona(store *Store, p *Persona) error {
	path := filepath.Join(store.Dir(), personaFilename(p.ID))
	p.LastSeenAt = time.Now()

	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "id: %s\n", p.ID)
	if p.DisplayHint != "" {
		fmt.Fprintf(&b, "display_hint: %s\n", p.DisplayHint)
	}
	fmt.Fprintf(&b, "confidence: %.4f\n", p.Confidence)
	fmt.Fprintf(&b, "created_at: %s\n", p.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "last_seen_at: %s\n", p.LastSeenAt.Format(time.RFC3339))
	b.WriteString("---\n\n")
	b.WriteString(formatPersonaBody(p))

	return fileops.AtomicWriteFile(path, []byte(b.String()), 0o644)
}

// LoadPersona loads a persona by ID from the store.
// Returns nil, nil if the persona file does not exist.
func LoadPersona(store *Store, id string) (*Persona, error) {
	path := filepath.Join(store.Dir(), personaFilename(id))
	return loadPersonaFile(path)
}

// ListPersonas returns all personas in the store.
func ListPersonas(store *Store) ([]*Persona, error) {
	entries, err := os.ReadDir(store.Dir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var personas []*Persona
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), personaFilePrefix) || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		p, err := loadPersonaFile(filepath.Join(store.Dir(), e.Name()))
		if err != nil || p == nil {
			continue
		}
		personas = append(personas, p)
	}
	return personas, nil
}

// MigrateProfile converts a legacy UserProfile into a Persona.
// Returns nil if no profile exists or the profile is empty.
func MigrateProfile(store *Store, env *EnvironmentSignals, id string) (*Persona, error) {
	profile, err := LoadProfile(store)
	if err != nil {
		return nil, nil
	}
	if profile.IsEmpty() {
		return nil, nil
	}

	p := NewPersona(id, env)

	// Migrate basic info.
	if name := profile.Get("basic_info.name"); name != "" {
		p.DisplayHint = name
	}
	if role := profile.Get("basic_info.role"); role != "" {
		p.Interactions.AddObservation(PersonaObservation{
			Text:       "Role: " + role,
			Category:   "expertise",
			Confidence: 0.8,
			ObservedAt: time.Now(),
			Source:     "explicit",
		})
	}

	// Migrate preferences.
	for _, k := range sortedKeys(profile.Preferences) {
		p.Interactions.AddObservation(PersonaObservation{
			Text:       fmt.Sprintf("Preference: %s = %s", k, profile.Preferences[k]),
			Category:   "preference",
			Confidence: 0.8,
			ObservedAt: time.Now(),
			Source:     "explicit",
		})
	}

	// Migrate expertise as knowledge domains.
	for _, e := range profile.Expertise {
		p.Knowledge.AddOrUpdateDomain(e, LevelAdvanced, 0.6)
	}

	// Migrate work patterns.
	for _, w := range profile.WorkPatterns {
		p.Interactions.AddObservation(PersonaObservation{
			Text:       "Work pattern: " + w,
			Category:   "workflow",
			Confidence: 0.7,
			ObservedAt: time.Now(),
			Source:     "explicit",
		})
	}

	return p, nil
}

// loadPersonaFile reads and parses a persona from a file path.
func loadPersonaFile(path string) (*Persona, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return parsePersonaFile(string(data))
}

// parsePersonaFile parses a persona from frontmatter + body.
func parsePersonaFile(data string) (*Persona, error) {
	p := &Persona{
		Knowledge:      &KnowledgeMap{},
		Communication:  &CommunicationStyle{Verbosity: 0.5, Formality: 0.5},
		Behavior:       &BehaviorProfile{},
		Interactions:   &InteractionSummary{},
		Environment:    &EnvironmentSignals{},
		SessionContext: &SessionContext{},
	}

	if !strings.HasPrefix(data, "---\n") {
		return nil, fmt.Errorf("missing frontmatter")
	}

	endIdx := strings.Index(data[4:], "\n---\n")
	if endIdx == -1 {
		return nil, fmt.Errorf("unterminated frontmatter")
	}

	frontmatter := data[4 : 4+endIdx]
	body := strings.TrimSpace(data[4+endIdx+5:])

	// Parse frontmatter.
	for _, line := range strings.Split(frontmatter, "\n") {
		key, value, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "id":
			p.ID = value
		case "display_hint":
			p.DisplayHint = value
		case "confidence":
			fmt.Sscanf(value, "%f", &p.Confidence)
		case "created_at":
			if t, err := time.Parse(time.RFC3339, value); err == nil {
				p.CreatedAt = t
			}
		case "last_seen_at":
			if t, err := time.Parse(time.RFC3339, value); err == nil {
				p.LastSeenAt = t
			}
		}
	}

	// Parse body sections.
	if body != "" {
		parsePersonaBody(p, body)
	}

	return p, nil
}

// formatPersonaBody renders the persona body as structured markdown.
func formatPersonaBody(p *Persona) string {
	var b strings.Builder

	// Knowledge section.
	if p.Knowledge != nil && len(p.Knowledge.Domains) > 0 {
		b.WriteString("## Knowledge\n")
		for _, d := range p.Knowledge.Domains {
			fmt.Fprintf(&b, "- **%s**: %s (confidence: %.2f, evidence: %d)\n",
				d.Name, d.Level, d.Confidence, d.EvidenceCount)
		}
		b.WriteString("\n")
	}

	// Communication section.
	if p.Communication != nil && p.Communication.Confidence > 0 {
		b.WriteString("## Communication\n")
		fmt.Fprintf(&b, "- verbosity: %.2f\n", p.Communication.Verbosity)
		fmt.Fprintf(&b, "- formality: %.2f\n", p.Communication.Formality)
		if p.Communication.JustDoIt {
			b.WriteString("- just_do_it: true\n")
		}
		if p.Communication.AsksClarify {
			b.WriteString("- asks_clarify: true\n")
		}
		fmt.Fprintf(&b, "- confidence: %.2f\n", p.Communication.Confidence)
		b.WriteString("\n")
	}

	// Behavior section.
	if p.Behavior != nil {
		b.WriteString("## Behavior\n")
		fmt.Fprintf(&b, "- reviews_diffs: %.2f\n", p.Behavior.ReviewsDiffs)
		fmt.Fprintf(&b, "- prefers_tdd: %.2f\n", p.Behavior.PrefersTDD)
		fmt.Fprintf(&b, "- tool_approval_rate: %.2f\n", p.Behavior.ToolApprovalRate)
		fmt.Fprintf(&b, "- correction_freq: %.2f\n", p.Behavior.CorrectionFreq)
		fmt.Fprintf(&b, "- question_to_command: %.2f\n", p.Behavior.QuestionToCommand)
		fmt.Fprintf(&b, "- avg_session_minutes: %.1f\n", p.Behavior.AvgSessionMinutes)
		fmt.Fprintf(&b, "- topic_breadth: %.2f\n", p.Behavior.TopicBreadth)
		b.WriteString("\n")
	}

	// Environment section.
	if p.Environment != nil {
		b.WriteString("## Environment\n")
		envJSON, _ := json.Marshal(p.Environment)
		b.Write(envJSON)
		b.WriteString("\n\n")
	}

	// Interactions section.
	if p.Interactions != nil {
		b.WriteString("## Interactions\n")
		fmt.Fprintf(&b, "- sessions: %d\n", p.Interactions.TotalSessions)
		fmt.Fprintf(&b, "- turns: %d\n", p.Interactions.TotalTurns)
		if !p.Interactions.FirstInteraction.IsZero() {
			fmt.Fprintf(&b, "- first: %s\n", p.Interactions.FirstInteraction.Format(time.RFC3339))
		}
		b.WriteString("\n")

		if len(p.Interactions.Observations) > 0 {
			b.WriteString("## Observations\n")
			// Sort by confidence descending for readability.
			sorted := make([]PersonaObservation, len(p.Interactions.Observations))
			copy(sorted, p.Interactions.Observations)
			sort.Slice(sorted, func(i, j int) bool {
				return sorted[i].Confidence > sorted[j].Confidence
			})
			for _, obs := range sorted {
				fmt.Fprintf(&b, "- [%s/%.2f/%s] %s\n", obs.Category, obs.Confidence, obs.Source, obs.Text)
			}
			b.WriteString("\n")
		}
	}

	return strings.TrimSpace(b.String())
}

// parsePersonaBody reconstructs persona fields from the markdown body.
func parsePersonaBody(p *Persona, body string) {
	var currentSection string
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)

		if after, ok := strings.CutPrefix(trimmed, "## "); ok {
			currentSection = strings.ToLower(after)
			continue
		}

		if !strings.HasPrefix(trimmed, "- ") && !strings.HasPrefix(trimmed, "{") {
			continue
		}

		item := strings.TrimPrefix(trimmed, "- ")

		switch currentSection {
		case "knowledge":
			parseKnowledgeLine(p.Knowledge, item)
		case "communication":
			parseCommunicationLine(p.Communication, item)
		case "behavior":
			parseBehaviorLine(p.Behavior, item)
		case "environment":
			// Environment is stored as JSON.
			if strings.HasPrefix(trimmed, "{") {
				json.Unmarshal([]byte(trimmed), p.Environment)
			}
		case "interactions":
			parseInteractionLine(p.Interactions, item)
		case "observations":
			parseObservationLine(p.Interactions, item)
		}
	}
}

// parseKnowledgeLine parses "**Go**: advanced (confidence: 0.80, evidence: 5)".
func parseKnowledgeLine(km *KnowledgeMap, line string) {
	// Strip bold markers.
	line = strings.TrimPrefix(line, "**")
	name, rest, ok := strings.Cut(line, "**")
	if !ok {
		return
	}
	rest = strings.TrimPrefix(rest, ": ")
	level, details, _ := strings.Cut(rest, " (")
	level = strings.TrimSpace(level)

	d := KnowledgeDomain{Name: name, Level: level}
	if details != "" {
		details = strings.TrimSuffix(details, ")")
		for _, part := range strings.Split(details, ", ") {
			k, v, ok := strings.Cut(part, ": ")
			if !ok {
				continue
			}
			switch k {
			case "confidence":
				fmt.Sscanf(v, "%f", &d.Confidence)
			case "evidence":
				fmt.Sscanf(v, "%d", &d.EvidenceCount)
			}
		}
	}
	km.Domains = append(km.Domains, d)
}

// parseCommunicationLine parses "verbosity: 0.30".
func parseCommunicationLine(cs *CommunicationStyle, line string) {
	key, value, ok := strings.Cut(line, ": ")
	if !ok {
		return
	}
	switch strings.TrimSpace(key) {
	case "verbosity":
		fmt.Sscanf(value, "%f", &cs.Verbosity)
	case "formality":
		fmt.Sscanf(value, "%f", &cs.Formality)
	case "just_do_it":
		cs.JustDoIt = strings.TrimSpace(value) == "true"
	case "asks_clarify":
		cs.AsksClarify = strings.TrimSpace(value) == "true"
	case "confidence":
		fmt.Sscanf(value, "%f", &cs.Confidence)
	}
}

// parseBehaviorLine parses "reviews_diffs: 0.70".
func parseBehaviorLine(bp *BehaviorProfile, line string) {
	key, value, ok := strings.Cut(line, ": ")
	if !ok {
		return
	}
	var f float64
	fmt.Sscanf(strings.TrimSpace(value), "%f", &f)
	switch strings.TrimSpace(key) {
	case "reviews_diffs":
		bp.ReviewsDiffs = f
	case "prefers_tdd":
		bp.PrefersTDD = f
	case "tool_approval_rate":
		bp.ToolApprovalRate = f
	case "correction_freq":
		bp.CorrectionFreq = f
	case "question_to_command":
		bp.QuestionToCommand = f
	case "avg_session_minutes":
		bp.AvgSessionMinutes = f
	case "topic_breadth":
		bp.TopicBreadth = f
	}
}

// parseInteractionLine parses "sessions: 42".
func parseInteractionLine(is *InteractionSummary, line string) {
	key, value, ok := strings.Cut(line, ": ")
	if !ok {
		return
	}
	switch strings.TrimSpace(key) {
	case "sessions":
		fmt.Sscanf(value, "%d", &is.TotalSessions)
	case "turns":
		fmt.Sscanf(value, "%d", &is.TotalTurns)
	case "first":
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(value)); err == nil {
			is.FirstInteraction = t
		}
	}
}

// parseObservationLine parses "[preference/0.80/explicit] Prefers terse responses".
func parseObservationLine(is *InteractionSummary, line string) {
	if !strings.HasPrefix(line, "[") {
		return
	}
	bracketEnd := strings.Index(line, "] ")
	if bracketEnd == -1 {
		return
	}
	meta := line[1:bracketEnd]
	text := line[bracketEnd+2:]

	parts := strings.SplitN(meta, "/", 3)
	if len(parts) != 3 {
		return
	}

	obs := PersonaObservation{
		Text:     text,
		Category: parts[0],
		Source:   parts[2],
	}
	fmt.Sscanf(parts[1], "%f", &obs.Confidence)
	is.Observations = append(is.Observations, obs)
}
