package main

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"github.com/qiangli/ycode/internal/gateway"
	"github.com/qiangli/ycode/internal/runtime/config"
)

// startServeGateway builds and starts the per-process localhost gateway
// that fronts ollama + the podman socket. It is best-effort: when a
// backend isn't available (ollama runner failed to start, container
// engine disabled, ...) the corresponding gateway endpoint is simply
// not opened. The caller is responsible for calling Close on the
// returned Gateway during shutdown.
//
// Mode resolution:
//   - container.gateway.mode == "remote" → reverse-proxy to cfg.URL with
//     Bearer auth (cfg.TokenFile).
//   - container.gateway.mode == "embedded" or empty → reverse-proxy to
//     the local engine's libpod socket, when stack.containers.Engine()
//     is healthy. If the engine isn't up, the podman gateway is skipped.
//   - same shape for inference.gateway.mode / ollama.
func startServeGateway(ctx context.Context, stack *stackComponents, fullCfg *config.Config) *gateway.Gateway {
	if fullCfg == nil {
		return nil
	}
	gwCfg := gateway.Config{}

	// Podman backend
	switch fullCfg.Container.Gateway.Mode {
	case "remote":
		gwCfg.Podman = gateway.PodmanBackend{
			Mode:      gateway.ModeRemote,
			URL:       fullCfg.Container.Gateway.URL,
			TokenFile: fullCfg.Container.Gateway.TokenFile,
		}
	default:
		// Embedded — only opens if the engine actually came up.
		if sock := embeddedPodmanSocket(stack); sock != "" {
			gwCfg.Podman = gateway.PodmanBackend{
				Mode:     gateway.ModeEmbedded,
				Upstream: sock,
			}
		}
	}

	// Ollama backend
	switch fullCfg.Inference.Gateway.Mode {
	case "remote":
		gwCfg.Ollama = gateway.OllamaBackend{
			Mode:      gateway.ModeRemote,
			URL:       fullCfg.Inference.Gateway.URL,
			TokenFile: fullCfg.Inference.Gateway.TokenFile,
		}
	default:
		if base := embeddedOllamaURL(stack); base != "" {
			gwCfg.Ollama = gateway.OllamaBackend{
				Mode:     gateway.ModeEmbedded,
				Upstream: base,
			}
		}
	}

	if gwCfg.Podman.Mode == "" && gwCfg.Ollama.Mode == "" {
		return nil
	}

	gw, err := gateway.Start(ctx, gwCfg)
	if err != nil {
		slog.Warn("gateway: not started", "err", err)
		return nil
	}

	// Publish env vars to this process. Children spawned by ycode
	// (sandbox_exec, agent_shell, etc.) inherit these; in remote mode
	// this is the whole point — DOCKER_HOST now transparently goes
	// through cloudbox. In embedded mode it's a small perf cost (one
	// proxy hop) in exchange for a uniform agent UX.
	for k, v := range gw.Env() {
		_ = os.Setenv(k, v)
	}
	slog.Info("gateway: started",
		"podmanSocket", gw.Endpoints().PodmanSocket,
		"podmanMode", gw.Endpoints().PodmanMode,
		"ollamaURL", gw.Endpoints().OllamaURL,
		"ollamaMode", gw.Endpoints().OllamaMode,
	)
	return gw
}

// embeddedPodmanSocket returns the libpod socket path of the local
// engine, or empty when the engine isn't up.
func embeddedPodmanSocket(stack *stackComponents) string {
	if stack == nil || stack.containers == nil {
		return ""
	}
	if !stack.containers.Healthy() {
		return ""
	}
	eng := stack.containers.Engine()
	if eng == nil {
		return ""
	}
	return eng.SocketPath()
}

// embeddedOllamaURL returns the in-process runner's base URL when
// healthy. Empty when ollama isn't up — the gateway skips that backend
// rather than fronting an unreachable upstream.
func embeddedOllamaURL(stack *stackComponents) string {
	if stack == nil || stack.ollama == nil {
		return ""
	}
	if !stack.ollama.Healthy() {
		return ""
	}
	return stack.ollama.BaseURL()
}

// gatewayManifest returns the manifest sub-block describing the
// gateway's published endpoints. Nil-safe — returns nil when the
// gateway didn't start (no block emitted).
func gatewayManifest(gw *gateway.Gateway) map[string]any {
	if gw == nil {
		return nil
	}
	ep := gw.Endpoints()
	if ep.PodmanSocket == "" && ep.OllamaURL == "" {
		return nil
	}
	out := map[string]any{}
	if ep.PodmanSocket != "" {
		out["podman"] = map[string]any{
			"socket": ep.PodmanSocket,
			"mode":   string(ep.PodmanMode),
		}
	}
	if ep.OllamaURL != "" {
		out["ollama"] = map[string]any{
			"url":  ep.OllamaURL,
			"mode": string(ep.OllamaMode),
		}
	}
	return out
}

// stopGateway is a thin wrapper used in the deferred shutdown path so
// the caller doesn't need an extra nil check.
func stopGateway(gw *gateway.Gateway) {
	if gw == nil {
		return
	}
	if err := gw.Close(); err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("gateway: close error", "err", err)
	}
}
