package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

const (
	stateMetricName = "aws_ec2_instance_state"
	scrapeTimeout   = 10 * time.Second
	pageSize        = 1000
	maxPages        = 64
	maxInstances    = 4096
	maxAWSStateCode = 0xffff
	publicStateMask = 0xff
)

var instanceIDPattern = regexp.MustCompile(`^i-([0-9a-f]{8}|[0-9a-f]{17})$`)

var stateNames = map[int32]string{
	0: "pending", 16: "running", 32: "shutting-down",
	48: "terminated", 64: "stopping", 80: "stopped",
}

type ec2API interface {
	DescribeInstances(context.Context, *ec2.DescribeInstancesInput, ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
}

type exporter struct {
	client ec2API
	region string
	mu     sync.Mutex
}

func validInstance(instance types.Instance) bool {
	if !instanceIDPattern.MatchString(aws.ToString(instance.InstanceId)) || instance.State == nil || instance.State.Code == nil {
		return false
	}
	rawCode := *instance.State.Code
	if rawCode < 0 || rawCode > maxAWSStateCode {
		return false
	}
	name, known := stateNames[rawCode&publicStateMask]
	return known && name == string(instance.State.Name)
}

func (e *exporter) collect(ctx context.Context) ([]types.Instance, error) {
	input := &ec2.DescribeInstancesInput{MaxResults: aws.Int32(pageSize)}
	var instances []types.Instance
	seenInstances := make(map[string]types.Instance)
	seenTokens := make(map[string]bool)
	sampleCount := 0
	for pageNumber := 0; pageNumber < maxPages; pageNumber++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		output, err := e.client.DescribeInstances(ctx, input)
		if err != nil {
			return nil, err
		}
		if output == nil {
			return nil, errors.New("missing AWS response")
		}
		for _, reservation := range output.Reservations {
			for _, instance := range reservation.Instances {
				sampleCount++
				if sampleCount > maxInstances || !validInstance(instance) {
					return nil, errors.New("invalid or oversized instance snapshot")
				}
				// AWS reserves the high byte for internal use. Do not mutate SDK-owned state.
				state := *instance.State
				state.Code = aws.Int32(*state.Code & publicStateMask)
				instance.State = &state
				id := aws.ToString(instance.InstanceId)
				if prior, duplicate := seenInstances[id]; duplicate {
					if *prior.State.Code != *instance.State.Code || prior.State.Name != instance.State.Name {
						return nil, errors.New("conflicting instance states")
					}
					continue
				}
				seenInstances[id] = instance
				instances = append(instances, instance)
			}
		}
		nextToken := aws.ToString(output.NextToken)
		if nextToken == "" {
			return instances, ctx.Err()
		}
		if seenTokens[nextToken] {
			return nil, errors.New("repeated pagination token")
		}
		seenTokens[nextToken] = true
		input.NextToken = output.NextToken
	}
	return nil, errors.New("pagination limit exceeded")
}

func (e *exporter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/metrics" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "EC2_STATE_EXPORTER: GET required", http.StatusMethodNotAllowed)
		return
	}
	if !e.mu.TryLock() {
		http.Error(w, "EC2_STATE_EXPORTER: scrape already in progress", http.StatusServiceUnavailable)
		return
	}
	defer e.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), scrapeTimeout)
	defer cancel()
	instances, err := e.collect(ctx)
	if err != nil {
		http.Error(w, "EC2_STATE_EXPORTER: collection failed", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP %s EC2 lifecycle state code reported by DescribeInstances.\n# TYPE %s gauge\n", stateMetricName, stateMetricName)
	for _, instance := range instances {
		fmt.Fprintf(w, "%s{instance_id=%q,region=%q,state_name=%q} %d\n", stateMetricName, aws.ToString(instance.InstanceId), e.region, instance.State.Name, aws.ToInt32(instance.State.Code))
	}
}
