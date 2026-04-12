package observability

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

// LogStoreComponent provides in-process log storage backed by SQLite.
type LogStoreComponent struct {
	dataDir       string
	retentionDays int

	mu      sync.Mutex
	db      *sql.DB
	healthy atomic.Bool
	cancel  context.CancelFunc
}

// NewLogStoreComponent creates a log storage component.
func NewLogStoreComponent(dataDir string, retentionDays int) *LogStoreComponent {
	if retentionDays <= 0 {
		retentionDays = 3
	}
	return &LogStoreComponent{
		dataDir:       dataDir,
		retentionDays: retentionDays,
	}
}

// Name implements Component.
func (l *LogStoreComponent) Name() string { return "logstore" }

// Start opens the SQLite database and starts the retention cleanup goroutine.
func (l *LogStoreComponent) Start(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	dbDir := filepath.Join(l.dataDir, "logs")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		return fmt.Errorf("logstore: create dir: %w", err)
	}

	dbPath := filepath.Join(dbDir, "logs.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("logstore: open db: %w", err)
	}

	if err := l.initSchema(db); err != nil {
		db.Close()
		return fmt.Errorf("logstore: init schema: %w", err)
	}
	l.db = db

	retCtx, cancel := context.WithCancel(ctx)
	l.cancel = cancel

	go l.retentionLoop(retCtx)

	l.healthy.Store(true)
	slog.Info("logstore: started", "db", dbPath, "retention_days", l.retentionDays)
	return nil
}

// Stop closes the database and stops cleanup.
func (l *LogStoreComponent) Stop(_ context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.healthy.Store(false)
	if l.cancel != nil {
		l.cancel()
	}
	if l.db != nil {
		if err := l.db.Close(); err != nil {
			slog.Warn("logstore: close db", "error", err)
		}
	}
	slog.Info("logstore: stopped")
	return nil
}

// Healthy implements Component.
func (l *LogStoreComponent) Healthy() bool { return l.healthy.Load() }

// HTTPHandler returns the log query HTTP handler.
func (l *LogStoreComponent) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/logs/query", l.handleQuery)
	mux.HandleFunc("/api/v1/logs/insert", l.handleInsert)
	mux.HandleFunc("/", l.handleUI)
	return mux
}

// Insert adds a log record to the store.
func (l *LogStoreComponent) Insert(ctx context.Context, rec LogRecord) error {
	if l.db == nil {
		return fmt.Errorf("logstore not initialized")
	}
	_, err := l.db.ExecContext(ctx,
		`INSERT INTO logs (ts, severity, body, resource, attributes, instance_id) VALUES (?, ?, ?, ?, ?, ?)`,
		rec.Timestamp.UnixMilli(), rec.Severity, rec.Body, rec.Resource, rec.Attributes, rec.InstanceID,
	)
	return err
}

// LogRecord represents a single log entry.
type LogRecord struct {
	Timestamp  time.Time
	Severity   string
	Body       string
	Resource   string
	Attributes string
	InstanceID string
}

