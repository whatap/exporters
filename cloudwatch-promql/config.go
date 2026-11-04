package main

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type queryConfig struct {
	Name  string `yaml:"name"`
	Help  string `yaml:"help"`
	Type  string `yaml:"type"`
	Query string `yaml:"query"`
	// Labels maps CloudWatch label names (for example "@resource.aws.omics.run.id") to exported
	// Prometheus label names. Labels that are not listed are dropped.
	Labels map[string]string `yaml:"labels"`
}

type config struct {
	Interval time.Duration `yaml:"interval"`
	Timeout  time.Duration `yaml:"timeout"`
	// MaxStaleness bounds how long the last good result is served while queries fail.
	MaxStaleness time.Duration `yaml:"max_staleness"`
	Queries      []queryConfig `yaml:"queries"`
}

const (
	defaultInterval = 60 * time.Second
	defaultTimeout  = 30 * time.Second
)

var (
	metricNamePattern = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)
	labelNamePattern  = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	metricTypes       = map[string]bool{"gauge": true, "counter": true, "untyped": true}
)

func parseConfig(raw []byte) (config, error) {
	var cfg config
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Interval == 0 {
		cfg.Interval = defaultInterval
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = min(defaultTimeout, cfg.Interval/2)
	}
	if cfg.MaxStaleness == 0 {
		cfg.MaxStaleness = 3 * cfg.Interval
	}
	switch {
	case cfg.Interval < 0:
		return cfg, errors.New("config: interval must be positive")
	case cfg.Timeout < 0 || cfg.Timeout >= cfg.Interval:
		return cfg, errors.New("config: timeout must be positive and shorter than interval")
	case cfg.MaxStaleness < 0:
		return cfg, errors.New("config: max_staleness must not be negative")
	case len(cfg.Queries) == 0:
		return cfg, errors.New("config: at least one query is required")
	}
	names := make(map[string]bool, len(cfg.Queries))
	for i := range cfg.Queries {
		q := &cfg.Queries[i]
		if err := validateQuery(q); err != nil {
			return cfg, fmt.Errorf("config: queries[%d]: %w", i, err)
		}
		if names[q.Name] {
			return cfg, fmt.Errorf("config: queries[%d]: duplicate metric name %q", i, q.Name)
		}
		names[q.Name] = true
	}
	return cfg, nil
}

func validateQuery(q *queryConfig) error {
	if !metricNamePattern.MatchString(q.Name) || strings.HasPrefix(q.Name, "cloudwatch_promql_") {
		return fmt.Errorf("invalid metric name %q", q.Name)
	}
	if strings.TrimSpace(q.Query) == "" {
		return errors.New("query is required")
	}
	if q.Type == "" {
		q.Type = "gauge"
	}
	if !metricTypes[q.Type] {
		return fmt.Errorf("type %q must be gauge, counter or untyped", q.Type)
	}
	if strings.ContainsAny(q.Help, "\r\n") {
		return errors.New("help must be a single line")
	}
	if q.Help == "" {
		q.Help = "CloudWatch PromQL: " + strings.Join(strings.Fields(q.Query), " ")
	}
	targets := make(map[string]bool, len(q.Labels))
	for from, to := range q.Labels {
		if !labelNamePattern.MatchString(to) || strings.HasPrefix(to, "__") {
			return fmt.Errorf("invalid label name %q for %q", to, from)
		}
		if targets[to] {
			return fmt.Errorf("label %q is mapped more than once", to)
		}
		targets[to] = true
	}
	return nil
}
