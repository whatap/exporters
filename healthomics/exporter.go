package main

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/omics"
	"github.com/aws/aws-sdk-go-v2/service/omics/types"
)

const pageSize = 100

type omicsAPI interface {
	ListRuns(context.Context, *omics.ListRunsInput, ...func(*omics.Options)) (*omics.ListRunsOutput, error)
	ListRunTasks(context.Context, *omics.ListRunTasksInput, ...func(*omics.Options)) (*omics.ListRunTasksOutput, error)
	GetWorkflow(context.Context, *omics.GetWorkflowInput, ...func(*omics.Options)) (*omics.GetWorkflowOutput, error)
	GetRun(context.Context, *omics.GetRunInput, ...func(*omics.Options)) (*omics.GetRunOutput, error)
}

// runAnalyzer computes the post-run cost and right-sizing analysis of a finished run.
type runAnalyzer interface {
	Analyze(ctx context.Context, runID string) (*analysis, error)
}

var activeRunStatuses = []types.RunStatus{
	types.RunStatusPending, types.RunStatusStarting, types.RunStatusRunning, types.RunStatusStopping,
}

func isActive(status types.RunStatus) bool {
	for _, s := range activeRunStatuses {
		if s == status {
			return true
		}
	}
	return false
}

type taskRecord struct {
	id, name, status, instanceType string
	created, started, stopped      time.Time
}

type runRecord struct {
	id, name, workflowID, status, storageType, failureReason string
	created, started, stopped                                time.Time
	tasks                                                    []taskRecord
	tasksFinal                                               bool // fetched after the run finished
}

type exporter struct {
	cfg      config
	api      omicsAPI
	analyzer runAnalyzer
	now      func() time.Time

	mu            sync.Mutex
	runs          map[string]*runRecord
	workflowNames map[string]string
	pollOK        bool
	lastSuccess   time.Time

	analyses      map[string]*analysisRecord
	totals        map[string]*workflowTotals
	baseline      map[string]time.Time // runs already finished at the first successful poll
	baselineTaken bool
}

func newExporter(cfg config, api omicsAPI, analyzer runAnalyzer, now func() time.Time) *exporter {
	return &exporter{
		cfg: cfg, api: api, analyzer: analyzer, now: now,
		runs: map[string]*runRecord{}, workflowNames: map[string]string{},
		analyses: map[string]*analysisRecord{}, totals: map[string]*workflowTotals{}, baseline: map[string]time.Time{},
	}
}

func toTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// pollOnce refreshes the run and task snapshot. A failed required API call keeps the
// previous snapshot, which is served until it is older than maxStaleness.
func (e *exporter) pollOnce(ctx context.Context) {
	runs, err := e.collect(ctx)
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	if err != nil {
		e.pollOK = false
		if now.Sub(e.lastSuccess) > e.cfg.maxStaleness {
			e.runs = map[string]*runRecord{}
		}
		log.Printf("HEALTHOMICS_EXPORTER: poll failed: %v", err)
		return
	}
	e.runs, e.pollOK, e.lastSuccess = runs, true, now
	e.trackFinishedRuns(now)
}

func (e *exporter) collect(ctx context.Context) (map[string]*runRecord, error) {
	items := map[string]types.RunListItem{}
	if err := e.listRuns(ctx, "", e.cfg.runPages, items); err != nil {
		return nil, err
	}
	// The unfiltered scan is capped, so active runs are also listed by status to never miss one.
	for _, status := range activeRunStatuses {
		if err := e.listRuns(ctx, status, e.cfg.runPages, items); err != nil {
			return nil, err
		}
	}
	e.mu.Lock()
	previous := e.runs
	e.mu.Unlock()
	now := e.now()
	runs := map[string]*runRecord{}
	for id, item := range items {
		active := isActive(item.Status)
		stopped := toTime(item.StopTime)
		finishedRecently := (item.Status == types.RunStatusCompleted || item.Status == types.RunStatusFailed ||
			item.Status == types.RunStatusCancelled) && !stopped.IsZero() && now.Sub(stopped) <= e.cfg.retention
		if !active && !finishedRecently {
			continue
		}
		record := &runRecord{
			id: id, name: aws.ToString(item.Name), workflowID: aws.ToString(item.WorkflowId),
			status: string(item.Status), storageType: string(item.StorageType),
			created: toTime(item.CreationTime), started: toTime(item.StartTime), stopped: stopped,
		}
		if prior, ok := previous[id]; ok && prior.tasksFinal && !active {
			record.tasks, record.tasksFinal, record.failureReason = prior.tasks, true, prior.failureReason
		} else {
			tasks, err := e.listTasks(ctx, id)
			if err != nil {
				return nil, err
			}
			record.tasks, record.tasksFinal = tasks, !active
			if item.Status == types.RunStatusFailed {
				record.failureReason = e.failureReason(ctx, id)
			}
		}
		e.resolveWorkflowName(ctx, record.workflowID)
		runs[id] = record
	}
	return runs, nil
}

func (e *exporter) listRuns(ctx context.Context, status types.RunStatus, maxPages int, into map[string]types.RunListItem) error {
	input := &omics.ListRunsInput{MaxResults: aws.Int32(pageSize), Status: status}
	for page := 0; page < maxPages; page++ {
		output, err := e.api.ListRuns(ctx, input)
		if err != nil {
			return err
		}
		for _, item := range output.Items {
			if id := aws.ToString(item.Id); id != "" {
				into[id] = item
			}
		}
		if aws.ToString(output.NextToken) == "" {
			return nil
		}
		input.StartingToken = output.NextToken
	}
	return nil
}

