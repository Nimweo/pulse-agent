package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if !strings.Contains(string(contents), `base_url: "https://pulse.test/api/"`) {
		t.Fatalf("created configuration does not contain the default base URL")
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
	if cfg.Agent.Hostname != "pulse-host" {
		t.Errorf("Agent.Hostname = %q", cfg.Agent.Hostname)
	}
	if !cfg.Collectors.CPU.PerCPU {
		t.Error("Collectors.CPU.PerCPU = false")
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

func TestLoadRejectsInvalidEnabledCollectorIntervals(t *testing.T) {
	tests := []struct {
		name      string
		collector string
	}{
		{name: "system", collector: "system"},
		{name: "disk", collector: "disk"},
		{name: "network", collector: "network"},
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
transport:
  compression: true
  max_retries: 3
  retry_backoff: 5
buffer:
  max_size: 10000
  disk_spool_enabled: false
`
}
