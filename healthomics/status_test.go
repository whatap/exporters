package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/omics"
	"github.com/aws/aws-sdk-go-v2/service/omics/types"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

var testNow = time.Date(2026, 11, 4, 3, 30, 0, 0, time.UTC)

func ago(d time.Duration) *time.Time {
	t := testNow.Add(-d)
	return &t
}

// fakeOmics is an in-memory HealthOmics API. Every list call returns a single page.
type fakeOmics struct {
	mu        sync.Mutex
	runs      []types.RunListItem
	tasks     map[string][]types.TaskListItem
	workflows map[string]string
	failure   map[string]string // run id -> failureReason returned by GetRun
	err       error
	calls     map[string]int
}

func (f *fakeOmics) count(op string) {
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[op]++
}

func (f *fakeOmics) callCount(op string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[op]
}

func (f *fakeOmics) ListRuns(_ context.Context, in *omics.ListRunsInput, _ ...func(*omics.Options)) (*omics.ListRunsOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count("ListRuns")
	if f.err != nil {
		return nil, f.err
	}
	var items []types.RunListItem
	for _, r := range f.runs {
		if in.Status == "" || r.Status == in.Status {
			items = append(items, r)
		}
	}
	return &omics.ListRunsOutput{Items: items}, nil
}

func (f *fakeOmics) ListRunTasks(_ context.Context, in *omics.ListRunTasksInput, _ ...func(*omics.Options)) (*omics.ListRunTasksOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count("ListRunTasks")
	if f.err != nil {
		return nil, f.err
	}
	return &omics.ListRunTasksOutput{Items: f.tasks[aws.ToString(in.Id)]}, nil
}

func (f *fakeOmics) GetWorkflow(_ context.Context, in *omics.GetWorkflowInput, _ ...func(*omics.Options)) (*omics.GetWorkflowOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count("GetWorkflow")
	name, ok := f.workflows[aws.ToString(in.Id)]
	if !ok {
		return nil, errors.New("workflow not found")
	}
	return &omics.GetWorkflowOutput{Id: in.Id, Name: aws.String(name)}, nil
}

func (f *fakeOmics) GetRun(_ context.Context, in *omics.GetRunInput, _ ...func(*omics.Options)) (*omics.GetRunOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count("GetRun")
	reason := f.failure[aws.ToString(in.Id)]
	return &omics.GetRunOutput{Id: in.Id, FailureReason: aws.String(reason)}, nil
}

func runningRun() types.RunListItem {
	return types.RunListItem{
		Id: aws.String("1111111"), Name: aws.String("sample-A"), WorkflowId: aws.String("4543093"),
		Status: types.RunStatusRunning, StorageType: types.StorageTypeDynamic,
		CreationTime: ago(10 * time.Minute), StartTime: ago(9 * time.Minute),
	}
}

func runningTasks() []types.TaskListItem {
	return []types.TaskListItem{
		{TaskId: aws.String("2222222"), Name: aws.String("align"), Status: types.TaskStatusRunning,
			InstanceType: aws.String("omics.c.large"), Cpus: aws.Int32(2), Memory: aws.Int32(4),
			CreationTime: ago(8 * time.Minute), StartTime: ago(6 * time.Minute)},
		{TaskId: aws.String("3333333"), Name: aws.String("call"), Status: types.TaskStatusPending,
			Cpus: aws.Int32(8), Memory: aws.Int32(16), CreationTime: ago(2 * time.Minute)},
	}
}

func newFake() *fakeOmics {
	return &fakeOmics{
		runs:      []types.RunListItem{runningRun()},
		tasks:     map[string][]types.TaskListItem{"1111111": runningTasks()},
		workflows: map[string]string{"4543093": "probe"},
	}
}

func newTestExporter(api omicsAPI, a runAnalyzer) *exporter {
	cfg := defaultConfig()
	cfg.region = "ap-northeast-2"
	return newExporter(cfg, api, a, func() time.Time { return testNow })
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

func containsSample(body, metric string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, metric+"{") || strings.HasPrefix(line, metric+" ") {
			return true
		}
	}
	return false
}

func TestPollExportsRunAndTaskStatusWithWaitAndDuration(t *testing.T) {
	e := newTestExporter(newFake(), nil)

	e.pollOnce(context.Background())

	body := scrape(t, e)
	requireLines(t, body,
		`aws_omics_run_info{run_id="1111111",run_name="sample-A",status="RUNNING",storage_type="DYNAMIC",workflow_id="4543093",workflow_name="probe"} 1`,
		`aws_omics_run_wait_seconds{run_id="1111111",workflow_id="4543093"} 60`,
		`aws_omics_run_duration_seconds{run_id="1111111",workflow_id="4543093"} 540`,
		`aws_omics_task_info{instance_type="omics.c.large",run_id="1111111",status="RUNNING",task_id="2222222",task_name="align",workflow_id="4543093"} 1`,
		`aws_omics_task_wait_seconds{run_id="1111111",task_id="2222222",workflow_id="4543093"} 120`,
		`aws_omics_task_duration_seconds{run_id="1111111",task_id="2222222",workflow_id="4543093"} 360`,
		`aws_omics_task_info{run_id="1111111",status="PENDING",task_id="3333333",task_name="call",workflow_id="4543093"} 1`,
		`aws_omics_task_wait_seconds{run_id="1111111",task_id="3333333",workflow_id="4543093"} 120`,
		`healthomics_exporter_poll_success 1`,
	)
	if strings.Contains(body, `aws_omics_task_duration_seconds{run_id="1111111",task_id="3333333"`) {
		t.Errorf("a task that has not started must not report a duration:\n%s", body)
	}
}
