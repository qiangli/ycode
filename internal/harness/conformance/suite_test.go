package conformance

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/harness/spec"
)

// TestLifecycleConformanceInventory keeps the release gate honest: every
// promised lifecycle surface must retain an executable assertion, rather than
// being represented only by prose in a sprint document.
func TestLifecycleConformanceInventory(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	gates := map[string]string{
		"providers":               "internal/harness/provider/provider_test.go#func TestAdaptersNormalizeOneBashyStream",
		"five-profile-goldens":    "internal/harness/pipeline/reconstruction_test.go#func TestReconstructionFixturesCompileAndMatchGoldenTraces",
		"bashy-sync-outcomes":     "internal/harness/bashy/executor_test.go#func TestExecuteBindsAllGraphAuthorityAndBinaryOutput",
		"bashy-durable-outcomes":  "internal/harness/bashy/executor_test.go#func TestDurableStartStdinAndCursorPoll",
		"event-replay":            "internal/harness/event/store_test.go#func TestStoreAppendAndReplay",
		"restart-hitl":            "internal/harness/stages/hitl/hitl_test.go#func TestAskCheckpointsBeforeWaitingAndApproveSurvivesRestart",
		"live-hitl-resume":        "pkg/ycode/harness_test.go#func TestHarnessResumeContinuesSuspendedGraphWithoutRestart",
		"compaction":              "internal/harness/stages/memory/memory_test.go#func TestCompactionUsesCompiledTriggerRoutePreservationAndFailure",
		"subagents":               "internal/harness/agent/roster_test.go#func TestRosterUsesSamePipelineForRootAndDelegatedAgent",
		"pty":                     "internal/harness/frontend/pty_test.go#func TestPTYREPLProjectsCanonicalEvents",
		"local-frontends":         "internal/harness/frontend/local_surfaces_test.go#func TestOneShotStdinREPLAndTUIHaveCanonicalParity",
		"http-websocket":          "internal/harness/frontend/network_ws_nats_test.go#func TestWebSocketAuthCanonicalParityAndCompiledAddress",
		"nats":                    "internal/harness/frontend/network_ws_nats_test.go#func TestNATSSubscriptionUsesCompiledBoundaryAndCanonicalEvents",
		"public-embedding":        "pkg/ycode/harness_test.go#func TestHarnessLoadValidateRunStreamsDurableEvents",
		"platform-effects":        "internal/harness/spec/spec_test.go#func TestCompileRejectsUnsupportedPlatformAndEffectFallback",
		"acp-protocol":            "cmd/ycode/acp_test.go#func TestServeACPHandshakeNegotiatesProtocolV1",
		"acp-restart-resume":      "cmd/ycode/acp_test.go#func TestACPRunnerReusesHarnessAndPersistsSessionAcrossRestart",
		"acp-fork-lineage":        "internal/harness/acp/store_test.go#func TestForkLineageHashChainSurvivesReplay",
		"utility-yaml-boundary":   "cmd/ycode/utility_harness_test.go#func TestShellOneShotUsesCompiledBashyPolicyBoundary",
		"observability-lifecycle": "internal/harness/observe/otel_test.go#func TestPipelineRunnerEmitsPipelineAndStageSpans",
	}
	for capability, anchor := range gates {
		parts := strings.SplitN(anchor, "#", 2)
		if len(parts) != 2 {
			t.Fatalf("%s has invalid gate anchor %q", capability, anchor)
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(parts[0])))
		if err != nil {
			t.Fatalf("%s gate: %v", capability, err)
		}
		if !bytes.Contains(data, []byte(parts[1])) {
			t.Errorf("%s gate is stale: %s", capability, anchor)
		}
	}

	for _, profile := range []string{"ycode", "codex-like", "opencode-like", "openclaw-like", "hermes-like"} {
		fixture := filepath.Join(root, "examples", "harness-reconstructions", profile+".yaml")
		if _, err := spec.Load(fixture); err != nil {
			t.Errorf("%s fixture: %v", profile, err)
		}
		if _, err := os.Stat(filepath.Join(root, "testdata", "harness-reconstructions", profile+".golden.jsonl")); err != nil {
			t.Errorf("%s golden: %v", profile, err)
		}
	}
}

func TestPlatformEffectDispositionIsExplicit(t *testing.T) {
	switch runtime.GOOS {
	case "darwin", "linux":
		t.Logf("supported: PTY lifecycle conformance runs on %s-%s", runtime.GOOS, runtime.GOARCH)
	case "windows":
		t.Log("intentionally unsupported: native ConPTY is outside the frozen harness contract; non-PTY frontends and strict unsupportedEffect=reject remain required")
	default:
		t.Logf("intentionally unsupported: PTY lifecycle conformance has no declared adapter for %s; strict unsupportedEffect=reject remains required", runtime.GOOS)
	}
}
