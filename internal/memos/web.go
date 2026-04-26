package memos

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// NewWebHandler returns an HTTP handler for the memos web UI and API.
func NewWebHandler(store Store) http.Handler {
	h := &webHandler{store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/memos", h.handleMemos)
	mux.HandleFunc("/api/memos/", h.handleMemo)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})
	mux.HandleFunc("/", h.handleIndex)
	return mux
}

type webHandler struct {
	store Store
}

func (h *webHandler) handleMemos(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listMemos(w, r)
	case http.MethodPost:
		h.createMemo(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *webHandler) handleMemo(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/memos/")
	if id == "" {
		http.Error(w, "memo id required", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.getMemo(w, r, id)
	case http.MethodPatch:
		h.updateMemo(w, r, id)
	case http.MethodDelete:
		h.deleteMemo(w, r, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *webHandler) listMemos(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	search := q.Get("search")
	tag := q.Get("tag")
	pageToken := q.Get("page_token")

	var result *ListResult
	var err error

	if search != "" {
		memos, searchErr := h.store.Search(r.Context(), search, 50)
		if searchErr != nil {
			writeJSONError(w, searchErr, http.StatusInternalServerError)
			return
		}
		result = &ListResult{Memos: memos}
	} else if tag != "" {
		memos, tagErr := h.store.SearchByTag(r.Context(), tag, 50)
		if tagErr != nil {
			writeJSONError(w, tagErr, http.StatusInternalServerError)
			return
		}
		result = &ListResult{Memos: memos}
	} else {
		result, err = h.store.List(r.Context(), ListOptions{PageSize: 50, PageToken: pageToken})
		if err != nil {
			writeJSONError(w, err, http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, result)
}

func (h *webHandler) createMemo(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Content    string `json:"content"`
		Visibility string `json:"visibility"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSONError(w, err, http.StatusBadRequest)
		return
	}
	memo := &Memo{Content: input.Content, Visibility: input.Visibility}
	if err := h.store.Create(r.Context(), memo); err != nil {
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, memo)
}

func (h *webHandler) getMemo(w http.ResponseWriter, r *http.Request, id string) {
	memo, err := h.store.Get(r.Context(), id)
	if err != nil {
		writeJSONError(w, err, http.StatusNotFound)
		return
	}
	writeJSON(w, memo)
}

func (h *webHandler) updateMemo(w http.ResponseWriter, r *http.Request, id string) {
	var input struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSONError(w, err, http.StatusBadRequest)
		return
	}
	memo, err := h.store.Update(r.Context(), id, input.Content)
	if err != nil {
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, memo)
}

func (h *webHandler) deleteMemo(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.store.Delete(r.Context(), id); err != nil {
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "deleted"})
}

func (h *webHandler) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, memosIndexHTML)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, err error, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

const memosIndexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Memos</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:#1a1a2e;color:#e0e0e0;max-width:800px;margin:0 auto;padding:20px}
h1{font-size:1.4em;font-weight:500;margin-bottom:20px;color:rgba(255,255,255,0.85)}
.controls{display:flex;gap:8px;margin-bottom:20px;flex-wrap:wrap}
.controls input,.controls select{background:#16213e;border:1px solid #333;color:#e0e0e0;padding:8px 12px;border-radius:6px;font-size:14px}
.controls input{flex:1;min-width:200px}
.controls button{background:#3478f6;color:#fff;border:none;padding:8px 16px;border-radius:6px;cursor:pointer;font-size:14px}
.controls button:hover{background:#2860d0}
.memo{background:#16213e;border-radius:8px;padding:16px;margin-bottom:12px;border:1px solid #333}
.memo-meta{font-size:12px;color:#888;margin-bottom:8px;display:flex;justify-content:space-between;align-items:center}
.memo-content{white-space:pre-wrap;line-height:1.5}
.memo-tags{margin-top:8px}
.tag{display:inline-block;background:#3478f6;color:#fff;padding:2px 8px;border-radius:10px;font-size:11px;margin-right:4px;cursor:pointer}
.memo-actions button{background:transparent;border:none;color:#888;cursor:pointer;font-size:12px;padding:2px 6px}
.memo-actions button:hover{color:#ff3b30}
.empty{text-align:center;color:#666;padding:40px}
#editor{display:none;margin-bottom:20px}
#editor textarea{width:100%;min-height:120px;background:#16213e;border:1px solid #333;color:#e0e0e0;padding:12px;border-radius:6px;font-family:inherit;font-size:14px;resize:vertical}
#editor .editor-actions{display:flex;gap:8px;margin-top:8px;justify-content:flex-end}
.next-page{text-align:center;margin-top:16px}
.next-page button{background:transparent;color:#3478f6;border:1px solid #3478f6;padding:8px 20px;border-radius:6px;cursor:pointer}
</style>
</head>
<body>
<h1>Memos</h1>
<div class="controls">
<input type="text" id="search" placeholder="Search memos...">
<button onclick="toggleEditor()">+ New</button>
</div>
<div id="editor">
<textarea id="content" placeholder="Write your memo... Use #tags for tagging."></textarea>
<div class="editor-actions">
<button onclick="cancelEdit()" style="background:#555">Cancel</button>
<button onclick="saveMemo()">Save</button>
</div>
</div>
<div id="memos"></div>
<div id="next-page" class="next-page" style="display:none">
<button onclick="loadMore()">Load more</button>
</div>
<script>
const base = window.location.pathname.replace(/\/+$/, '');
let nextToken = '';
let editingId = null;

async function loadMemos(append) {
  const search = document.getElementById('search').value;
  let url = base + '/api/memos?';
  if (search) url += 'search=' + encodeURIComponent(search);
  else if (nextToken && append) url += 'page_token=' + encodeURIComponent(nextToken);

  const r = await fetch(url);
  const data = await r.json();
  const memos = data.Memos || data.memos || [];
  nextToken = data.NextPageToken || data.nextPageToken || '';

  const container = document.getElementById('memos');
  if (!append) container.innerHTML = '';

  if (memos.length === 0 && !append) {
    container.innerHTML = '<div class="empty">No memos yet. Click + New to create one.</div>';
  }
  for (const m of memos) {
    container.innerHTML += renderMemo(m);
  }
  document.getElementById('next-page').style.display = nextToken ? 'block' : 'none';
}

function renderMemo(m) {
  const date = m.createdAt ? new Date(m.createdAt).toLocaleString() : m.CreatedAt || '';
  const tags = (m.tags || m.Tags || []).map(t =>
    '<span class="tag" onclick="filterTag(\'' + t + '\')">#' + t + '</span>'
  ).join('');
  const id = m.id || m.ID;
  return '<div class="memo" id="memo-' + id + '">' +
    '<div class="memo-meta"><span>' + date + '</span>' +
    '<span class="memo-actions">' +
    '<button onclick="editMemo(\'' + id + '\')">edit</button>' +
    '<button onclick="deleteMemo(\'' + id + '\')">delete</button></span></div>' +
    '<div class="memo-content">' + escapeHtml(m.content || m.Content) + '</div>' +
    (tags ? '<div class="memo-tags">' + tags + '</div>' : '') +
    '</div>';
}

function escapeHtml(s) {
  const d = document.createElement('div');
  d.textContent = s;
  return d.innerHTML;
}

function filterTag(tag) {
  document.getElementById('search').value = '';
  fetch(base + '/api/memos?tag=' + encodeURIComponent(tag))
    .then(r => r.json())
    .then(data => {
      const memos = data.Memos || data.memos || [];
      const container = document.getElementById('memos');
      container.innerHTML = '';
      for (const m of memos) container.innerHTML += renderMemo(m);
      document.getElementById('next-page').style.display = 'none';
    });
}

function toggleEditor() {
  const ed = document.getElementById('editor');
  ed.style.display = ed.style.display === 'none' ? 'block' : 'none';
  if (ed.style.display === 'block') {
    document.getElementById('content').focus();
  }
}

function cancelEdit() {
  document.getElementById('editor').style.display = 'none';
  document.getElementById('content').value = '';
  editingId = null;
}

async function saveMemo() {
  const content = document.getElementById('content').value.trim();
  if (!content) return;
  const url = editingId ? base + '/api/memos/' + editingId : base + '/api/memos';
  const method = editingId ? 'PATCH' : 'POST';
  await fetch(url, {
    method, headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({content})
  });
  cancelEdit();
  loadMemos(false);
}

async function editMemo(id) {
  const r = await fetch(base + '/api/memos/' + id);
  const m = await r.json();
  document.getElementById('content').value = m.content || m.Content;
  editingId = id;
  document.getElementById('editor').style.display = 'block';
  document.getElementById('content').focus();
}

async function deleteMemo(id) {
  if (!confirm('Delete this memo?')) return;
  await fetch(base + '/api/memos/' + id, {method: 'DELETE'});
  const el = document.getElementById('memo-' + id);
  if (el) el.remove();
}

function loadMore() { loadMemos(true); }

let searchTimeout;
document.getElementById('search').addEventListener('input', function() {
  clearTimeout(searchTimeout);
  searchTimeout = setTimeout(() => loadMemos(false), 300);
});

loadMemos(false);
</script>
</body>
</html>`
