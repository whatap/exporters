package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

var runIDPattern = regexp.MustCompile(`^[0-9]+$`)

// cliAnalyzer runs the AWS Run Analyzer CLI (`aws-healthomics-tools run_analyzer`) as is, so
// cost and right-sizing numbers match what AWS's own tool reports.
type cliAnalyzer struct {
	command string // path or name of the aws-healthomics-tools executable
	region  string
	timeout time.Duration
	workDir string // temporary CSV files are created and removed here
}

func (a *cliAnalyzer) Analyze(ctx context.Context, runID string) (*analysis, error) {
	if !runIDPattern.MatchString(runID) {
		return nil, fmt.Errorf("invalid run id %q", runID)
	}
	dir, err := os.MkdirTemp(a.workDir, "run-analyzer-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	output := filepath.Join(dir, "run-"+runID+".csv")

	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.command, "run_analyzer", runID, "--region", a.region, "-o", output)
	cmd.Env = append(os.Environ(), "PYTHONWARNINGS=ignore")
	// Kill the whole process group on timeout: the CLI may be a wrapper that starts Python.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	stderr := &tailBuffer{limit: 2048}
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("run_analyzer %s: timed out after %s", runID, a.timeout)
		}
		return nil, fmt.Errorf("run_analyzer %s: %v: %s", runID, err, strings.TrimSpace(stderr.String()))
	}
	file, err := os.Open(output)
	if err != nil {
		return nil, fmt.Errorf("run_analyzer %s wrote no CSV: %w", runID, err)
	}
	defer file.Close()
	return parseRunAnalyzerCSV(file)
}

// tailBuffer keeps the last limit bytes written to it.
type tailBuffer struct {
	limit int
	data  []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	if len(b.data) > b.limit {
		b.data = b.data[len(b.data)-b.limit:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string { return string(b.data) }
