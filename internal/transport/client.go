package transport

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nimweo/pulse-agent/internal/model"
)

const maxRetryDelay = 30 * time.Second

type Client struct {
	healthURL   string
	ingestURL   string
	apiKey      string
	compression bool
	maxRetries  int
	retryDelay  time.Duration
	http        *http.Client
}

func New(server model.ServerConfig, transport model.TransportConfig) (*Client, error) {
	if server.Timeout <= 0 {
		return nil, fmt.Errorf("timeout must be greater than zero")
	}
	if transport.MaxRetries < 0 {
		return nil, fmt.Errorf("maximum retries must not be negative")
	}
	if transport.RetryBackoff < 0 {
		return nil, fmt.Errorf("retry backoff must not be negative")
	}

	parsed, err := url.Parse(server.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("base URL must be an absolute HTTP or HTTPS URL")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("base URL must not contain a query or fragment")
	}

	healthURL, err := url.JoinPath(server.BaseURL, "health")
	if err != nil {
		return nil, fmt.Errorf("build health URL: %w", err)
	}
	ingestURL, err := url.JoinPath(server.BaseURL, "ingest")
	if err != nil {
		return nil, fmt.Errorf("build ingest URL: %w", err)
	}

	return &Client{
		healthURL:   healthURL,
		ingestURL:   ingestURL,
		apiKey:      strings.TrimSpace(server.APIKey),
		compression: transport.Compression,
		maxRetries:  transport.MaxRetries,
		retryDelay:  time.Duration(transport.RetryBackoff) * time.Second,
		http:        &http.Client{Timeout: time.Duration(server.Timeout) * time.Second},
	}, nil
}

func (c *Client) CheckHealth(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.healthURL, nil)
	if err != nil {
		return fmt.Errorf("create health request: %w", err)
	}
	c.authorize(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("perform health request: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned %s, expected 200 OK", resp.Status)
	}

	return nil
}

func (c *Client) Send(ctx context.Context, payload model.Payload) error {
	body, contentEncoding, err := encodePayload(payload, c.compression)
	if err != nil {
		return err
	}

	var (
		attempts int
		lastErr  error
	)
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		attempts = attempt + 1
		retry, err := c.sendAttempt(ctx, body, contentEncoding)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retry || attempt == c.maxRetries {
			break
		}
		if err := waitForRetry(ctx, retryDelay(c.retryDelay, attempt)); err != nil {
			return fmt.Errorf("wait before ingest retry: %w", err)
		}
	}

	return fmt.Errorf("send metrics after %d attempt(s): %w", attempts, lastErr)
}

func encodePayload(payload model.Payload, compression bool) ([]byte, string, error) {
	var body bytes.Buffer
	if !compression {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			return nil, "", fmt.Errorf("encode payload: %w", err)
		}
		return body.Bytes(), "", nil
	}

	writer := gzip.NewWriter(&body)
	if err := json.NewEncoder(writer).Encode(payload); err != nil {
		_ = writer.Close()
		return nil, "", fmt.Errorf("encode compressed payload: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("close gzip writer: %w", err)
	}

	return body.Bytes(), "gzip", nil
}

func (c *Client) sendAttempt(
	ctx context.Context,
	body []byte,
	contentEncoding string,
) (bool, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.ingestURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return false, fmt.Errorf("create ingest request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	if contentEncoding != "" {
		req.Header.Set("Content-Encoding", contentEncoding)
	}
	c.authorize(req)

	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return true, fmt.Errorf("perform ingest request: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return false, nil
	}

	err = fmt.Errorf("ingest endpoint returned %s", resp.Status)
	return isRetryableStatus(resp.StatusCode), err
}

func (c *Client) authorize(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}

func isRetryableStatus(status int) bool {
	return status == http.StatusRequestTimeout ||
		status == http.StatusTooEarly ||
		status == http.StatusTooManyRequests ||
		status >= http.StatusInternalServerError
}

func retryDelay(base time.Duration, retry int) time.Duration {
	delay := base
	for range retry {
		if delay >= maxRetryDelay/2 {
			return maxRetryDelay
		}
		delay *= 2
	}
	if delay > maxRetryDelay {
		return maxRetryDelay
	}
	return delay
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
