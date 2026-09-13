package spec

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadDesignFixture(t *testing.T) {
	doc, err := Load(fixturePath())
	if err != nil {
		t.Fatal(err)
	}
	if doc.Spec.Runtime.DefaultAgentRef != "coder" {
		t.Fatalf("default agent = %q", doc.Spec.Runtime.DefaultAgentRef)
	}
	if doc.Spec.Providers["openai"].Credentials.APIKey.SecretRef.Name != "OPENAI_API_KEY" {
		t.Fatal("secretRef was not preserved structurally")
	}
	if doc.Spec.Providers["openai"].Endpoint.ValueFrom.Env != "OPENAI_BASE_URL" {
		t.Fatal("valueFrom was not preserved structurally")
	}
	if doc.Spec.Bashy.Contract != "bashy-run-v1" {
		t.Fatal("singleton Bashy contract was not decoded")
	}
}

func TestJSONSchemaHasStrictSpecEnvelope(t *testing.T) {
	data, err := JSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	props := schema["properties"].(map[string]any)
	for _, name := range []string{"apiVersion", "kind", "metadata", "spec"} {
		if _, ok := props[name]; !ok {
			t.Fatalf("missing %q", name)
		}
	}
	if len(props) != 4 {
		t.Fatalf("top-level properties = %v", props)
	}
}

