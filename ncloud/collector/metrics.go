package collector

import (
	"encoding/json"
	"regexp"
	"strings"
)

var (
	specialCharsRe    = regexp.MustCompile(`[/\s()"']`)
	multiUnderscoreRe = regexp.MustCompile(`_+`)
	nonMetricCharRe   = regexp.MustCompile(`[^a-z0-9_]`)
	leadingUnderRe    = regexp.MustCompile(`^_+`)
	trailingUnderRe   = regexp.MustCompile(`_+$`)
)

// EscapeTagField replaces special characters in metric/field names, matching
// the Java escapeTagField logic: / ( ) " ' and spaces become _.
func EscapeTagField(name string) string {
	return specialCharsRe.ReplaceAllString(name, "_")
}

// SanitizeMetricName converts a raw metric name into a Prometheus-safe metric name.
// It lowercases, replaces special chars, removes consecutive underscores, etc.
func SanitizeMetricName(name string) string {
	s := strings.ToLower(name)
	s = specialCharsRe.ReplaceAllString(s, "_")
	s = nonMetricCharRe.ReplaceAllString(s, "_")
	s = multiUnderscoreRe.ReplaceAllString(s, "_")
	s = leadingUnderRe.ReplaceAllString(s, "")
	s = trailingUnderRe.ReplaceAllString(s, "")
	return s
}

// SanitizeLabelName converts a raw label key into a Prometheus-safe label name.
func SanitizeLabelName(name string) string {
	s := strings.ToLower(name)
	s = nonMetricCharRe.ReplaceAllString(s, "_")
	s = multiUnderscoreRe.ReplaceAllString(s, "_")
	s = leadingUnderRe.ReplaceAllString(s, "")
	s = trailingUnderRe.ReplaceAllString(s, "")
	return s
}

// BuildMetricFQName builds a fully-qualified Prometheus metric name.
// Pattern: ncloud_{namespace}_{metric}_{aggregation}
func BuildMetricFQName(namespace, metric, aggregation string) string {
	ns := sanitizeNamespace(namespace)
	m := SanitizeMetricName(metric)
	a := strings.ToLower(aggregation)
	return "ncloud_" + ns + "_" + m + "_" + a
}

func sanitizeNamespace(namespace string) string {
	// "ncloud.vserver" -> "vserver"
	s := strings.TrimPrefix(namespace, "ncloud.")
	s = strings.ToLower(s)
	s = nonMetricCharRe.ReplaceAllString(s, "_")
	s = multiUnderscoreRe.ReplaceAllString(s, "_")
	return s
}

// ExtractLatestDatapoint returns the value of the most recent datapoint from
// a Cloud Insight API response [[timestamp, value], ...].
func ExtractLatestDatapoint(dps [][]interface{}) (float64, bool) {
	if len(dps) == 0 {
		return 0, false
	}

	var latestTS float64
	var latestVal float64
	found := false

	for _, dp := range dps {
		if len(dp) < 2 {
			continue
		}
		ts, ok := toFloat64(dp[0])
		if !ok {
			continue
		}
		val, ok := toFloat64(dp[1])
		if !ok {
			continue
		}
		if !found || ts > latestTS {
			latestTS = ts
			latestVal = val
			found = true
		}
	}

	return latestVal, found
}

func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
