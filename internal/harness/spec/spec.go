// Package spec defines and compiles the declarative ycode harness document.
package spec

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

const (
	APIVersion = "ycode.dev/v1alpha1"
	Kind       = "Harness"
)

type Document struct {
	APIVersion   string   `yaml:"apiVersion" json:"apiVersion"`
	Kind         string   `yaml:"kind" json:"kind"`
	Metadata     Metadata `yaml:"metadata" json:"metadata"`
	Spec         Spec     `yaml:"spec" json:"spec"`
	Source       string   `yaml:"-" json:"-"`
	BaseDir      string   `yaml:"-" json:"-"`
	ConfigDigest string   `yaml:"-" json:"-"`
	// Compiled views retained while the execution packages migrate.
	Agents    map[string]Agent    `yaml:"-" json:"-"`
	Pipelines map[string]Pipeline `yaml:"-" json:"-"`
}
type Metadata struct {
	Name        string            `yaml:"name" json:"name"`
	Version     int               `yaml:"version" json:"version"`
	Annotations map[string]string `yaml:"annotations,omitempty" json:"annotations,omitempty"`
}
type Spec struct {
	Interfaces    Interfaces                `yaml:"interfaces,omitempty" json:"interfaces,omitempty"`
	Runtime       Runtime                   `yaml:"runtime" json:"runtime"`
	Imports       map[string]Import         `yaml:"imports,omitempty" json:"imports,omitempty"`
	Sources       map[string]Source         `yaml:"sources" json:"sources"`
	Providers     map[string]Provider       `yaml:"providers" json:"providers"`
	Models        map[string]Model          `yaml:"models" json:"models"`
	Routes        map[string]Route          `yaml:"routes" json:"routes"`
	Bashy         BashyResource             `yaml:"bashy" json:"bashy"`
	Contexts      map[string]Context        `yaml:"contexts" json:"contexts"`
	Memories      map[string]Memory         `yaml:"memories" json:"memories"`
	Queues        map[string]Queue          `yaml:"queues" json:"queues"`
	Sessions      map[string]Session        `yaml:"sessions" json:"sessions"`
	Lifecycles    map[string]Lifecycle      `yaml:"lifecycles" json:"lifecycles"`
	Policies      map[string]Policy         `yaml:"policies" json:"policies"`
	Placements    map[string]Placement      `yaml:"placements" json:"placements"`
	Locks         map[string]Lock           `yaml:"locks" json:"locks"`
	Hooks         map[string]Hook           `yaml:"hooks" json:"hooks"`
	Skills        map[string]Skill          `yaml:"skills" json:"skills"`
	Pipelines     map[string]Pipeline       `yaml:"pipelines" json:"pipelines"`
	Agents        map[string]Agent          `yaml:"agents" json:"agents"`
	Frontends     map[string]Frontend       `yaml:"frontends" json:"frontends"`
	Triggers      map[string]Trigger        `yaml:"triggers" json:"triggers"`
	Sinks         map[string]Sink           `yaml:"sinks" json:"sinks"`
	Observability Observability             `yaml:"observability" json:"observability"`
	Extensions    map[string]map[string]any `yaml:"extensions" json:"extensions"`
}
type Import struct {
	Source    string   `yaml:"source" json:"source"`
	Digest    string   `yaml:"digest" json:"digest"`
	Namespace string   `yaml:"namespace" json:"namespace"`
	Exports   []string `yaml:"exports" json:"exports"`
	Exported  bool     `yaml:"exported,omitempty" json:"exported,omitempty"`
}
type Runtime struct {
	DefaultAgentRef string      `yaml:"defaultAgentRef" json:"defaultAgentRef"`
	Workspace       string      `yaml:"workspace" json:"workspace"`
	ReadableRoots   []string    `yaml:"readableRoots" json:"readableRoots"`
	WritableRoots   []string    `yaml:"writableRoots" json:"writableRoots"`
	ControlRoot     ControlRoot `yaml:"controlRoot" json:"controlRoot"`
	RootOverlap     string      `yaml:"rootOverlap" json:"rootOverlap"`
}
type ControlRoot struct {
	PlatformDataDir string `yaml:"platformDataDir" json:"platformDataDir"`
}

type Value struct {
	ValueFrom ValueFrom `yaml:"valueFrom" json:"valueFrom"`
	Resolved  string    `yaml:"-" json:"-"`
}
type ValueFrom struct {
	Env     string `yaml:"env" json:"env"`
	Default string `yaml:"default,omitempty" json:"default,omitempty"`
}
type SecretValue struct {
	SecretRef SecretRef `yaml:"secretRef" json:"secretRef"`
}
type SecretRef struct {
	Provider string `yaml:"provider" json:"provider"`
	Name     string `yaml:"name" json:"name"`
}

