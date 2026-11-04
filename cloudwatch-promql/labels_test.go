package main

import (
	"context"
	"net/http"
	"testing"
)

func TestLabelValuesUsePrometheusEscaping(t *testing.T) {
	// JSON "\t" and "\n" decode to real control characters; Prometheus only defines \\, \" and \n.
	response := `{"status":"success","data":{"resultType":"vector","result":[
 {"metric":{"@resource.aws.omics.run.id":"a\"b\\c\nd\te-한글"},"value":[1793761200,"2"]}]}}`
	server, _ := fakeCloudWatch(t, http.StatusOK, response)
	e := newTestExporter(t, cpuQueryConfig, server.URL)

	e.pollOnce(context.Background())

	requireLines(t, scrape(t, e), "aws_omics_task_cpu_usage{run_id=\"a\\\"b\\\\c\\nd\te-한글\"} 2")
}

func TestSeriesThatCollideAfterLabelMappingMarkQueryDown(t *testing.T) {
	// Both series differ only in a label that is not exported, so they would collide.
	response := `{"status":"success","data":{"resultType":"vector","result":[
 {"metric":{"@resource.aws.omics.run.id":"1","network.io.direction":"receive"},"value":[1793761200,"10"]},
 {"metric":{"@resource.aws.omics.run.id":"1","network.io.direction":"transmit"},"value":[1793761200,"20"]}]}}`
	server, _ := fakeCloudWatch(t, http.StatusOK, response)
	e := newTestExporter(t, cpuQueryConfig, server.URL)

	e.pollOnce(context.Background())

	body := scrape(t, e)
	requireLines(t, body, `cloudwatch_promql_query_up{query="aws_omics_task_cpu_usage"} 0`)
	if containsSample(body, "aws_omics_task_cpu_usage") {
		t.Fatalf("colliding series exported:\n%s", body)
	}
}
