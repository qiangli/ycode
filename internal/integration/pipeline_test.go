//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	neturl "net/url"
	"strings"
	"testing"
	"time"
)

func TestPipeline(t *testing.T) {
	requireConnectivity(t)

	// Use a unique service name per run to avoid collisions.
	svc := fmt.Sprintf("pipeline-test-%d", rand.Int64())

	t.Run("TraceToJaeger", func(t *testing.T) {
		host := otelHost(t)
		base := baseURL(t)

		// Send a trace with a unique service name via OTEL HTTP.
		tracePayload := fmt.Sprintf(`{
			"resourceSpans": [{
				"resource": {
					"attributes": [{"key": "service.name", "value": {"stringValue": %q}}]
				},
				"scopeSpans": [{
					"spans": [{
						"traceId": "00000000000000000000000000abcdef",
						"spanId": "00000000000000a1",
						"name": "pipeline-smoke",
						"kind": 1,
						"startTimeUnixNano": "%d",
						"endTimeUnixNano": "%d"
					}]
				}]
			}]
		}`, svc, time.Now().Add(-time.Second).UnixNano(), time.Now().UnixNano())

		url := fmt.Sprintf("http://%s:4318/v1/traces", host)
		status, _ := httpPost(t, url, "application/json", tracePayload)
		if status != http.StatusOK {
			t.Fatalf("send trace returned %d, want 200", status)
		}

		// Poll Jaeger API until the service appears.
		servicesURL := base + "/traces/api/services"
		pollUntil(t, 30*time.Second, time.Second, func() bool {
			resp, err := httpClient().Get(servicesURL)
			if err != nil || resp.StatusCode != http.StatusOK {
				if resp != nil {
					resp.Body.Close()
				}
				return false
			}
			defer resp.Body.Close()
			body, _ := readBody(resp)
			return strings.Contains(body, svc)
		})
	})

	t.Run("MetricToPrometheus", func(t *testing.T) {
		host := otelHost(t)
		base := baseURL(t)

		metricName := "pipeline_test_gauge"
		metricPayload := fmt.Sprintf(`{
			"resourceMetrics": [{
				"resource": {
					"attributes": [{"key": "service.name", "value": {"stringValue": %q}}]
				},
				"scopeMetrics": [{
					"metrics": [{
						"name": %q,
						"gauge": {
							"dataPoints": [{
								"asDouble": 42.0,
								"timeUnixNano": "%d"
							}]
						}
					}]
				}]
			}]
		}`, svc, metricName, time.Now().UnixNano())

		url := fmt.Sprintf("http://%s:4318/v1/metrics", host)
		status, _ := httpPost(t, url, "application/json", metricPayload)
		if status != http.StatusOK {
			t.Fatalf("send metric returned %d, want 200", status)
		}

		// Poll Prometheus until the metric resolves with our unique service name.
		// OTEL maps resource attribute "service.name" to Prometheus "job" label.
		query := fmt.Sprintf(`%s{job="%s"}`, metricName, svc)
		queryURL := fmt.Sprintf("%s/prometheus/api/v1/query?query=%s", base, neturl.QueryEscape(query))
		pollUntil(t, 60*time.Second, 2*time.Second, func() bool {
			resp, err := httpClient().Get(queryURL)
			if err != nil || resp.StatusCode != http.StatusOK {
				if resp != nil {
					resp.Body.Close()
				}
				return false
			}
			defer resp.Body.Close()
			body, _ := readBody(resp)

			var promResp struct {
				Status string `json:"status"`
				Data   struct {
					Result []any `json:"result"`
				} `json:"data"`
			}
			if err := json.Unmarshal([]byte(body), &promResp); err != nil {
				return false
			}
			return promResp.Status == "success" && len(promResp.Data.Result) > 0
		})
	})

	t.Run("LogToVictoriaLogs", func(t *testing.T) {
		host := otelHost(t)
		base := baseURL(t)

		logMsg := fmt.Sprintf("pipeline-test-log-%s", svc)
		logPayload := fmt.Sprintf(`{
			"resourceLogs": [{
				"resource": {
					"attributes": [{"key": "service.name", "value": {"stringValue": %q}}]
				},
				"scopeLogs": [{
					"logRecords": [{
						"timeUnixNano": "%d",
						"body": {"stringValue": %q},
						"severityText": "INFO"
					}]
				}]
			}]
		}`, svc, time.Now().UnixNano(), logMsg)

		url := fmt.Sprintf("http://%s:4318/v1/logs", host)
		status, _ := httpPost(t, url, "application/json", logPayload)
		if status != http.StatusOK {
			t.Fatalf("send log returned %d, want 200", status)
		}

		// Poll VictoriaLogs until the log entry appears.
		queryURL := fmt.Sprintf("%s/logs/select/logsql/query?query=%s&limit=10", base, logMsg)
		pollUntil(t, 30*time.Second, 2*time.Second, func() bool {
			resp, err := httpClient().Get(queryURL)
			if err != nil || resp.StatusCode != http.StatusOK {
				if resp != nil {
					resp.Body.Close()
				}
				return false
			}
			defer resp.Body.Close()
			body, _ := readBody(resp)
			return strings.Contains(body, logMsg)
		})
	})
}
