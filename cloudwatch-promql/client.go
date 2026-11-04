package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// The CloudWatch PromQL HTTP API is signed with SigV4 using "monitoring" as the service name.
const signingService = "monitoring"

// A query returns at most 500 series, so a few MiB is ample; the cap bounds memory on bad replies.
const maxResponseBytes = 8 << 20

type promQLClient struct {
	endpoint    string // https://monitoring.<region>.amazonaws.com
	region      string
	credentials aws.CredentialsProvider
	httpClient  *http.Client
	now         func() time.Time
}

type sample struct {
	Metric map[string]string `json:"metric"`
	Value  []json.RawMessage `json:"value"`
}

type queryResponse struct {
	Status    string `json:"status"`
	ErrorType string `json:"errorType"`
	Error     string `json:"error"`
	Data      struct {
		ResultType string   `json:"resultType"`
		Result     []sample `json:"result"`
	} `json:"data"`
}

// instantQuery runs one PromQL instant query and returns the raw result vector.
func (c *promQLClient) instantQuery(ctx context.Context, expr string) ([]sample, error) {
	now := c.now()
	form := url.Values{}
	form.Set("query", expr)
	form.Set("time", fmt.Sprintf("%.3f", float64(now.UnixMilli())/1000))
	body := form.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.endpoint, "/")+"/api/v1/query", strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	credentials, err := c.credentials.Retrieve(ctx)
	if err != nil {
		return nil, fmt.Errorf("AWS credentials unavailable: %w", err)
	}
	payloadHash := sha256.Sum256([]byte(body))
	if err := v4.NewSigner().SignHTTP(ctx, credentials, request, hex.EncodeToString(payloadHash[:]), signingService, c.region, now); err != nil {
		return nil, fmt.Errorf("sign request: %w", err)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxResponseBytes {
		return nil, fmt.Errorf("HTTP %d: response exceeds %d bytes", response.StatusCode, maxResponseBytes)
	}
	var decoded queryResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("HTTP %d: non-JSON response", response.StatusCode)
	}
	if response.StatusCode != http.StatusOK || decoded.Status != "success" {
		return nil, fmt.Errorf("HTTP %d: %s %s", response.StatusCode, decoded.ErrorType, truncate(decoded.Error, 300))
	}
	if decoded.Data.ResultType != "vector" {
		return nil, fmt.Errorf("result type %q is not an instant vector", truncate(decoded.Data.ResultType, 20))
	}
	return decoded.Data.Result, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
