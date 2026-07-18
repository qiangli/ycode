package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/qiangli/ycode/internal/runtime/fileops"
	"github.com/qiangli/ycode/internal/runtime/vfs"
	yotel "github.com/qiangli/ycode/internal/telemetry/otel"
	"github.com/qiangli/ycode/pkg/memex/store"
)

// codeSearchIndex is an optional Bleve index for natural-language code search fallback.
var codeSearchIndex store.SearchIndex

// searchInstruments holds optional OTEL instruments for search metrics.
var searchInstruments *yotel.Instruments

const codeIndexName = "code"

// SetCodeSearchIndex sets the Bleve index used for natural-language grep fallback.
func SetCodeSearchIndex(idx store.SearchIndex) {
	codeSearchIndex = idx
}

// SetSearchInstruments sets the OTEL instruments for search metrics.
func SetSearchInstruments(inst *yotel.Instruments) {
	searchInstruments = inst
}

// RegisterSearchHandlers registers glob and grep tool handlers with VFS path validation.
func RegisterSearchHandlers(r *Registry, v *vfs.VFS) {
	// glob_search
	if spec, ok := r.Get("glob_search"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			var params fileops.GlobParams
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse glob input: %w", err)
			}
			// Validate base path if provided.
			if params.Path != "" {
				absPath, err := v.ValidatePath(ctx, params.Path)
				if err != nil {
					return "", err
				}
				params.Path = absPath
				r.NotifyFileAccess(absPath)
			}
			result, err := fileops.GlobSearch(params)
			if err != nil {
				return "", err
			}
			if len(result.Files) == 0 {
				return "No files matched the pattern.", nil
			}
			return strings.Join(result.Files, "\n"), nil
		}
	}

	// grep_search
	if spec, ok := r.Get("grep_search"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			var params fileops.GrepParams
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse grep input: %w", err)
			}
			// Validate base path if provided.
			if params.Path != "" {
				absPath, err := v.ValidatePath(ctx, params.Path)
				if err != nil {
					return "", err
				}
				params.Path = absPath
				r.NotifyFileAccess(absPath)
			}
			start := time.Now()
			// Pure ripgrep. The former bleve "indexed grep" pre-filter discarded its
			// candidates and ran a full grep anyway (dead code), and the full-text
			// code index it needed cost ~27 GB + a 15-min per-workspace build for no
			// grep benefit — removed. Full-text search, if ever wanted, belongs in
			// `bashy search`, not baked into the ycode harness.
			result, err := fileops.GrepSearch(params)
			dur := time.Since(start)
			if searchInstruments != nil {
				searchInstruments.SearchGrepTotal.Add(ctx, 1)
				searchInstruments.SearchGrepDuration.Record(ctx, float64(dur.Milliseconds()),
					metric.WithAttributes(
						attribute.String("search.pattern", params.Pattern),
						attribute.String("search.mode", string(params.OutputMode)),
					))
			}
			slog.Debug("grep_search", "pattern", params.Pattern, "duration_ms", dur.Milliseconds(),
				"files", len(result.Files), "matches", len(result.Matches))
			if err != nil {
				return "", err
			}

			switch params.OutputMode {
			case fileops.GrepOutputContent:
				var lines []string
				for _, m := range result.Matches {
					lines = append(lines, fmt.Sprintf("%s:%d: %s", m.File, m.Line, m.Content))
				}
				if len(lines) == 0 {
					return "No matches found.", nil
				}
				return strings.Join(lines, "\n"), nil
			case fileops.GrepOutputCount:
				return fmt.Sprintf("%d matches", result.Count), nil
			default: // files_with_matches
				if len(result.Files) == 0 {
					return "No matches found.", nil
				}
				return strings.Join(result.Files, "\n"), nil
			}
		}
	}
}
