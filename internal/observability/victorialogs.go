package observability

import "fmt"

// VictoriaLogsArgs returns command-line arguments for VictoriaLogs.
func VictoriaLogsArgs(port int, dataDir string) []string {
	return []string{
		"-httpListenAddr", fmt.Sprintf("127.0.0.1:%d", port),
		"-storageDataPath", dataDir,
	}
}
