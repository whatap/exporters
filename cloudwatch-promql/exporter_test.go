package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

var fixedNow = time.Date(2026, 11, 4, 3, 0, 0, 0, time.UTC)

const cpuQueryConfig = `
interval: 60s
queries:
  - name: aws_omics_task_cpu_usage
    help: vCPUs in use per HealthOmics task.
    query: '{"aws.omics.task.cpu.usage"}'
    labels:
      "@resource.aws.omics.run.id": run_id
      "@resource.aws.omics.task.id": task_id
`

const cpuVectorResponse = `{"status":"success","data":{"resultType":"vector","result":[
 {"metric":{"__name__":"aws.omics.task.cpu.usage","@resource.aws.omics.run.id":"1234567","@resource.aws.omics.task.id":"7654321","@aws.region":"ap-northeast-2"},"value":[1793761200.000,"1.5"]}
]}}`

type recordedRequest struct {
	method, path, authorization, amzDate, contentType string
	form                                              url.Values
}

type requestLog struct {
	mu       sync.Mutex
	requests []recordedRequest
}

func (l *requestLog) all() []recordedRequest {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]recordedRequest(nil), l.requests...)
}

// fakeCloudWatch serves canned PromQL API responses and records each request.
func fakeCloudWatch(t *testing.T, status int, body string) (*httptest.Server, *requestLog) {
	t.Helper()
	log := &requestLog{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(raw))
		log.mu.Lock()
		log.requests = append(log.requests, recordedRequest{
			method: r.Method, path: r.URL.Path, authorization: r.Header.Get("Authorization"),
			amzDate: r.Header.Get("X-Amz-Date"), contentType: r.Header.Get("Content-Type"), form: form,
		})
		log.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server, log
}

func staticCredentials() aws.CredentialsProvider {
	return aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
		return aws.Credentials{AccessKeyID: "AKIDEXAMPLE", SecretAccessKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"}, nil
	})
}

func newTestExporter(t *testing.T, configYAML, endpoint string) *exporter {
	t.Helper()
	cfg, err := parseConfig([]byte(configYAML))
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	return newExporter(cfg, &promQLClient{
		endpoint: endpoint, region: "ap-northeast-2", credentials: staticCredentials(),
		httpClient: http.DefaultClient, now: func() time.Time { return fixedNow },
	})
}

func scrape(t *testing.T, e *exporter) string {
	t.Helper()
	response := httptest.NewRecorder()
	e.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("scrape status=%d body=%q", response.Code, response.Body.String())
	}
	body := response.Body.String()
	// External oracle: the Prometheus text parser must accept the whole exposition.
	parser := expfmt.NewTextParser(model.LegacyValidation)
	if _, err := parser.TextToMetricFamilies(bytes.NewBufferString(body)); err != nil {
		t.Fatalf("invalid exposition: %v\n%s", err, body)
	}
	return body
}

func requireLines(t *testing.T, body string, lines ...string) {
	t.Helper()
	for _, line := range lines {
		if !strings.Contains("\n"+body, "\n"+line+"\n") {
			t.Errorf("missing line %q in:\n%s", line, body)
		}
	}
}

// containsSample reports whether the exposition has any sample line for metric.
func containsSample(body, metric string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, metric+"{") || strings.HasPrefix(line, metric+" ") {
			return true
		}
	}
	return false
}

func TestPollExposesSignedInstantQueryResult(t *testing.T) {
	server, requests := fakeCloudWatch(t, http.StatusOK, cpuVectorResponse)
	e := newTestExporter(t, cpuQueryConfig, server.URL)

	e.pollOnce(context.Background())

	all := requests.all()
	if len(all) != 1 {
		t.Fatalf("requests=%d, want 1", len(all))
	}
	got := all[0]
	if got.method != http.MethodPost || got.path != "/api/v1/query" {
		t.Errorf("request %s %s, want POST /api/v1/query", got.method, got.path)
	}
	if got.form.Get("query") != `{"aws.omics.task.cpu.usage"}` || got.form.Get("time") != "1793761200.000" {
		t.Errorf("form=%v", got.form)
	}
	if got.contentType != "application/x-www-form-urlencoded" {
		t.Errorf("content-type=%q", got.contentType)
	}
	wantScope := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20261104/ap-northeast-2/monitoring/aws4_request"
	if !strings.HasPrefix(got.authorization, wantScope) || got.amzDate != "20261104T030000Z" {
		t.Errorf("authorization=%q x-amz-date=%q, want SigV4 scope %q", got.authorization, got.amzDate, wantScope)
	}

	body := scrape(t, e)
	requireLines(t, body,
		"# HELP aws_omics_task_cpu_usage vCPUs in use per HealthOmics task.",
		"# TYPE aws_omics_task_cpu_usage gauge",
		`aws_omics_task_cpu_usage{run_id="1234567",task_id="7654321"} 1.5`,
		`cloudwatch_promql_query_up{query="aws_omics_task_cpu_usage"} 1`,
		`cloudwatch_promql_query_series{query="aws_omics_task_cpu_usage"} 1`,
		`cloudwatch_promql_query_last_success_timestamp_seconds{query="aws_omics_task_cpu_usage"} 1.7937612e+09`,
	)
}