type Source struct {
	Text     string       `yaml:"text,omitempty" json:"text,omitempty"`
	File     *SourceFile  `yaml:"file,omitempty" json:"file,omitempty"`
	Limits   SourceLimits `yaml:"limits" json:"limits"`
	Exported bool         `yaml:"exported,omitempty" json:"exported,omitempty"`
	Snapshot string       `yaml:"snapshot,omitempty" json:"snapshot,omitempty"`
	Resolved string       `yaml:"-" json:"-"`
	Digest   string       `yaml:"-" json:"digest,omitempty"`
}
type SourceFile struct {
	Path     string `yaml:"path" json:"path"`
	Required bool   `yaml:"required" json:"required"`
}
type SourceLimits struct {
	MaxBytes int `yaml:"maxBytes" json:"maxBytes"`
}

type Provider struct {
	Protocol    string                 `yaml:"protocol" json:"protocol"`
	Endpoint    Value                  `yaml:"endpoint" json:"endpoint"`
	Headers     map[string]SecretValue `yaml:"headers,omitempty" json:"headers,omitempty"`
	Credentials ProviderCredentials    `yaml:"credentials" json:"credentials"`
	Transport   ProviderTransport      `yaml:"transport" json:"transport"`
}
type ProviderCredentials struct {
	APIKey SecretValue `yaml:"apiKey" json:"apiKey"`
}
type ProviderTransport struct {
	ConnectTimeoutMS int `yaml:"connectTimeoutMs" json:"connectTimeoutMs"`
	RequestTimeoutMS int `yaml:"requestTimeoutMs" json:"requestTimeoutMs"`
	MaxResponseBytes int `yaml:"maxResponseBytes" json:"maxResponseBytes"`
}
type Model struct {
	ProviderRef  string            `yaml:"providerRef" json:"providerRef"`
	ID           string            `yaml:"id" json:"id"`
	Limits       ModelLimits       `yaml:"limits" json:"limits"`
	Capabilities ModelCapabilities `yaml:"capabilities" json:"capabilities"`
}
type ModelLimits struct {
	ContextTokens   int `yaml:"contextTokens" json:"contextTokens"`
	MaxOutputTokens int `yaml:"maxOutputTokens" json:"maxOutputTokens"`
}
type ModelCapabilities struct {
	Streaming         bool `yaml:"streaming" json:"streaming"`
	ToolCalls         bool `yaml:"toolCalls" json:"toolCalls"`
	ParallelToolCalls bool `yaml:"parallelToolCalls" json:"parallelToolCalls"`
	StructuredOutput  bool `yaml:"structuredOutput" json:"structuredOutput"`
}
type Route struct {
	Attempts        []RouteAttempt `yaml:"attempts" json:"attempts"`
	FallbackOn      []string       `yaml:"fallbackOn" json:"fallbackOn"`
	ProviderSession string         `yaml:"providerSession" json:"providerSession"`
	Budget          TokenBudget    `yaml:"budget" json:"budget"`
	QuotaPool       QuotaPool      `yaml:"quotaPool" json:"quotaPool"`
}
type RouteAttempt struct {
	ModelRef       string         `yaml:"modelRef" json:"modelRef"`
	TransportRetry TransportRetry `yaml:"transportRetry" json:"transportRetry"`
	TimeoutMS      int            `yaml:"timeoutMs" json:"timeoutMs"`
}
type TransportRetry struct {
	MaxAttempts int      `yaml:"maxAttempts" json:"maxAttempts"`
	Backoff     Backoff  `yaml:"backoff" json:"backoff"`
	RetryOn     []string `yaml:"retryOn" json:"retryOn"`
}
type TokenBudget struct {
	MaxInputTokens  int     `yaml:"maxInputTokens" json:"maxInputTokens"`
	MaxOutputTokens int     `yaml:"maxOutputTokens" json:"maxOutputTokens"`
	MaxCostUSD      float64 `yaml:"maxCostUSD" json:"maxCostUSD"`
}
type QuotaPool struct {
	Strategy    string `yaml:"strategy" json:"strategy"`
	OnExhausted string `yaml:"onExhausted" json:"onExhausted"`
}

