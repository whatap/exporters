package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func serve(client ec2API) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	(&exporter{client: client, region: "ap-northeast-2"}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	return response
}

func TestMetricsRejectsInvalidStateWithoutPartialSnapshot(t *testing.T) {
	missingCode := instance("i-0123456789abcdef1", 0, "pending")
	missingCode.State.Code = nil
	missingState := instance("i-0123456789abcdef1", 0, "pending")
	missingState.State = nil
	missingID := instance("i-0123456789abcdef1", 0, "pending")
	missingID.InstanceId = nil
	cases := map[string]types.Instance{
		"code/name disagreement": instance("i-0123456789abcdef1", 48, "running"),
		"unknown code":           instance("i-0123456789abcdef1", 99, "terminated"),
		"negative code":          instance("i-0123456789abcdef1", -1, "terminated"),
		"overflow code":          instance("i-0123456789abcdef1", 65536, "pending"),
		"missing code":           missingCode,
		"missing state":          missingState,
		"missing ID":             missingID,
		"invalid ID":             instance("not-an-instance", 48, "terminated"),
		"missing name":           instance("i-0123456789abcdef1", 48, ""),
	}
	for name, invalid := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if value := recover(); value != nil {
					t.Errorf("handler panicked instead of rejecting the snapshot: %v", value)
				}
			}()
			response := serve(fakeEC2(func(context.Context, *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
				return page(instance("i-0123456789abcdef0", 16, "running"), invalid), nil
			}))
			if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "aws_ec2_instance_state{") {
				t.Fatalf("invalid/partial snapshot exposed: status=%d body=%s", response.Code, response.Body)
			}
		})
	}
}
