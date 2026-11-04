package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type scriptedCloudWatch struct {
	mu     sync.Mutex
	status int
	body   string
}

func (s *scriptedCloudWatch) set(status int, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.body = status, body
}

func (s *scriptedCloudWatch) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	status, body := s.status, s.body
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func TestFailedPollsKeepLastResultOnlyUntilMaxStaleness(t *testing.T) {
	script := &scriptedCloudWatch{status: http.StatusOK, body: cpuVectorResponse}
	server := httptest.NewServer(script)
	t.Cleanup(server.Close)
	cfg, err := parseConfig([]byte(cpuQueryConfig)) // interval 60s -> default max_staleness 3m
	if err != nil {
		t.Fatal(err)
	}
	now := fixedNow
	e := newExporter(cfg, &promQLClient{
		endpoint: server.URL, region: "ap-northeast-2", credentials: staticCredentials(),
		httpClient: server.Client(), now: func() time.Time { return now },
	})
	sampleLine := `aws_omics_task_cpu_usage{run_id="1234567",task_id="7654321"} 1.5`
	lastSuccess := `cloudwatch_promql_query_last_success_timestamp_seconds{query="aws_omics_task_cpu_usage"} 1.7937612e+09`

	e.pollOnce(context.Background())
	requireLines(t, scrape(t, e), sampleLine)

	script.set(http.StatusServiceUnavailable, `{"status":"error","errorType":"unavailable","error":"try later"}`)
	now = fixedNow.Add(2 * time.Minute)
	e.pollOnce(context.Background())
	requireLines(t, scrape(t, e), sampleLine, lastSuccess, `cloudwatch_promql_query_up{query="aws_omics_task_cpu_usage"} 0`)

	now = fixedNow.Add(3*time.Minute + time.Second)
	e.pollOnce(context.Background())
	body := scrape(t, e)
	if containsSample(body, "aws_omics_task_cpu_usage") {
		t.Fatalf("stale series still exported after max_staleness:\n%s", body)
	}
	requireLines(t, body, lastSuccess, `cloudwatch_promql_query_series{query="aws_omics_task_cpu_usage"} 0`)
}
