package spec

import (
	"strings"
	"testing"
)

func TestSessionDispatchValidation(t *testing.T) {
	d := &Document{}
	cases := []struct {
		route CLIDispatch
		args  CLIArgs
		want  string
	}{
		{CLIDispatch{Operation: "session", Action: "list"}, CLIArgs{}, ""},
		{CLIDispatch{Operation: "session", Action: "rename"}, CLIArgs{Min: 2, Max: -1}, ""},
		{CLIDispatch{Operation: "session", Action: "rename"}, CLIArgs{Min: 1, Max: 1}, "requires args"},
		{CLIDispatch{Operation: "session", Action: "delete"}, CLIArgs{}, "session.action must be"},
		{CLIDispatch{Operation: "session", Action: "list", Resource: "models"}, CLIArgs{}, "no resource"},
		{CLIDispatch{Operation: "session", Action: "list", AgentRef: "coder"}, CLIArgs{}, "no resource or routing"},
		{CLIDispatch{Operation: "version", Action: "list"}, CLIArgs{}, "does not accept resource or action"},
	}
	for _, c := range cases {
		err := validateCLIDispatch(d, c.route, c.args, nil)
		if c.want == "" && err != nil || c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)) {
			t.Errorf("%+v %+v: err=%v, want %q", c.route, c.args, err, c.want)
		}
	}
}
