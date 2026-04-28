package embedding

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/runtime/fileops"
	"github.com/qiangli/ycode/internal/storage"
)

const (
	codeCollection    = "codebase"
	memoryCollection  = "memory"
	sessionCollection = "sessions"
	docsCollection    = "docs"

	// maxEmbedFilesPerPass limits the number of files embedded per code pass
	// to prevent runaway API costs when using an API-based embedding provider.
	maxEmbedFilesPerPass = 500
)

// Embedder runs background embedding tasks for code, memory, and sessions.
type Embedder struct {
	provider Provider
	vector   storage.VectorStore
	kv       storage.KVStore // for tracking what's been embedded
	workDir  string
}

// New creates a background embedder.
func New(provider Provider, vector storage.VectorStore, kv storage.KVStore, workDir string) *Embedder {
	return &Embedder{
		provider: provider,
		vector:   vector,
		kv:       kv,
		workDir:  workDir,
	}
}

// EmbedMemory computes and stores an embedding for a memory entry.
func (e *Embedder) EmbedMemory(ctx context.Context, name, description, content string, metadata map[string]string) error {
	text := name + " " + description + " " + content
	emb, err := e.provider.Embed(ctx, text)
	if err != nil {
		return err
	}

	doc := storage.VectorDocument{
		Document: storage.Document{
			ID:       name,
			Content:  text,
			Metadata: metadata,
		},
		Embedding: emb,
	}
	return e.vector.AddDocuments(ctx, memoryCollection, []storage.VectorDocument{doc})
}

// EmbedSessionSummary computes and stores an embedding for a session compaction summary.
func (e *Embedder) EmbedSessionSummary(ctx context.Context, sessionID, summary string) error {
	emb, err := e.provider.Embed(ctx, summary)
	if err != nil {
		return err
	}

	doc := storage.VectorDocument{
		Document: storage.Document{
			ID:      sessionID,
			Content: summary,
			Metadata: map[string]string{
				"session_id": sessionID,
				"type":       "summary",
			},
		},
		Embedding: emb,
	}
	return e.vector.AddDocuments(ctx, sessionCollection, []storage.VectorDocument{doc})
}

// EmbedCodeFile reads a source file and embeds its chunks.
func (e *Embedder) EmbedCodeFile(ctx context.Context, relPath string) error {
	absPath := filepath.Join(e.workDir, relPath)
	content, err := os.ReadFile(absPath)
	if err != nil {
		return err
	}

	// Split into function-sized chunks (~2KB each).
	chunks := splitCodeChunks(string(content), 2048)
	ext := strings.TrimPrefix(filepath.Ext(relPath), ".")

	// If the provider supports learning (e.g. TF-IDF), build vocabulary first.
	if learner, ok := e.provider.(Learner); ok {
		for _, chunk := range chunks {
			learner.Learn(chunk)
		}
	}

	var docs []storage.VectorDocument
	for i, chunk := range chunks {
		emb, err := e.provider.Embed(ctx, chunk)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err() // context cancelled — stop silently
			}
			slog.Debug("embedder: embed chunk", "path", relPath, "chunk", i, "error", err)
			continue
		}

		docID := relPath
		if len(chunks) > 1 {
			docID = relPath + "#" + strconv.Itoa(i)
		}

		docs = append(docs, storage.VectorDocument{
			Document: storage.Document{
				ID:      docID,
				Content: chunk,
				Metadata: map[string]string{
					"path":     relPath,
					"language": ext,
					"chunk":    strconv.Itoa(i),
				},
			},
			Embedding: emb,
		})
	}

	if len(docs) > 0 {
		return e.vector.AddDocuments(ctx, codeCollection, docs)
	}
	return nil
}

// EmbedDocFile embeds a documentation file (CLAUDE.md, README, etc.).
func (e *Embedder) EmbedDocFile(ctx context.Context, relPath, content string) error {
	chunks := splitDocChunks(content, 2048)

	var docs []storage.VectorDocument
	for i, chunk := range chunks {
		emb, err := e.provider.Embed(ctx, chunk)
		if err != nil {
			continue
		}

		docID := relPath
		if len(chunks) > 1 {
			docID = relPath + "#" + strconv.Itoa(i)
		}

		docs = append(docs, storage.VectorDocument{
			Document: storage.Document{
				ID:      docID,
				Content: chunk,
				Metadata: map[string]string{
					"path":  relPath,
					"type":  "documentation",
					"chunk": strconv.Itoa(i),
				},
			},
			Embedding: emb,
		})
	}

	if len(docs) > 0 {
		return e.vector.AddDocuments(ctx, docsCollection, docs)
	}
	return nil
}

// RunCodeEmbedding performs a single pass of code embedding over the workspace.
func (e *Embedder) RunCodeEmbedding(ctx context.Context) (int, error) {
	indexed := 0
	walkOpts := &fileops.WalkOptions{MaxFileSize: 1 << 20}
	err := fileops.WalkSourceFiles(e.workDir, walkOpts, func(path string, d fs.DirEntry) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		ext := strings.ToLower(filepath.Ext(path))
		if !fileops.IsSourceExt(ext) {
			return nil
		}

		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}

		relPath, _ := filepath.Rel(e.workDir, path)

		// Check if already embedded (use KV store for tracking).
		if e.kv != nil {
			key := "emb:" + relPath
			stored, _ := e.kv.Get("embeddings", key)
			if stored != nil {
				storedTime := string(stored)
				if storedTime == info.ModTime().UTC().Format(time.RFC3339) {
					return nil // already embedded and not changed
				}
			}
		}

		if indexed >= maxEmbedFilesPerPass {
			return filepath.SkipAll // cap reached, stop walking
		}

		if err := e.EmbedCodeFile(ctx, relPath); err != nil {
			slog.Debug("embedder: code file", "path", relPath, "error", err)
			return nil
		}

		// Record embedding time.
		if e.kv != nil {
			_ = e.kv.Put("embeddings", "emb:"+relPath, []byte(info.ModTime().UTC().Format(time.RFC3339)))
		}

		indexed++
		return nil
	})

	return indexed, err
}

// splitCodeChunks splits code into approximately equal chunks, preferring line boundaries.
func splitCodeChunks(content string, maxSize int) []string {
	if len(content) <= maxSize {
		return []string{content}
	}

	lines := strings.Split(content, "\n")
	var chunks []string
	var current strings.Builder

	for _, line := range lines {
		if current.Len()+len(line)+1 > maxSize && current.Len() > 0 {
			chunks = append(chunks, current.String())
			current.Reset()
		}
		current.WriteString(line)
		current.WriteByte('\n')
	}
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}

// splitDocChunks splits documentation into paragraph-based chunks.
func splitDocChunks(content string, maxSize int) []string {
	if len(content) <= maxSize {
		return []string{content}
	}

	// Split on double newlines (paragraphs/sections).
	paragraphs := strings.Split(content, "\n\n")
	var chunks []string
	var current strings.Builder

	for _, para := range paragraphs {
		if current.Len()+len(para)+2 > maxSize && current.Len() > 0 {
			chunks = append(chunks, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(para)
	}
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}

// NOTE: isSkipDir and isSourceExt removed — now using fileops.ShouldSkipDir
// and fileops.IsSourceExt from the shared walker (walker.go).
