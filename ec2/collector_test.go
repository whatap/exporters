package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/prometheus/common/expfmt"
)

type fakeEC2 func(context.Context, *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error)

func (f fakeEC2) DescribeInstances(ctx context.Context, input *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	return f(ctx, input)
}

func instance(id string, code int32, name string) types.Instance {
	return types.Instance{InstanceId: aws.String(id), State: &types.InstanceState{Code: aws.Int32(code), Name: types.InstanceStateName(name)}}
}

func page(instances ...types.Instance) *ec2.DescribeInstancesOutput {
	return &ec2.DescribeInstancesOutput{Reservations: []types.Reservation{{OwnerId: aws.String("123456789012"), Instances: instances}}}
}

func TestMetricsExportsEC2StateAsNumericGauge(t *testing.T) {
	client := fakeEC2(func(ctx context.Context, input *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			t.Fatal("EC2 read must have a deadline")
		}
		if len(input.Filters) != 0 {
			t.Fatal("must not filter out stopped or terminated instances")
		}
		return page(instance("i-0123456789abcdef0", 16, "running")), nil
	})
	response := httptest.NewRecorder()
	(&exporter{client: client, region: "ap-northeast-2"}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body)
	}
	var parser expfmt.TextParser
	families, err := parser.TextToMetricFamilies(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	family := families["aws_ec2_instance_state"]
	if family == nil || len(family.Metric) != 1 {
		t.Fatalf("missing EC2 state sample: %v", families)
	}
	metric := family.Metric[0]
	if metric.GetGauge().GetValue() != 16 || metric.TimestampMs != nil {
		t.Fatalf("want numeric state 16 without an explicit timestamp, got %v", metric)
	}
	labels := map[string]string{}
	for _, label := range metric.Label {
		labels[label.GetName()] = label.GetValue()
	}
	if labels["instance_id"] != "i-0123456789abcdef0" || labels["state_name"] != "running" || labels["region"] != "ap-northeast-2" {
		t.Fatalf("unexpected labels: %v", labels)
	}
}
