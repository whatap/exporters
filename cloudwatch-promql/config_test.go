package main

import (
	"strings"
	"testing"
)

func TestParseConfigRejectsInvalidQueries(t *testing.T) {
	for name, test := range map[string]struct{ yaml, want string }{
		"no queries":          {"interval: 60s\n", "at least one query"},
		"missing name":        {"queries:\n  - query: up\n", "invalid metric name"},
		"dotted metric name":  {"queries:\n  - name: aws.omics.cpu\n    query: up\n", "invalid metric name"},
		"duplicate name":      {"queries:\n  - name: a\n    query: up\n  - name: a\n    query: up\n", "duplicate"},
		"empty query":         {"queries:\n  - name: a\n", "query is required"},
		"dotted label":        {"queries:\n  - name: a\n    query: up\n    labels: {\"@resource.x\": run.id}\n", "invalid label name"},
		"reserved label":      {"queries:\n  - name: a\n    query: up\n    labels: {\"@resource.x\": __run}\n", "invalid label name"},
		"label mapped twice":  {"queries:\n  - name: a\n    query: up\n    labels: {x: id, y: id}\n", "mapped more than once"},
		"multi-line help":     {"queries:\n  - name: a\n    query: up\n    help: \"a\\nb\"\n", "help"},
		"unknown type":        {"queries:\n  - name: a\n    query: up\n    type: summary\n", "type"},
		"unknown field":       {"queries:\n  - name: a\n    query: up\n    lables: {x: y}\n", "lables"},
		"timeout >= interval": {"interval: 30s\ntimeout: 30s\nqueries:\n  - name: a\n    query: up\n", "timeout"},
		"negative staleness":  {"max_staleness: -1s\nqueries:\n  - name: a\n    query: up\n", "max_staleness"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseConfig([]byte(test.yaml))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err=%v, want error containing %q", err, test.want)
			}
		})
	}
}

func TestParseConfigAcceptsCounterType(t *testing.T) {
	cfg, err := parseConfig([]byte("queries:\n  - name: aws_omics_task_network_io_bytes_total\n    type: counter\n    query: up\n"))
	if err != nil || cfg.Queries[0].Type != "counter" {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
	cfg, err = parseConfig([]byte("queries:\n  - name: a\n    query: up\n"))
	if err != nil || cfg.Queries[0].Type != "gauge" || cfg.Queries[0].Help == "" {
		t.Fatalf("defaults not applied: cfg=%+v err=%v", cfg, err)
	}
}
