package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func assertNoState(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "aws_ec2_instance_state{") {
		t.Fatalf("want failed scrape without partial state: status=%d body=%.200s", response.Code, response.Body.String())
	}
}

func TestMetricsRejectsRepeatedPaginationToken(t *testing.T) {
	calls := 0
	response := serve(fakeEC2(func(context.Context, *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
		calls++
		if calls == 3 {
			return nil, errors.New("test guard: repeated token was not rejected")
		}
		return &ec2.DescribeInstancesOutput{NextToken: aws.String("repeat")}, nil
	}))
	assertNoState(t, response)
	if calls != 2 {
		t.Fatalf("repeated token should stop after two reads, got %d", calls)
	}
}

func TestMetricsBoundsPagination(t *testing.T) {
	calls := 0
	response := serve(fakeEC2(func(context.Context, *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
		calls++
		if calls == 65 {
			return page(), nil
		}
		return &ec2.DescribeInstancesOutput{NextToken: aws.String(fmt.Sprint(calls))}, nil
	}))
	assertNoState(t, response)
	if calls != 64 {
		t.Fatalf("page limit = %d calls, want 64", calls)
	}
}

func TestMetricsBoundsSnapshotToConsumerCapacity(t *testing.T) {
	for _, count := range []int{4096, 4097} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			instances := make([]types.Instance, count)
			for i := range instances {
				instances[i] = instance(fmt.Sprintf("i-%017x", i), 16, "running")
			}
			response := serve(fakeEC2(func(context.Context, *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
				return page(instances...), nil
			}))
			if count == 4096 {
				if response.Code != 200 || strings.Count(response.Body.String(), "aws_ec2_instance_state{") != count {
					t.Fatal("valid boundary snapshot was not preserved")
				}
			} else {
				assertNoState(t, response)
			}
		})
	}
}

func TestMetricsRejectsDuplicateIdentityConflicts(t *testing.T) {
	response := serve(fakeEC2(func(context.Context, *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
		return page(instance("i-0123456789abcdef0", 16, "running"), instance("i-0123456789abcdef0", 48, "terminated")), nil
	}))
	assertNoState(t, response)
}

func TestMetricsDeduplicatesIdenticalState(t *testing.T) {
	response := serve(fakeEC2(func(context.Context, *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
		return page(instance("i-0123456789abcdef0", 16, "running"), instance("i-0123456789abcdef0", 16, "running")), nil
	}))
	if response.Code != 200 || strings.Count(response.Body.String(), "aws_ec2_instance_state{") != 1 {
		t.Fatalf("duplicate series emitted: status=%d body=%s", response.Code, response.Body)
	}
}

func TestMetricsEmitsNoPartialStateWhenLaterPageFails(t *testing.T) {
	calls := 0
	response := serve(fakeEC2(func(context.Context, *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
		calls++
		if calls == 2 {
			return nil, errors.New("upstream unavailable; sensitive provider detail")
		}
		output := page(instance("i-0123456789abcdef0", 48, "terminated"))
		output.NextToken = aws.String("next")
		return output, nil
	}))
	assertNoState(t, response)
	if strings.Contains(response.Body.String(), "sensitive provider detail") {
		t.Fatal("provider error leaked into HTTP response")
	}
}

func TestMetricsRejectsNilAWSResponse(t *testing.T) {
	defer func() {
		if value := recover(); value != nil {
			t.Errorf("nil response panicked: %v", value)
		}
	}()
	assertNoState(t, serve(fakeEC2(func(context.Context, *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
		return nil, nil
	})))
}

func TestMetricsRejectsSnapshotAfterRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exporter := &exporter{region: "ap-northeast-2", client: fakeEC2(func(context.Context, *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
		cancel()
		return page(instance("i-0123456789abcdef0", 48, "terminated")), nil
	})}
	response := httptest.NewRecorder()
	exporter.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil).WithContext(ctx))
	assertNoState(t, response)
}