func (l *LogStoreComponent) initSchema(db *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS logs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	ts INTEGER NOT NULL,
	severity TEXT NOT NULL DEFAULT 'INFO',
	body TEXT NOT NULL DEFAULT '',
	resource TEXT NOT NULL DEFAULT '',
	attributes TEXT NOT NULL DEFAULT '',
	instance_id TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_logs_ts ON logs(ts);
CREATE INDEX IF NOT EXISTS idx_logs_instance ON logs(instance_id);
CREATE INDEX IF NOT EXISTS idx_logs_severity ON logs(severity);
`
	_, err := db.Exec(schema)
	return err
}

func (l *LogStoreComponent) retentionLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	// Run once immediately.
	l.deleteExpired()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.deleteExpired()
		}
	}
}

func (l *LogStoreComponent) deleteExpired() {
	if l.db == nil {
		return
	}
	cutoff := time.Now().Add(-time.Duration(l.retentionDays) * 24 * time.Hour).UnixMilli()
	result, err := l.db.Exec("DELETE FROM logs WHERE ts < ?", cutoff)
	if err != nil {
		slog.Debug("logstore: retention cleanup", "error", err)
		return
	}
	if n, _ := result.RowsAffected(); n > 0 {
		slog.Info("logstore: retention cleanup", "deleted", n)
	}
}

// handleQuery serves log queries.
func (l *LogStoreComponent) handleQuery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if l.db == nil {
		http.Error(w, `{"error":"logstore not initialized"}`, http.StatusServiceUnavailable)
		return
	}

	// Query params.
	severity := r.FormValue("severity")
	search := r.FormValue("q")
	instanceID := r.FormValue("instance_id")
	limitStr := r.FormValue("limit")
	limit := 100
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}

	// Build query.
	query := "SELECT ts, severity, body, resource, attributes, instance_id FROM logs WHERE 1=1"
	var args []any

	if severity != "" {
		query += " AND severity = ?"
		args = append(args, severity)
	}
	if search != "" {
		query += " AND body LIKE ?"
		args = append(args, "%"+search+"%")
	}
	if instanceID != "" {
		query += " AND instance_id = ?"
		args = append(args, instanceID)
	}

	query += " ORDER BY ts DESC LIMIT ?"
	args = append(args, limit)

	rows, err := l.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type logEntry struct {
		Timestamp  int64  `json:"timestamp"`
		Severity   string `json:"severity"`
		Body       string `json:"body"`
		Resource   string `json:"resource,omitempty"`
		Attributes string `json:"attributes,omitempty"`
		InstanceID string `json:"instance_id,omitempty"`
	}

	var entries []logEntry
	for rows.Next() {
		var e logEntry
		if err := rows.Scan(&e.Timestamp, &e.Severity, &e.Body, &e.Resource, &e.Attributes, &e.InstanceID); err != nil {
			continue
		}
		entries = append(entries, e)
	}

	json.NewEncoder(w).Encode(map[string]any{
		"status": "success",
		"data":   entries,
	})
}

// handleInsert accepts log records via POST.
func (l *LogStoreComponent) handleInsert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var rec LogRecord
	if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusBadRequest)
		return
	}
	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now()
	}

	if err := l.Insert(r.Context(), rec); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"ok"}`)
}

// handleUI serves a simple log viewer page.
func (l *LogStoreComponent) handleUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!DOCTYPE html><html><head><title>ycode Logs</title>
<style>body{font-family:sans-serif;max-width:1200px;margin:20px auto;padding:0 20px}
table{width:100%;border-collapse:collapse}th,td{text-align:left;padding:4px 8px;border-bottom:1px solid #ddd}
th{background:#f5f5f5}input,select{margin:4px;padding:4px}
</style></head><body>
<h2>ycode Log Store</h2>
<form id="f" onsubmit="return query()">
<input name="q" placeholder="Search text...">
<select name="severity"><option value="">All</option><option>INFO</option><option>WARN</option><option>ERROR</option><option>DEBUG</option></select>
<input name="instance_id" placeholder="Instance ID">
<input name="limit" value="100" size="5">
<button>Query</button>
</form>
<table><thead><tr><th>Time</th><th>Severity</th><th>Instance</th><th>Body</th></tr></thead>
<tbody id="rows"></tbody></table>
<script>
function query(){
var f=document.getElementById('f');
var p=new URLSearchParams(new FormData(f));
fetch('/api/v1/logs/query?'+p).then(r=>r.json()).then(d=>{
var h='';
(d.data||[]).forEach(e=>{
h+='<tr><td>'+new Date(e.timestamp).toISOString()+'</td><td>'+e.severity+'</td><td>'+(e.instance_id||'-')+'</td><td>'+e.body+'</td></tr>';
});
document.getElementById('rows').innerHTML=h||'<tr><td colspan=4>No results</td></tr>';
});
return false;
}
</script></body></html>`)
}
