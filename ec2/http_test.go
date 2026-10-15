package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

func TestMetricsOnlyServesReadOnlyMetricsEndpoint(t *testing.T) {
	for _, test := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/unknown", http.StatusNotFound},
		{http.MethodPost, "/metrics", http.StatusMethodNotAllowed},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			calls := 0
			e := &exporter{region: "ap-northeast-2", client: fakeEC2(func(context.Context, *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
				calls++
				return page(), nil
			})}
			response := httptest.NewRecorder()
			e.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
			if response.Code != test.status || calls != 0 {
				t.Fatalf("status=%d calls=%d, want status=%d without AWS access", response.Code, calls, test.status)
			}
			if test.status == http.StatusMethodNotAllowed && response.Header().Get("Allow") != "GET" {
				t.Fatal("missing Allow: GET header")
			}
		})
	}
}

func TestConcurrentScrapesDoNotIssueConcurrentAWSReads(t *testing.T) {
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls int32
	e := &exporter{region: "ap-northeast-2", client: fakeEC2(func(context.Context, *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
		if atomic.AddInt32(&calls, 1) == 1 {
			close(entered)
			<-release
		}
		return page(instance("i-0123456789abcdef0", 16, "running")), nil
	})}
	t.Cleanup(func() {
		close(release)
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Error("first scrape did not finish")
		}
	})
	first := httptest.NewRecorder()
	go func() {
		defer close(finished)
		e.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first AWS read was not entered")
	}
	second := httptest.NewRecorder()
	e.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assertNoState(t, second)
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatal("concurrent scrape started another AWS read")
	}
}

func TestMetricsNeverReplaysMissingOrFailedInstancesAsTerminated(t *testing.T) {
	calls := 0
	e := &exporter{region: "ap-northeast-2", client: fakeEC2(func(context.Context, *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
		calls++
		switch calls {
		case 1:
			return page(instance("i-0123456789abcdef0", 48, "terminated")), nil
		case 2:
			return page(), nil
		default:
			return nil, errors.New("AWS read failed")
		}
	})}
	for i := 0; i < 3; i++ {
		response := httptest.NewRecorder()
		e.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		if i == 0 && (response.Code != 200 || !strings.Contains(response.Body.String(), "state_name=\"terminated\"} 48")) {
			t.Fatal("authoritative termination was not exported")
		}
		if i > 0 && strings.Contains(response.Body.String(), "aws_ec2_instance_state{") {
			t.Fatal("missing/failed instance was replayed as terminated")
		}
		if i == 1 && response.Code != 200 {
			t.Fatal("empty valid snapshot failed")
		}
		if i == 2 {
			assertNoState(t, response)
		}
	}
}
