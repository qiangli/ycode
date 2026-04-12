package cluster

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// NATSInfo is the JSON written to nats.json so other instances can discover the server.
type NATSInfo struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	PID        int    `json:"pid"`
	InstanceID string `json:"instanceID"`
}

// natsServer manages the embedded NATS server lifecycle.
type natsServer struct {
	srv      *server.Server
	port     int
	storeDir string
	infoPath string // path to nats.json
}

// startNATSServer starts an embedded NATS server with JetStream enabled.
func startNATSServer(port int, storeDir, infoPath string) (*natsServer, error) {
	// Pre-flight port check to fail fast instead of waiting for the 5s timeout.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, fmt.Errorf("nats port %d already in use", port)
	}
	ln.Close()

	opts := &server.Options{
		Host:           "127.0.0.1",
		Port:           port,
		NoLog:          true,
		NoSigs:         true,
		MaxControlLine: 4096,
		JetStream:      true,
		StoreDir:       filepath.Join(storeDir, "jetstream"),
	}

	srv, err := server.NewServer(opts)
	if err != nil {
		return nil, fmt.Errorf("create nats server: %w", err)
	}

	srv.Start()

	if !srv.ReadyForConnections(5 * time.Second) {
		srv.Shutdown()
		return nil, fmt.Errorf("nats server not ready within timeout")
	}

	ns := &natsServer{
		srv:      srv,
		port:     port,
		storeDir: storeDir,
		infoPath: infoPath,
	}

	if err := ns.writeInfo(); err != nil {
		srv.Shutdown()
		return nil, fmt.Errorf("write nats info: %w", err)
	}

	slog.Info("cluster: embedded NATS server started", "port", port)
	return ns, nil
}

// stop shuts down the embedded NATS server and removes nats.json.
func (ns *natsServer) stop() {
	if ns.srv != nil {
		ns.srv.Shutdown()
		ns.srv.WaitForShutdown()
		slog.Info("cluster: embedded NATS server stopped")
	}
	os.Remove(ns.infoPath)
}

func (ns *natsServer) writeInfo() error {
	info := NATSInfo{
		Host:       "127.0.0.1",
		Port:       ns.port,
		PID:        os.Getpid(),
		InstanceID: "", // filled by caller
	}
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return os.WriteFile(ns.infoPath, data, 0o644)
}

// connectNATS connects to a NATS server. If addr is empty, connects to the
// address in nats.json. Configures automatic reconnection.
func connectNATS(addr string, infoPath string) (*nats.Conn, error) {
	if addr == "" {
		info, err := readNATSInfo(infoPath)
		if err != nil {
			return nil, fmt.Errorf("read nats info: %w", err)
		}
		addr = fmt.Sprintf("nats://127.0.0.1:%d", info.Port)
	}

	nc, err := nats.Connect(addr,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(time.Second),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			slog.Warn("cluster: NATS disconnected", "error", err)
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) {
			slog.Info("cluster: NATS reconnected")
		}),
		nats.ClosedHandler(func(_ *nats.Conn) {
			slog.Info("cluster: NATS connection closed")
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("connect nats: %w", err)
	}
	return nc, nil
}

// readNATSInfo reads the nats.json discovery file.
func readNATSInfo(path string) (*NATSInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var info NATSInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	return &info, nil
}
