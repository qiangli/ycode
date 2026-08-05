package prompt

import (
	"strings"
	"testing"
)

func TestTasksSectionOperationalEvidence(t *testing.T) {
	section := TasksSection()
	for _, want := range []string{
		"BatchMode=yes",
		"Silence is not evidence that a process is hung",
		"compare at least two observations",
		"Separate observation from attribution",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("TasksSection missing operational guidance %q", want)
		}
	}
}
