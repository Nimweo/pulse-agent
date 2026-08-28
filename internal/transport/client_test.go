package transport

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nimweo/pulse-agent/internal/model"
)

func TestCheckHealthRequires200AndUsesHealthEndpoint(t *testing.T) {
	tests := []struct {
		name              string
		apiKey            string
		wantAuthorization string
	}{
		{name: "without API key"},
		{name: "with API key", apiKey: "secret", wantAuthorization: "Bearer secret"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("method = %q, want GET", r.Method)
				}
				if r.URL.Path != "/api/health" {
					t.Errorf("path = %q, want /api/health", r.URL.Path)
				}
				if got := r.Header.Get("Authorization"); got != tt.wantAuthorization {
					t.Errorf("Authorization = %q, want %q", got, tt.wantAuthorization)
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := New(
				model.ServerConfig{
					BaseURL: server.URL + "/api/",
					APIKey:  tt.apiKey,
					Timeout: 1,
				},
				model.TransportConfig{},
			)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if err := client.CheckHealth(context.Background()); err != nil {
				t.Fatalf("CheckHealth() error = %v", err)
			}
		})
	}
}

func TestCheckHealthRejectsNon200Response(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err := New(
		model.ServerConfig{BaseURL: server.URL, Timeout: 1},
		model.TransportConfig{},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	err = client.CheckHealth(context.Background())
	if err == nil {
		t.Fatal("CheckHealth() error = nil, want non-200 error")
	}
	if !strings.Contains(err.Error(), "expected 200 OK") {
		t.Fatalf("CheckHealth() error = %q, want expected status", err)
	}
}

func TestSendUsesIngestEndpointAndOptionalAPIKey(t *testing.T) {
	want := model.Payload{
		SchemaVersion: model.PayloadSchemaVersion,
		BatchID:       "batch-1",
		SentAt:        123,
		AgentVersion:  "0.3.0",
		Hostname:      "pulse-host",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/api/ingest" {
			t.Errorf("path = %q, want /api/ingest", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q, want Bearer secret", got)
		}
		if got := r.Header.Get("Content-Encoding"); got != "gzip" {
			t.Errorf("Content-Encoding = %q, want gzip", got)
		}

		reader, err := gzip.NewReader(r.Body)
		if err != nil {
			t.Fatalf("NewReader() error = %v", err)
		}
		defer reader.Close()

		var got model.Payload
		if err := json.NewDecoder(reader).Decode(&got); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		if got.AgentVersion != want.AgentVersion || got.Hostname != want.Hostname {
			t.Errorf("payload = %#v, want %#v", got, want)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client, err := New(
		model.ServerConfig{
			BaseURL: server.URL + "/api",
			APIKey:  "secret",
			Timeout: 1,
		},
		model.TransportConfig{Compression: true},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := client.Send(context.Background(), want); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
}

func TestNewRejectsInvalidBaseURL(t *testing.T) {
	_, err := New(
		model.ServerConfig{BaseURL: "pulse.test/api", Timeout: 1},
		model.TransportConfig{},
	)
	if err == nil {
		t.Fatal("New() error = nil, want invalid base URL error")
	}
}

func TestSendSupportsUncompressedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Encoding"); got != "" {
			t.Errorf("Content-Encoding = %q, want empty", got)
		}

		var got model.Payload
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		if got.BatchID != "plain-json" {
			t.Errorf("BatchID = %q, want plain-json", got.BatchID)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client, err := New(
		model.ServerConfig{BaseURL: server.URL, Timeout: 1},
		model.TransportConfig{Compression: false},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := client.Send(context.Background(), model.Payload{BatchID: "plain-json"}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
}

func TestSendRetriesRetryableResponse(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client, err := New(
		model.ServerConfig{BaseURL: server.URL, Timeout: 1},
		model.TransportConfig{MaxRetries: 1},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := client.Send(context.Background(), model.Payload{}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestSendDoesNotRetryClientError(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client, err := New(
		model.ServerConfig{BaseURL: server.URL, Timeout: 1},
		model.TransportConfig{MaxRetries: 3},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := client.Send(context.Background(), model.Payload{}); err == nil {
		t.Fatal("Send() error = nil, want client error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}
