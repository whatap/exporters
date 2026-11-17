package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RobustPerception/azure_metrics_exporter/config"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

const eventHubID = "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.EventHub/namespaces/demo"

// Metric value responses, keyed by the metricnames of the request.
var fakeMetricResponses = map[string]string{
	// OutgoingMessages comes first and has no time series in the window. The
	// IncomingMessages series after it must still be exported. hub-a appears
	// twice to check that a repeated series does not fail the whole scrape.
	"IncomingMessages,OutgoingMessages": `{"value": [
	  {"name": {"value": "OutgoingMessages"}, "unit": "Count", "timeseries": []},
	  {"name": {"value": "IncomingMessages"}, "unit": "Count", "timeseries": [
	    {"metadatavalues": [{"name": {"value": "entityname"}, "value": "hub-a"}], "data": [{"total": 10, "count": 1}, {"total": 40, "count": 4}]},
	    {"metadatavalues": [{"name": {"value": "entityname"}, "value": "hub-b"}], "data": [{"total": 5, "count": 1}]},
	    {"metadatavalues": [{"name": {"value": "entityname"}, "value": "hub-a"}], "data": [{"total": 99, "count": 99}]}
	  ]}
	]}`,
	"ActiveConnections": `{"value": [
	  {"name": {"value": "ActiveConnections"}, "unit": "Count", "timeseries": [{"data": [{"total": 30, "count": 3}]}]}
	]}`,
}

type fakeAzure struct {
	mu             sync.Mutex
	metricRequests []*url.URL
}

func (f *fakeAzure) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/oauth2/token"):
		fmt.Fprintf(w, `{"access_token": "token", "expires_on": "%d"}`, time.Now().Add(time.Hour).Unix())
	case r.URL.Path == "/batch":
		var body batchBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var responses []string
		for _, req := range body.Requests {
			u, err := url.Parse(req.RelativeURL)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if !strings.HasSuffix(u.Path, "/providers/microsoft.insights/metrics") {
				responses = append(responses, fmt.Sprintf(`{"httpStatusCode": 200, "content": {"id": %q, "name": "demo", "type": "Microsoft.EventHub/namespaces", "location": "koreacentral"}}`, eventHubID))
				continue
			}
			f.mu.Lock()
			f.metricRequests = append(f.metricRequests, u)
			f.mu.Unlock()
			content, ok := fakeMetricResponses[u.Query().Get("metricnames")]
			if !ok {
				responses = append(responses, `{"httpStatusCode": 400, "content": {"error": {"code": "BadRequest", "message": "unexpected metricnames"}}}`)
				continue
			}
			responses = append(responses, `{"httpStatusCode": 200, "content": `+content+`}`)
		}
		fmt.Fprintf(w, `{"responses": [%s]}`, strings.Join(responses, ","))
	default:
		http.NotFound(w, r)
	}
}

func seriesByLabels(t *testing.T, families []*dto.MetricFamily, name string) map[string]float64 {
	t.Helper()
	for _, mf := range families {
		if mf.GetName() != name {
			continue
		}
		out := map[string]float64{}
		for _, m := range mf.GetMetric() {
			var pairs []string
			for _, lp := range m.GetLabel() {
				pairs = append(pairs, lp.GetName()+"="+lp.GetValue())
			}
			sort.Strings(pairs)
			out[strings.Join(pairs, ",")] = m.GetGauge().GetValue()
		}
		return out
	}
	return nil
}

func TestCollectSplitsByDimensionAndExportsCount(t *testing.T) {
	fake := &fakeAzure{}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	sc.C = &config.Config{
		ActiveDirectoryAuthorityURL: srv.URL + "/",
		ResourceManagerURL:          srv.URL + "/",
		Credentials:                 config.Credentials{SubscriptionID: "sub", TenantID: "tenant", ClientID: "client", ClientSecret: "secret"},
		Targets: []config.Target{{
			Resource: "/resourceGroups/rg/providers/Microsoft.EventHub/namespaces/demo",
			Metrics: []config.Metric{
				{Name: "IncomingMessages", Aggregations: []string{"Total", "Count"}, Dimensions: []string{"EntityName"}, Top: 50},
				{Name: "OutgoingMessages", Aggregations: []string{"Total", "Count"}, Dimensions: []string{"EntityName"}, Top: 50},
				{Name: "ActiveConnections", Aggregations: []string{"Total", "Count"}},
			},
		}},
	}
	ac = NewAzureClient()
	ac.APIVersions = APIVersionMap{"Microsoft.EventHub/namespaces": "2024-01-01"}

	reg := prometheus.NewRegistry()
	reg.MustRegister(&Collector{})
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("scrape failed: %v", err)
	}

	if len(fake.metricRequests) != 2 {
		t.Fatalf("want 2 metric requests (split by dimensions), got %d: %v", len(fake.metricRequests), fake.metricRequests)
	}
	for _, u := range fake.metricRequests {
		q := u.Query()
		if got := q.Get("aggregation"); got != "Total,Count" {
			t.Errorf("%s: aggregation = %q, want Total,Count", q.Get("metricnames"), got)
		}
		switch q.Get("metricnames") {
		case "IncomingMessages,OutgoingMessages":
			if q.Get("$filter") != "EntityName eq '*'" || q.Get("top") != "50" || q.Get("api-version") != "2019-07-01" {
				t.Errorf("dimension request query = %v", q)
			}
		case "ActiveConnections":
			if _, ok := q["$filter"]; ok || q.Get("api-version") != "2018-01-01" {
				t.Errorf("plain request query = %v", q)
			}
		default:
			t.Errorf("unexpected request %v", u)
		}
	}

	resource := "resource_group=rg,resource_name=demo,resource_type=Microsoft.EventHub/namespaces"
	hubA := "dimension_entityname=hub-a," + resource
	hubB := "dimension_entityname=hub-b," + resource
	checks := map[string]map[string]float64{
		"incomingmessages_count_count":  {hubA: 4, hubB: 1},
		"incomingmessages_count_total":  {hubA: 40, hubB: 5},
		"activeconnections_count_count": {resource: 3},
		"activeconnections_count_total": {resource: 30},
	}
	for name, want := range checks {
		got := seriesByLabels(t, families, name)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%s\ngot:  %v\nwant: %v", name, got, want)
		}
	}
	if got := seriesByLabels(t, families, "outgoingmessages_count_total"); got != nil {
		t.Errorf("a metric without time series must not be exported, got %v", got)
	}
}