type BashyResource struct {
	Contract    string   `yaml:"contract" json:"contract"`
	RequestType string   `yaml:"requestType" json:"requestType"`
	ResultType  string   `yaml:"resultType" json:"resultType"`
	Operations  []string `yaml:"operations" json:"operations"`
	Execution   Bashy    `yaml:"execution" json:"execution"`
}
type Bashy struct {
	CWDRoot             string      `yaml:"cwdRoot" json:"cwdRoot"`
	Environment         Environment `yaml:"environment" json:"environment"`
	CommandResolution   string      `yaml:"commandResolution" json:"commandResolution"`
	TimeoutMS           int         `yaml:"timeoutMs" json:"timeoutMs"`
	JobLifetimeMS       int         `yaml:"jobLifetimeMs" json:"jobLifetimeMs"`
	MaxOutputChars      int         `yaml:"maxOutputChars" json:"maxOutputChars"`
	MaxSpillBytes       int         `yaml:"maxSpillBytes" json:"maxSpillBytes"`
	MaxParallel         int         `yaml:"maxParallel" json:"maxParallel"`
	PermissionCeiling   string      `yaml:"permissionCeiling" json:"permissionCeiling"`
	EffectsCeiling      []string    `yaml:"effectsCeiling" json:"effectsCeiling"`
	Preflight           string      `yaml:"preflight" json:"preflight"`
	IncompletePreflight string      `yaml:"incompletePreflight" json:"incompletePreflight"`
	HostFallback        string      `yaml:"hostFallback" json:"hostFallback"`
	ToolName            string      `yaml:"-" json:"-"`
}
type Environment struct {
	Inherit string   `yaml:"inherit" json:"inherit"`
	Names   []string `yaml:"names" json:"names"`
}

type Context struct {
	Fragments []ContextFragment `yaml:"fragments" json:"fragments"`
	Budget    ContextBudget     `yaml:"budget" json:"budget"`
}
type ContextFragment struct {
	ID        string       `yaml:"id" json:"id"`
	SourceRef string       `yaml:"sourceRef" json:"sourceRef"`
	Role      string       `yaml:"role" json:"role"`
	Cache     ContextCache `yaml:"cache" json:"cache"`
}
type ContextCache struct {
	Scope      string `yaml:"scope" json:"scope"`
	BreakAfter bool   `yaml:"breakAfter" json:"breakAfter"`
}
type ContextBudget struct {
	MaxTokens int    `yaml:"maxTokens" json:"maxTokens"`
	Overflow  string `yaml:"overflow" json:"overflow"`
}
type Memory struct {
	Provider   string           `yaml:"provider" json:"provider"`
	Recall     RecallPolicy     `yaml:"recall" json:"recall"`
	Write      WritePolicy      `yaml:"write" json:"write"`
	Compaction CompactionPolicy `yaml:"compaction" json:"compaction"`
}
type RecallPolicy struct {
	Rings     []string `yaml:"rings" json:"rings"`
	Forms     []string `yaml:"forms" json:"forms"`
	MaxItems  int      `yaml:"maxItems" json:"maxItems"`
	MaxTokens int      `yaml:"maxTokens" json:"maxTokens"`
}
type WritePolicy struct {
	EveryTurns int `yaml:"everyTurns" json:"everyTurns"`
}
type CompactionPolicy struct {
	PreserveRecentTokens       int    `yaml:"preserveRecentTokens" json:"preserveRecentTokens"`
	PreserveUserMessagesTokens int    `yaml:"preserveUserMessagesTokens" json:"preserveUserMessagesTokens"`
	ReserveTokens              int    `yaml:"reserveTokens" json:"reserveTokens"`
	RouteRef                   string `yaml:"routeRef" json:"routeRef"`
	PromptSourceRef            string `yaml:"promptSourceRef" json:"promptSourceRef"`
	UpdatePromptSourceRef      string `yaml:"updatePromptSourceRef" json:"updatePromptSourceRef"`
	OnFailure                  string `yaml:"onFailure" json:"onFailure"`
}
type CompactionTrigger struct {
	ContextTokensGTE int `yaml:"contextTokensGte" json:"contextTokensGte"`
}

type ControlPath struct {
	ControlPath string `yaml:"controlPath" json:"controlPath"`
}
type Queue struct {
	Store         ControlPath    `yaml:"store" json:"store"`
	Discipline    string         `yaml:"discipline" json:"discipline"`
	Capacity      int            `yaml:"capacity" json:"capacity"`
	Overflow      string         `yaml:"overflow" json:"overflow"`
	Priorities    map[string]int `yaml:"priorities" json:"priorities"`
	LeaseMS       int            `yaml:"leaseMs" json:"leaseMs"`
	StaleLease    string         `yaml:"staleLease" json:"staleLease"`
	DeduplicateBy string         `yaml:"deduplicateBy" json:"deduplicateBy"`
}

