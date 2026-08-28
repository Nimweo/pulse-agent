package transport

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nimweo/pulse-agent/internal/model"
)

type Client struct {
	url  string
	http *http.Client
}

func New(url string, timeout time.Duration) *Client {
	return &Client{
		url:  url,
		http: &http.Client{Timeout: timeout},
	}
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, &buf)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Content-Encoding", "gzip")

	resp, err := c.http.Do(req)

	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			fmt.Printf("failed to close response body: %v\n", err)
		}
	}(resp.Body)

	if resp.StatusCode > 300 {
		return fmt.Errorf("http status: %s", resp.Status)
	}

	return nil
}
