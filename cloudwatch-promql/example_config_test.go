package main

import (
	"os"
	"strings"
	"testing"
)

// The shipped HealthOmics preset is part of the product contract: every run metric is
// exported, cumulative sums are counters, and every series carries run_id and workflow_id.
func TestHealthOmicsExampleConfigCoversAllRunMetrics(t *testing.T) {
	raw, err := os.ReadFile("config.example.yml")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := parseConfig(raw)
	if err != nil {
		t.Fatalf("example config invalid: %v", err)
	}
	if len(cfg.Queries) != 14 {
		t.Fatalf("queries=%d, want the 14 documented aws.omics.* run metrics", len(cfg.Queries))
	}
	for _, q := range cfg.Queries {
		mapped := map[string]bool{}
		for _, to := range q.Labels {
			mapped[to] = true
		}
		if !mapped["run_id"] || !mapped["workflow_id"] {
			t.Errorf("%s does not export run_id and workflow_id", q.Name)
		}
		if strings.HasSuffix(q.Name, "_total") != (q.Type == "counter") {
			t.Errorf("%s: type %s does not match the _total suffix convention", q.Name, q.Type)
		}
	}
}