type Session struct {
	Events      SessionEvents      `yaml:"events" json:"events"`
	Payloads    SessionPayloads    `yaml:"payloads" json:"payloads"`
	Checkpoints SessionCheckpoints `yaml:"checkpoints" json:"checkpoints"`
	Branches    SessionBranches    `yaml:"branches" json:"branches"`
	History     SessionHistory     `yaml:"history" json:"history"`
	Retention   SessionRetention   `yaml:"retention" json:"retention"`
}
type SessionEvents struct {
	Format      string          `yaml:"format" json:"format"`
	Store       string          `yaml:"store" json:"store"`
	Directory   ControlPath     `yaml:"directory" json:"directory"`
	Fsync       string          `yaml:"fsync" json:"fsync"`
	Permissions FilePermissions `yaml:"permissions" json:"permissions"`
	Sequence    string          `yaml:"sequence" json:"sequence"`
	Integrity   string          `yaml:"integrity" json:"integrity"`
}
type SessionCheckpoints struct {
	Directory ControlPath `yaml:"directory" json:"directory"`
	Bind      []string    `yaml:"bind" json:"bind"`
}
type FilePermissions struct {
	Directory int `yaml:"directory" json:"directory"`
	File      int `yaml:"file" json:"file"`
}
type SessionPayloads struct {
	Store                   string      `yaml:"store" json:"store"`
	Directory               ControlPath `yaml:"directory" json:"directory"`
	Digest                  string      `yaml:"digest" json:"digest"`
	InlineMaxBytes          int         `yaml:"inlineMaxBytes" json:"inlineMaxBytes"`
	RetainModelVisibleBytes bool        `yaml:"retainModelVisibleBytes" json:"retainModelVisibleBytes"`
}
type SessionBranches struct {
	Enabled    bool   `yaml:"enabled" json:"enabled"`
	CopyEvents string `yaml:"copyEvents" json:"copyEvents"`
}
type SessionHistory struct {
	MaxTokens        int                     `yaml:"maxTokens" json:"maxTokens"`
	Unit             string                  `yaml:"unit" json:"unit"`
	ClearToolResults SessionClearToolResults `yaml:"clearToolResults" json:"clearToolResults"`
	Repair           string                  `yaml:"repair" json:"repair"`
}
type SessionClearToolResults struct {
	OlderThanTurns int    `yaml:"olderThanTurns" json:"olderThanTurns"`
	Placeholder    string `yaml:"placeholder" json:"placeholder"`
}
type SessionRetention struct {
	MaxAgeDays                 int   `yaml:"maxAgeDays" json:"maxAgeDays"`
	MaxBytes                   int64 `yaml:"maxBytes" json:"maxBytes"`
	PreserveReferencedPayloads bool  `yaml:"preserveReferencedPayloads" json:"preserveReferencedPayloads"`
}
type Lifecycle struct {
	Initial     string                `yaml:"initial" json:"initial"`
	Terminal    []string              `yaml:"terminal" json:"terminal"`
	Transitions []LifecycleTransition `yaml:"transitions" json:"transitions"`
}
type LifecycleTransition struct {
	From string `yaml:"from" json:"from"`
	To   string `yaml:"to" json:"to"`
}
type Policy struct {
	UnavailableHuman    string       `yaml:"unavailableHuman" json:"unavailableHuman"`
	IncompletePreflight string       `yaml:"incompletePreflight" json:"incompletePreflight"`
	Rules               []PolicyRule `yaml:"rules" json:"rules"`
	Resume              []string     `yaml:"resume" json:"resume"`
	ApprovalBinding     string       `yaml:"approvalBinding" json:"approvalBinding"`
	MaxEdits            int          `yaml:"maxEdits" json:"maxEdits"`
}
type PolicyRule struct {
	ID       string      `yaml:"id" json:"id"`
	Match    PolicyMatch `yaml:"match" json:"match"`
	Decision string      `yaml:"decision" json:"decision"`
}
type PolicyMatch struct {
	EffectsAny        []string `yaml:"effectsAny,omitempty" json:"effectsAny,omitempty"`
	EffectsAllWithin  []string `yaml:"effectsAllWithin,omitempty" json:"effectsAllWithin,omitempty"`
	PathsAllWithin    []string `yaml:"pathsAllWithin,omitempty" json:"pathsAllWithin,omitempty"`
	PreflightComplete *bool    `yaml:"preflightComplete,omitempty" json:"preflightComplete,omitempty"`
	Always            bool     `yaml:"always,omitempty" json:"always,omitempty"`
}
type Placement struct {
	Kind               string   `yaml:"kind" json:"kind"`
	Workspace          string   `yaml:"workspace" json:"workspace"`
	Isolation          string   `yaml:"isolation" json:"isolation"`
	Network            string   `yaml:"network" json:"network"`
	Mounts             []string `yaml:"mounts" json:"mounts"`
	SupportedPlatforms []string `yaml:"supportedPlatforms" json:"supportedPlatforms"`
	UnsupportedEffect  string   `yaml:"unsupportedEffect" json:"unsupportedEffect"`
}
type Lock struct {
	Backend      string       `yaml:"backend" json:"backend"`
	ResourceKey  LiteralValue `yaml:"resourceKey" json:"resourceKey"`
	Mode         string       `yaml:"mode" json:"mode"`
	LeaseMS      int          `yaml:"leaseMs" json:"leaseMs"`
	RenewEveryMS int          `yaml:"renewEveryMs" json:"renewEveryMs"`
	Fencing      string       `yaml:"fencing" json:"fencing"`
	OnTimeout    string       `yaml:"onTimeout" json:"onTimeout"`
}
type LiteralValue struct {
	Literal any `yaml:"literal" json:"literal"`
}
type Hook struct {
	Phase          string `yaml:"phase" json:"phase"`
	PipelineRef    string `yaml:"pipelineRef" json:"pipelineRef"`
	InputType      string `yaml:"inputType" json:"inputType"`
	OutputType     string `yaml:"outputType" json:"outputType"`
	Order          int    `yaml:"order" json:"order"`
	MaxInvocations int    `yaml:"maxInvocations" json:"maxInvocations"`
	Reentrant      bool   `yaml:"reentrant" json:"reentrant"`
	Failure        string `yaml:"failure" json:"failure"`
}
type Skill struct {
	Source         SkillSource `yaml:"source" json:"source"`
	Version        int         `yaml:"version" json:"version"`
	EffectsCeiling []string    `yaml:"effectsCeiling" json:"effectsCeiling"`
}
type SkillSource struct {
	CatalogRef string `yaml:"catalogRef" json:"catalogRef"`
}
type Preset struct {
	ContextRef   string   `yaml:"contextRef" json:"contextRef"`
	MemoryRef    string   `yaml:"memoryRef" json:"memoryRef"`
	QueueRef     string   `yaml:"queueRef" json:"queueRef"`
	PolicyRef    string   `yaml:"policyRef" json:"policyRef"`
	PlacementRef string   `yaml:"placementRef" json:"placementRef"`
	SkillRefs    []string `yaml:"skillRefs" json:"skillRefs"`
}