func TestCompileRejectsOldFlatShape(t *testing.T) {
	data := []byte("apiVersion: ycode.dev/v1alpha1\nkind: Harness\nharness:\n  tools: {}\n")
	_, err := Compile("old-flat.yaml", data)
	if err == nil || !strings.Contains(err.Error(), "field harness not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestCompileRejectsUnknownDuplicateAliasTagAndMultipleDocuments(t *testing.T) {
	cases := []string{
		"apiVersion: ycode.dev/v1alpha1\nkind: Harness\nunknown: true\n",
		"apiVersion: one\napiVersion: two\n",
		"apiVersion: &v ycode.dev/v1alpha1\nkind: *v\n",
		"apiVersion: !unsafe ycode.dev/v1alpha1\nkind: Harness\n",
		"apiVersion: ycode.dev/v1alpha1\nkind: Harness\n---\napiVersion: ycode.dev/v1alpha1\n",
	}
	for _, input := range cases {
		if _, err := Compile("agent.yaml", []byte(input)); err == nil {
			t.Fatalf("Compile(%q) succeeded", input)
		}
	}
}

func TestCompileRejectsScalarEnvironmentAndSecret(t *testing.T) {
	fixture := readFixture(t)
	mutations := []func(string) string{
		func(s string) string {
			return strings.Replace(s, "valueFrom:\n          env: OPENAI_BASE_URL\n          default: https://api.openai.com/v1", "${OPENAI_BASE_URL}", 1)
		},
		func(s string) string {
			return strings.Replace(s, "secretRef: {provider: env, name: OPENAI_API_KEY}", "plain-text-secret", 1)
		},
	}
	for _, mutate := range mutations {
		if _, err := Compile(fixturePath(), []byte(mutate(string(fixture)))); err == nil {
			t.Fatal("unsafe scalar value was accepted")
		}
	}
}

func TestCompileRejectsTypeConfusedReference(t *testing.T) {
	fixture := strings.Replace(string(readFixture(t)), "providerRef: openai", "providerRef: main", 1)
	_, err := Compile(fixturePath(), []byte(fixture))
	if err == nil || !strings.Contains(err.Error(), `references unknown spec.providers "main"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestCompileRejectsMissingListReference(t *testing.T) {
	fixture := strings.Replace(string(readFixture(t)), "hookRefs: [after-tool-audit]", "hookRefs: [missing-hook]", 1)
	_, err := Compile(fixturePath(), []byte(fixture))
	if err == nil || !strings.Contains(err.Error(), `references unknown spec.hooks "missing-hook"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestCompileValidatesReferencesInsideStageConfiguration(t *testing.T) {
	fixture := strings.Replace(string(readFixture(t)), "sinkRefs: [primary-output]", "sinkRefs: [missing-sink]", 1)
	_, err := Compile(fixturePath(), []byte(fixture))
	if err == nil || !strings.Contains(err.Error(), `references unknown spec.sinks "missing-sink"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestCompileValidatesRuntimeAndSubagentReferences(t *testing.T) {
	for _, fixture := range []string{
		strings.Replace(string(readFixture(t)), "defaultAgentRef: coder", "defaultAgentRef: missing", 1),
		strings.Replace(string(readFixture(t)), "agentRef: reviewer", "agentRef: missing", 1),
	} {
		if _, err := Compile(fixturePath(), []byte(fixture)); err == nil {
			t.Fatal("missing typed reference was accepted")
		}
	}
}

func TestResolveSourceRejectsSymlinkEscape(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}
	doc := Document{BaseDir: dir, Spec: Spec{Runtime: Runtime{ReadableRoots: []string{"."}}, Sources: map[string]Source{"bad": {File: &SourceFile{Path: "escape", Required: true}, Limits: SourceLimits{MaxBytes: 8}}}}}
	if err := doc.resolveSources(); err == nil || !strings.Contains(err.Error(), "outside readableRoots") {
		t.Fatalf("error = %v", err)
	}
}

func TestCompileRejectsUnknownStage(t *testing.T) {
	assertFixtureRejects(t, "stage: input.normalize", "stage: product.magic", `unknown stage "product.magic"`)
}

func TestCompileRejectsUnknownStagePort(t *testing.T) {
	assertFixtureRejects(t, "in: {request: request}", "in: {prompt: request}", `binds unknown input port "prompt"`)
}

func TestCompileRejectsExpressionWithWrongArity(t *testing.T) {
	assertFixtureRejects(t,
		"{eq: [{field: state.resolution.action}, {literal: approve}]}",
		"{eq: [{field: state.resolution.action}]}",
		"operator eq requires 2 operand(s), got 1")
}

func TestCompileRejectsExpressionWithMultipleOperators(t *testing.T) {
	assertFixtureRejects(t,
		"{eq: [{field: state.resolution.action}, {literal: approve}]}",
		"{eq: [{field: state.resolution.action}, {literal: approve}], exists: [{field: state.resolution}]}",
		"expression must select exactly one operator")
}

func TestCompileRejectsUnknownExpressionOperand(t *testing.T) {
	assertFixtureRejects(t,
		"{field: state.resolution.action}",
		"{environment: HOME}",
		`unknown expression operand "environment"`)
}

func TestCompileRejectsReadWithoutProducerDependency(t *testing.T) {
	assertFixtureRejects(t,
		"needs: [context, recall, history]\n          run:\n            stage: prompt.assemble",
		"needs: [context, history]\n          run:\n            stage: prompt.assemble",
		`reads "memory" before it is produced by a dependency`)
}

func TestCompileRejectsConditionReadBeforeProduction(t *testing.T) {
	assertFixtureRejects(t,
		"when: {eq: [{field: prepared.remainingIterations}, {literal: 1}]}",
		"when: {eq: [{field: missing.remainingIterations}, {literal: 1}]}",
		`reads "missing.remainingIterations" before it is produced`)
}

func TestCompileRejectsLoopConditionReadBeforeProduction(t *testing.T) {
	assertFixtureRejects(t,
		"until: {eq: [{field: command.terminal}, {literal: true}]}",
		"until: {eq: [{field: missing.terminal}, {literal: true}]}",
		`reads "missing.terminal" before it is produced`)
}

func TestCompileRejectsExpressionOperandType(t *testing.T) {
	assertFixtureRejects(t,
		"{eq: [{field: state.resolution.action}, {literal: approve}]}",
		"{exists: [{literal: approve}]}",
		"operator exists requires a field operand")
}

func TestCompileRejectsTypedPipelineCallMismatch(t *testing.T) {
	assertFixtureRejects(t,
		"compact-state:\n      inputs: {state: ycode.agent-loop/v1}",
		"compact-state:\n      inputs: {state: bool}",
		`binds "state" (ycode.agent-loop/v1) to "compact-state" input "state" (bool)`)
}

func TestCompileRejectsUnboundedFanOut(t *testing.T) {
	assertFixtureRejects(t, "as: call\n              maxItems: 16", "as: call\n              maxItems: 0", "forEach maxItems must be positive")
}

func TestCompileRejectsUnreachableResource(t *testing.T) {
	assertFixtureRejects(t, "  sources:\n", "  sources:\n    unused:\n      text: unused\n      limits: {maxBytes: 64}\n", "unreachable resources must declare exported: true: sources/unused")
}

func TestCompileAllowsExportedUnreachableResource(t *testing.T) {
	original := string(readFixture(t))
	fixture := strings.Replace(original, "  sources:\n", "  sources:\n    reusable:\n      text: reusable\n      limits: {maxBytes: 64}\n      exported: true\n", 1)
	if _, err := Compile(fixturePath(), []byte(fixture)); err != nil {
		t.Fatalf("exported resource rejected: %v", err)
	}
}

func TestControlRootRejectsOverlapSymlinkAndPublicPermissions(t *testing.T) {
	for name, setup := range map[string]func(*testing.T) (Runtime, string, string){
		"overlap": func(t *testing.T) (Runtime, string, string) {
			root := t.TempDir()
			return Runtime{Workspace: ".", ReadableRoots: []string{"control"}, ControlRoot: ControlRoot{PlatformDataDir: "control"}, RootOverlap: "deny"}, root, root
		},
		"symlink": func(t *testing.T) (Runtime, string, string) {
			root := t.TempDir()
			if err := os.Symlink(t.TempDir(), filepath.Join(root, "control")); err != nil {
				t.Fatal(err)
			}
			return Runtime{Workspace: ".", ReadableRoots: []string{"."}, ControlRoot: ControlRoot{PlatformDataDir: "control"}, RootOverlap: "deny"}, t.TempDir(), root
		},
		"permissions": func(t *testing.T) (Runtime, string, string) {
			root := t.TempDir()
			path := filepath.Join(root, "control")
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0o755); err != nil {
				t.Fatal(err)
			}
			return Runtime{Workspace: ".", ReadableRoots: []string{"."}, ControlRoot: ControlRoot{PlatformDataDir: "control"}, RootOverlap: "deny"}, t.TempDir(), root
		},
		"readable-symlink-overlap": func(t *testing.T) (Runtime, string, string) {
			platform, document := t.TempDir(), t.TempDir()
			control := filepath.Join(platform, "control")
			if err := os.Mkdir(control, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(control, filepath.Join(document, "visible")); err != nil {
				t.Fatal(err)
			}
			return Runtime{Workspace: ".", ReadableRoots: []string{"visible"}, ControlRoot: ControlRoot{PlatformDataDir: "control"}, RootOverlap: "deny"}, document, platform
		},
	} {
		t.Run(name, func(t *testing.T) {
			runtime, documentDir, platformDir := setup(t)
			if err := validateControlRootAt(runtime, nil, documentDir, platformDir); err == nil {
				t.Fatal("unsafe control root accepted")
			}
		})
	}
}

func TestCompileRejectsPublicControlStorageModes(t *testing.T) {
	assertFixtureRejects(t, "permissions: {directory: 0700, file: 0600}", "permissions: {directory: 0755, file: 0644}", "control storage permissions must be private")
}

func TestCompileRejectsAuthorityEscalation(t *testing.T) {
	cases := []struct{ old, replacement, want string }{
		{"permissionCeiling: workspace-write\n      effectsCeiling: [read, write, exec, net]", "permissionCeiling: read-only\n      effectsCeiling: [read, write, exec, net]", "agent coder permission"},
		{"effectsCeiling: [read, write, exec, net]\n      delegations:", "effectsCeiling: [read, write, exec, net, destroy]\n      delegations:", `effect "destroy" escalates`},
		{"permissionCeiling: read-only\n          effectsCeiling: [read]", "permissionCeiling: workspace-write\n          effectsCeiling: [read]", "delegation coder->reviewer permission"},
		{"budget: {maxInputTokens: 100000, maxOutputTokens: 20000, maxCostUSD: 5}", "budget: {maxInputTokens: 600000, maxOutputTokens: 20000, maxCostUSD: 5}", "budget exceeds route ceiling"},
		{"effectsAllWithin: [read, write, exec]", "effectsAllWithin: [read, write, exec, destroy]", `policy workspace rule workspace-work effect "destroy"`},
		{"mounts: [workspace]", "mounts: [host-root]", "exceeds Bashy cwdRoot"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) { assertFixtureRejects(t, tc.old, tc.replacement, tc.want) })
	}
}

func TestCompileResolvesDigestPinnedNamespacedImport(t *testing.T) {
	doc, data := compileWithImport(t, importSourceFixture(true), "sources/common")
	if _, ok := doc.Spec.Sources["shared.common"]; !ok {
		t.Fatal("namespaced imported source is absent")
	}
	if !strings.HasPrefix(doc.ConfigDigest, "sha256:") || len(doc.ConfigDigest) != 71 {
		t.Fatalf("config digest = %q", doc.ConfigDigest)
	}
	first, err := doc.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	second, err := doc.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) || len(data) == 0 {
		t.Fatal("canonical dump is not deterministic")
	}
}

func TestCompileRejectsImportDigestMismatch(t *testing.T) {
	dir := t.TempDir()
	raw := []byte(importSourceFixture(true))
	if err := os.WriteFile(filepath.Join(dir, "fragment.yaml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	data := withImport(readFixture(t), "sha256:"+strings.Repeat("0", 64), "sources/common")
	if _, err := Compile(filepath.Join(dir, "agent.yaml"), data); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("error = %v", err)
	}
}

func TestCompileRejectsImportRuntimeDefaultsAndUnselectedDependency(t *testing.T) {
	runtimeFragment := strings.Replace(importSourceFixture(true), "spec:\n", "spec:\n  runtime: {defaultAgentRef: injected}\n", 1)
	assertImportRejects(t, runtimeFragment, "sources/common", "cannot contribute runtime defaults")
	dependencyFragment := `apiVersion: ycode.dev/v1alpha1
kind: Harness
metadata: {name: fragment, version: 1}
spec:
  sources:
    common: {text: common, limits: {maxBytes: 64}}
  contexts:
    shared:
      fragments: [{id: common, sourceRef: common, role: system, cache: {scope: stable, breakAfter: true}}]
      budget: {maxTokens: 10, overflow: fail}
`
	assertImportRejects(t, dependencyFragment, "contexts/shared", "references non-exported sources/common")
}

func TestCompileRejectsImportCollision(t *testing.T) {
	dir := t.TempDir()
	fragment := []byte(importSourceFixture(true))
	if err := os.WriteFile(filepath.Join(dir, "fragment.yaml"), fragment, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(fragment)
	input := withImport(readFixture(t), fmt.Sprintf("sha256:%x", sum), "sources/common")
	input = []byte(strings.Replace(string(input), "  sources:\n", "  sources:\n    shared.common:\n      text: collision\n      limits: {maxBytes: 64}\n      exported: true\n", 1))
	_, err := Compile(filepath.Join(dir, "agent.yaml"), input)
	if err == nil || !strings.Contains(err.Error(), "resource collision") {
		t.Fatalf("error = %v", err)
	}
}

func TestCompileRejectsUnsupportedPlatformAndEffectFallback(t *testing.T) {
	host := runtime.GOOS + "-" + runtime.GOARCH
	fixture := strings.Replace(string(readFixture(t)), host, "freebsd-amd64", 1)
	if _, err := Compile(fixturePath(), []byte(fixture)); err == nil || !strings.Contains(err.Error(), "unsupported platform") {
		t.Fatalf("error = %v", err)
	}
	assertFixtureRejects(t, "unsupportedEffect: reject", "unsupportedEffect: degrade", "unsupportedEffect must be reject")
}

func TestPipelineRejectsAmbiguousBranchMerge(t *testing.T) {
	pipeline := Pipeline{Inputs: map[string]string{"input": "string"}, State: map[string]StateSlot{"shared": {Type: "string", Writer: "single"}}, Concurrency: 2, Nodes: []Stage{
		{ID: "left", Run: Run{Stage: "state.forward", In: map[string]string{"value": "input"}, Out: map[string]string{"value": "shared"}}},
		{ID: "right", Run: Run{Stage: "state.forward", In: map[string]string{"value": "input"}, Out: map[string]string{"value": "shared"}}},
		{ID: "join", Needs: []string{"left", "right"}, Run: Run{Stage: "lifecycle.transition"}},
	}}
	if err := validateOnePipeline("merge", pipeline, map[string]Pipeline{"merge": pipeline}); err == nil || !strings.Contains(err.Error(), "without a deterministic merge") {
		t.Fatalf("error = %v", err)
	}
}

func TestCompileRejectsParallelCollectionWithoutMerge(t *testing.T) {
	assertFixtureRejects(t, "writer: single, merge: input-order", "writer: single", "without a deterministic state merge")
}

func TestCompilerErrorsCarrySourceLocation(t *testing.T) {
	_, err := Compile("named-agent.yaml", []byte("apiVersion: ycode.dev/v1alpha1\nkind: Harness\nunknown: true\n"))
	if err == nil || !strings.Contains(err.Error(), "named-agent.yaml") || !strings.Contains(err.Error(), "line 3") {
		t.Fatalf("error = %v", err)
	}
}

func TestCanonicalDumpDoesNotResolveSecrets(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "must-not-appear")
	doc, err := Load(fixturePath())
	if err != nil {
		t.Fatal(err)
	}
	data, err := doc.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "must-not-appear") {
		t.Fatal("canonical dump contains resolved secret")
	}
}

func TestCompileResolvesStructuredNonSecretEnvironment(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "https://local.invalid/v1")
	doc, err := Load(fixturePath())
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.Spec.Providers["openai"].Endpoint.Resolved; got != "https://local.invalid/v1" {
		t.Fatalf("resolved endpoint = %q", got)
	}
	data, err := doc.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "local.invalid") {
		t.Fatal("resolved environment value leaked into canonical dump")
	}
}

func importSourceFixture(exported bool) string {
	return fmt.Sprintf("apiVersion: ycode.dev/v1alpha1\nkind: Harness\nmetadata: {name: fragment, version: 1}\nspec:\n  sources:\n    common:\n      text: common\n      limits: {maxBytes: 64}\n      exported: %t\n", exported)
}

func withImport(base []byte, digest, export string) []byte {
	block := fmt.Sprintf("  imports:\n    common-pack:\n      source: fragment.yaml\n      digest: %s\n      namespace: shared\n      exports: [%s]\n", digest, export)
	return []byte(strings.Replace(string(base), "  sources:\n", block+"  sources:\n", 1))
}

func compileWithImport(t *testing.T, fragment, export string) (*Document, []byte) {
	t.Helper()
	dir := t.TempDir()
	raw := []byte(fragment)
	if err := os.WriteFile(filepath.Join(dir, "fragment.yaml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	data := withImport(readFixture(t), fmt.Sprintf("sha256:%x", sum), export)
	doc, err := Compile(filepath.Join(dir, "agent.yaml"), data)
	if err != nil {
		t.Fatal(err)
	}
	return doc, data
}

func assertImportRejects(t *testing.T, fragment, export, want string) {
	t.Helper()
	dir := t.TempDir()
	raw := []byte(fragment)
	if err := os.WriteFile(filepath.Join(dir, "fragment.yaml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	_, err := Compile(filepath.Join(dir, "agent.yaml"), withImport(readFixture(t), fmt.Sprintf("sha256:%x", sum), export))
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v; want %q", err, want)
	}
}

func assertFixtureRejects(t *testing.T, old, replacement, want string) {
	t.Helper()
	original := string(readFixture(t))
	fixture := strings.Replace(original, old, replacement, 1)
	if fixture == original {
		t.Fatalf("test mutation target %q not found", old)
	}
	_, err := Compile(fixturePath(), []byte(fixture))
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v; want substring %q", err, want)
	}
}

func readFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(fixturePath())
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func fixturePath() string { return filepath.Join("..", "..", "..", "examples", "agent.yaml") }
