package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeCLI(t *testing.T, mode string) *cliAnalyzer {
	t.Helper()
	script, err := filepath.Abs("testdata/fake-aho.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_AHO_MODE", mode)
	return &cliAnalyzer{command: script, region: "ap-northeast-2", timeout: 5 * time.Second, workDir: t.TempDir()}
}

func TestCLIAnalyzerRunsRunAnalyzerAndParsesItsCSV(t *testing.T) {
	a := fakeCLI(t, "")

	got, err := a.Analyze(context.Background(), "5930278")

	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(got.tasks) != 4 {
		t.Fatalf("tasks=%d, want 4", len(got.tasks))
	}
	if leftovers, _ := os.ReadDir(a.workDir); len(leftovers) != 0 {
		t.Errorf("CSV files left in work dir: %v", leftovers)
	}
}

func TestCLIAnalyzerReportsCommandFailure(t *testing.T) {
	_, err := fakeCLI(t, "fail").Analyze(context.Background(), "5930278")
	if err == nil || !strings.Contains(err.Error(), "No run manifest found") {
		t.Fatalf("err=%v, want the command's stderr", err)
	}
}

func TestCLIAnalyzerStopsAtTimeout(t *testing.T) {
	a := fakeCLI(t, "hang")
	a.timeout = 200 * time.Millisecond
	started := time.Now()

	_, err := a.Analyze(context.Background(), "5930278")

	if err == nil || time.Since(started) > 5*time.Second {
		t.Fatalf("err=%v after %s, want a timeout error well before the 30s sleep", err, time.Since(started))
	}
}

func TestCLIAnalyzerRejectsNonNumericRunID(t *testing.T) {
	_, err := fakeCLI(t, "").Analyze(context.Background(), "5930278 --batch")
	if err == nil || !strings.Contains(err.Error(), "run id") {
		t.Fatalf("err=%v, want invalid run id", err)
	}
}