type Pipeline struct {
	Exported    bool                 `yaml:"exported,omitempty" json:"exported,omitempty"`
	Inputs      map[string]string    `yaml:"inputs" json:"inputs"`
	State       map[string]StateSlot `yaml:"state" json:"state"`
	Outputs     map[string]string    `yaml:"outputs" json:"outputs"`
	Concurrency int                  `yaml:"concurrency" json:"concurrency"`
	FailFast    bool                 `yaml:"failFast" json:"failFast"`
	Nodes       []Stage              `yaml:"nodes" json:"nodes"`
}
type StateSlot struct {
	Type   string `yaml:"type" json:"type"`
	Writer string `yaml:"writer" json:"writer"`
	Merge  string `yaml:"merge,omitempty" json:"merge,omitempty"`
}
type Stage struct {
	ID             string         `yaml:"id" json:"id"`
	Needs          []string       `yaml:"needs" json:"needs"`
	WhenExpr       *Expression    `yaml:"when,omitempty" json:"when,omitempty"`
	LockRef        string         `yaml:"lockRef,omitempty" json:"lockRef,omitempty"`
	RetryV1        *RetryPolicy   `yaml:"retry,omitempty" json:"retry,omitempty"`
	RunOn          []string       `yaml:"runOn,omitempty" json:"runOn,omitempty"`
	AcceptsSkipped []string       `yaml:"acceptsSkipped,omitempty" json:"acceptsSkipped,omitempty"`
	Run            Run            `yaml:"run" json:"run"`
	Use            string         `yaml:"-" json:"-"`
	Call           string         `yaml:"-" json:"-"`
	With           map[string]any `yaml:"-" json:"-"`
	When           string         `yaml:"-" json:"-"`
	Stages         []Stage        `yaml:"-" json:"-"`
	Repeat         *Repeat        `yaml:"-" json:"-"`
	Retry          *Retry         `yaml:"-" json:"-"`
	Switch         *Switch        `yaml:"-" json:"-"`
	ForEach        *ForEach       `yaml:"-" json:"-"`
	Fallback       []Stage        `yaml:"-" json:"-"`
}
type Run struct {
	Stage       string            `yaml:"stage,omitempty" json:"stage,omitempty"`
	PipelineRef string            `yaml:"pipelineRef,omitempty" json:"pipelineRef,omitempty"`
	Repeat      *RepeatRun        `yaml:"repeat,omitempty" json:"repeat,omitempty"`
	ForEach     *ForEachRun       `yaml:"forEach,omitempty" json:"forEach,omitempty"`
	Switch      *SwitchRun        `yaml:"switch,omitempty" json:"switch,omitempty"`
	Fallback    *FallbackRun      `yaml:"fallback,omitempty" json:"fallback,omitempty"`
	HookInvoke  string            `yaml:"hook.invoke,omitempty" json:"hook.invoke,omitempty"`
	With        map[string]any    `yaml:"with,omitempty" json:"with,omitempty"`
	In          map[string]string `yaml:"in,omitempty" json:"in,omitempty"`
	Out         map[string]string `yaml:"out,omitempty" json:"out,omitempty"`
}
type Expression struct {
	Eq     []Operand    `yaml:"eq,omitempty" json:"eq,omitempty"`
	Ne     []Operand    `yaml:"ne,omitempty" json:"ne,omitempty"`
	LT     []Operand    `yaml:"lt,omitempty" json:"lt,omitempty"`
	LTE    []Operand    `yaml:"lte,omitempty" json:"lte,omitempty"`
	GT     []Operand    `yaml:"gt,omitempty" json:"gt,omitempty"`
	GTE    []Operand    `yaml:"gte,omitempty" json:"gte,omitempty"`
	Exists []Operand    `yaml:"exists,omitempty" json:"exists,omitempty"`
	In     []Operand    `yaml:"in,omitempty" json:"in,omitempty"`
	All    []Expression `yaml:"all,omitempty" json:"all,omitempty"`
	Any    []Expression `yaml:"any,omitempty" json:"any,omitempty"`
	Not    []Expression `yaml:"not,omitempty" json:"not,omitempty"`
}
type Operand struct {
	Field      string `yaml:"field,omitempty" json:"field,omitempty"`
	Literal    any    `yaml:"literal,omitempty" json:"literal,omitempty"`
	literalSet bool
}

