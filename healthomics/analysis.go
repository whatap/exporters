package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
)

// analysis is the Run Analyzer result for one finished run. Values that Run Analyzer leaves
// blank are NaN and are not exported.
type analysis struct {
	cpuUtilization    float64 // run row cpuUtilizationRatio
	memoryUtilization float64 // run row memoryUtilizationRatio
	tasks             []taskAnalysis
}

type taskAnalysis struct {
	id, name                       string
	reservedType, minimumType      string // omicsInstanceTypeReserved / omicsInstanceTypeMinimum
	estimatedUSD, minimumUSD       float64
	cpuUtilization, memUtilization float64
}

// estimatedUSD is the run's compute cost estimate: Run Analyzer reports cost per task only.
func (a *analysis) estimatedUSD() float64 {
	return a.sum(func(t taskAnalysis) float64 { return t.estimatedUSD })
}

// minimumUSD is what the run would have cost on the smallest instance type that fits each task.
func (a *analysis) minimumUSD() float64 {
	return a.sum(func(t taskAnalysis) float64 { return t.minimumUSD })
}

func (a *analysis) sum(value func(taskAnalysis) float64) float64 {
	total := 0.0
	for _, task := range a.tasks {
		if v := value(task); !math.IsNaN(v) {
			total += v
		}
	}
	return roundUSD(total)
}

// roundUSD drops binary floating-point noise from sums of Run Analyzer's 6-decimal amounts.
func roundUSD(v float64) float64 {
	return math.Round(v*1e9) / 1e9
}

var (
	requiredColumns = []string{"type", "arn", "name", "estimatedUSD", "minimumUSD", "cpuUtilizationRatio",
		"memoryUtilizationRatio", "omicsInstanceTypeReserved", "omicsInstanceTypeMinimum"}
	taskARNPattern = regexp.MustCompile(`:task/([0-9]+)$`)
)

// parseRunAnalyzerCSV reads the CSV written by `aws-healthomics-tools run_analyzer <runId> -o`.
// It fails on a missing column or a malformed row instead of exporting partial results.
func parseRunAnalyzerCSV(r io.Reader) (*analysis, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("run analyzer CSV header: %w", err)
	}
	column := map[string]int{}
	for i, name := range header {
		column[name] = i
	}
	for _, name := range requiredColumns {
		if _, ok := column[name]; !ok {
			return nil, fmt.Errorf("run analyzer CSV has no %s column", name)
		}
	}
	result := &analysis{}
	sawRun := false
	for line := 2; ; line++ {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("run analyzer CSV line %d: %w", line, err)
		}
		field := func(name string) string {
			if i := column[name]; i < len(row) {
				return row[i]
			}
			return ""
		}
		number := func(name string) (float64, error) {
			text := field(name)
			if text == "" {
				return math.NaN(), nil
			}
			value, err := strconv.ParseFloat(text, 64)
			if err != nil {
				return 0, fmt.Errorf("run analyzer CSV line %d: %s %q is not a number", line, name, text)
			}
			return value, nil
		}
		var numbers [4]float64
		for i, name := range []string{"estimatedUSD", "minimumUSD", "cpuUtilizationRatio", "memoryUtilizationRatio"} {
			if numbers[i], err = number(name); err != nil {
				return nil, err
			}
		}
		switch field("type") {
		case "run":
			sawRun = true
			result.cpuUtilization, result.memoryUtilization = numbers[2], numbers[3]
		case "task":
			match := taskARNPattern.FindStringSubmatch(field("arn"))
			if match == nil {
				return nil, fmt.Errorf("run analyzer CSV line %d: unexpected task ARN %q", line, field("arn"))
			}
			result.tasks = append(result.tasks, taskAnalysis{
				id: match[1], name: field("name"),
				reservedType: field("omicsInstanceTypeReserved"), minimumType: field("omicsInstanceTypeMinimum"),
				estimatedUSD: numbers[0], minimumUSD: numbers[1], cpuUtilization: numbers[2], memUtilization: numbers[3],
			})
		default:
			return nil, fmt.Errorf("run analyzer CSV line %d: unexpected row type %q", line, field("type"))
		}
	}
	if !sawRun {
		return nil, errors.New("run analyzer CSV has no run row")
	}
	return result, nil
}
