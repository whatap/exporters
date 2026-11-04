package main

import (
	"context"
	"log"
	"math"
	"sort"
	"time"
)

type analysisRecord struct {
	workflowID  string
	stopped     time.Time
	result      *analysis
	attempts    int
	lastAttempt time.Time
}

type workflowTotals struct {
	runs, estimatedUSD, minimumUSD float64
}

func isFinished(status string) bool {
	return status == "COMPLETED" || status == "FAILED" || status == "CANCELLED"
}

// trackFinishedRuns must be called with e.mu held after each successful poll. Runs that had
// already finished at the first successful poll form the baseline: after a restart the exporter
// cannot tell whether they were counted before, so they are analyzed but never counted.
func (e *exporter) trackFinishedRuns(now time.Time) {
	for id, run := range e.runs {
		if !e.baselineTaken && isFinished(run.status) {
			e.baseline[id] = run.stopped
		}
		if run.workflowID != "" && e.totals[run.workflowID] == nil {
			// Start counters at 0 so that the first counted run shows up as an increase.
			e.totals[run.workflowID] = &workflowTotals{}
		}
	}
	e.baselineTaken = true
	for id, record := range e.analyses {
		if now.Sub(record.stopped) > e.cfg.retention {
			delete(e.analyses, id)
		}
	}
	for id, stopped := range e.baseline {
		if now.Sub(stopped) > e.cfg.retention {
			delete(e.baseline, id)
		}
	}
}

// analyzePending runs Run Analyzer, one run at a time, for finished runs whose manifest
// should be written by now. It is safe to call concurrently with pollOnce and scrapes.
func (e *exporter) analyzePending(ctx context.Context) {
	if e.analyzer == nil {
		return
	}
	type candidate struct {
		id, workflowID string
		stopped        time.Time
	}
	e.mu.Lock()
	now := e.now()
	var due []candidate
	for id, run := range e.runs {
		if !isFinished(run.status) || run.stopped.IsZero() || now.Sub(run.stopped) < e.cfg.analyzerDelay {
			continue
		}
		if record := e.analyses[id]; record != nil && (record.result != nil ||
			record.attempts >= e.cfg.analyzerAttempts || now.Sub(record.lastAttempt) < e.cfg.analyzerRetry) {
			continue
		}
		due = append(due, candidate{id: id, workflowID: run.workflowID, stopped: run.stopped})
	}
	e.mu.Unlock()
	sort.Slice(due, func(i, j int) bool { return due[i].stopped.Before(due[j].stopped) })

	for _, run := range due {
		if ctx.Err() != nil {
			return
		}
		result, err := e.analyzer.Analyze(ctx, run.id)
		e.mu.Lock()
		record := e.analyses[run.id]
		if record == nil {
			record = &analysisRecord{workflowID: run.workflowID, stopped: run.stopped}
			e.analyses[run.id] = record
		}
		record.attempts++
		record.lastAttempt = e.now()
		if err == nil {
			record.result = result
			if _, before := e.baseline[run.id]; !before && run.workflowID != "" {
				totals := e.totals[run.workflowID]
				if totals == nil {
					totals = &workflowTotals{}
					e.totals[run.workflowID] = totals
				}
				totals.runs++
				totals.estimatedUSD = roundUSD(totals.estimatedUSD + result.estimatedUSD())
				totals.minimumUSD = roundUSD(totals.minimumUSD + result.minimumUSD())
			}
		}
		attempts := record.attempts
		e.mu.Unlock()
		if err != nil {
			log.Printf("HEALTHOMICS_EXPORTER: run analyzer attempt %d/%d for run %s failed: %v", attempts, e.cfg.analyzerAttempts, run.id, err)
		}
	}
}