func (o *Operand) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode || len(node.Content) != 2 {
		return fmt.Errorf("expression operand must contain exactly one field or literal")
	}
	key, value := node.Content[0].Value, node.Content[1]
	switch key {
	case "field":
		if err := value.Decode(&o.Field); err != nil {
			return err
		}
		if o.Field == "" {
			return fmt.Errorf("expression field operand is empty")
		}
	case "literal":
		if err := value.Decode(&o.Literal); err != nil {
			return err
		}
		o.literalSet = true
	default:
		return fmt.Errorf("unknown expression operand %q", key)
	}
	return nil
}

type RetryPolicy struct {
	MaxAttempts int        `yaml:"maxAttempts" json:"maxAttempts"`
	Backoff     Backoff    `yaml:"backoff" json:"backoff"`
	When        Expression `yaml:"when" json:"when"`
}
type Backoff struct {
	InitialMS  int     `yaml:"initialMs" json:"initialMs"`
	Multiplier float64 `yaml:"multiplier" json:"multiplier"`
	MaxMS      int     `yaml:"maxMs" json:"maxMs"`
	Jitter     string  `yaml:"jitter" json:"jitter"`
}
type RepeatRun struct {
	PipelineRef   string            `yaml:"pipelineRef" json:"pipelineRef"`
	MaxIterations int               `yaml:"maxIterations" json:"maxIterations"`
	Until         Expression        `yaml:"until" json:"until"`
	Carry         map[string]string `yaml:"carry,omitempty" json:"carry,omitempty"`
	In            map[string]string `yaml:"in,omitempty" json:"in,omitempty"`
	Out           map[string]string `yaml:"out" json:"out"`
}
type FieldOperand struct {
	Field string `yaml:"field" json:"field"`
}
type ForEachRun struct {
	Items       FieldOperand      `yaml:"items" json:"items"`
	As          string            `yaml:"as" json:"as"`
	MaxParallel int               `yaml:"maxParallel" json:"maxParallel"`
	MaxItems    int               `yaml:"maxItems" json:"maxItems"`
	Ordered     bool              `yaml:"ordered" json:"ordered"`
	PipelineRef string            `yaml:"pipelineRef" json:"pipelineRef"`
	In          map[string]string `yaml:"in" json:"in"`
	Collect     map[string]string `yaml:"collect" json:"collect"`
}
type SwitchRun struct {
	Cases              []SwitchRunCase   `yaml:"cases" json:"cases"`
	DefaultPipelineRef string            `yaml:"defaultPipelineRef,omitempty" json:"defaultPipelineRef,omitempty"`
	NoMatch            string            `yaml:"noMatch,omitempty" json:"noMatch,omitempty"`
	In                 map[string]string `yaml:"in,omitempty" json:"in,omitempty"`
	Out                map[string]string `yaml:"out,omitempty" json:"out,omitempty"`
}
type SwitchRunCase struct {
	When        Expression        `yaml:"when" json:"when"`
	PipelineRef string            `yaml:"pipelineRef" json:"pipelineRef"`
	In          map[string]string `yaml:"in,omitempty" json:"in,omitempty"`
}
type FallbackRun struct {
	MaxAttempts int               `yaml:"maxAttempts" json:"maxAttempts"`
	Attempts    []FallbackAttempt `yaml:"attempts" json:"attempts"`
	NoMatch     string            `yaml:"noMatch" json:"noMatch"`
	In          map[string]string `yaml:"in,omitempty" json:"in,omitempty"`
	Out         map[string]string `yaml:"out,omitempty" json:"out,omitempty"`
}
type FallbackAttempt struct {
	PipelineRef string            `yaml:"pipelineRef" json:"pipelineRef"`
	On          []string          `yaml:"on" json:"on"`
	In          map[string]string `yaml:"in,omitempty" json:"in,omitempty"`
}

