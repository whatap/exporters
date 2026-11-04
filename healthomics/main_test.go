package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestParseOptionsDefaults(t *testing.T) {
	options, err := parseOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if options.listenAddress != "127.0.0.1:9109" || options.cfg.interval != time.Minute ||
		options.cfg.retention != 24*time.Hour || options.cfg.maxStaleness != 3*time.Minute ||
		!options.analyzerEnabled || options.analyzerCommand != "aws-healthomics-tools" || options.analyzerTimeout != 10*time.Minute {
		t.Fatalf("defaults: %+v", options)
	}
}

func TestParseOptionsRejectsInvalidValues(t *testing.T) {
	for _, args := range [][]string{
		{"--interval=0s"}, {"--retention=-1h"}, {"--analyzer.timeout=0s"}, {"extra"},
	} {
		if _, err := parseOptions(args); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
	options, err := parseOptions([]string{"--interval=30s"})
	if err != nil || options.cfg.maxStaleness != 90*time.Second {
		t.Fatalf("max staleness should follow the interval: %+v err=%v", options.cfg, err)
	}
}

func TestRunPollsAndAnalyzesUntilCancelled(t *testing.T) {
	api, analyzer := newFake(), &fakeAnalyzer{result: fixtureAnalysis(t)}
	clock := &fakeClock{t: testNow}
	finish(api, clock, time.Hour)
	cfg := defaultConfig()
	cfg.interval = 20 * time.Millisecond
	e := newExporter(cfg, api, analyzer, clock.Now)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.run(ctx)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for api.callCount("ListRuns") < 10 || analyzer.callCount() < 1 {
		if time.Now().After(deadline) {
			t.Fatalf("ListRuns calls=%d analyzer calls=%d after 2s", api.callCount("ListRuns"), analyzer.callCount())
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return after cancellation")
	}
	if !strings.Contains(scrape(t, e), "aws_omics_run_estimated_cost_usd") {
		t.Error("analysis result not exported")
	}
}
