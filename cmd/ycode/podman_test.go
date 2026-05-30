package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/qiangli/ycode/internal/container"
)

func TestPodmanCmdStructure(t *testing.T) {
	cmd := newPodmanCmd()

	if cmd.Use != "podman" {
		t.Errorf("expected Use 'podman', got %q", cmd.Use)
	}

	// Verify the docker alias.
	found := false
	for _, a := range cmd.Aliases {
		if a == "docker" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'docker' alias")
	}

	// Verify subcommands are registered.
	expected := map[string]bool{
		"ps":      false,
		"images":  false,
		"pull":    false,
		"exec":    false,
		"logs":    false,
		"stop":    false,
		"rm":      false,
		"run":     false,
		"version": false,
		"inspect": false,
		"build":   false,
		"network": false,
	}

	for _, sub := range cmd.Commands() {
		if _, ok := expected[sub.Name()]; ok {
			expected[sub.Name()] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("missing subcommand: %s", name)
		}
	}
}

func TestPodmanPsFlags(t *testing.T) {
	cmd := newPodmanCmd()
	ps, _, err := cmd.Find([]string{"ps"})
	if err != nil {
		t.Fatalf("find ps: %v", err)
	}
	f := ps.Flags().Lookup("all")
	if f == nil {
		t.Error("ps missing --all flag")
	}
	if f.Shorthand != "a" {
		t.Errorf("expected -a shorthand, got %q", f.Shorthand)
	}
}

func TestPodmanLogsFlags(t *testing.T) {
	cmd := newPodmanCmd()
	logs, _, err := cmd.Find([]string{"logs"})
	if err != nil {
		t.Fatalf("find logs: %v", err)
	}

	f := logs.Flags().Lookup("follow")
	if f == nil {
		t.Error("logs missing --follow flag")
	}
	if f.Shorthand != "f" {
		t.Errorf("expected -f shorthand, got %q", f.Shorthand)
	}

	tail := logs.Flags().Lookup("tail")
	if tail == nil {
		t.Error("logs missing --tail flag")
	}
}

func TestPodmanRmFlags(t *testing.T) {
	cmd := newPodmanCmd()
	rm, _, err := cmd.Find([]string{"rm"})
	if err != nil {
		t.Fatalf("find rm: %v", err)
	}
	f := rm.Flags().Lookup("force")
	if f == nil {
		t.Error("rm missing --force flag")
	}
	if f.Shorthand != "f" {
		t.Errorf("expected -f shorthand, got %q", f.Shorthand)
	}
}

func TestPodmanRunFlags(t *testing.T) {
	cmd := newPodmanCmd()
	run, _, err := cmd.Find([]string{"run"})
	if err != nil {
		t.Fatalf("find run: %v", err)
	}

	if rm := run.Flags().Lookup("rm"); rm == nil {
		t.Error("run missing --rm flag")
	}
	if d := run.Flags().Lookup("detach"); d == nil {
		t.Error("run missing --detach flag")
	}
}

