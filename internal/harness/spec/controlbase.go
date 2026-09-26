package spec

import (
	"os"
	"path/filepath"
	"strings"
)

// ControlBase is the directory runtime.controlRoot.platformDataDir is resolved
// under. It follows bashy's home convention on every OS — $BASHY_HOME, else
// ~/.bashy, else the system temp dir — never a per-OS config location
// (~/Library/Application Support, %AppData%), so a control root is at the
// same place on macOS, Linux, Windows and the FROM-scratch bashy image.
func ControlBase() string {
	if home := strings.TrimSpace(os.Getenv("BASHY_HOME")); home != "" {
		return home
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".bashy")
	}
	return filepath.Join(os.TempDir(), "bashy")
}

// ControlRootPath is the absolute control root the document declares.
func ControlRootPath(d *Document) string {
	return filepath.Join(ControlBase(), d.Spec.Runtime.ControlRoot.PlatformDataDir)
}
