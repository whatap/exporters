package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestQueryErrorResponseMarksQueryDown(t *testing.T) {
	server, _ := fakeCloudWatch(t, http.StatusBadRequest, `{"status":"error","errorType":"bad_data","error":"1:2: parse error"}`)
	e := newTestExporter(t, cpuQueryConfig, server.URL)

	e.pollOnce(context.Background())

	body := scrape(t, e)
	requireLines(t, body,
		`cloudwatch_promql_query_up{query="aws_omics_task_cpu_usage"} 0`,
		`cloudwatch_promql_query_series{query="aws_omics_task_cpu_usage"} 0`,
	)
	if strings.Contains(body, "aws_omics_task_cpu_usage{") {
		t.Fatalf("failed query exported samples:\n%s", body)
	}
}

func TestNonFloatSamplesMarkQueryDownInsteadOfPanicking(t *testing.T) {
	for name, response := range map[string]string{
		"histogram": `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"@resource.aws.omics.run.id":"1"},"histogram":[1793761200,{"count":"3","sum":"1.5","buckets":[]}]}]}}`,
		"matrix":    `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"@resource.aws.omics.run.id":"1"},"values":[[1793761200,"1"]]}]}}`,
		"bad value": `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"@resource.aws.omics.run.id":"1"},"value":[1793761200,"one"]}]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			server, _ := fakeCloudWatch(t, http.StatusOK, response)
			e := newTestExporter(t, cpuQueryConfig, server.URL)

			e.pollOnce(context.Background())

			body := scrape(t, e)
			requireLines(t, body, `cloudwatch_promql_query_up{query="aws_omics_task_cpu_usage"} 0`)
			if strings.Contains(body, "aws_omics_task_cpu_usage{") {
				t.Fatalf("unsupported sample exported:\n%s", body)
			}
		})
	}
}
