package main

import (
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// The fixture is real output of aws-healthomics-tools 0.14.0 run_analyzer for a probe run.
func TestParseRunAnalyzerCSVFromRealOutput(t *testing.T) {
	f, err := os.Open("testdata/run-analyzer-0.14.0-run-5930278.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	got, err := parseRunAnalyzerCSV(f)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if !approx(got.cpuUtilization, 0.187169) || !approx(got.memoryUtilization, 0.154471) {
		t.Errorf("run utilization cpu=%v mem=%v", got.cpuUtilization, got.memoryUtilization)
	}
	if len(got.tasks) != 4 {
		t.Fatalf("tasks=%d, want 4", len(got.tasks))
	}
	byName := map[string]taskAnalysis{}
	for _, task := range got.tasks {
		byName[task.name] = task
	}
	idle := byName["idle_overprovisioned"]
	if idle.id != "4841160" || idle.reservedType != "omics.c.2xlarge" || idle.minimumType != "omics.c.large" ||
		!approx(idle.estimatedUSD, 0.028357) || !approx(idle.minimumUSD, 0.007095) ||
		!approx(idle.cpuUtilization, 0.000191) || !approx(idle.memUtilization, 0.065369) {
		t.Errorf("idle_overprovisioned parsed as %+v", idle)
	}
	busy := byName["cpu_busy"]
	if busy.id != "4083544" || !approx(busy.cpuUtilization, 0.978141) || busy.reservedType != busy.minimumType {
		t.Errorf("cpu_busy parsed as %+v", busy)
	}
	if !approx(got.estimatedUSD(), 0.049191) || !approx(got.minimumUSD(), 0.027929) {
		t.Errorf("run cost estimated=%v minimum=%v", got.estimatedUSD(), got.minimumUSD())
	}
	// Sums of the 6-decimal CSV values must not carry binary floating-point tails.
	if s := strconv.FormatFloat(got.estimatedUSD(), 'g', -1, 64); s != "0.049191" {
		t.Errorf("estimatedUSD renders as %s, want 0.049191", s)
	}
}

func TestParseRunAnalyzerCSVRejectsUnexpectedFormat(t *testing.T) {
	header := "type,arn,name,estimatedUSD,minimumUSD,cpuUtilizationRatio,memoryUtilizationRatio,omicsInstanceTypeReserved,omicsInstanceTypeMinimum\n"
	runRow := "run,arn:aws:omics:ap-northeast-2:1:run/1,r,,,0.5,0.5,,\n"
	for name, test := range map[string]struct{ csv, want string }{
		"missing column":  {strings.Replace(header, ",minimumUSD", "", 1) + runRow, "minimumUSD"},
		"no run row":      {header + "task,arn:aws:omics:ap-northeast-2:1:task/2,t,0.1,0.1,0.5,0.5,a,b\n", "run row"},
		"bad number":      {header + runRow + "task,arn:aws:omics:ap-northeast-2:1:task/2,t,abc,0.1,0.5,0.5,a,b\n", "estimatedUSD"},
		"bad task arn":    {header + runRow + "task,arn:aws:omics:ap-northeast-2:1:run/2,t,0.1,0.1,0.5,0.5,a,b\n", "task ARN"},
		"empty":           {"", "header"},
		"unexpected type": {header + runRow + "batch,x,t,0.1,0.1,0.5,0.5,a,b\n", "row type"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseRunAnalyzerCSV(strings.NewReader(test.csv))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err=%v, want error mentioning %q", err, test.want)
			}
		})
	}
}
