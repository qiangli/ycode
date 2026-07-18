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
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/runtime/fileops"
	yotel "github.com/qiangli/ycode/internal/telemetry/otel"
	"github.com/qiangli/ycode/pkg/memex/store"
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
	search   store.SearchIndex
	kv       store.KVStore      // for tracking file mtimes
	RefGraph *RefGraph          // optional reference graph for Go files
	Trigrams *TrigramIndex      // optional trigram index for regex acceleration
	Inst     *yotel.Instruments // optional OTEL instruments
}

// New creates a codebase indexer.
func New(workDir string, search store.SearchIndex, kv store.KVStore) *Indexer {
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

		lang := strings.TrimPrefix(ext, ".")

		// Full-text content indexing (bleve "code" index) removed: it cost ~27 GB
		// + a 15-min per-workspace build, yet the grep path discarded its results
		// (see the former grep_indexed.go) and re-ran a full ripgrep anyway. Symbol
		// + reference indexing below stay — they give what ripgrep can't.

		// Index symbols from this file.
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
	idx.runIndexPass(ctx, "initial")

	ticker := time.NewTicker(IndexInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			idx.runIndexPass(ctx, "periodic")
		}
	}
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

// runIndexPass performs a single indexing pass with OTEL metrics and logging.
func (idx *Indexer) runIndexPass(ctx context.Context, kind string) {
	start := time.Now()
	n, err := idx.IndexOnce(ctx)
	dur := time.Since(start)

	if err != nil {
		slog.Debug("indexer: "+kind+" pass", "error", err)
		return
	}
	if n > 0 {
		slog.Debug("indexer: "+kind+" pass", "indexed", n, "duration_ms", dur.Milliseconds())
	}
	if idx.Inst != nil {
		idx.Inst.SearchIndexerDuration.Record(ctx, float64(dur.Milliseconds()))
		if n > 0 {
			idx.Inst.SearchIndexerFiles.Add(ctx, int64(n))
		}
	}
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

	// Re-index SYMBOLS for the just-edited file (the full-text code index was
	// removed — see IndexOnce), so symbol search stays fresh for the active file.
	ctx := context.Background()
	lang := strings.TrimPrefix(ext, ".")
	if err := idx.IndexSymbols(ctx, relPath, absPath, lang); err != nil {
		slog.Debug("indexer: notify file changed", "path", relPath, "error", err)
		return
	}
	idx.recordMtime(relPath, info.ModTime())
	slog.Debug("indexer: re-indexed changed file symbols", "path", relPath)
}

func hashMtime(t time.Time) string {
	h := sha256.Sum256([]byte(t.UTC().Format(time.RFC3339Nano)))
	return hex.EncodeToString(h[:8])
}
