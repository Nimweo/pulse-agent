package transport

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

			client, err := New(server.URL+"/api/", tt.apiKey, time.Second)
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

	client, err := New(server.URL, "", time.Second)
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
	want := model.Payload{AgentVersion: "0.3.0", Hostname: "pulse-host"}
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

	client, err := New(server.URL+"/api", "secret", time.Second)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := client.Send(context.Background(), want); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
}

func TestNewRejectsInvalidBaseURL(t *testing.T) {
	_, err := New("pulse.test/api", "", time.Second)
	if err == nil {
		t.Fatal("New() error = nil, want invalid base URL error")
	}
}
