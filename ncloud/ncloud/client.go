package ncloud

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

const (
	httpStatusThrottleLimited = 410
	maxRetries                = 3
	initialBackoff            = 5 * time.Second
)

// Client is an authenticated HTTP client for NCloud APIs.
type Client struct {
	AccessKey  string
	SecretKey  string
	HTTPClient *http.Client
}

// NewClient creates a new NCloud API client.
func NewClient(accessKey, secretKey string) *Client {
	return &Client{
		AccessKey: accessKey,
		SecretKey: secretKey,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// DoGet performs an authenticated GET request and decodes the JSON response.
func (c *Client) DoGet(url string, result interface{}) error {
	return c.doWithRetry("GET", url, nil, result)
}

// DoPost performs an authenticated POST request with a JSON body and decodes the response.
func (c *Client) DoPost(url string, body interface{}, result interface{}) error {
	return c.doWithRetry("POST", url, body, result)
}

func (c *Client) doWithRetry(method, url string, body interface{}, result interface{}) error {
	backoff := initialBackoff

	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := c.do(method, url, body, result)
		if err == nil {
			return nil
		}

		if apiErr, ok := err.(*APIError); ok && apiErr.StatusCode == httpStatusThrottleLimited {
			slog.Warn("throttled, retrying", "attempt", attempt, "maxRetries", maxRetries, "backoff", backoff)
			time.Sleep(backoff)
			backoff *= 2
			continue
		}

		return err
	}

	return fmt.Errorf("exceeded max retries (%d) for %s %s", maxRetries, method, url)
}

func (c *Client) do(method, url string, body interface{}, result interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	SignRequest(req, c.AccessKey, c.SecretKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
		}
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("decode response: %w (body: %s)", err, string(respBody))
		}
	}

	return nil
}

// APIError represents an HTTP error response from the NCloud API.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("ncloud api error (status %d): %s", e.StatusCode, e.Body)
}
