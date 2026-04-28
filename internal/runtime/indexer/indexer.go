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
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/runtime/fileops"
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

// NOTE: Source extensions and skip directories are now defined in
// fileops.SourceExtensions and fileops.DefaultSkipDirs (walker.go)
// to avoid duplication across grep, glob, indexer, and embedder.

// Indexer scans workspace files and indexes them in Bleve.
type Indexer struct {
	workDir  string
	search   storage.SearchIndex
	kv       storage.KVStore // for tracking file mtimes
	RefGraph *RefGraph       // optional reference graph for Go files
	Trigrams *TrigramIndex   // optional trigram index for regex acceleration
}

// New creates a codebase indexer.
func New(workDir string, search storage.SearchIndex, kv storage.KVStore) *Indexer {
	return &Indexer{
		workDir:  workDir,
		search:   search,
		kv:       kv,
		RefGraph: NewRefGraph(kv),
		Trigrams: NewTrigramIndex(kv),
	}
}

// IndexOnce performs a single indexing pass over the workspace.
// Returns the number of files indexed.
func (idx *Indexer) IndexOnce(ctx context.Context) (int, error) {
	indexed := 0
	walkOpts := &fileops.WalkOptions{MaxFileSize: MaxFileSize}
	err := fileops.WalkSourceFiles(idx.workDir, walkOpts, func(path string, d fs.DirEntry) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Only index source files.
		ext := strings.ToLower(filepath.Ext(path))
		if !fileops.IsSourceExt(ext) {
			return nil
		}

		// Check if file has changed since last index.
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		relPath, _ := filepath.Rel(idx.workDir, path)
		if !idx.hasChanged(relPath, info.ModTime()) {
			return nil
		}

		// Read and index the file.
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		lang := strings.TrimPrefix(ext, ".")
		if err := idx.indexFile(ctx, relPath, string(content), ext); err != nil {
			slog.Debug("indexer: index file", "path", relPath, "error", err)
			return nil
		}

		// Also index symbols from this file.
		if err := idx.IndexSymbols(ctx, relPath, path, lang); err != nil {
			slog.Debug("indexer: index symbols", "path", relPath, "error", err)
		}

		// Build reference graph for Go files.
		if lang == "go" && idx.RefGraph != nil {
			idx.RefGraph.IndexFileReferences(path, relPath)
		}

		// Update trigram index.
		if idx.Trigrams != nil {
			idx.Trigrams.IndexFile(path, relPath)
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

// NotifyFileChanged immediately indexes a single file that was modified.
// This keeps the index fresh for files the agent is actively editing,
// without waiting for the next periodic scan.
func (idx *Indexer) NotifyFileChanged(path string) {
	// Resolve to absolute path and check it's within workDir.
	absPath, err := filepath.Abs(path)
	if err != nil {
		return
	}
	relPath, err := filepath.Rel(idx.workDir, absPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return // outside workspace
	}

	// Check if it's a source file.
	ext := strings.ToLower(filepath.Ext(absPath))
	if !fileops.IsSourceExt(ext) {
		return
	}

	// Check file size.
	info, err := os.Stat(absPath)
	if err != nil || info.Size() > MaxFileSize || info.Size() == 0 || info.IsDir() {
		return
	}

	// Read and index.
	content, err := os.ReadFile(absPath)
	if err != nil {
		return
	}

	ctx := context.Background()
	if err := idx.indexFile(ctx, relPath, string(content), ext); err != nil {
		slog.Debug("indexer: notify file changed", "path", relPath, "error", err)
		return
	}
	idx.recordMtime(relPath, info.ModTime())
	slog.Debug("indexer: re-indexed changed file", "path", relPath)
}

func hashMtime(t time.Time) string {
	h := sha256.Sum256([]byte(t.UTC().Format(time.RFC3339Nano)))
	return hex.EncodeToString(h[:8])
}
