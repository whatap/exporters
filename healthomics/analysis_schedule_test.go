package main

import (
	"context"
	"errors"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/omics/types"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type fakeAnalyzer struct {
	mu     sync.Mutex
	result *analysis
	err    error
	calls  []string
}

func (f *fakeAnalyzer) Analyze(_ context.Context, runID string) (*analysis, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, runID)
	return f.result, f.err
}

func (f *fakeAnalyzer) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func fixtureAnalysis(t *testing.T) *analysis {
	t.Helper()
	f, err := os.Open("testdata/run-analyzer-0.14.0-run-5930278.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	a, err := parseRunAnalyzerCSV(f)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func newClockedExporter(api omicsAPI, a runAnalyzer) (*exporter, *fakeClock) {
	clock := &fakeClock{t: testNow}
	cfg := defaultConfig()
	cfg.region = "ap-northeast-2"
	return newExporter(cfg, api, a, clock.Now), clock
}

// finish marks the fake run as completed `ago` before the clock's current time.
func finish(api *fakeOmics, clock *fakeClock, ago time.Duration) {
	api.mu.Lock()
	defer api.mu.Unlock()
	stop := clock.Now().Add(-ago)
	api.runs[0].Status, api.runs[0].StopTime = types.RunStatusCompleted, &stop
}

// requireValue checks the value of the sample whose line starts with series.
func requireValue(t *testing.T, body, series string, want float64) {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, series+" ") {
			got, err := strconv.ParseFloat(strings.TrimPrefix(line, series+" "), 64)
			if err != nil || math.Abs(got-want) > 1e-9 {
				t.Errorf("%s = %q, want %v", series, line, want)
			}
			return
		}
	}
	t.Errorf("missing series %s in:\n%s", series, body)
}

func TestFinishedRunIsAnalyzedOnceAndCountedForItsWorkflow(t *testing.T) {
	api, analyzer := newFake(), &fakeAnalyzer{result: fixtureAnalysis(t)}
	e, clock := newClockedExporter(api, analyzer)
	ctx := context.Background()

	e.pollOnce(ctx)
	requireLines(t, scrape(t, e), `aws_omics_workflow_analyzed_runs_total{workflow_id="4543093"} 0`)
	finish(api, clock, 5*time.Minute)
	e.pollOnce(ctx)
	e.analyzePending(ctx)
	e.analyzePending(ctx)
	// Even after the retry delay a successful run must not be analyzed (and counted) again.
	clock.advance(e.cfg.analyzerRetry)
	e.pollOnce(ctx)
	e.analyzePending(ctx)

	if analyzer.callCount() != 1 {
		t.Fatalf("analyzer calls=%d, want 1", analyzer.callCount())
	}
	body := scrape(t, e)
	requireValue(t, body, `aws_omics_run_estimated_cost_usd{run_id="1111111",workflow_id="4543093"}`, 0.049191)
	requireValue(t, body, `aws_omics_run_minimum_cost_usd{run_id="1111111",workflow_id="4543093"}`, 0.027929)
	requireValue(t, body, `aws_omics_run_cpu_utilization_ratio{run_id="1111111",workflow_id="4543093"}`, 0.187169)
	requireValue(t, body, `aws_omics_task_estimated_cost_usd{run_id="1111111",task_id="4841160",task_name="idle_overprovisioned",workflow_id="4543093"}`, 0.028357)
	requireValue(t, body, `aws_omics_task_minimum_cost_usd{run_id="1111111",task_id="4841160",task_name="idle_overprovisioned",workflow_id="4543093"}`, 0.007095)
	requireValue(t, body, `aws_omics_task_cpu_utilization_ratio{run_id="1111111",task_id="4083544",task_name="cpu_busy",workflow_id="4543093"}`, 0.978141)
	requireValue(t, body, `aws_omics_workflow_estimated_cost_usd_total{workflow_id="4543093"}`, 0.049191)
	requireValue(t, body, `aws_omics_workflow_minimum_cost_usd_total{workflow_id="4543093"}`, 0.027929)
	requireLines(t, body,
		`aws_omics_task_instance_type_info{minimum_instance_type="omics.c.large",reserved_instance_type="omics.c.2xlarge",run_id="1111111",task_id="4841160",task_name="idle_overprovisioned",workflow_id="4543093"} 1`,
		`aws_omics_run_analysis_success{run_id="1111111",workflow_id="4543093"} 1`,
		`aws_omics_workflow_analyzed_runs_total{workflow_id="4543093"} 1`,
		`aws_omics_workflow_info{workflow_id="4543093",workflow_name="probe"} 1`,
	)
}

