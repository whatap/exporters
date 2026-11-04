package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type series struct {
	labels string // rendered {a="b",...}
	value  float64
}

type queryState struct {
	up          bool
	series      []series
	lastSuccess time.Time
}

type exporter struct {
	cfg    config
	client *promQLClient
	mu     sync.Mutex
	states map[string]*queryState
	polls  int
}

func newExporter(cfg config, client *promQLClient) *exporter {
	states := make(map[string]*queryState, len(cfg.Queries))
	for _, q := range cfg.Queries {
		states[q.Name] = &queryState{}
	}
	return &exporter{cfg: cfg, client: client, states: states}
}

// run polls immediately and then every configured interval until ctx is cancelled.
// Scrapes only read the cache, so the CloudWatch cost follows the interval, not the scrape rate.
func (e *exporter) run(ctx context.Context) {
	ticker := time.NewTicker(e.cfg.Interval)
	defer ticker.Stop()
	for {
		e.pollOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (e *exporter) pollCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.polls
}

// pollOnce runs every configured query once and replaces the cached results.
func (e *exporter) pollOnce(ctx context.Context) {
	defer func() {
		e.mu.Lock()
		e.polls++
		e.mu.Unlock()
	}()
	for _, q := range e.cfg.Queries {
		queryContext, cancel := context.WithTimeout(ctx, e.cfg.Timeout)
		result, err := e.client.instantQuery(queryContext, q.Query)
		cancel()
		var converted []series
		if err == nil {
			converted, err = convertSamples(q, result)
		}
		e.mu.Lock()
		state := e.states[q.Name]
		if err != nil {
			state.up = false
			if e.client.now().Sub(state.lastSuccess) > e.cfg.MaxStaleness {
				state.series = nil
			}
		} else {
			state.up, state.series, state.lastSuccess = true, converted, e.client.now()
		}
		e.mu.Unlock()
		if err != nil {
			log.Printf("CLOUDWATCH_PROMQL_EXPORTER: query %s failed: %v", q.Name, err)
		}
	}
}

// convertSamples maps CloudWatch labels to exported labels and parses float values.
// Any sample that cannot be represented exactly fails the whole query: partial results
// would silently under-report.
func convertSamples(q queryConfig, result []sample) ([]series, error) {
	converted := make([]series, 0, len(result))
	seen := make(map[string]bool, len(result))
	for _, s := range result {
		if len(s.Value) != 2 {
			return nil, errors.New("result contains non-float samples (histograms need histogram_avg/histogram_quantile)")
		}
		var text string
		if err := json.Unmarshal(s.Value[1], &text); err != nil {
			return nil, fmt.Errorf("sample value is not a string: %w", err)
		}
		value, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, fmt.Errorf("sample value %q is not a float", truncate(text, 40))
		}
		var names []string
		for from := range q.Labels {
			if _, ok := s.Metric[from]; ok {
				names = append(names, from)
			}
		}
		sort.Slice(names, func(i, j int) bool { return q.Labels[names[i]] < q.Labels[names[j]] })
		var parts []string
		for _, from := range names {
			parts = append(parts, q.Labels[from]+`="`+escapeLabelValue(s.Metric[from])+`"`)
		}
		labels := "{" + strings.Join(parts, ",") + "}"
		if seen[labels] {
			return nil, fmt.Errorf("series collide after label mapping on %s; export the label that distinguishes them", truncate(labels, 200))
		}
		seen[labels] = true
		converted = append(converted, series{labels: labels, value: value})
	}
	return converted, nil
}

var labelValueEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

// escapeLabelValue applies the only escapes the Prometheus text format defines.
func escapeLabelValue(value string) string {
	return labelValueEscaper.Replace(value)
}

func (e *exporter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/metrics" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "CLOUDWATCH_PROMQL_EXPORTER: GET required", http.StatusMethodNotAllowed)
		return
	}
	// Render under the lock but write after releasing it, so a slow scraper never delays polling.
	var body bytes.Buffer
	e.render(&body)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write(body.Bytes())
}

func (e *exporter) render(w io.Writer) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, q := range e.cfg.Queries {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", q.Name, strings.ReplaceAll(q.Help, `\`, `\\`), q.Name, q.Type)
		for _, s := range e.states[q.Name].series {
			fmt.Fprintf(w, "%s%s %s\n", q.Name, s.labels, strconv.FormatFloat(s.value, 'g', -1, 64))
		}
	}
	fmt.Fprint(w, "# HELP cloudwatch_promql_query_up Whether the last attempt of the query succeeded.\n# TYPE cloudwatch_promql_query_up gauge\n")
	for _, q := range e.cfg.Queries {
		up := 0
		if e.states[q.Name].up {
			up = 1
		}
		fmt.Fprintf(w, "cloudwatch_promql_query_up{query=\"%s\"} %d\n", q.Name, up)
	}
	fmt.Fprint(w, "# HELP cloudwatch_promql_query_series Number of series currently exported for the query.\n# TYPE cloudwatch_promql_query_series gauge\n")
	for _, q := range e.cfg.Queries {
		fmt.Fprintf(w, "cloudwatch_promql_query_series{query=\"%s\"} %d\n", q.Name, len(e.states[q.Name].series))
	}
	fmt.Fprint(w, "# HELP cloudwatch_promql_query_last_success_timestamp_seconds Unix time of the last successful query (0 if none).\n# TYPE cloudwatch_promql_query_last_success_timestamp_seconds gauge\n")
	for _, q := range e.cfg.Queries {
		ts := 0.0
		if last := e.states[q.Name].lastSuccess; !last.IsZero() {
			ts = float64(last.Unix())
		}
		fmt.Fprintf(w, "cloudwatch_promql_query_last_success_timestamp_seconds{query=\"%s\"} %s\n", q.Name, strconv.FormatFloat(ts, 'g', -1, 64))
	}
}
