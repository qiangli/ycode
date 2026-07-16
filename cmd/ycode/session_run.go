package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/session"
)

func resolveSessionDir(home, configured string) string {
	if configured != "" {
		return configured
	}
	return filepath.Join(home, ".agents", "ycode", "sessions")
}

func createSessionForRun(sessionDir, instanceID, forkID, resumeID string) (*session.Session, error) {
	forkID = strings.TrimSpace(forkID)
	resumeID = strings.TrimSpace(resumeID)
	if forkID != "" && resumeID != "" {
		return nil, fmt.Errorf("--fork and --resume cannot be used together")
	}
	if resumeID != "" {
		sess, err := session.Load(sessionDir, resumeID)
		if err != nil {
			return nil, unknownSessionError("resume", resumeID, sessionDir, err)
		}
		return sess, nil
	}
	if forkID != "" {
		source, err := session.Load(sessionDir, forkID)
		if err != nil {
			return nil, unknownSessionError("fork", forkID, sessionDir, err)
		}
		forked, err := session.NewWithID(sessionDir, instanceID)
		if err != nil {
			return nil, fmt.Errorf("create forked session: %w", err)
		}
		for _, msg := range source.Messages {
			if err := forked.AddMessage(msg); err != nil {
				return nil, fmt.Errorf("seed forked session: %w", err)
			}
		}
		return forked, nil
	}
	sess, err := session.NewWithID(sessionDir, instanceID)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return sess, nil
}

func unknownSessionError(action, id, dir string, err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("--%s session %q not found in %s", action, id, dir)
	}
	return fmt.Errorf("--%s session %q: %w", action, id, err)
}
