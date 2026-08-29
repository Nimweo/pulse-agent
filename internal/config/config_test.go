package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolvePathUsesPlatformConfigurationDirectory(t *testing.T) {
	t.Setenv(EnvPath, "")

	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir() error = %v", err)
	}
	want, err := filepath.Abs(filepath.Join(configDir, "nimweo", "pulse-agent", "config.yaml"))
	if err != nil {
		t.Fatalf("Abs() error = %v", err)
	}

	got, err := ResolvePath("")
	if err != nil {
		t.Fatalf("ResolvePath() error = %v", err)
	}
	if got != want {
		t.Fatalf("ResolvePath() = %q, want %q", got, want)
	}
}

func TestResolvePathPrefersExplicitPathOverEnvironment(t *testing.T) {
	t.Setenv(EnvPath, filepath.Join(t.TempDir(), "environment.yaml"))
	explicit := filepath.Join(t.TempDir(), "explicit.yaml")
	want, err := filepath.Abs(explicit)
	if err != nil {
		t.Fatalf("Abs() error = %v", err)
	}

	got, err := ResolvePath(explicit)
	if err != nil {
		t.Fatalf("ResolvePath() error = %v", err)
	}
	if got != want {
		t.Fatalf("ResolvePath() = %q, want %q", got, want)
	}
}

func TestResolvePathUsesEnvironmentOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv(EnvPath, override)
	want, err := filepath.Abs(override)
	if err != nil {
		t.Fatalf("Abs() error = %v", err)
	}

	got, err := ResolvePath("")
	if err != nil {
		t.Fatalf("ResolvePath() error = %v", err)
	}
	if got != want {
		t.Fatalf("ResolvePath() = %q, want %q", got, want)
	}
}

func TestEnsureCreatesEmbeddedExampleWithoutOverwritingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nimweo", "pulse-agent", "config.yaml")

	created, err := Ensure(path)
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if !created {
		t.Fatal("Ensure() created = false, want true")
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(contents), "configured: false") {
		t.Fatalf("created configuration does not contain the setup guard")
	}
	if !strings.Contains(string(contents), `base_url: "https://pulse.nimweo.dev/api/v1/"`) {
		t.Fatalf("created configuration does not contain the default base URL")
	}
	if !strings.Contains(string(contents), "monitored_processes: []") {
		t.Fatalf("created configuration does not contain process collector settings")
	}

	if err := os.WriteFile(path, []byte("custom"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	created, err = Ensure(path)
	if err != nil {
		t.Fatalf("second Ensure() error = %v", err)
	}
	if created {
		t.Fatal("second Ensure() created = true, want false")
	}
	contents, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(contents) != "custom" {
		t.Fatalf("Ensure() overwrote existing configuration: %q", contents)
	}
}

func TestLoadRejectsConfigurationThatWasNotConfirmed(t *testing.T) {
	path := writeConfig(t, `
configured: false
server: { base_url: "https://pulse.test/api/", timeout: 10 }
`)

	_, err := Load(path)
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Load() error = %v, want ErrNotConfigured", err)
	}
}

func TestLoadRejectsAgentVersionOverride(t *testing.T) {
	path := writeConfig(t, strings.Replace(
		validConfig(""),
		"configured: true",
		"configured: true\nversion: \"999.0.0\"",
		1,
	))

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want unknown version field error")
	}
	if !strings.Contains(err.Error(), "field version not found") {
		t.Fatalf("Load() error = %q, want unknown version field error", err)
	}
}

func TestLoadParsesConfigWithOptionalAPIKey(t *testing.T) {
	path := writeConfig(t, validConfig(""))

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.BaseURL != "https://pulse.test/api/" {
		t.Errorf("Server.BaseURL = %q", cfg.Server.BaseURL)
	}
	if cfg.Server.APIKey != "" {
		t.Errorf("Server.APIKey = %q, want empty", cfg.Server.APIKey)
	}
	if cfg.Server.APIEndpoints.Health != "health" || cfg.Server.APIEndpoints.Ingest != "ingest" {
		t.Errorf("Server.APIEndpoints = %#v", cfg.Server.APIEndpoints)
	}
	if cfg.Agent.Hostname != "pulse-host" {
		t.Errorf("Agent.Hostname = %q", cfg.Agent.Hostname)
	}
	if !cfg.Collectors.CPU.PerCPU {
		t.Error("Collectors.CPU.PerCPU = false")
	}
	if !cfg.Collectors.GPU.Enabled || cfg.Collectors.GPU.Interval != 5 {
		t.Errorf("Collectors.GPU = %#v", cfg.Collectors.GPU)
	}
	if !cfg.Collectors.Process.Enabled || cfg.Collectors.Process.Interval != 5 {
		t.Errorf("Collectors.Process = %#v", cfg.Collectors.Process)
	}
	if cfg.Collectors.Process.TopCPU != 5 || cfg.Collectors.Process.TopMemory != 3 {
		t.Errorf("Collectors.Process top limits = %#v", cfg.Collectors.Process)
	}
	if len(cfg.Collectors.Process.MonitoredProcesses) != 2 {
		t.Errorf("Collectors.Process monitored names = %#v", cfg.Collectors.Process.MonitoredProcesses)
	}
	if !cfg.Updates.Enabled || cfg.Updates.Interval != "24h" {
		t.Errorf("Updates = %#v", cfg.Updates)
	}
}

func TestLoadRejectsInvalidBaseURL(t *testing.T) {
	path := writeConfig(t, strings.Replace(
		validConfig("secret"),
		"https://pulse.test/api/",
		"pulse.test/api/",
		1,
	))

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want invalid base URL error")
	}
	if !strings.Contains(err.Error(), "server.base_url") {
		t.Fatalf("Load() error = %q, want server.base_url reference", err)
	}
}

