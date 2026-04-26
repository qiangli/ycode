package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"net"

	"github.com/qiangli/ycode/internal/memos"
	"github.com/qiangli/ycode/internal/storage/sqlite"
)

func findFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func TestMemosComponent(t *testing.T) {
	dir := t.TempDir()
	db, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	store := memos.NewSQLStore(db)
	handler := memos.NewWebHandler(store)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	t.Run("index page", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		if !strings.Contains(string(body), "<title>Memos</title>") {
			t.Error("should serve memos index page")
		}
	})

	t.Run("healthz", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/healthz")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d", resp.StatusCode)
		}
	})

	t.Run("CRUD workflow", func(t *testing.T) {
		// Create.
		body := strings.NewReader(`{"content":"Hello #test memo","visibility":"PRIVATE"}`)
		resp, err := http.Post(ts.URL+"/api/memos", "application/json", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 201 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("create: status = %d, body = %s", resp.StatusCode, b)
		}
		var created memos.Memo
		json.NewDecoder(resp.Body).Decode(&created)
		if created.ID == "" {
			t.Fatal("created memo has no ID")
		}
		if created.Content != "Hello #test memo" {
			t.Errorf("content = %q", created.Content)
		}

		// Get.
		resp2, _ := http.Get(ts.URL + "/api/memos/" + created.ID)
		var got memos.Memo
		json.NewDecoder(resp2.Body).Decode(&got)
		resp2.Body.Close()
		if got.Content != "Hello #test memo" {
			t.Errorf("get content = %q", got.Content)
		}

		// Update.
		req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/memos/"+created.ID,
			strings.NewReader(`{"content":"Updated #test memo"}`))
		req.Header.Set("Content-Type", "application/json")
		resp3, _ := http.DefaultClient.Do(req)
		var updated memos.Memo
		json.NewDecoder(resp3.Body).Decode(&updated)
		resp3.Body.Close()
		if updated.Content != "Updated #test memo" {
			t.Errorf("update content = %q", updated.Content)
		}

		// List.
		resp4, _ := http.Get(ts.URL + "/api/memos")
		var listResult memos.ListResult
		json.NewDecoder(resp4.Body).Decode(&listResult)
		resp4.Body.Close()
		if len(listResult.Memos) != 1 {
			t.Errorf("list: got %d memos", len(listResult.Memos))
		}

		// Search by content.
		resp5, _ := http.Get(ts.URL + "/api/memos?search=Updated")
		var searchBody struct {
			Memos []*memos.Memo `json:"Memos"`
		}
		json.NewDecoder(resp5.Body).Decode(&searchBody)
		resp5.Body.Close()
		if len(searchBody.Memos) != 1 {
			t.Errorf("search: got %d results", len(searchBody.Memos))
		}

		// Search by tag.
		resp6, _ := http.Get(ts.URL + "/api/memos?tag=test")
		var tagBody struct {
			Memos []*memos.Memo `json:"Memos"`
		}
		json.NewDecoder(resp6.Body).Decode(&tagBody)
		resp6.Body.Close()
		if len(tagBody.Memos) != 1 {
			t.Errorf("tag search: got %d results", len(tagBody.Memos))
		}

		// Delete.
		req2, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/memos/"+created.ID, nil)
		resp7, _ := http.DefaultClient.Do(req2)
		resp7.Body.Close()
		if resp7.StatusCode != 200 {
			t.Fatalf("delete: status = %d", resp7.StatusCode)
		}

		// Verify deleted.
		resp8, _ := http.Get(ts.URL + "/api/memos/" + created.ID)
		resp8.Body.Close()
		if resp8.StatusCode != 404 {
			t.Errorf("after delete: status = %d, want 404", resp8.StatusCode)
		}
	})
}

func TestMemosProxyIntegration(t *testing.T) {
	dir := t.TempDir()
	db, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	store := memos.NewSQLStore(db)
	handler := memos.NewWebHandler(store)

	port := findFreePort(t)
	proxy := NewProxyServer("127.0.0.1", port)
	proxy.AddHandler("/memos/", handler)

	ctx := context.Background()
	if err := proxy.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer proxy.Stop(ctx)
	time.Sleep(50 * time.Millisecond)

	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	t.Run("HTML served from internal handler", func(t *testing.T) {
		resp, err := http.Get(base + "/memos/")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "<title>Memos</title>") {
			t.Errorf("should serve memos page")
		}
	})

	t.Run("API accessible through proxy", func(t *testing.T) {
		resp, err := http.Get(base + "/memos/api/memos")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d", resp.StatusCode)
		}
	})
}
