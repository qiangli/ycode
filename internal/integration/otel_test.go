//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestOTEL(t *testing.T) {
	requireConnectivity(t)
	host := otelHost(t)

	t.Run("CollectorTracesEndpoint", func(t *testing.T) {
		url := fmt.Sprintf("http://%s:4318/v1/traces", host)
		status, _ := httpPost(t, url, "application/json", `{"resourceSpans":[]}`)
		if status != http.StatusOK {
			t.Errorf("OTEL traces endpoint returned %d, want 200", status)
		}
	})

	t.Run("PrometheusMetrics", func(t *testing.T) {
		url := fmt.Sprintf("http://%s:8889/metrics", host)
		status, body := httpGet(t, url)
		if status != http.StatusOK {
			t.Fatalf("Prometheus metrics returned %d, want 200", status)
		}
		if !strings.HasPrefix(body, "#") {
			t.Errorf("Prometheus metrics response should start with '#', got %q", body[:min(len(body), 50)])
		}
	})

	t.Run("SendTestTrace", func(t *testing.T) {
		url := fmt.Sprintf("http://%s:4318/v1/traces", host)
		payload := `{
			"resourceSpans": [{
				"resource": {
					"attributes": [{"key": "service.name", "value": {"stringValue": "integration-test"}}]
				},
				"scopeSpans": [{
					"spans": [{
						"traceId": "00000000000000000000000000000001",
						"spanId": "0000000000000001",
						"name": "integration-smoke",
						"kind": 1,
						"startTimeUnixNano": "1000000000",
						"endTimeUnixNano": "2000000000"
					}]
				}]
			}]
		}`
		status, _ := httpPost(t, url, "application/json", payload)
		if status != http.StatusOK {
			t.Errorf("send test trace returned %d, want 200", status)
		}
	})

	t.Run("SendTestMetrics", func(t *testing.T) {
		url := fmt.Sprintf("http://%s:4318/v1/metrics", host)
		payload := `{
			"resourceMetrics": [{
				"resource": {
					"attributes": [{"key": "service.name", "value": {"stringValue": "integration-test"}}]
				},
				"scopeMetrics": [{
					"metrics": [{
						"name": "integration_test_counter",
						"sum": {
							"dataPoints": [{
								"asInt": "1",
								"startTimeUnixNano": "1000000000",
								"timeUnixNano": "1000000000"
							}],
							"isMonotonic": true,
							"aggregationTemporality": 2
						}
					}]
				}]
			}]
		}`
		status, _ := httpPost(t, url, "application/json", payload)
		if status != http.StatusOK {
			t.Errorf("send test metrics returned %d, want 200", status)
		}
	})

	t.Run("SendTestLog", func(t *testing.T) {
		url := fmt.Sprintf("http://%s:4318/v1/logs", host)
		payload := `{
			"resourceLogs": [{
				"resource": {
					"attributes": [{"key": "service.name", "value": {"stringValue": "integration-test"}}]
				},
				"scopeLogs": [{
					"logRecords": [{
						"timeUnixNano": "1000000000",
						"body": {"stringValue": "integration smoke test"},
						"severityText": "INFO"
					}]
				}]
			}]
		}`
		status, _ := httpPost(t, url, "application/json", payload)
		if status != http.StatusOK {
			t.Errorf("send test log returned %d, want 200", status)
		}
	})
}
