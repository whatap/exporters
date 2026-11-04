package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/omics"
	"github.com/aws/aws-sdk-go-v2/service/omics/types"
)

// pagedOmics serves ListRuns in pages of MaxResults to exercise the scan cap.
type pagedOmics struct{ *fakeOmics }

func (p pagedOmics) ListRuns(ctx context.Context, in *omics.ListRunsInput, opts ...func(*omics.Options)) (*omics.ListRunsOutput, error) {
	all, err := p.fakeOmics.ListRuns(ctx, in, opts...)
	if err != nil {
		return nil, err
	}
	start := 0
	if in.StartingToken != nil {
		start, _ = strconv.Atoi(*in.StartingToken)
	}
	end := min(start+int(aws.ToInt32(in.MaxResults)), len(all.Items))
	out := &omics.ListRunsOutput{Items: all.Items[start:end]}
	if end < len(all.Items) {
		out.NextToken = aws.String(strconv.Itoa(end))
	}
	return out, nil
}

func TestActiveRunBeyondTheScanCapIsStillExported(t *testing.T) {
	api := newFake()
	var runs []types.RunListItem
	for i := 0; i < 2*pageSize; i++ { // older finished runs fill the first pages
		stop := testNow.Add(-time.Hour)
		runs = append(runs, types.RunListItem{Id: aws.String(fmt.Sprint(9000000 + i)), Status: types.RunStatusCompleted,
			WorkflowId: aws.String("4543093"), CreationTime: ago(2 * time.Hour), StopTime: &stop})
	}
	api.runs = append(runs, runningRun())
	cfg := defaultConfig()
	cfg.runPages = 1
	e := newExporter(cfg, pagedOmics{api}, nil, func() time.Time { return testNow })

	e.pollOnce(context.Background())

	requireLines(t, scrape(t, e),
		`aws_omics_run_info{run_id="1111111",run_name="sample-A",status="RUNNING",storage_type="DYNAMIC",workflow_id="4543093",workflow_name="probe"} 1`)
}

func TestFailedPollsServeTheLastSnapshotOnlyUntilMaxStaleness(t *testing.T) {
	api := newFake()
	e, clock := newClockedExporter(api, nil)
	e.pollOnce(context.Background())

	api.err = errors.New("ThrottlingException")
	clock.advance(e.cfg.maxStaleness - time.Second)
	e.pollOnce(context.Background())
	body := scrape(t, e)
	requireLines(t, body, `healthomics_exporter_poll_success 0`)
	if !containsSample(body, "aws_omics_run_info") {
		t.Fatalf("snapshot dropped before max staleness:\n%s", body)
	}

	clock.advance(2 * time.Second)
	e.pollOnce(context.Background())
	if body := scrape(t, e); containsSample(body, "aws_omics_run_info") || containsSample(body, "aws_omics_task_info") {
		t.Fatalf("stale snapshot still exported:\n%s", body)
	}
}

func TestFailedRunCarriesFailureReasonAndFinishedTasksAreFetchedOnce(t *testing.T) {
	api := newFake()
	stop := testNow.Add(-10 * time.Minute)
	api.runs[0].Status, api.runs[0].StopTime = types.RunStatusFailed, &stop
	api.failure = map[string]string{"1111111": "Task call failed: exit code 137"}
	e := newTestExporter(api, nil)

	e.pollOnce(context.Background())
	e.pollOnce(context.Background())
	e.pollOnce(context.Background())

	requireLines(t, scrape(t, e),
		`aws_omics_run_info{failure_reason="Task call failed: exit code 137",run_id="1111111",run_name="sample-A",status="FAILED",storage_type="DYNAMIC",workflow_id="4543093",workflow_name="probe"} 1`)
	if n := api.callCount("ListRunTasks"); n != 1 {
		t.Errorf("ListRunTasks calls=%d for a finished run, want 1", n)
	}
	if n := api.callCount("GetRun"); n != 1 {
		t.Errorf("GetRun calls=%d for a failed run, want 1", n)
	}
	if n := api.callCount("GetWorkflow"); n != 1 {
		t.Errorf("GetWorkflow calls=%d, want 1 (cached)", n)
	}
}

func TestMetricsEndpointIsReadOnly(t *testing.T) {
	e := newTestExporter(newFake(), nil)
	for _, test := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/unknown", http.StatusNotFound},
		{http.MethodPost, "/metrics", http.StatusMethodNotAllowed},
		{http.MethodGet, "/metrics", http.StatusOK},
	} {
		response := httptest.NewRecorder()
		e.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
		if response.Code != test.status {
			t.Errorf("%s %s status=%d, want %d", test.method, test.path, response.Code, test.status)
		}
	}
}
