package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestParseOptionsRequiresConfigFile(t *testing.T) {
	if _, err := parseOptions(nil); err == nil || !strings.Contains(err.Error(), "--config") {
		t.Fatalf("err=%v, want missing --config error", err)
	}
	options, err := parseOptions([]string{"--config", "c.yml"})
	if err != nil || options.configFile != "c.yml" || options.listenAddress != "127.0.0.1:9108" {
		t.Fatalf("options=%+v err=%v", options, err)
	}
}

func TestDefaultEndpointUsesRegionalMonitoringHost(t *testing.T) {
	if got := defaultEndpoint("ap-northeast-2"); got != "https://monitoring.ap-northeast-2.amazonaws.com" {
		t.Fatalf("endpoint=%q", got)
	}
}

func TestRunPollsImmediatelyThenEveryIntervalUntilCancelled(t *testing.T) {
	server, requests := fakeCloudWatch(t, http.StatusOK, cpuVectorResponse)
	e := newTestExporter(t, strings.Replace(cpuQueryConfig, "interval: 60s", "interval: 20ms", 1), server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.run(ctx)
		close(done)
	}()
	deadline := time.After(2 * time.Second)
	for e.pollCount() < 3 {
		select {
		case <-deadline:
			t.Fatalf("polls=%d after 2s, want >= 3", e.pollCount())
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("run did not return after cancellation")
	}
	if n := len(requests.all()); n < 3 {
		t.Fatalf("requests=%d, want >= 3", n)
	}
}