func TestLoadRejectsInvalidAPIEndpoints(t *testing.T) {
	for _, endpoint := range []string{"https://example.test/health", "health?token=1", "//other-host/health"} {
		t.Run(endpoint, func(t *testing.T) {
			contents := strings.Replace(validConfig(""), "health: \"health\"", "health: \""+endpoint+"\"", 1)
			path := writeConfig(t, contents)
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), "server.api_endpoints.health") {
				t.Fatalf("Load() error = %v, want health endpoint validation error", err)
			}
		})
	}
}

func TestLoadRejectsInvalidEnabledCollectorIntervals(t *testing.T) {
	tests := []struct {
		name      string
		collector string
	}{
		{name: "system", collector: "system"},
		{name: "disk", collector: "disk"},
		{name: "network", collector: "network"},
		{name: "gpu", collector: "gpu"},
		{name: "process", collector: "process"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, `
configured: true
server: { base_url: "https://pulse.test/api/", timeout: 10 }
intervals: { collect: 1, send: 60 }
collectors:
  `+tt.collector+`: { enabled: true, interval: 0 }
`)

			_, err := Load(path)
			if err == nil {
				t.Fatal("Load() error = nil, want invalid collector interval error")
			}
			field := "collectors." + tt.collector + ".interval"
			if !strings.Contains(err.Error(), field) {
				t.Fatalf("Load() error = %q, want %q", err, field)
			}
		})
	}
}

func TestLoadRejectsInvalidProcessCollectorSettings(t *testing.T) {
	tests := []struct {
		name  string
		from  string
		to    string
		field string
	}{
		{
			name:  "negative top CPU limit",
			from:  "top_cpu: 5",
			to:    "top_cpu: -1",
			field: "collectors.process.top_cpu",
		},
		{
			name:  "negative top memory limit",
			from:  "top_memory: 3",
			to:    "top_memory: -1",
			field: "collectors.process.top_memory",
		},
		{
			name:  "empty monitored process name",
			from:  `monitored_processes: ["nginx", "redis-server"]`,
			to:    `monitored_processes: ["nginx", " "]`,
			field: "collectors.process.monitored_processes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, strings.Replace(validConfig(""), tt.from, tt.to, 1))
			_, err := Load(path)
			if err == nil {
				t.Fatal("Load() error = nil, want invalid process collector setting error")
			}
			if !strings.Contains(err.Error(), tt.field) {
				t.Fatalf("Load() error = %q, want %q", err, tt.field)
			}
		})
	}
}

func TestLoadRejectsNegativeTransportSettings(t *testing.T) {
	tests := []struct {
		name  string
		from  string
		to    string
		field string
	}{
		{
			name:  "maximum retries",
			from:  "max_retries: 3",
			to:    "max_retries: -1",
			field: "transport.max_retries",
		},
		{
			name:  "retry backoff",
			from:  "retry_backoff: 5",
			to:    "retry_backoff: -1",
			field: "transport.retry_backoff",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, strings.Replace(validConfig(""), tt.from, tt.to, 1))
			_, err := Load(path)
			if err == nil {
				t.Fatal("Load() error = nil, want invalid transport setting error")
			}
			if !strings.Contains(err.Error(), tt.field) {
				t.Fatalf("Load() error = %q, want %q", err, tt.field)
			}
		})
	}
}

func TestLoadRejectsInvalidUpdateSettings(t *testing.T) {
	tests := []struct {
		name string
		from string
		to   string
	}{
		{
			name: "missing interval",
			from: `interval: "24h"`,
			to:   `interval: ""`,
		},
		{
			name: "unsupported interval",
			from: `interval: "24h"`,
			to:   `interval: "2h"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, strings.Replace(validConfig(""), tt.from, tt.to, 1))
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), "updates.interval") {
				t.Fatalf("Load() error = %v, want updates.interval error", err)
			}
		})
	}
}

func TestUpdateInterval(t *testing.T) {
	tests := map[string]time.Duration{
		"1h":      time.Hour,
		"6h":      6 * time.Hour,
		"12h":     12 * time.Hour,
		"24h":     24 * time.Hour,
		"weekly":  7 * 24 * time.Hour,
		"monthly": 30 * 24 * time.Hour,
	}
	for value, want := range tests {
		got, err := UpdateInterval(value)
		if err != nil {
			t.Fatalf("UpdateInterval(%q) error = %v", value, err)
		}
		if got != want {
			t.Errorf("UpdateInterval(%q) = %s, want %s", value, got, want)
		}
	}
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func validConfig(apiKey string) string {
	return `
configured: true
server:
  base_url: "https://pulse.test/api/"
  api_key: "` + apiKey + `"
  timeout: 10
  api_endpoints:
    health: "health"
    ingest: "ingest"
agent:
  hostname: "pulse-host"
intervals:
  collect: 1
  send: 60
logging:
  level: "info"
  format: "text"
collectors:
  system: { enabled: true, interval: 60 }
  load: { enabled: true, interval: 1 }
  cpu: { enabled: true, interval: 1, per_cpu: true }
  memory: { enabled: true, interval: 1 }
  disk: { enabled: false, interval: 1 }
  network: { enabled: false, interval: 1 }
  gpu: { enabled: true, interval: 5 }
  process:
    enabled: true
    interval: 5
    top_cpu: 5
    top_memory: 3
    monitored_processes: ["nginx", "redis-server"]
transport:
  compression: true
  max_retries: 3
  retry_backoff: 5
buffer:
  max_size: 10000
  disk_spool_enabled: false
updates:
  enabled: true
  interval: "24h"
`
}
