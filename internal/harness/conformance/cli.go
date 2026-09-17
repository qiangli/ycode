package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/qiangli/ycode/internal/harness/cli"
	"github.com/qiangli/ycode/internal/harness/spec"
)

// CLIFixture supplies deterministic transport inputs for any compiled profile.
type CLIFixture struct {
	Args     []string          `json:"args" yaml:"args"`
	Stdin    string            `json:"stdin" yaml:"stdin"`
	Terminal bool              `json:"terminal" yaml:"terminal"`
	Env      map[string]string `json:"env" yaml:"env"`
}

type CLIOutcome struct {
	Stdout               string            `json:"stdout"`
	Stderr               string            `json:"stderr"`
	ExitCode             int               `json:"exitCode"`
	ErrorClass           string            `json:"errorClass,omitempty"`
	Invocations          []cli.Invocation  `json:"invocations,omitempty"`
	DispatchPayloads     []json.RawMessage `json:"dispatchPayloads,omitempty"`
	CompletionCandidates []string          `json:"completionCandidates,omitempty"`
}

// CLIDispatcher executes or records a fixture's dispatch, returning its observed
// payload for comparison. Runtime-backed fixtures can use the real harness;
// parser fixtures can inject a deterministic recorder without provider calls.
type CLIDispatcher func(context.Context, cli.Invocation, cli.IO) (json.RawMessage, error)

// RunCLI exercises the same compiler-backed tree and error printing contract as
// the binary. It creates no runtime, default configuration or alternate loop.
func RunCLI(ctx context.Context, doc *spec.Document, fixture CLIFixture, options cli.Options, dispatcher CLIDispatcher) CLIOutcome {
	var stdout, stderr bytes.Buffer
	outcome := CLIOutcome{}
	options.IsTerminal = fixture.Terminal
	options.LookupEnv = func(name string) (string, bool) { value, ok := fixture.Env[name]; return value, ok }
	root, err := cli.New(doc, func(ctx context.Context, invocation cli.Invocation, streams cli.IO) error {
		outcome.Invocations = append(outcome.Invocations, invocation)
		if dispatcher == nil {
			return errors.New("conformance fixture requires an explicit dispatcher")
		}
		payload, err := dispatcher(ctx, invocation, streams)
		if len(payload) != 0 {
			outcome.DispatchPayloads = append(outcome.DispatchPayloads, append(json.RawMessage(nil), payload...))
		}
		return err
	}, options)
	if err == nil {
		root.SetIn(strings.NewReader(fixture.Stdin))
		root.SetOut(&stdout)
		root.SetErr(&stderr)
		root.SetArgs(fixture.Args)
		err = root.ExecuteContext(ctx)
	}
	if err != nil {
		fmt.Fprintln(&stderr, err)
		var classified *cli.Error
		if errors.As(err, &classified) {
			outcome.ErrorClass = classified.Class
		} else {
			outcome.ErrorClass = "runtime"
		}
	}
	outcome.Stdout, outcome.Stderr, outcome.ExitCode = stdout.String(), stderr.String(), cli.ExitCode(err)
	if len(fixture.Args) > 0 && (fixture.Args[0] == "__complete" || fixture.Args[0] == "__completeNoDesc") {
		for _, line := range strings.Split(strings.TrimSuffix(outcome.Stdout, "\n"), "\n") {
			if line == "" || strings.HasPrefix(line, ":") {
				continue
			}
			name, _, _ := strings.Cut(line, "\t")
			outcome.CompletionCandidates = append(outcome.CompletionCandidates, name)
		}
	}
	return outcome
}
