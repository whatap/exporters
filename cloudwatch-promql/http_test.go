package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMetricsEndpointIsReadOnlyAndNeverQueriesCloudWatch(t *testing.T) {
	server, requests := fakeCloudWatch(t, http.StatusOK, cpuVectorResponse)
	e := newTestExporter(t, cpuQueryConfig, server.URL)
	for _, test := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/unknown", http.StatusNotFound},
		{http.MethodPost, "/metrics", http.StatusMethodNotAllowed},
		{http.MethodGet, "/metrics", http.StatusOK},
	} {
		response := httptest.NewRecorder()
		e.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
		if response.Code != test.status {
			t.Errorf("%s %s status=%d, want %d", test.method, test.path, response.Code, test.status)
		}
		if test.status == http.StatusMethodNotAllowed && response.Header().Get("Allow") != http.MethodGet {
			t.Errorf("missing Allow: GET header")
		}
	}
	if n := len(requests.all()); n != 0 {
		t.Fatalf("scrapes issued %d CloudWatch requests, want 0 (polling is separate)", n)
	}
}
