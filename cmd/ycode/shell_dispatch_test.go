package main

import (
	"reflect"
	"testing"
)

func TestParseShellArgs(t *testing.T) {
	cases := []struct {
		name        string
		args        []string
		wantCommand string
		wantHelp    bool
		wantErr     bool
		check       func(t *testing.T, f *shellFlags)
	}{
		{
			name:        "bare -c",
			args:        []string{"-c", "echo hello"},
			wantCommand: "echo hello",
		},
		{
			name:        "combined -lc",
			args:        []string{"-lc", "echo hello"},
			wantCommand: "echo hello",
		},
		{
			name:        "split -l -c",
			args:        []string{"-l", "-c", "echo hello"},
			wantCommand: "echo hello",
		},
		{
			name:        "long --login then -c",
			args:        []string{"--login", "-c", "echo hello"},
			wantCommand: "echo hello",
		},
		{
			name:        "shebang positional then -lc",
			args:        []string{"/Users/x/bin/ycode-wrappers/bash", "-lc", "echo hello"},
			wantCommand: "echo hello",
		},
		{
			name:        "agent + workdir + -lc",
			args:        []string{"--agent", "--workdir", "/tmp", "-lc", "pwd"},
			wantCommand: "pwd",
			check: func(t *testing.T, f *shellFlags) {
				if !f.agent {
					t.Errorf("agent: want true")
				}
				if f.workDir != "/tmp" {
					t.Errorf("workDir = %q, want /tmp", f.workDir)
				}
			},
		},
		{
			name:        "rcfile takes value but is ignored",
			args:        []string{"--rcfile", "/etc/profile", "-c", "true"},
			wantCommand: "true",
		},
		{
			name:        "long flag with =value",
			args:        []string{"--workdir=/tmp", "-c", "pwd"},
			wantCommand: "pwd",
			check: func(t *testing.T, f *shellFlags) {
				if f.workDir != "/tmp" {
					t.Errorf("workDir = %q, want /tmp", f.workDir)
				}
			},
		},
		{
			name:     "long --help",
			args:     []string{"--help"},
			wantHelp: true,
		},
		{
			name:     "short -h",
			args:     []string{"-h"},
			wantHelp: true,
		},
		{
			name:    "unknown short -z",
			args:    []string{"-z"},
			wantErr: true,
		},
		{
			name:    "unknown long --bogus",
			args:    []string{"--bogus"},
			wantErr: true,
		},
		{
			name:    "-c with no value",
			args:    []string{"-l", "-c"},
			wantErr: true,
		},
		{
			name:        "-cVALUE attached form",
			args:        []string{"-cecho hi"},
			wantCommand: "echo hi",
		},
		{
			name:        "-- halts parsing, trailing positionals dropped",
			args:        []string{"-c", "echo hi", "--", "--bogus", "ignored"},
			wantCommand: "echo hi",
		},
		{
			name: "default permission",
			args: []string{},
			check: func(t *testing.T, f *shellFlags) {
				if f.permission != "danger-full-access" {
					t.Errorf("permission = %q, want danger-full-access", f.permission)
				}
			},
		},
		{
			name:        "allowed-dirs CSV",
			args:        []string{"--allowed-dirs", "/a,/b,/c", "-c", "true"},
			wantCommand: "true",
			check: func(t *testing.T, f *shellFlags) {
				want := []string{"/a", "/b", "/c"}
				if !reflect.DeepEqual(f.allowedDirs, want) {
					t.Errorf("allowedDirs = %v, want %v", f.allowedDirs, want)
				}
			},
		},
		{
			name: "manifest only",
			args: []string{"--manifest"},
			check: func(t *testing.T, f *shellFlags) {
				if !f.manifestOnly {
					t.Errorf("manifestOnly: want true")
				}
			},
		},
		{
			name:        "all bash compat shorts then -c",
			args:        []string{"-irvxs", "-c", "true"},
			wantCommand: "true",
		},
		{
			// Bash semantics: `-c -l <cmd>` runs <cmd> (with -l consumed
			// as the login flag), NOT -l. The prior implementation bound
			// -l as the value of -c, which surfaced as
			// `"-l": executable file not found in $PATH` once the inner
			// exec ran the literal "-l".
			name:        "-c -l <cmd> binds <cmd> to command",
			args:        []string{"-c", "-l", "echo hi"},
			wantCommand: "echo hi",
		},
		{
			// Same shape but with multiple no-op flags interleaved.
			name:        "-c then several flags then <cmd>",
			args:        []string{"-c", "-l", "-i", "-x", "echo hi"},
			wantCommand: "echo hi",
		},
		{
			// Wrapper-shebang case: kernel injects the script path as a
			// positional before -c, then real args follow. -l after -c
			// must not be bound as the command.
			name:        "shebang positional + -c -l <cmd>",
			args:        []string{"/Users/x/bin/ycode-wrappers/bash", "-c", "-l", "yc symbols foo.go"},
			wantCommand: "yc symbols foo.go",
		},
		{
			// `--` after `-c` makes the very next argv the command,
			// even if it starts with `-`.
			name:        "-c -- -l (literal -l command)",
			args:        []string{"-c", "--", "-l"},
			wantCommand: "-l",
		},
		{
			// Trailing positionals after the command are dropped (they
			// would be $0/$1/... in real bash; ycode shell does not
			// expose those).
			name:        "-c <cmd> trailing positionals dropped",
			args:        []string{"-c", "echo hi", "arg0", "arg1"},
			wantCommand: "echo hi",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, helpReq, err := parseShellArgs(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if helpReq != tc.wantHelp {
				t.Errorf("helpReq = %v, want %v", helpReq, tc.wantHelp)
			}
			if tc.wantCommand != "" && f.command != tc.wantCommand {
				t.Errorf("command = %q, want %q", f.command, tc.wantCommand)
			}
			if tc.check != nil {
				tc.check(t, f)
			}
		})
	}
}
