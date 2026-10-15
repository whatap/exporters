package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/prometheus/common/expfmt"
)

func TestMetricsReadsEveryPageIncludingPendingAndStopped(t *testing.T) {
	calls := 0
	client := fakeEC2(func(_ context.Context, input *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
		calls++
		if aws.ToInt32(input.MaxResults) != 1000 {
			t.Errorf("pagination size = %d, want 1000", aws.ToInt32(input.MaxResults))
		}
		switch calls {
		case 1:
			if input.NextToken != nil {
				t.Fatal("first page has a continuation token")
			}
			output := page(instance("i-0123456789abcdef0", 0, "pending"))
			output.NextToken = aws.String("next-page")
			return output, nil
		case 2:
			if aws.ToString(input.NextToken) != "next-page" {
				t.Fatal("continuation token was not preserved")
			}
			return page(instance("i-0123456789abcdef1", 80, "stopped")), nil
		default:
			t.Fatal("read past the last page")
			return nil, nil
		}
	})
	response := httptest.NewRecorder()
	(&exporter{client: client, region: "ap-northeast-2"}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	var parser expfmt.TextParser
	families, err := parser.TextToMetricFamilies(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	family := families["aws_ec2_instance_state"]
	if response.Code != 200 || calls != 2 || family == nil || len(family.Metric) != 2 {
		t.Fatalf("status=%d, calls=%d, family=%v; need both EC2 pages", response.Code, calls, family)
	}
	if family.Metric[0].GetGauge().GetValue() != 0 || family.Metric[1].GetGauge().GetValue() != 80 {
		t.Fatalf("pending=0 and stopped=80 must be preserved: %v", family)
	}
}
