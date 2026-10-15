package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

func TestMetricsUsesPublicStateCodeLowByte(t *testing.T) {
	states := []struct {
		code int32
		name string
	}{
		{0, "pending"}, {16, "running"}, {32, "shutting-down"},
		{48, "terminated"}, {64, "stopping"}, {80, "stopped"},
	}
	for _, state := range states {
		t.Run(state.name, func(t *testing.T) {
			input := instance("i-0123456789abcdef0", 0xff00|state.code, state.name)
			response := serve(fakeEC2(func(context.Context, *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
				return page(input), nil
			}))
			expected := fmt.Sprintf("state_name=%q} %d\n", state.name, state.code)
			if response.Code != 200 || !strings.Contains(response.Body.String(), expected) {
				t.Fatalf("AWS internal high byte must not change public lifecycle code: status=%d body=%s", response.Code, response.Body)
			}
			if *input.State.Code != 0xff00|state.code {
				t.Fatal("AWS response was mutated")
			}
		})
	}
}
