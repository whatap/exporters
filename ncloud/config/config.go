package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultInterval is the Cloud Insight query interval used when a namespace
// does not specify one.
const DefaultInterval = "Min5"

var validIntervals = map[string]bool{
	"Min1": true, "Min5": true, "Min30": true, "Hour2": true, "Day1": true,
}

var validAggregations = map[string]bool{
	"AVG": true, "MAX": true, "MIN": true, "SUM": true, "COUNT": true,
}

// Config holds all exporter configuration.
type Config struct {
	NCloud     NCloudConfig      `yaml:"ncloud"`
	Namespaces []NamespaceConfig `yaml:"namespaces"`
}

// NCloudConfig holds NCloud API credentials.
type NCloudConfig struct {
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
}

// NamespaceConfig defines a monitored NCloud namespace.
type NamespaceConfig struct {
	Name    string   `yaml:"name"`
	Enabled bool     `yaml:"enabled"`
	Regions []string `yaml:"regions"`

	// Interval is the Cloud Insight aggregation interval (Min1, Min5, Min30,
	// Hour2, Day1). Defaults to Min5.
	Interval string `yaml:"interval"`

	// Aggregations is the namespace-wide default aggregation list, applied to
	// every metric that does not define its own. Empty means "whatever the
	// Cloud Insight API reports as available for the interval".
	Aggregations []string `yaml:"aggregations"`

	// Metrics selects which metrics to collect. Empty means all metrics
	// discovered from the API.
	Metrics []MetricConfig `yaml:"metrics"`
}

// MetricConfig selects a single metric (or a glob of metrics) to collect.
type MetricConfig struct {
	Name         string   `yaml:"name"`
	Aggregations []string `yaml:"aggregations"`

	re *regexp.Regexp // compiled from Name, set by Load
}

// Matches reports whether the raw Cloud Insight metric name is selected by
// this entry. Matching is case-insensitive and supports '*' wildcards.
func (m *MetricConfig) Matches(metricName string) bool {
	if m.re == nil {
		return strings.EqualFold(m.Name, metricName)
	}
	return m.re.MatchString(metricName)
}

// ResolveInterval returns the effective query interval for the namespace.
func (n *NamespaceConfig) ResolveInterval() string {
	if n.Interval == "" {
		return DefaultInterval
	}
	return n.Interval
}

// SelectMetric reports whether the given raw metric name should be collected,
// and returns the configured aggregations for it (nil = use API defaults).
// The second return value is the index of the matching metrics[] entry, or -1
// when the namespace has no metrics filter (collect everything).
func (n *NamespaceConfig) SelectMetric(metricName string) (aggregations []string, matched int, ok bool) {
	if len(n.Metrics) == 0 {
		return n.Aggregations, -1, true
	}
	for i := range n.Metrics {
		if n.Metrics[i].Matches(metricName) {
			aggrs := n.Metrics[i].Aggregations
			if len(aggrs) == 0 {
				aggrs = n.Aggregations
			}
			return aggrs, i, true
		}
	}
	return nil, -1, false
}

// Load reads and parses a YAML config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Override from environment variables if set
	if v := os.Getenv("NCLOUD_ACCESS_KEY"); v != "" {
		cfg.NCloud.AccessKey = v
	}
	if v := os.Getenv("NCLOUD_SECRET_KEY"); v != "" {
		cfg.NCloud.SecretKey = v
	}

	if cfg.NCloud.AccessKey == "" || cfg.NCloud.SecretKey == "" {
		return nil, fmt.Errorf("ncloud access_key and secret_key are required")
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) validate() error {
	for i := range c.Namespaces {
		ns := &c.Namespaces[i]
		if ns.Name == "" {
			return fmt.Errorf("namespaces[%d]: name is required", i)
		}

		if ns.Interval != "" && !validIntervals[ns.Interval] {
			return fmt.Errorf("namespaces[%q]: unknown interval %q (use Min1, Min5, Min30, Hour2, Day1)",
				ns.Name, ns.Interval)
		}

		normalized, err := normalizeAggregations(ns.Aggregations)
		if err != nil {
			return fmt.Errorf("namespaces[%q]: %w", ns.Name, err)
		}
		ns.Aggregations = normalized

		for j := range ns.Metrics {
			m := &ns.Metrics[j]
			if m.Name == "" {
				return fmt.Errorf("namespaces[%q].metrics[%d]: name is required", ns.Name, j)
			}

			normalized, err := normalizeAggregations(m.Aggregations)
			if err != nil {
				return fmt.Errorf("namespaces[%q].metrics[%q]: %w", ns.Name, m.Name, err)
			}
			m.Aggregations = normalized

			if strings.Contains(m.Name, "*") {
				re, err := compileGlob(m.Name)
				if err != nil {
					return fmt.Errorf("namespaces[%q].metrics[%q]: %w", ns.Name, m.Name, err)
				}
				m.re = re
			}
		}
	}
	return nil
}

func normalizeAggregations(aggrs []string) ([]string, error) {
	if len(aggrs) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(aggrs))
	seen := make(map[string]bool, len(aggrs))
	for _, a := range aggrs {
		up := strings.ToUpper(strings.TrimSpace(a))
		if !validAggregations[up] {
			return nil, fmt.Errorf("unknown aggregation %q (use AVG, MAX, MIN, SUM, COUNT)", a)
		}
		if seen[up] {
			continue
		}
		seen[up] = true
		out = append(out, up)
	}
	return out, nil
}

// compileGlob turns a '*'-wildcard pattern into a case-insensitive anchored regexp.
func compileGlob(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("(?i)^")
	for i, part := range strings.Split(pattern, "*") {
		if i > 0 {
			b.WriteString(".*")
		}
		b.WriteString(regexp.QuoteMeta(part))
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}