func TestPodmanPullArgsValidation(t *testing.T) {
	cmd := newPodmanCmd()

	// pull requires exactly 1 arg — invoke via parent with no image arg.
	cmd.SetArgs([]string{"pull"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Error("pull without args should fail")
	}
}

func TestPodmanExecArgsValidation(t *testing.T) {
	cmd := newPodmanCmd()

	// exec requires at least 2 args (container + command).
	cmd.SetArgs([]string{"exec", "mycontainer"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Error("exec with only 1 arg should fail")
	}
}

func TestPodmanStopArgsValidation(t *testing.T) {
	cmd := newPodmanCmd()

	// stop requires at least 1 arg.
	cmd.SetArgs([]string{"stop"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Error("stop without args should fail")
	}
}

func TestPodmanRmArgsValidation(t *testing.T) {
	cmd := newPodmanCmd()

	// rm requires at least 1 arg.
	cmd.SetArgs([]string{"rm"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Error("rm without args should fail")
	}
}

func TestPodmanInspectArgsValidation(t *testing.T) {
	cmd := newPodmanCmd()

	// inspect requires exactly 1 arg.
	cmd.SetArgs([]string{"inspect"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Error("inspect without args should fail")
	}
}

func TestPodmanRunArgsValidation(t *testing.T) {
	cmd := newPodmanCmd()

	// run requires at least 1 arg (image).
	cmd.SetArgs([]string{"run"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Error("run without args should fail")
	}
}

func TestPodmanSubcommandShortDescriptions(t *testing.T) {
	cmd := newPodmanCmd()
	for _, sub := range cmd.Commands() {
		if sub.Short == "" {
			t.Errorf("subcommand %q has no short description", sub.Name())
		}
	}
}

func TestPodmanRunFlagsExtended(t *testing.T) {
	cmd := newPodmanCmd()
	run, _, err := cmd.Find([]string{"run"})
	if err != nil {
		t.Fatalf("find run: %v", err)
	}
	for _, name := range []string{"name", "network", "publish", "volume", "env", "env-file", "privileged", "cap-add"} {
		if f := run.Flags().Lookup(name); f == nil {
			t.Errorf("run missing --%s flag", name)
		}
	}
	if f := run.Flags().Lookup("publish"); f != nil && f.Shorthand != "p" {
		t.Errorf("expected -p shorthand for --publish, got %q", f.Shorthand)
	}
	if f := run.Flags().Lookup("volume"); f != nil && f.Shorthand != "v" {
		t.Errorf("expected -v shorthand for --volume, got %q", f.Shorthand)
	}
	if f := run.Flags().Lookup("env"); f != nil && f.Shorthand != "e" {
		t.Errorf("expected -e shorthand for --env, got %q", f.Shorthand)
	}
}

func TestPodmanBuildCmd(t *testing.T) {
	cmd := newPodmanCmd()
	build, _, err := cmd.Find([]string{"build"})
	if err != nil {
		t.Fatalf("find build: %v", err)
	}
	for _, name := range []string{"tag", "file", "build-arg"} {
		if f := build.Flags().Lookup(name); f == nil {
			t.Errorf("build missing --%s flag", name)
		}
	}

	// Sanity: build without --tag should fail at the CLI layer (before we
	// try to dial the engine). Route through the parent so cobra's flag
	// parsing runs the same way real CLI invocations do.
	cmd.SetArgs([]string{"build", "."})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Error("build without --tag should fail")
	}
}

func TestPodmanNetworkCmd(t *testing.T) {
	cmd := newPodmanCmd()
	net, _, err := cmd.Find([]string{"network"})
	if err != nil {
		t.Fatalf("find network: %v", err)
	}
	got := map[string]bool{}
	for _, sub := range net.Commands() {
		got[sub.Name()] = true
	}
	for _, want := range []string{"create", "ls", "rm"} {
		if !got[want] {
			t.Errorf("network missing %s subcommand", want)
		}
	}
}

func TestParsePortMappings(t *testing.T) {
	tests := []struct {
		in      []string
		want    []container.PortMapping
		wantErr bool
	}{
		{in: nil, want: nil},
		{
			in: []string{"8080:80"},
			want: []container.PortMapping{
				{HostPort: 8080, ContainerPort: 80, Protocol: "tcp"},
			},
		},
		{
			in: []string{"5353:53/udp"},
			want: []container.PortMapping{
				{HostPort: 5353, ContainerPort: 53, Protocol: "udp"},
			},
		},
		{
			in: []string{"80"},
			want: []container.PortMapping{
				{HostPort: 80, ContainerPort: 80, Protocol: "tcp"},
			},
		},
		{in: []string{"abc"}, wantErr: true},
		{in: []string{"99999:80"}, wantErr: true},
	}
	for _, tt := range tests {
		got, err := parsePortMappings(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("parsePortMappings(%v) err=%v wantErr=%v", tt.in, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parsePortMappings(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestParseMounts(t *testing.T) {
	tmp := t.TempDir()
	got, err := parseMounts([]string{tmp + ":/etc/conf:ro"})
	if err != nil {
		t.Fatalf("parseMounts: %v", err)
	}
	if len(got) != 1 || got[0].Source != tmp || got[0].Target != "/etc/conf" || !got[0].ReadOnly {
		t.Errorf("parseMounts ro mount: got %#v", got)
	}

	// Relative source gets resolved to absolute.
	cwd, _ := os.Getwd()
	got2, err := parseMounts([]string{"./localdir:/in"})
	if err != nil {
		t.Fatalf("parseMounts relative: %v", err)
	}
	if got2[0].Source != filepath.Join(cwd, "localdir") {
		t.Errorf("parseMounts relative: source not resolved, got %q", got2[0].Source)
	}
	if got2[0].ReadOnly {
		t.Errorf("parseMounts default rw: got ReadOnly=true")
	}

	if _, err := parseMounts([]string{"bad"}); err == nil {
		t.Error("malformed volume spec should error")
	}
	if _, err := parseMounts([]string{"/a:/b:wat"}); err == nil {
		t.Error("invalid mount option should error")
	}
}

func TestIsNamedVolumeSource(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"cloudbox-gomodcache", true},
		{"my_data", true},
		{"vol.1", true},
		{"v1", true},
		{"/abs/path", false},     // absolute path
		{"./relative", false},    // relative path with dot prefix
		{"../parent", false},     // parent-relative path
		{"sub/dir", false},       // contains slash
		{`win\path`, false},      // contains backslash
		{"a", false},             // too short (1 char)
		{"-leading-dash", false}, // must start alphanumeric
		{"_underscore", false},   // must start alphanumeric
		{"has space", false},     // space invalid
		{"with$dollar", false},   // $ invalid
	}
	for _, c := range cases {
		got := isNamedVolumeSource(c.in)
		if got != c.want {
			t.Errorf("isNamedVolumeSource(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestMaterializeNamedVolume(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := materializeNamedVolume("cloudbox-gomodcache")
	if err != nil {
		t.Fatalf("materializeNamedVolume: %v", err)
	}
	want := filepath.Join(home, ".agents", "ycode", "container", "volumes", "cloudbox-gomodcache")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Errorf("expected dir at %q, stat err=%v", path, err)
	}

	// Idempotent: second call returns same path, no error.
	path2, err := materializeNamedVolume("cloudbox-gomodcache")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if path2 != path {
		t.Errorf("non-idempotent: got %q vs %q", path2, path)
	}

	// Invalid names rejected.
	if _, err := materializeNamedVolume("bad name"); err == nil {
		t.Error("space in name should error")
	}
	if _, err := materializeNamedVolume(""); err == nil {
		t.Error("empty name should error")
	}
}

func TestParseMountsNamedVolume(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := parseMounts([]string{"cloudbox-gomodcache:/go/pkg/mod"})
	if err != nil {
		t.Fatalf("parseMounts: %v", err)
	}
	want := filepath.Join(home, ".agents", "ycode", "container", "volumes", "cloudbox-gomodcache")
	if len(got) != 1 {
		t.Fatalf("want 1 mount, got %d", len(got))
	}
	if got[0].Source != want {
		t.Errorf("Source = %q, want %q", got[0].Source, want)
	}
	if got[0].Target != "/go/pkg/mod" {
		t.Errorf("Target = %q, want /go/pkg/mod", got[0].Target)
	}
	if got[0].ReadOnly {
		t.Errorf("named volume defaulted to read-only")
	}
	if info, err := os.Stat(want); err != nil || !info.IsDir() {
		t.Errorf("named-volume dir not created at %q (err=%v)", want, err)
	}
}

func TestCollectEnv(t *testing.T) {
	tmp := t.TempDir()
	envFile := filepath.Join(tmp, "env")
	body := "# comment\n\nFOO=fromfile\nBAR=keep\n"
	if err := os.WriteFile(envFile, []byte(body), 0o644); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	got, err := collectEnv([]string{"FOO=fromflag", "BAZ=newvar"}, []string{envFile})
	if err != nil {
		t.Fatalf("collectEnv: %v", err)
	}
	// -e should win on collision with --env-file
	want := map[string]string{"FOO": "fromflag", "BAR": "keep", "BAZ": "newvar"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("collectEnv: got %v, want %v", got, want)
	}

	if _, err := collectEnv([]string{"noequals"}, nil); err == nil {
		t.Error("malformed -e value should error")
	}
}

func TestParseBuildArgs(t *testing.T) {
	got, err := parseBuildArgs([]string{"K1=V1", "K2=V=with=equals"})
	if err != nil {
		t.Fatalf("parseBuildArgs: %v", err)
	}
	want := map[string]string{"K1": "V1", "K2": "V=with=equals"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseBuildArgs: got %v, want %v", got, want)
	}

	if _, err := parseBuildArgs([]string{"noequals"}); err == nil {
		t.Error("malformed --build-arg should error")
	}
}

func TestHelpers(t *testing.T) {
	if got := truncStr("abcdefghijklmnop", 12); got != "abcdefghijkl" {
		t.Errorf("truncStr: got %q", got)
	}
	if got := truncStr("short", 12); got != "short" {
		t.Errorf("truncStr short: got %q", got)
	}

	if got := formatSize(1.5e9); got != "1.5 GB" {
		t.Errorf("formatSize GB: got %q", got)
	}
	if got := formatSize(50e6); got != "50.0 MB" {
		t.Errorf("formatSize MB: got %q", got)
	}
	if got := formatSize(1024); got != "1024 B" {
		t.Errorf("formatSize B: got %q", got)
	}
}
