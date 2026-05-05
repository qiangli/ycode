package memory

import (
	"math"
	"time"
)

// personaEMAAlpha is the exponential moving average alpha for session-end updates.
// 0.2 means 20% weight for the new session, 80% for history.
const personaEMAAlpha = 0.2

// UpdatePersonaFromSession updates a persona's persistent fields based on
// the session context accumulated during the conversation. This should be
// called at session end before saving the persona.
func UpdatePersonaFromSession(p *Persona) {
	if p == nil || p.SessionContext == nil || len(p.SessionContext.SignalHistory) == 0 {
		return
	}

	signals := p.SessionContext.SignalHistory
	n := float64(len(signals))

	// Aggregate session-level statistics.
	var totalLength float64
	var totalQuestions float64
	var totalCorrections float64
	var totalApprovals float64
	var totalDenials float64

	for _, sig := range signals {
		totalLength += float64(sig.MessageLength)
		totalQuestions += float64(sig.QuestionCount)
		totalCorrections += float64(sig.Corrections)
		totalApprovals += float64(sig.ToolApprovals)
		totalDenials += float64(sig.ToolDenials)
	}

	// Update communication style.
	if p.Communication != nil {
		// Verbosity: normalize average message length to [0, 1].
		// <10 words = 0, 50+ words = 1.
		avgLength := totalLength / n
		sessionVerbosity := clamp01((avgLength - 10) / 40)
		p.Communication.Verbosity = ema(p.Communication.Verbosity, sessionVerbosity, personaEMAAlpha)

		// Question-to-command ratio informs AsksClarify.
		qRatio := totalQuestions / math.Max(n, 1)
		p.Communication.AsksClarify = qRatio > 0.3

		// JustDoIt: low verbosity + few questions.
		p.Communication.JustDoIt = p.Communication.Verbosity < 0.3 && qRatio < 0.2

		// Increase confidence gradually.
		p.Communication.Confidence = clamp01(p.Communication.Confidence + 0.05)
	}

	// Update behavior profile.
	if p.Behavior != nil {
		// Tool approval rate.
		totalTools := totalApprovals + totalDenials
		if totalTools > 0 {
			sessionApprovalRate := totalApprovals / totalTools
			p.Behavior.ToolApprovalRate = ema(p.Behavior.ToolApprovalRate, sessionApprovalRate, personaEMAAlpha)
		}

		// Correction frequency.
		sessionCorrFreq := clamp01(totalCorrections / math.Max(n, 1))
		p.Behavior.CorrectionFreq = ema(p.Behavior.CorrectionFreq, sessionCorrFreq, personaEMAAlpha)

		// Question to command ratio.
		sessionQC := clamp01(totalQuestions / math.Max(n, 1))
		p.Behavior.QuestionToCommand = ema(p.Behavior.QuestionToCommand, sessionQC, personaEMAAlpha)

		// Session duration.
		if len(signals) >= 2 {
			duration := signals[len(signals)-1].Timestamp.Sub(signals[0].Timestamp)
			sessionMinutes := duration.Minutes()
			if p.Behavior.AvgSessionMinutes == 0 {
				p.Behavior.AvgSessionMinutes = sessionMinutes
			} else {
				p.Behavior.AvgSessionMinutes = ema(p.Behavior.AvgSessionMinutes, sessionMinutes, personaEMAAlpha)
			}
		}
	}

	// Update interaction summary.
	if p.Interactions != nil {
		p.Interactions.TotalSessions++
		p.Interactions.TotalTurns += len(signals)

		// Create observations from strong session signals.
		if p.SessionContext.DetectedRole != "" && p.SessionContext.TurnsSinceSwitch > 3 {
			p.Interactions.AddObservation(PersonaObservation{
				Text:       "Frequently works in " + p.SessionContext.DetectedRole + " mode",
				Category:   "workflow",
				Confidence: 0.6,
				ObservedAt: time.Now(),
				Source:     "inferred",
			})
		}
	}

	p.LastSeenAt = time.Now()
}

// ema computes the exponential moving average.
func ema(current, newValue, alpha float64) float64 {
	return alpha*newValue + (1-alpha)*current
}

// clamp01 clamps a value to [0.0, 1.0].
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