func (e *exporter) listTasks(ctx context.Context, runID string) ([]taskRecord, error) {
	input := &omics.ListRunTasksInput{Id: aws.String(runID), MaxResults: aws.Int32(pageSize)}
	var tasks []taskRecord
	for page := 0; page < e.cfg.taskPages; page++ {
		output, err := e.api.ListRunTasks(ctx, input)
		if err != nil {
			return nil, err
		}
		for _, item := range output.Items {
			tasks = append(tasks, taskRecord{
				id: aws.ToString(item.TaskId), name: aws.ToString(item.Name), status: string(item.Status),
				instanceType: aws.ToString(item.InstanceType),
				created:      toTime(item.CreationTime), started: toTime(item.StartTime), stopped: toTime(item.StopTime),
			})
		}
		if aws.ToString(output.NextToken) == "" {
			break
		}
		input.StartingToken = output.NextToken
	}
	return tasks, nil
}

// failureReason is optional detail: a GetRun error leaves it empty.
func (e *exporter) failureReason(ctx context.Context, runID string) string {
	output, err := e.api.GetRun(ctx, &omics.GetRunInput{Id: aws.String(runID)})
	if err != nil {
		return ""
	}
	return aws.ToString(output.FailureReason)
}

// resolveWorkflowName caches workflow names; ListRuns leaves workflowName empty for
// private workflows. A failed lookup is retried on the next poll.
func (e *exporter) resolveWorkflowName(ctx context.Context, workflowID string) {
	if workflowID == "" {
		return
	}
	e.mu.Lock()
	_, known := e.workflowNames[workflowID]
	e.mu.Unlock()
	if known {
		return
	}
	output, err := e.api.GetWorkflow(ctx, &omics.GetWorkflowInput{Id: aws.String(workflowID)})
	if err != nil {
		return
	}
	e.mu.Lock()
	e.workflowNames[workflowID] = aws.ToString(output.Name)
	e.mu.Unlock()
}

func (e *exporter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/metrics" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "HEALTHOMICS_EXPORTER: GET required", http.StatusMethodNotAllowed)
		return
	}
	// Scrapes only read the cached snapshot; they never call AWS.
	var body bytes.Buffer
	writeFamilies(&body, e.families())
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write(body.Bytes())
}

func (e *exporter) families() []*family {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	runInfo := &family{name: "aws_omics_run_info", kind: "gauge", help: "HealthOmics run in the export window (active, or finished within the retention)."}
	runWait := &family{name: "aws_omics_run_wait_seconds", kind: "gauge", help: "Seconds from run creation to start, or until now if the run has not started."}
	runDuration := &family{name: "aws_omics_run_duration_seconds", kind: "gauge", help: "Seconds from run start to stop, or until now if the run is still active."}
	taskInfo := &family{name: "aws_omics_task_info", kind: "gauge", help: "HealthOmics task of a run in the export window."}
	taskWait := &family{name: "aws_omics_task_wait_seconds", kind: "gauge", help: "Seconds from task creation to start, or until now if the task has not started."}
	taskDuration := &family{name: "aws_omics_task_duration_seconds", kind: "gauge", help: "Seconds from task start to stop, or until now if the task is still running."}
	for _, run := range e.runs {
		runLabels := []label{{"run_id", run.id}, {"workflow_id", run.workflowID}}
		runInfo.add(1, label{"run_id", run.id}, label{"run_name", run.name}, label{"status", run.status},
			label{"storage_type", run.storageType}, label{"workflow_id", run.workflowID},
			label{"workflow_name", e.workflowNames[run.workflowID]}, label{"failure_reason", run.failureReason})
		addSpans(runWait, runDuration, run.created, run.started, run.stopped, now, runLabels)
		for _, task := range run.tasks {
			taskLabels := []label{{"run_id", run.id}, {"task_id", task.id}, {"workflow_id", run.workflowID}}
			taskInfo.add(1, label{"run_id", run.id}, label{"task_id", task.id}, label{"task_name", task.name},
				label{"status", task.status}, label{"instance_type", task.instanceType}, label{"workflow_id", run.workflowID})
			addSpans(taskWait, taskDuration, task.created, task.started, task.stopped, now, taskLabels)
		}
	}
	pollSuccess := &family{name: "healthomics_exporter_poll_success", kind: "gauge", help: "Whether the last HealthOmics API poll succeeded."}
	pollSuccess.add(boolValue(e.pollOK))
	families := []*family{runInfo, runWait, runDuration, taskInfo, taskWait, taskDuration}
	families = append(families, e.analysisFamilies(now)...)
	return append(families, pollSuccess)
}

// addSpans records the wait (creation to start) and the duration (start to stop) of a run
// or task; a span that has not ended yet is measured until now.
func addSpans(wait, duration *family, created, started, stopped, now time.Time, labels []label) {
	if !created.IsZero() {
		end := now
		if !started.IsZero() {
			end = started
		}
		wait.add(end.Sub(created).Seconds(), labels...)
	}
	if !started.IsZero() {
		end := now
		if !stopped.IsZero() {
			end = stopped
		}
		duration.add(end.Sub(started).Seconds(), labels...)
	}
}

func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
