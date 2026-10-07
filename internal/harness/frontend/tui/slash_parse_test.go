package tui

// Sprint: #387; Story: #1625; Story-ID: 27b3dfdf7e76

import (
	"reflect"
	"strings"
	"testing"
)

// /config FILE takes a quoted path with spaces as one literal FILE; an
// ordinary path is unchanged, nothing is expanded or substituted, and the
// free-text slashes still split on whitespace as before.
func TestParseSlashConfigQuoting(t *testing.T) {
	for _, tc := range []struct {
		line string
		name string
		args []string
	}{
		{"/config", "/config", nil},
		{"/config agents/b.yaml", "/config", []string{"agents/b.yaml"}},
		{`/config "my configs/b.yaml"`, "/config", []string{"my configs/b.yaml"}},
		{"/config\t'my configs/b.yaml'  ", "/config", []string{"my configs/b.yaml"}},
		{`/config my' 'dir/"it's.yaml"`, "/config", []string{"my dir/it's.yaml"}},
		{"/config '$(touch pwned) `id` $HOME ~/*.yaml'", "/config", []string{"$(touch pwned) `id` $HOME ~/*.yaml"}},
		{`/config a\ b.yaml`, "/config", []string{`a\`, "b.yaml"}},
		{"/config a.yaml b.yaml", "/config", []string{"a.yaml", "b.yaml"}},
		{"/plan don't stop   now", "/plan", []string{"don't", "stop", "now"}},
		{`/save "my title"`, "/save", []string{`"my`, `title"`}},
		{"/model\tsecondary", "/model", []string{"secondary"}},
	} {
		s, args, ok, err := parseSlash(tc.line)
		if !ok || err != nil || s.Name != tc.name || !reflect.DeepEqual(args, tc.args) {
			t.Errorf("parseSlash(%q) = %q %q ok=%v err=%v, want %q %q", tc.line, s.Name, args, ok, err, tc.name, tc.args)
		}
	}
	for _, line := range []string{`/config "my configs/b.yaml`, "/config it's.yaml"} {
		if s, _, ok, err := parseSlash(line); !ok || s.Name != "/config" || err == nil || !strings.Contains(err.Error(), "unterminated") {
			t.Errorf("parseSlash(%q) = %q ok=%v err=%v, want an unterminated-quote error", line, s.Name, ok, err)
		}
	}
	for _, line := range []string{"", "hello /config", "/configx a", "/usr/bin/ls"} {
		if _, _, ok, _ := parseSlash(line); ok {
			t.Errorf("parseSlash(%q) matched a slash", line)
		}
	}
}