// After a restart the exporter cannot know whether a run that had already finished was
// counted before, so it exports that run's analysis but leaves it out of the counters.
func TestRunsFinishedBeforeTheFirstPollAreAnalyzedButNotCounted(t *testing.T) {
	api, analyzer := newFake(), &fakeAnalyzer{result: fixtureAnalysis(t)}
	e, clock := newClockedExporter(api, analyzer)
	finish(api, clock, 30*time.Minute)

	e.pollOnce(context.Background())
	e.analyzePending(context.Background())

	body := scrape(t, e)
	requireValue(t, body, `aws_omics_run_estimated_cost_usd{run_id="1111111",workflow_id="4543093"}`, 0.049191)
	requireLines(t, body,
		`aws_omics_workflow_analyzed_runs_total{workflow_id="4543093"} 0`,
		`aws_omics_workflow_estimated_cost_usd_total{workflow_id="4543093"} 0`,
	)
}

func TestAnalysisWaitsForTheManifestThenRetriesAndGivesUp(t *testing.T) {
	api, analyzer := newFake(), &fakeAnalyzer{err: errors.New("No run manifest found")}
	e, clock := newClockedExporter(api, analyzer)
	ctx := context.Background()
	e.pollOnce(ctx) // running: becomes countable once it finishes

	finish(api, clock, 30*time.Second)
	e.pollOnce(ctx)
	e.analyzePending(ctx)
	if analyzer.callCount() != 0 {
		t.Fatalf("analyzed %d times before the manifest delay passed", analyzer.callCount())
	}
	for i := 0; i < 6; i++ {
		clock.advance(e.cfg.analyzerRetry)
		e.pollOnce(ctx)
		e.analyzePending(ctx)
		if i == 0 {
			// A failed attempt is not retried before analyzerRetry has passed.
			clock.advance(e.cfg.analyzerRetry - time.Second)
			e.analyzePending(ctx)
			if analyzer.callCount() != 1 {
				t.Fatalf("analyzer calls=%d right after a failure, want 1", analyzer.callCount())
			}
			clock.advance(-(e.cfg.analyzerRetry - time.Second))
		}
	}

	if analyzer.callCount() != e.cfg.analyzerAttempts {
		t.Fatalf("analyzer calls=%d, want %d attempts", analyzer.callCount(), e.cfg.analyzerAttempts)
	}
	body := scrape(t, e)
	requireLines(t, body,
		`aws_omics_run_analysis_success{run_id="1111111",workflow_id="4543093"} 0`,
		`aws_omics_workflow_analyzed_runs_total{workflow_id="4543093"} 0`,
	)
	if containsSample(body, "aws_omics_run_estimated_cost_usd") {
		t.Errorf("failed analysis exported a cost:\n%s", body)
	}
}

func TestFinishedRunsLeaveTheExportAfterRetention(t *testing.T) {
	api, analyzer := newFake(), &fakeAnalyzer{result: fixtureAnalysis(t)}
	e, clock := newClockedExporter(api, analyzer)
	ctx := context.Background()
	e.pollOnce(ctx)
	finish(api, clock, 5*time.Minute)
	e.pollOnce(ctx)
	e.analyzePending(ctx)

	clock.advance(e.cfg.retention)
	e.pollOnce(ctx)
	e.analyzePending(ctx)

	body := scrape(t, e)
	for _, metric := range []string{"aws_omics_run_info", "aws_omics_task_info", "aws_omics_run_estimated_cost_usd", "aws_omics_task_estimated_cost_usd"} {
		if containsSample(body, metric) {
			t.Errorf("%s still exported after retention:\n%s", metric, body)
		}
	}
	// Counters keep their totals: they are cumulative per workflow.
	requireLines(t, body, `aws_omics_workflow_analyzed_runs_total{workflow_id="4543093"} 1`)
}
