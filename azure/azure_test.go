package main

import (
	"net/url"
	"testing"

	"github.com/RobustPerception/azure_metrics_exporter/config"
)

const redisResource = "/resourceGroups/rg/providers/Microsoft.Cache/redis/demo"

func parseResourceURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("resourceURLFrom returned an unparsable URL %q: %v", raw, err)
	}
	return u
}

func TestResourceURLFromKeepsLegacyQueryWithoutDimensions(t *testing.T) {
	sc.C = &config.Config{Credentials: config.Credentials{SubscriptionID: "sub"}}

	u := parseResourceURL(t, resourceURLFrom(redisResource, "", "connectedclients,usedmemory", []string{"Average"}, nil, 0))
	q := u.Query()

	if want := "/subscriptions/sub" + redisResource + "/providers/microsoft.insights/metrics"; u.Path != want {
		t.Errorf("path = %q, want %q", u.Path, want)
	}
	if got := q.Get("api-version"); got != "2018-01-01" {
		t.Errorf("api-version = %q, want 2018-01-01 for requests without dimensions", got)
	}
	if _, ok := q["$filter"]; ok {
		t.Errorf("unexpected $filter %q", q.Get("$filter"))
	}
	if _, ok := q["top"]; ok {
		t.Errorf("unexpected top %q", q.Get("top"))
	}
	if got := q.Get("aggregation"); got != "Average" {
		t.Errorf("aggregation = %q, want Average", got)
	}
}

func TestResourceURLFromSplitsByDimensions(t *testing.T) {
	sc.C = &config.Config{Credentials: config.Credentials{SubscriptionID: "sub"}}

	u := parseResourceURL(t, resourceURLFrom(redisResource, "", "errors", []string{"Total", "Count"}, []string{"ShardId", "ErrorType"}, 50))
	q := u.Query()

	if got, want := q.Get("$filter"), "ShardId eq '*' and ErrorType eq '*'"; got != want {
		t.Errorf("$filter = %q, want %q", got, want)
	}
	if got := q.Get("top"); got != "50" {
		t.Errorf("top = %q, want 50", got)
	}
	if got := q.Get("api-version"); got != "2019-07-01" {
		t.Errorf("api-version = %q, want 2019-07-01 (multi-dimensional values need 2019-07-01 or later)", got)
	}
	if got := q.Get("aggregation"); got != "Total,Count" {
		t.Errorf("aggregation = %q, want Total,Count", got)
	}
}

func TestResourceURLFromLeavesTopToAzureWhenUnset(t *testing.T) {
	sc.C = &config.Config{Credentials: config.Credentials{SubscriptionID: "sub"}}

	q := parseResourceURL(t, resourceURLFrom(redisResource, "", "errors", []string{"Total"}, []string{"ShardId"}, 0)).Query()
	if _, ok := q["top"]; ok {
		t.Errorf("top must be omitted when not configured, got %q", q.Get("top"))
	}
	if got := q.Get("$filter"); got != "ShardId eq '*'" {
		t.Errorf("$filter = %q", got)
	}
}
