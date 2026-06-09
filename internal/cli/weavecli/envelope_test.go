package weavecli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestIsAgent(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"", false},
		{"0", false},
		{"false", false},
		{"no", false},
		{"1", true},
		{"true", true},
		{"yes", true},
		{"anything", true},
	}
	for _, tc := range cases {
		t.Setenv("YCODE_AGENT", tc.val)
		if got := IsAgent(); got != tc.want {
			t.Errorf("IsAgent with YCODE_AGENT=%q got %v want %v", tc.val, got, tc.want)
		}
	}
}

func TestResolveOutputMode_Precedence(t *testing.T) {
	t.Setenv("YCODE_AGENT", "")
	cases := []struct {
		jsonF, plainF, quietF bool
		want                  OutputMode
	}{
		{true, false, false, OutputJSON},
		{false, true, false, OutputPlain},
		{false, false, true, OutputQuiet},
		{false, false, false, OutputAuto},
		{true, true, true, OutputJSON}, // json wins
	}
	for _, tc := range cases {
		got := ResolveOutputMode(tc.jsonF, tc.plainF, tc.quietF)
		if got != tc.want {
			t.Errorf("ResolveOutputMode(json=%v plain=%v quiet=%v)=%v want %v",
				tc.jsonF, tc.plainF, tc.quietF, got, tc.want)
		}
	}
}

func TestResolveOutputMode_AgentForcesJSON(t *testing.T) {
	t.Setenv("YCODE_AGENT", "1")
	if got := ResolveOutputMode(false, true, false); got != OutputJSON {
		t.Errorf("YCODE_AGENT=1 with --plain should still produce JSON; got %v", got)
	}
}

func TestEmitOK_JSONShape(t *testing.T) {
	var buf bytes.Buffer
	code := EmitOK(&buf, OutputJSON, "weave start", map[string]any{"issue": 123})
	if code != ExitOK {
		t.Errorf("EmitOK returned %d want %d", code, ExitOK)
	}
	var got Envelope
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.SchemaVersion != SchemaVersion {
		t.Errorf("schema_version=%q want %q", got.SchemaVersion, SchemaVersion)
	}
	if got.Command != "weave start" {
		t.Errorf("command=%q want 'weave start'", got.Command)
	}
	if got.Status != "ok" {
		t.Errorf("status=%q want ok", got.Status)
	}
}

func TestEmitError_JSONShape(t *testing.T) {
	var buf bytes.Buffer
	code := EmitError(&buf, OutputJSON, "weave start", ExitPrecondFail, errors.New("queue empty"))
	if code != ExitPrecondFail {
		t.Errorf("EmitError returned %d want %d", code, ExitPrecondFail)
	}
	var got Envelope
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Status != "error" {
		t.Errorf("status=%q want error", got.Status)
	}
	if got.Error == nil || got.Error.Code != "precondition_failed" {
		t.Errorf("error envelope wrong: %+v", got.Error)
	}
	if !strings.Contains(got.Error.Message, "queue empty") {
		t.Errorf("error message lost: %q", got.Error.Message)
	}
}

func TestEmitError_PlainShape(t *testing.T) {
	var buf bytes.Buffer
	code := EmitError(&buf, OutputPlain, "weave add", ExitInvalidArg, errors.New("title required"))
	if code != ExitInvalidArg {
		t.Errorf("EmitError returned %d want %d", code, ExitInvalidArg)
	}
	got := buf.String()
	if !strings.Contains(got, "weave add:") || !strings.Contains(got, "title required") {
		t.Errorf("plain text format wrong: %q", got)
	}
}
