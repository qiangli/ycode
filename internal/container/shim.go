// Package container is now a thin ALIAS SHIM over the relocated, canonical
// podman engine in coreutils (`github.com/qiangli/coreutils/external/podman/
// engine`). The implementation moved to coreutils as part of the AgentOS
// extraction (Phase 4) so bashy + outpost embed an isolated podman without
// ycode; ycode's 17 in-package consumers keep importing
// `github.com/qiangli/ycode/internal/container` unchanged — the names below
// just re-export the engine's. The one ycode-specific piece kept local is
// MCPHandler (mcpserver.go), which depends on ycode's internal/runtime/mcp.
//
// See dhnt/docs/agentos-substrate-extraction-plan.md + local-p2p-cicd.md.
package container

import "github.com/qiangli/coreutils/external/podman/engine"

// Types.
type (
	Engine             = engine.Engine
	EngineConfig       = engine.EngineConfig
	Container          = engine.Container
	ContainerConfig    = engine.ContainerConfig
	ContainerInfo      = engine.ContainerInfo
	ContainerComponent = engine.ContainerComponent
	ComponentConfig    = engine.ComponentConfig
	ExecResult         = engine.ExecResult
	RunResult          = engine.RunResult
	Mount              = engine.Mount
	PortMapping        = engine.PortMapping
	Resources          = engine.Resources
	ImageInfo          = engine.ImageInfo
	NetworkInfo        = engine.NetworkInfo
	PodInfo            = engine.PodInfo
	PodOptions         = engine.PodOptions
	Pool               = engine.Pool
	MachineConfig      = engine.MachineConfig
	OTELConfig         = engine.OTELConfig
	HostCleanupOptions = engine.HostCleanupOptions
	HostCleanupReport  = engine.HostCleanupReport
	HostResources      = engine.HostResources
	OrphanedProcess    = engine.OrphanedProcess
	StaleSocket        = engine.StaleSocket
	PreflightError     = engine.PreflightError
	PreflightErrorKind = engine.PreflightErrorKind
	PreflightOptions   = engine.PreflightOptions
	ResourceProbe      = engine.ResourceProbe
	DefaultProbe       = engine.DefaultProbe
	SizingSource       = engine.SizingSource
)

// Constructors + funcs.
var (
	NewEngine             = engine.NewEngine
	SharedEngine          = engine.SharedEngine
	NewContainer          = engine.NewContainer
	NewContainerComponent = engine.NewContainerComponent
	NewPool               = engine.NewPool
	DefaultMachineConfig  = engine.DefaultMachineConfig
	DefaultSocketPath     = engine.DefaultSocketPath
	InitMachine           = engine.InitMachine
	StartMachine          = engine.StartMachine
	StopMachine           = engine.StopMachine
	ListMachines          = engine.ListMachines
	RemoveMachine         = engine.RemoveMachine
	ResetMachines         = engine.ResetMachines
	EnsureMachine         = engine.EnsureMachine
	CleanupHost           = engine.CleanupHost
	PreflightAndCleanup   = engine.PreflightAndCleanup
	CheckHostResources    = engine.CheckHostResources
	RecommendMachineSizing = engine.RecommendMachineSizing
	RecordContainerExec   = engine.RecordContainerExec
)

// Consts.
const (
	SessionLabel       = engine.SessionLabel
	DefaultMachineName = engine.DefaultMachineName
)
