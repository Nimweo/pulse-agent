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

type Client struct {
	healthURL string
	ingestURL string
	apiKey    string
	http      *http.Client
}

func New(baseURL string, apiKey string, timeout time.Duration) (*Client, error) {
	if timeout <= 0 {
		return nil, fmt.Errorf("timeout must be greater than zero")
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("base URL must be an absolute HTTP or HTTPS URL")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("base URL must not contain a query or fragment")
	}

	healthURL, err := url.JoinPath(baseURL, "health")
	if err != nil {
		return nil, fmt.Errorf("build health URL: %w", err)
	}
	ingestURL, err := url.JoinPath(baseURL, "ingest")
	if err != nil {
		return nil, fmt.Errorf("build ingest URL: %w", err)
	}

	return &Client{
		healthURL: healthURL,
		ingestURL: ingestURL,
		apiKey:    strings.TrimSpace(apiKey),
		http:      &http.Client{Timeout: timeout},
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

func (c *Client) Send(ctx context.Context, p model.Payload) error {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if err := json.NewEncoder(zw).Encode(p); err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("gzip close: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.ingestURL, &buf)
	if err != nil {
		return fmt.Errorf("create ingest request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Content-Encoding", "gzip")
	c.authorize(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("perform ingest request: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("ingest endpoint returned %s", resp.Status)
	}

	return nil
}

func (c *Client) authorize(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}
