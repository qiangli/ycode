// Package indexer provides background codebase indexing into Bleve for full-text search.
//
// The indexer scans source files in the workspace, splits them into chunks,
// and indexes them in Bleve. It tracks file modification times in the KV store
// to avoid re-indexing unchanged files. It can be run periodically or on demand.
package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/storage"
)

const (
	codeIndexName = "code"
	kvBucket      = "file_metadata"

	// MaxFileSize is the maximum file size to index (1MB).
	MaxFileSize = 1 << 20
	// ChunkSize is the approximate size of each indexed chunk.
	ChunkSize = 4096
	// IndexInterval is how often the indexer re-scans the workspace.
	IndexInterval = 5 * time.Minute
)

// sourceExtensions is the set of file extensions to index.
var sourceExtensions = map[string]bool{
	".go": true, ".rs": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".py": true, ".java": true, ".c": true, ".cpp": true, ".h": true, ".hpp": true,
	".rb": true, ".sh": true, ".bash": true, ".zsh": true,
	".json": true, ".yaml": true, ".yml": true, ".toml": true,
	".md": true, ".txt": true, ".sql": true, ".graphql": true,
	".css": true, ".scss": true, ".html": true, ".xml": true,
	".proto": true, ".swift": true, ".kt": true, ".scala": true,
}

// skipDirs is the set of directories to always skip.
var skipDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true,
	"node_modules": true, "vendor": true, "__pycache__": true,
	".ycode": true, ".claw": true, ".claude": true,
	"dist": true, "build": true, "target": true, "bin": true,
	"priorart": true,
}

// Indexer scans workspace files and indexes them in Bleve.
type Indexer struct {
	workDir string
	search  storage.SearchIndex
	kv      storage.KVStore // for tracking file mtimes
}

// New creates a codebase indexer.
func New(workDir string, search storage.SearchIndex, kv storage.KVStore) *Indexer {
	return &Indexer{
		workDir: workDir,
		search:  search,
		kv:      kv,
	}
}

// IndexOnce performs a single indexing pass over the workspace.
// Returns the number of files indexed.
func (idx *Indexer) IndexOnce(ctx context.Context) (int, error) {
	indexed := 0
	err := filepath.WalkDir(idx.workDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Skip directories.
		if d.IsDir() {
			base := filepath.Base(path)
			if skipDirs[base] {
				return filepath.SkipDir
			}
			return nil
		}

		// Only index source files.
		ext := strings.ToLower(filepath.Ext(path))
		if !sourceExtensions[ext] {
			return nil
		}

		// Check file size.
		info, err := d.Info()
		if err != nil || info.Size() > MaxFileSize || info.Size() == 0 {
			return nil
		}

		// Check if file has changed since last index.
		relPath, _ := filepath.Rel(idx.workDir, path)
		if !idx.hasChanged(relPath, info.ModTime()) {
			return nil
		}

		// Read and index the file.
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		if err := idx.indexFile(ctx, relPath, string(content), ext); err != nil {
			slog.Debug("indexer: index file", "path", relPath, "error", err)
			return nil
		}

		// Record mtime.
		idx.recordMtime(relPath, info.ModTime())
		indexed++
		return nil
	})

	return indexed, err
}

// Run starts the background indexing loop. It performs an initial index
// and then re-scans periodically. Blocks until ctx is cancelled.
func (idx *Indexer) Run(ctx context.Context) {
	// Initial indexing pass.
	if n, err := idx.IndexOnce(ctx); err != nil {
		slog.Debug("indexer: initial pass", "error", err)
	} else if n > 0 {
		slog.Debug("indexer: initial pass", "indexed", n)
	}

	ticker := time.NewTicker(IndexInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := idx.IndexOnce(ctx); err != nil {
				slog.Debug("indexer: periodic pass", "error", err)
			} else if n > 0 {
				slog.Debug("indexer: periodic pass", "indexed", n)
			}
		}
	}
}

// indexFile splits content into chunks and indexes each in Bleve.
func (idx *Indexer) indexFile(ctx context.Context, relPath, content, ext string) error {
	lang := strings.TrimPrefix(ext, ".")

	// For small files, index as a single document.
	if len(content) <= ChunkSize {
		doc := storage.Document{
			ID:      relPath,
			Content: content,
			Metadata: map[string]string{
				"path":     relPath,
				"language": lang,
			},
		}
		return idx.search.Index(ctx, codeIndexName, doc)
	}

	// Split into chunks for larger files.
	lines := strings.Split(content, "\n")
	var docs []storage.Document
	var chunk strings.Builder
	chunkStart := 1

	for i, line := range lines {
		chunk.WriteString(line)
		chunk.WriteByte('\n')

		if chunk.Len() >= ChunkSize || i == len(lines)-1 {
			docID := fmt.Sprintf("%s#L%d", relPath, chunkStart)
			docs = append(docs, storage.Document{
				ID:      docID,
				Content: chunk.String(),
				Metadata: map[string]string{
					"path":     relPath,
					"language": lang,
					"lines":    fmt.Sprintf("%d-%d", chunkStart, i+1),
				},
			})
			chunk.Reset()
			chunkStart = i + 2
		}
	}

	if len(docs) > 0 {
		return idx.search.BatchIndex(ctx, codeIndexName, docs)
	}
	return nil
}

// hasChanged checks if a file has been modified since the last index.
func (idx *Indexer) hasChanged(relPath string, mtime time.Time) bool {
	if idx.kv == nil {
		return true
	}
	stored, err := idx.kv.Get(kvBucket, relPath)
	if err != nil || stored == nil {
		return true
	}
	storedHash := string(stored)
	currentHash := hashMtime(mtime)
	return storedHash != currentHash
}

// recordMtime stores the file's modification time hash.
func (idx *Indexer) recordMtime(relPath string, mtime time.Time) {
	if idx.kv == nil {
		return
	}
	hash := hashMtime(mtime)
	_ = idx.kv.Put(kvBucket, relPath, []byte(hash))
}

func hashMtime(t time.Time) string {
	h := sha256.Sum256([]byte(t.UTC().Format(time.RFC3339Nano)))
	return hex.EncodeToString(h[:8])
}
