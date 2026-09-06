package event

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PayloadStore retains replay-critical bytes outside the JSONL envelope. A
// digest is the address, so retries are idempotent and corruption is detected
// before bytes are returned to a replay.
type PayloadStore struct {
	root string
}

func OpenPayloadStore(root string) (*PayloadStore, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("payload root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("create payload root: %w", err)
	}
	return &PayloadStore{root: abs}, nil
}

func (s *PayloadStore) Put(data []byte) (string, error) {
	if s == nil || s.root == "" {
		return "", errors.New("payload store is not open")
	}
	digest := digestBytes(data)
	path := s.path(digest)
	if existing, err := os.ReadFile(path); err == nil {
		if digestBytes(existing) != digest {
			return "", fmt.Errorf("payload %s is corrupt", digest)
		}
		return digest, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read payload %s: %w", digest, err)
	}
	tmp, err := os.CreateTemp(s.root, ".payload-*")
	if err != nil {
		return "", fmt.Errorf("create payload: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", fmt.Errorf("write payload: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		if _, statErr := os.Stat(path); statErr == nil {
			return digest, nil
		}
		return "", fmt.Errorf("publish payload: %w", err)
	}
	return digest, nil
}

func (s *PayloadStore) Get(digest string) ([]byte, error) {
	if s == nil || s.root == "" || !validDigest(digest) {
		return nil, errors.New("invalid payload digest")
	}
	data, err := os.ReadFile(s.path(digest))
	if err != nil {
		return nil, fmt.Errorf("read payload %s: %w", digest, err)
	}
	if digestBytes(data) != digest {
		return nil, fmt.Errorf("payload %s is corrupt", digest)
	}
	return data, nil
}

func (s *PayloadStore) path(digest string) string {
	return filepath.Join(s.root, strings.TrimPrefix(digest, "sha256:"))
}

func validDigest(value string) bool {
	if len(value) != len("sha256:")+sha256HexLen || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, r := range strings.TrimPrefix(value, "sha256:") {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

const sha256HexLen = 64
