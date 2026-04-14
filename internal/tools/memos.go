package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/qiangli/ycode/internal/memos"
)

// memosClient is the module-level Memos client, set via SetMemosClient.
// Nil when Memos is not available (e.g. serve mode not running).
var memosClient *memos.Client

// SetMemosClient injects the Memos REST API client for the memo tools.
func SetMemosClient(c *memos.Client) {
	memosClient = c
}

// RegisterMemosHandlers wires up the MemosStore, MemosSearch, MemosList,
// and MemosDelete tool handlers.
func RegisterMemosHandlers(r *Registry) {
	if spec, ok := r.Get("MemosStore"); ok {
		spec.Handler = handleMemosStore
	}
	if spec, ok := r.Get("MemosSearch"); ok {
		spec.Handler = handleMemosSearch
	}
	if spec, ok := r.Get("MemosList"); ok {
		spec.Handler = handleMemosList
	}
	if spec, ok := r.Get("MemosDelete"); ok {
		spec.Handler = handleMemosDelete
	}
}

func checkMemosClient() error {
	if memosClient == nil {
		return fmt.Errorf("Memos is not available. Start the server with `ycode serve` first.")
	}
	return nil
}

func handleMemosStore(ctx context.Context, input json.RawMessage) (string, error) {
	if err := checkMemosClient(); err != nil {
		return "", err
	}

	var params struct {
		Content    string `json:"content"`
		Visibility string `json:"visibility"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse MemosStore input: %w", err)
	}
	if params.Content == "" {
		return "", fmt.Errorf("content is required")
	}

	memo, err := memosClient.CreateMemo(ctx, params.Content, params.Visibility)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Memo saved successfully.\n")
	fmt.Fprintf(&b, "- ID: %s\n", memo.ID())
	fmt.Fprintf(&b, "- Created: %s\n", memo.CreateTime)
	if len(memo.Tags) > 0 {
		fmt.Fprintf(&b, "- Tags: %s\n", strings.Join(memo.Tags, ", "))
	}
	return b.String(), nil
}

func handleMemosSearch(ctx context.Context, input json.RawMessage) (string, error) {
	if err := checkMemosClient(); err != nil {
		return "", err
	}

	var params struct {
		Query      string `json:"query"`
		Tag        string `json:"tag"`
		MaxResults int    `json:"max_results"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse MemosSearch input: %w", err)
	}

	if params.MaxResults <= 0 {
		params.MaxResults = 20
	}

	var results []memos.Memo
	var err error

	if params.Tag != "" {
		results, err = memosClient.SearchMemosByTag(ctx, params.Tag, params.MaxResults)
	} else if params.Query != "" {
		results, err = memosClient.SearchMemos(ctx, params.Query, params.MaxResults)
	} else {
		// No filter — list recent.
		resp, listErr := memosClient.ListMemos(ctx, params.MaxResults, "", "")
		if listErr != nil {
			return "", listErr
		}
		results = resp.Memos
	}
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "No memos found.", nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d memo(s):\n\n", len(results))
	for _, m := range results {
		fmt.Fprintf(&b, "---\n")
		fmt.Fprintf(&b, "**ID**: %s | **Created**: %s", m.ID(), m.CreateTime)
		if len(m.Tags) > 0 {
			fmt.Fprintf(&b, " | **Tags**: %s", strings.Join(m.Tags, ", "))
		}
		fmt.Fprintf(&b, "\n\n%s\n\n", m.Content)
	}
	return b.String(), nil
}

func handleMemosList(ctx context.Context, input json.RawMessage) (string, error) {
	if err := checkMemosClient(); err != nil {
		return "", err
	}

	var params struct {
		PageSize  int    `json:"page_size"`
		PageToken string `json:"page_token"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse MemosList input: %w", err)
	}

	if params.PageSize <= 0 {
		params.PageSize = 20
	}
	if params.PageSize > 100 {
		params.PageSize = 100
	}

	resp, err := memosClient.ListMemos(ctx, params.PageSize, "", params.PageToken)
	if err != nil {
		return "", err
	}

	if len(resp.Memos) == 0 {
		return "No memos found.", nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Showing %d memo(s):\n\n", len(resp.Memos))
	for _, m := range resp.Memos {
		fmt.Fprintf(&b, "---\n")
		fmt.Fprintf(&b, "**ID**: %s | **Created**: %s", m.ID(), m.CreateTime)
		if len(m.Tags) > 0 {
			fmt.Fprintf(&b, " | **Tags**: %s", strings.Join(m.Tags, ", "))
		}
		fmt.Fprintf(&b, "\n\n%s\n\n", m.Content)
	}
	if resp.NextPageToken != "" {
		fmt.Fprintf(&b, "---\nMore results available. Use page_token: %q\n", resp.NextPageToken)
	}
	return b.String(), nil
}

func handleMemosDelete(ctx context.Context, input json.RawMessage) (string, error) {
	if err := checkMemosClient(); err != nil {
		return "", err
	}

	var params struct {
		MemoID string `json:"memo_id"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse MemosDelete input: %w", err)
	}
	if params.MemoID == "" {
		return "", fmt.Errorf("memo_id is required")
	}

	if err := memosClient.DeleteMemo(ctx, params.MemoID); err != nil {
		return "", err
	}

	return fmt.Sprintf("Memo %s deleted.", params.MemoID), nil
}
