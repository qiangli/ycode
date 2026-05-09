package agentmode

import (
	"bufio"
	"encoding/json"
	"io"
	"sort"
	"strings"
)

// MissedEntry is a single row in the un-hinted-commands report.
type MissedEntry struct {
	Cmd    string
	Count  int
	Sample string // first verbatim cmd seen for this group (for display)
}

// Stats summarizes catalog hit-rate and per-id distribution over a JSONL
// sink stream.
type Stats struct {
	TotalRecords int
	PreRecords   int
	PostRecords  int
	HitPre       int // pre-records with at least one fired hint
	MissPre      int // pre-records with no fired hint
	HitRatePre   float64
	ByID         map[string]int
	ByPhase      map[string]int
}

// Missed returns un-hinted pre-execution commands, grouped by a
// normalization (lowercase first whitespace-separated token) and sorted
// by frequency. Only "pre" records participate; post records have no
// command string and would muddy the signal.
func Missed(r io.Reader) ([]MissedEntry, error) {
	groups := map[string]*MissedEntry{}
	if err := walk(r, func(rec Record) {
		if rec.Phase != "pre" {
			return
		}
		if len(rec.FiredIDs) > 0 {
			return
		}
		key := normalize(rec.Cmd)
		if key == "" {
			return
		}
		e, ok := groups[key]
		if !ok {
			e = &MissedEntry{Cmd: key, Sample: rec.Cmd}
			groups[key] = e
		}
		e.Count++
	}); err != nil {
		return nil, err
	}
	out := make([]MissedEntry, 0, len(groups))
	for _, e := range groups {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Cmd < out[j].Cmd
	})
	return out, nil
}

// ComputeStats returns aggregate hit-rate and per-id counts over the
// JSONL stream.
func ComputeStats(r io.Reader) (Stats, error) {
	s := Stats{
		ByID:    map[string]int{},
		ByPhase: map[string]int{},
	}
	if err := walk(r, func(rec Record) {
		s.TotalRecords++
		s.ByPhase[rec.Phase]++
		switch rec.Phase {
		case "pre":
			s.PreRecords++
			if len(rec.FiredIDs) > 0 {
				s.HitPre++
			} else {
				s.MissPre++
			}
		case "post":
			s.PostRecords++
		}
		for _, id := range rec.FiredIDs {
			s.ByID[id]++
		}
	}); err != nil {
		return s, err
	}
	if s.PreRecords > 0 {
		s.HitRatePre = float64(s.HitPre) / float64(s.PreRecords)
	}
	return s, nil
}

// walk decodes the JSONL stream, calling visit for each record.
// Malformed lines are skipped (best-effort: an old format or partial
// write shouldn't poison the report).
func walk(r io.Reader, visit func(Record)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		visit(rec)
	}
	return scanner.Err()
}

// normalize collapses a raw command into a frequency-grouping key. Today
// it returns the lowercase first whitespace-separated token — enough to
// answer "which utilities miss the catalog the most." More elaborate
// canonicalization (flag stripping, path generalization) is Phase 2.
func normalize(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	if i := strings.IndexAny(cmd, " \t"); i > 0 {
		return strings.ToLower(cmd[:i])
	}
	return strings.ToLower(cmd)
}
