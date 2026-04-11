package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"

	"github.com/qiangli/ycode/internal/collector"
	"github.com/qiangli/ycode/internal/runtime/config"
)

// ComponentStatus describes the status of a stack component.
type ComponentStatus struct {
	Name      string `json:"name"`
	PID       int    `json:"pid"`
	Port      int    `json:"port"`
	ProxyPath string `json:"proxy_path"`
	Healthy   bool   `json:"healthy"`
}

// StackManager orchestrates all observability components and the reverse proxy.
type StackManager struct {
	cfg     *config.ObservabilityConfig
	binDir  string // ~/.ycode/bin/
	dataDir string // ~/.ycode/observability/
	otelDir string // ~/.ycode/otel/

	ports     *PortAllocator
	proxy     *ProxyServer
	collector *collector.Manager
	processes map[string]*Process
}

// NewStackManager creates a stack manager.
func NewStackManager(cfg *config.ObservabilityConfig, binDir, dataDir, otelDir string) *StackManager {
	return &StackManager{
		cfg:       cfg,
		binDir:    binDir,
		dataDir:   dataDir,
		otelDir:   otelDir,
		ports:     NewPortAllocator(dataDir),
		processes: make(map[string]*Process),
	}
}

// Start downloads binaries, allocates ports, generates configs, and starts all components.
func (s *StackManager) Start(ctx context.Context) error {
	slog.Info("observability: starting stack")

	// 1. Allocate ports for all components.
	portNames := []string{
		"collector-grpc", "collector-http", "collector-prom", "collector-health",
		"prometheus", "alertmanager", "karma", "perses", "victoria-logs",
	}
	for _, name := range portNames {
		if _, err := s.ports.Allocate(name); err != nil {
			return fmt.Errorf("allocate port %s: %w", name, err)
		}
	}

	// 2. Start components in dependency order.

	// VictoriaLogs first (log sink).
	if err := s.startVictoriaLogs(ctx); err != nil {
		slog.Warn("observability: victoria-logs start failed", "error", err)
	}

	// OTEL Collector.
	if err := s.startCollector(ctx); err != nil {
		slog.Warn("observability: collector start failed", "error", err)
	}

	// Prometheus.
	if err := s.startPrometheus(ctx); err != nil {
		slog.Warn("observability: prometheus start failed", "error", err)
	}

	// Alertmanager.
	if err := s.startAlertmanager(ctx); err != nil {
		slog.Warn("observability: alertmanager start failed", "error", err)
	}

	// Karma.
	if err := s.startKarma(ctx); err != nil {
		slog.Warn("observability: karma start failed", "error", err)
	}

	// Perses.
	if err := s.startPerses(ctx); err != nil {
		slog.Warn("observability: perses start failed", "error", err)
	}

	// 3. Start reverse proxy.
	proxyPort := s.cfg.ProxyPort
	if proxyPort == 0 {
		proxyPort = 58080
	}
	bindAddr := s.cfg.ProxyBindAddr
	if bindAddr == "" {
		bindAddr = "127.0.0.1"
	}
	s.proxy = NewProxyServer(bindAddr, proxyPort)
	s.registerProxyRoutes()
	if err := s.proxy.Start(ctx); err != nil {
		return fmt.Errorf("start proxy: %w", err)
	}

	slog.Info("observability: stack started", "proxy", s.proxy.Addr())
	return nil
}

// Stop shuts down all components and the proxy.
func (s *StackManager) Stop() error {
	slog.Info("observability: stopping stack")

	if s.proxy != nil {
		_ = s.proxy.Stop(context.Background())
	}
	for name, proc := range s.processes {
		if err := proc.Stop(); err != nil {
			slog.Warn("observability: stop failed", "component", name, "error", err)
		}
	}
	if s.collector != nil {
		_ = s.collector.Stop()
	}
	s.ports.ReleaseAll()
	return nil
}

// Status returns the health status of each component.
func (s *StackManager) Status() []ComponentStatus {
	ctx := context.Background()
	var statuses []ComponentStatus

	// Collector.
	statuses = append(statuses, ComponentStatus{
		Name:      "otel-collector",
		PID:       s.collectorPID(),
		Port:      s.ports.Get("collector-grpc"),
		ProxyPath: "/collector/",
		Healthy:   s.collector != nil && s.collector.Running(),
	})

	proxyPaths := map[string]string{
		"prometheus":    "/prometheus/",
		"alertmanager":  "/alerts/",
		"karma":         "/karma/",
		"perses":        "/dashboard/",
		"victoria-logs": "/logs/",
	}
	for name, proc := range s.processes {
		statuses = append(statuses, ComponentStatus{
			Name:      name,
			PID:       proc.PID(),
			Port:      proc.Port,
			ProxyPath: proxyPaths[name],
			Healthy:   proc.Healthy(ctx),
		})
	}

	return statuses
}

// CollectorGRPCPort returns the port the collector's OTLP gRPC receiver is on.
func (s *StackManager) CollectorGRPCPort() int {
	return s.ports.Get("collector-grpc")
}

func (s *StackManager) startVictoriaLogs(ctx context.Context) error {
	port := s.ports.Get("victoria-logs")
	dataDir := filepath.Join(s.dataDir, "vlogs", "data")
	proc := &Process{
		Name:       "victoria-logs",
		BinaryPath: filepath.Join(s.binDir, "victoria-logs"), // simplified
		Args:       VictoriaLogsArgs(port, dataDir),
		Port:       port,
		DataDir:    filepath.Join(s.dataDir, "vlogs"),
		HealthPath: "/health",
	}
	if err := proc.Start(ctx); err != nil {
		return err
	}
	s.processes["victoria-logs"] = proc
	return nil
}

