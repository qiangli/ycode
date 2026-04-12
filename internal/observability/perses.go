package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/qiangli/ycode/internal/observability/dashboards"
)

// PersesComponent serves Perses-compatible dashboard configurations
// with a built-in web UI for visualizing ycode metrics.
type PersesComponent struct {
	prometheusPath string // path prefix where Prometheus API is mounted (e.g. "/prometheus")
	project        *DashboardProject
	healthy        atomic.Bool
}

// DashboardProject is the in-memory representation of dashboard configs.
type DashboardProject struct {
	Name       string      `json:"name"`
	Dashboards []Dashboard `json:"dashboards"`
}

// Dashboard holds a named set of panels.
type Dashboard struct {
	Name   string  `json:"name"`
	Panels []Panel `json:"panels"`
}

// Panel is a single visualization panel.
type Panel struct {
	Title string `json:"title"`
	Query string `json:"query"`
	Type  string `json:"type"` // "timeseries", "stat", "table"
}

// NewPersesComponent creates a dashboard component.
func NewPersesComponent(prometheusPath string) *PersesComponent {
	return &PersesComponent{
		prometheusPath: prometheusPath,
	}
}

// Name implements Component.
func (p *PersesComponent) Name() string { return "perses" }

// Start loads the embedded dashboard configs.
func (p *PersesComponent) Start(_ context.Context) error {
	var project DashboardProject
	if err := json.Unmarshal(dashboards.DefaultProjectJSON, &project); err != nil {
		return fmt.Errorf("perses: parse default project: %w", err)
	}
	p.project = &project
	p.healthy.Store(true)
	slog.Info("perses: started", "project", project.Name, "dashboards", len(project.Dashboards))
	return nil
}

// Stop implements Component.
func (p *PersesComponent) Stop(_ context.Context) error {
	p.healthy.Store(false)
	slog.Info("perses: stopped")
	return nil
}

// Healthy implements Component.
func (p *PersesComponent) Healthy() bool { return p.healthy.Load() }

// HTTPHandler serves the dashboard UI and API.
func (p *PersesComponent) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/projects", p.handleProjects)
	mux.HandleFunc("/api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("/", p.handleUI)
	return mux
}

// handleProjects returns the dashboard project configuration.
func (p *PersesComponent) handleProjects(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(p.project)
}

// handleUI serves the dashboard web UI.
func (p *PersesComponent) handleUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Determine which dashboard to show.
	selectedDash := r.FormValue("d")

	fmt.Fprint(w, `<!DOCTYPE html><html><head><title>ycode Dashboards</title>
<style>
body{font-family:sans-serif;max-width:1400px;margin:0 auto;padding:20px;background:#fafafa}
h1{margin-bottom:4px}
nav{margin:12px 0;display:flex;gap:8px;flex-wrap:wrap}
nav a{padding:6px 12px;background:#e0e0e0;border-radius:4px;text-decoration:none;color:#333}
nav a.active{background:#1976d2;color:#fff}
.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(400px,1fr));gap:16px;margin-top:16px}
.panel{background:#fff;border:1px solid #ddd;border-radius:8px;padding:16px}
.panel h3{margin:0 0 8px;font-size:14px;color:#555}
.panel .value{font-size:28px;font-weight:bold;color:#1976d2}
.panel pre{background:#f5f5f5;padding:8px;border-radius:4px;font-size:12px;overflow-x:auto}
.panel .result{margin-top:8px;max-height:300px;overflow-y:auto}
table{width:100%;border-collapse:collapse;font-size:13px}
th,td{text-align:left;padding:4px 8px;border-bottom:1px solid #eee}
th{background:#f5f5f5}
</style>
<script>
const PROM_API = '`)
	fmt.Fprintf(w, "%s", p.prometheusPath)
	fmt.Fprint(w, `/api/v1/query';
async function queryProm(query) {
  try {
    const r = await fetch(PROM_API + '?query=' + encodeURIComponent(query));
    const d = await r.json();
    if (d.status === 'success') return d.data;
    return null;
  } catch(e) { return null; }
}
async function loadPanels() {
  document.querySelectorAll('.panel[data-query]').forEach(async (el) => {
    const q = el.dataset.query;
    const type = el.dataset.type;
    const resultEl = el.querySelector('.result');
    const data = await queryProm(q);
    if (!data || !data.result || data.result.length === 0) {
      resultEl.innerHTML = '<em>No data</em>';
      return;
    }
    if (type === 'stat') {
      const v = data.result[0].value ? data.result[0].value[1] : '0';
      resultEl.innerHTML = '<div class="value">' + parseFloat(v).toFixed(2) + '</div>';
    } else if (type === 'table') {
      let h = '<table><thead><tr><th>Labels</th><th>Value</th></tr></thead><tbody>';
      data.result.forEach(r => {
        const lbl = Object.entries(r.metric).filter(([k])=>!k.startsWith('__')).map(([k,v])=>k+'='+v).join(', ');
        h += '<tr><td>' + lbl + '</td><td>' + (r.value?r.value[1]:'') + '</td></tr>';
      });
      h += '</tbody></table>';
      resultEl.innerHTML = h;
    } else {
      let h = '<table><thead><tr><th>Series</th><th>Value</th></tr></thead><tbody>';
      data.result.forEach(r => {
        const lbl = Object.entries(r.metric).filter(([k])=>!k.startsWith('__')).map(([k,v])=>k+'='+v).join(', ') || 'value';
        h += '<tr><td>' + lbl + '</td><td>' + (r.value?r.value[1]:'') + '</td></tr>';
      });
      h += '</tbody></table>';
      resultEl.innerHTML = h;
    }
  });
}
window.onload = () => { loadPanels(); setInterval(loadPanels, 15000); };
</script></head><body>
<h1>ycode Dashboards</h1>
<nav>`)

	if p.project != nil {
		for _, dash := range p.project.Dashboards {
			active := ""
			if selectedDash == dash.Name || (selectedDash == "" && dash.Name == p.project.Dashboards[0].Name) {
				active = " class=\"active\""
			}
			fmt.Fprintf(w, `<a href="?d=%s"%s>%s</a>`, dash.Name, active, dash.Name)
		}
	}

	fmt.Fprint(w, `</nav><div class="grid">`)

	// Find selected dashboard.
	var activeDash *Dashboard
	if p.project != nil {
		for i := range p.project.Dashboards {
			if p.project.Dashboards[i].Name == selectedDash || (selectedDash == "" && i == 0) {
				activeDash = &p.project.Dashboards[i]
				break
			}
		}
	}

	if activeDash != nil {
		for _, panel := range activeDash.Panels {
			fmt.Fprintf(w, `<div class="panel" data-query="%s" data-type="%s">
<h3>%s</h3>
<pre>%s</pre>
<div class="result">Loading...</div>
</div>`, panel.Query, panel.Type, panel.Title, panel.Query)
		}
	}

	fmt.Fprint(w, `</div></body></html>`)
}