// Legacy control values are in-memory only and disappear with the old executor.
type Repeat struct {
	Max    int
	Until  string
	Stages []Stage
}
type Retry struct {
	Max    int
	Stages []Stage
}
type Switch struct {
	Cases   []Case
	Default []Stage
}
type Case struct {
	When   string
	Stages []Stage
}
type ForEach struct {
	Items, As, Collect string
	MaxParallel        int
	Ordered            bool
	Stages             []Stage
}

type Agent struct {
	DisplayName       string         `yaml:"displayName" json:"displayName"`
	Description       string         `yaml:"description" json:"description"`
	InputSchema       map[string]any `yaml:"inputSchema" json:"inputSchema"`
	OutputSchema      map[string]any `yaml:"outputSchema" json:"outputSchema"`
	ContextRef        string         `yaml:"contextRef" json:"contextRef"`
	ModelRouteRef     string         `yaml:"modelRouteRef" json:"modelRouteRef"`
	PipelineRef       string         `yaml:"pipelineRef" json:"pipelineRef"`
	MemoryRef         string         `yaml:"memoryRef" json:"memoryRef"`
	QueueRef          string         `yaml:"queueRef" json:"queueRef"`
	SessionRef        string         `yaml:"sessionRef" json:"sessionRef"`
	PolicyRef         string         `yaml:"policyRef" json:"policyRef"`
	PlacementRef      string         `yaml:"placementRef" json:"placementRef"`
	LifecycleRef      string         `yaml:"lifecycleRef" json:"lifecycleRef"`
	HookRefs          []string       `yaml:"hookRefs" json:"hookRefs"`
	PermissionCeiling string         `yaml:"permissionCeiling" json:"permissionCeiling"`
	EffectsCeiling    []string       `yaml:"effectsCeiling" json:"effectsCeiling"`
	Delegations       []Delegation   `yaml:"delegations" json:"delegations"`
	Presentation      Presentation   `yaml:"presentation" json:"presentation"`
}
type Delegation struct {
	AgentRef          string            `yaml:"agentRef" json:"agentRef"`
	ModelRouteRef     string            `yaml:"modelRouteRef" json:"modelRouteRef"`
	MaxDepth          int               `yaml:"maxDepth" json:"maxDepth"`
	MaxParallel       int               `yaml:"maxParallel" json:"maxParallel"`
	TimeoutMS         int               `yaml:"timeoutMs" json:"timeoutMs"`
	PermissionCeiling string            `yaml:"permissionCeiling" json:"permissionCeiling"`
	EffectsCeiling    []string          `yaml:"effectsCeiling" json:"effectsCeiling"`
	Budget            TokenBudget       `yaml:"budget" json:"budget"`
	Session           DelegationSession `yaml:"session" json:"session"`
}
type DelegationSession struct {
	Branch string `yaml:"branch" json:"branch"`
	Join   string `yaml:"join" json:"join"`
	Cancel string `yaml:"cancel" json:"cancel"`
}
type Presentation struct {
	Icon     string `yaml:"icon" json:"icon"`
	Category string `yaml:"category" json:"category"`
}
type Frontend struct {
	Kind      string         `yaml:"kind" json:"kind"`
	Trust     string         `yaml:"trust,omitempty" json:"trust,omitempty"`
	HITL      bool           `yaml:"hitl,omitempty" json:"hitl,omitempty"`
	Resume    bool           `yaml:"resume,omitempty" json:"resume,omitempty"`
	Fork      bool           `yaml:"fork,omitempty" json:"fork,omitempty"`
	Listen    *Listen        `yaml:"listen,omitempty" json:"listen,omitempty"`
	Auth      *FrontendAuth  `yaml:"auth,omitempty" json:"auth,omitempty"`
	TLS       *TLS           `yaml:"tls,omitempty" json:"tls,omitempty"`
	Limits    FrontendLimits `yaml:"limits" json:"limits"`
	Endpoint  string         `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	Subject   string         `yaml:"subject,omitempty" json:"subject,omitempty"`
	Transport string         `yaml:"transport,omitempty" json:"transport,omitempty"`
}
type TLS struct {
	Mode string `yaml:"mode" json:"mode"`
}
type FrontendLimits struct {
	MaxInputBytes int `yaml:"maxInputBytes" json:"maxInputBytes"`
	MaxConcurrent int `yaml:"maxConcurrent,omitempty" json:"maxConcurrent,omitempty"`
}
type Listen struct {
	Network string `yaml:"network" json:"network"`
	Address string `yaml:"address" json:"address"`
}
type FrontendAuth struct {
	Mode      string    `yaml:"mode" json:"mode"`
	SecretRef SecretRef `yaml:"secretRef" json:"secretRef"`
}
type Trigger struct {
	Kind         string       `yaml:"kind" json:"kind"`
	FrontendRefs []string     `yaml:"frontendRefs" json:"frontendRefs"`
	Route        TriggerRoute `yaml:"route" json:"route"`
	InputMapping string       `yaml:"inputMapping" json:"inputMapping"`
	Idempotency  string       `yaml:"idempotency" json:"idempotency"`
	Admission    Admission    `yaml:"admission" json:"admission"`
}
type TriggerRoute struct {
	AgentRef string `yaml:"agentRef" json:"agentRef"`
	QueueRef string `yaml:"queueRef" json:"queueRef"`
}
type Admission struct {
	MaxPendingPerPrincipal int    `yaml:"maxPendingPerPrincipal" json:"maxPendingPerPrincipal"`
	OnLimit                string `yaml:"onLimit" json:"onLimit"`
}
type Sink struct {
	Kind                 string   `yaml:"kind" json:"kind"`
	FrontendRefs         []string `yaml:"frontendRefs" json:"frontendRefs"`
	DestinationAllowlist []string `yaml:"destinationAllowlist" json:"destinationAllowlist"`
	Redact               string   `yaml:"redact" json:"redact"`
	Delivery             Delivery `yaml:"delivery" json:"delivery"`
}
type Delivery struct {
	Mode          string        `yaml:"mode" json:"mode"`
	DeduplicateBy string        `yaml:"deduplicateBy" json:"deduplicateBy"`
	Retry         DeliveryRetry `yaml:"retry" json:"retry"`
	DeadLetter    ControlPath   `yaml:"deadLetter" json:"deadLetter"`
}
type DeliveryRetry struct {
	MaxAttempts  int `yaml:"maxAttempts" json:"maxAttempts"`
	BackoffMS    int `yaml:"backoffMs" json:"backoffMs"`
	MaxBackoffMS int `yaml:"maxBackoffMs" json:"maxBackoffMs"`
}
type Observability struct {
	Enabled       bool              `yaml:"enabled" json:"enabled"`
	Exporter      string            `yaml:"exporter" json:"exporter"`
	Endpoint      Value             `yaml:"endpoint" json:"endpoint"`
	RecordContent bool              `yaml:"recordContent" json:"recordContent"`
	Attributes    map[string]string `yaml:"attributes" json:"attributes"`
}

func (d *Document) RedactedJSON() ([]byte, error) { return json.MarshalIndent(d, "", "  ") }
