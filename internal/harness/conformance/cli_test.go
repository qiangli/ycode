package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/qiangli/ycode/internal/harness/cli"
	"github.com/qiangli/ycode/internal/harness/spec"
)

func TestReusableCLIConformanceRecordsObservedOutcomes(t *testing.T) {
	doc, err := spec.Load(filepath.Join("..", "..", "..", "examples", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	observed := RunCLI(context.Background(), doc, CLIFixture{Args: []string{"prompt", "hello"}}, cli.Options{}, func(_ context.Context, inv cli.Invocation, streams cli.IO) (json.RawMessage, error) {
		if inv.Dispatch.FrontendRef != "one-shot" || inv.Mode != "args" {
			t.Fatalf("invocation = %#v", inv)
		}
		_, err := fmt.Fprintln(streams.Out, "fixture answer")
		return json.RawMessage(`{"request":"hello"}`), err
	})
	if observed.Stdout != "fixture answer\n" || observed.Stderr != "" || observed.ExitCode != 0 || len(observed.Invocations) != 1 || string(observed.DispatchPayloads[0]) != `{"request":"hello"}` {
		t.Fatalf("outcome = %#v", observed)
	}
	failure := RunCLI(context.Background(), doc, CLIFixture{Args: []string{"prompt", "--undeclared"}}, cli.Options{}, nil)
	if failure.ExitCode != 2 || failure.ErrorClass != "usage" || failure.Stdout != "" || len(failure.Invocations) != 0 || failure.Stderr != "error: invalid command flags\n" {
		t.Fatalf("failure = %#v", failure)
	}
	completion := RunCLI(context.Background(), doc, CLIFixture{Args: []string{"__complete", "completion", "f"}}, cli.Options{}, nil)
	if completion.ExitCode != 0 || !reflect.DeepEqual(completion.CompletionCandidates, []string{"fish"}) {
		t.Fatalf("completion = %#v", completion)
	}
}