// analysisFamilies must be called with e.mu held.
func (e *exporter) analysisFamilies(now time.Time) []*family {
	runCost := &family{name: "aws_omics_run_estimated_cost_usd", kind: "gauge", help: "Run Analyzer estimated compute cost of the run (sum of its tasks, list price at analysis time)."}
	runMinimum := &family{name: "aws_omics_run_minimum_cost_usd", kind: "gauge", help: "Run Analyzer cost of the run if every task used its minimum fitting instance type."}
	runCPU := &family{name: "aws_omics_run_cpu_utilization_ratio", kind: "gauge", help: "Run Analyzer CPU utilization ratio of the run."}
	runMemory := &family{name: "aws_omics_run_memory_utilization_ratio", kind: "gauge", help: "Run Analyzer memory utilization ratio of the run."}
	runSuccess := &family{name: "aws_omics_run_analysis_success", kind: "gauge", help: "Whether Run Analyzer succeeded for the run (0 after a failed attempt)."}
	taskCost := &family{name: "aws_omics_task_estimated_cost_usd", kind: "gauge", help: "Run Analyzer estimated compute cost of the task."}
	taskMinimum := &family{name: "aws_omics_task_minimum_cost_usd", kind: "gauge", help: "Run Analyzer cost of the task on its minimum fitting instance type."}
	taskCPU := &family{name: "aws_omics_task_cpu_utilization_ratio", kind: "gauge", help: "Run Analyzer CPU utilization ratio of the task."}
	taskMemory := &family{name: "aws_omics_task_memory_utilization_ratio", kind: "gauge", help: "Run Analyzer memory utilization ratio of the task."}
	taskType := &family{name: "aws_omics_task_instance_type_info", kind: "gauge", help: "Reserved and minimum fitting HealthOmics instance types of the task (Run Analyzer)."}

	for id, record := range e.analyses {
		if record.attempts == 0 || now.Sub(record.stopped) > e.cfg.retention {
			continue
		}
		runLabels := []label{{"run_id", id}, {"workflow_id", record.workflowID}}
		runSuccess.add(boolValue(record.result != nil), runLabels...)
		result := record.result
		if result == nil {
			continue
		}
		runCost.add(result.estimatedUSD(), runLabels...)
		runMinimum.add(result.minimumUSD(), runLabels...)
		addIfNumber(runCPU, result.cpuUtilization, runLabels)
		addIfNumber(runMemory, result.memoryUtilization, runLabels)
		for _, task := range result.tasks {
			taskLabels := []label{{"run_id", id}, {"task_id", task.id}, {"task_name", task.name}, {"workflow_id", record.workflowID}}
			addIfNumber(taskCost, task.estimatedUSD, taskLabels)
			addIfNumber(taskMinimum, task.minimumUSD, taskLabels)
			addIfNumber(taskCPU, task.cpuUtilization, taskLabels)
			addIfNumber(taskMemory, task.memUtilization, taskLabels)
			if task.reservedType != "" || task.minimumType != "" {
				taskType.add(1, append(taskLabels, label{"reserved_instance_type", task.reservedType},
					label{"minimum_instance_type", task.minimumType})...)
			}
		}
	}

	analyzedRuns := &family{name: "aws_omics_workflow_analyzed_runs_total", kind: "counter", help: "Finished runs of the workflow analyzed since the exporter started (runs already finished at startup are excluded)."}
	estimated := &family{name: "aws_omics_workflow_estimated_cost_usd_total", kind: "counter", help: "Sum of Run Analyzer estimated cost of the counted runs of the workflow."}
	minimum := &family{name: "aws_omics_workflow_minimum_cost_usd_total", kind: "counter", help: "Sum of Run Analyzer minimum cost of the counted runs of the workflow."}
	for workflowID, totals := range e.totals {
		labels := []label{{"workflow_id", workflowID}}
		analyzedRuns.add(totals.runs, labels...)
		estimated.add(totals.estimatedUSD, labels...)
		minimum.add(totals.minimumUSD, labels...)
	}
	workflowInfo := &family{name: "aws_omics_workflow_info", kind: "gauge", help: "Name of a HealthOmics workflow seen by the exporter."}
	for workflowID, name := range e.workflowNames {
		if name != "" {
			workflowInfo.add(1, label{"workflow_id", workflowID}, label{"workflow_name", name})
		}
	}
	return []*family{runCost, runMinimum, runCPU, runMemory, runSuccess, taskCost, taskMinimum, taskCPU, taskMemory,
		taskType, analyzedRuns, estimated, minimum, workflowInfo}
}

func addIfNumber(f *family, value float64, labels []label) {
	if !math.IsNaN(value) {
		f.add(value, labels...)
	}
}