func (s *StackManager) startCollector(ctx context.Context) error {
	collCfg := collector.Config{
		GRPCPort:         s.ports.Get("collector-grpc"),
		HTTPPort:         s.ports.Get("collector-http"),
		PrometheusPort:   s.ports.Get("collector-prom"),
		HealthPort:       s.ports.Get("collector-health"),
		VictoriaLogsPort: s.ports.Get("victoria-logs"),
	}
	s.collector = collector.NewManager(s.binDir, filepath.Join(s.otelDir, "collector"), "")
	return s.collector.Start(ctx, collCfg)
}

func (s *StackManager) startPrometheus(ctx context.Context) error {
	port := s.ports.Get("prometheus")
	promDir := filepath.Join(s.dataDir, "prometheus")
	promCfg := PrometheusConfig{
		Port:             port,
		CollectorMetrics: s.ports.Get("collector-prom"),
		AlertmanagerPort: s.ports.Get("alertmanager"),
		VictoriaLogsPort: s.ports.Get("victoria-logs"),
		DataDir:          filepath.Join(promDir, "data"),
	}
	// Convert config remote-write targets.
	for _, rw := range s.cfg.RemoteWrite {
		target := RemoteWriteTarget{URL: rw.URL, Headers: rw.Headers}
		if rw.BasicAuth != nil {
			target.BasicAuth = &BasicAuthConfig{
				Username: rw.BasicAuth.Username,
				Password: rw.BasicAuth.Password,
			}
		}
		promCfg.RemoteWrite = append(promCfg.RemoteWrite, target)
	}
	for _, fed := range s.cfg.Federation {
		promCfg.Federation = append(promCfg.Federation, FederationTarget{URL: fed.URL, Match: fed.Match})
	}

	configPath, err := WritePrometheusConfig(promDir, promCfg)
	if err != nil {
		return err
	}
	proc := &Process{
		Name:       "prometheus",
		BinaryPath: filepath.Join(s.binDir, "prometheus"),
		Args: []string{
			"--config.file=" + configPath,
			fmt.Sprintf("--web.listen-address=127.0.0.1:%d", port),
			"--web.external-url=/prometheus/",
			"--web.route-prefix=/",
			"--storage.tsdb.path=" + filepath.Join(promDir, "data"),
		},
		Port:       port,
		DataDir:    promDir,
		HealthPath: "/-/healthy",
	}
	if err := proc.Start(ctx); err != nil {
		return err
	}
	s.processes["prometheus"] = proc
	return nil
}

func (s *StackManager) startAlertmanager(ctx context.Context) error {
	port := s.ports.Get("alertmanager")
	dir := filepath.Join(s.dataDir, "alertmanager")
	configPath, err := WriteAlertmanagerConfig(dir)
	if err != nil {
		return err
	}
	proc := &Process{
		Name:       "alertmanager",
		BinaryPath: filepath.Join(s.binDir, "alertmanager"),
		Args: []string{
			"--config.file=" + configPath,
			fmt.Sprintf("--web.listen-address=127.0.0.1:%d", port),
			"--web.external-url=/alerts/",
			"--web.route-prefix=/",
		},
		Port:       port,
		DataDir:    dir,
		HealthPath: "/-/healthy",
	}
	if err := proc.Start(ctx); err != nil {
		return err
	}
	s.processes["alertmanager"] = proc
	return nil
}

func (s *StackManager) startKarma(ctx context.Context) error {
	port := s.ports.Get("karma")
	dir := filepath.Join(s.dataDir, "karma")
	configPath, err := WriteKarmaConfig(dir, s.ports.Get("alertmanager"))
	if err != nil {
		return err
	}
	proc := &Process{
		Name:       "karma",
		BinaryPath: filepath.Join(s.binDir, "karma"),
		Args: []string{
			"--config.file=" + configPath,
			fmt.Sprintf("--listen.address=127.0.0.1"),
			fmt.Sprintf("--listen.port=%d", port),
			"--listen.prefix=/karma/",
		},
		Port:       port,
		DataDir:    dir,
		HealthPath: "/karma/",
	}
	if err := proc.Start(ctx); err != nil {
		return err
	}
	s.processes["karma"] = proc
	return nil
}

func (s *StackManager) startPerses(ctx context.Context) error {
	port := s.ports.Get("perses")
	dir := filepath.Join(s.dataDir, "perses")
	configPath, err := WritePersesConfig(dir, s.ports.Get("prometheus"))
	if err != nil {
		return err
	}
	proc := &Process{
		Name:       "perses",
		BinaryPath: filepath.Join(s.binDir, "perses"),
		Args: []string{
			"--config=" + configPath,
			fmt.Sprintf("--web.listen-address=127.0.0.1:%d", port),
		},
		Port:       port,
		DataDir:    dir,
		HealthPath: "/api/v1/health",
	}
	if err := proc.Start(ctx); err != nil {
		return err
	}
	s.processes["perses"] = proc
	return nil
}

func (s *StackManager) registerProxyRoutes() {
	routes := map[string]string{
		"/prometheus/": "prometheus",
		"/alerts/":     "alertmanager",
		"/karma/":      "karma",
		"/dashboard/":  "perses",
		"/logs/":       "victoria-logs",
		"/collector/":  "collector-health",
	}
	for path, component := range routes {
		port := s.ports.Get(component)
		if port > 0 {
			backend, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
			s.proxy.AddRoute(path, backend)
		}
	}
}

func (s *StackManager) collectorPID() int {
	if s.collector != nil {
		return s.collector.PID()
	}
	return 0
}
