package turn

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// observationText renders one tool result the way a terminal shows it: the
// exit status, the command's stdout, its stderr labelled, and a marker when
// output was cut. It is what the model reads when the agent's YAML asks for
// messages.append-tool-results with {format: observation}; the full record
// (binding, intent, effects, job ids) stays in the event store either way.
// A small model cannot read base64 chunks or pick an exit code out of an
// execution envelope; it can read this.
func observationText(fields map[string]any) string {
	outcome := text(fields["outcome"])
	switch outcome {
	case "denied":
		why := unsupportedReasons(fields["unsupported"])
		if rule := text(fields["rule_id"]); rule != "" {
			return fmt.Sprintf("denied by policy rule %q; the command did not run", rule) + why
		}
		return "denied by policy; the command did not run" + why
	case "rejected":
		return "rejected by the reviewer; the command did not run"
	}
	var b strings.Builder
	process, _ := fields["process"].(map[string]any)
	if code, ok := number(process["exitCode"]); ok {
		fmt.Fprintf(&b, "exit %d", code)
		if outcome != "" && outcome != "completed" {
			fmt.Fprintf(&b, " (%s)", outcome)
		}
	} else if outcome != "" {
		b.WriteString(outcome)
	} else {
		b.WriteString("finished")
	}
	output, _ := fields["output"].(map[string]any)
	stdout := decodeChunks(output["stdout"])
	stderr := decodeChunks(output["stderr"])
	if stdout != "" {
		b.WriteString("\n")
		b.WriteString(strings.TrimRight(stdout, "\n"))
	}
	if stderr != "" {
		b.WriteString("\nstderr:\n")
		b.WriteString(strings.TrimRight(stderr, "\n"))
	}
	if truncated, _ := output["truncated"].(bool); truncated {
		b.WriteString("\n[output truncated]")
	}
	return b.String()
}

// decodeChunks joins an output stream's chunks in order. A chunk is
// {data, encoding, from, to}; base64 is decoded, anything else is taken as
// text.
func decodeChunks(raw any) string {
	list, _ := raw.([]any)
	type chunk struct {
		from int
		data string
	}
	chunks := make([]chunk, 0, len(list))
	for _, item := range list {
		entry, _ := item.(map[string]any)
		if entry == nil {
			continue
		}
		data := text(entry["data"])
		if text(entry["encoding"]) == "base64" {
			decoded, err := base64.StdEncoding.DecodeString(data)
			if err != nil {
				continue
			}
			data = string(decoded)
		}
		from, _ := number(entry["from"])
		chunks = append(chunks, chunk{from: from, data: data})
	}
	sort.SliceStable(chunks, func(i, j int) bool { return chunks[i].from < chunks[j].from })
	var b strings.Builder
	for _, c := range chunks {
		b.WriteString(c.data)
	}
	return b.String()
}

// unsupportedReasons renders what the preflight could not prove (a parse
// error, a dynamic command) as ": reason; reason", or "" when there is none.
func unsupportedReasons(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	var facts []struct {
		Reason string `json:"reason"`
	}
	if json.Unmarshal(data, &facts) != nil {
		return ""
	}
	var reasons []string
	seen := map[string]bool{}
	for _, fact := range facts {
		if fact.Reason != "" && !seen[fact.Reason] {
			seen[fact.Reason] = true
			reasons = append(reasons, fact.Reason)
		}
	}
	if len(reasons) == 0 {
		return ""
	}
	return ": " + strings.Join(reasons, "; ")
}
