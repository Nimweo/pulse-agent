package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultPath(t *testing.T) {
	const want = "configs/config.yaml"
	if DefaultPath != want {
		t.Fatalf("DefaultPath = %q, want %q", DefaultPath, want)
	}
}

func TestLoadReturnsHelpfulErrorWhenFileDoesNotExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want missing file error")
	}
	if !strings.Contains(err.Error(), ExamplePath) {
		t.Fatalf("Load() error = %q, want reference to %q", err, ExamplePath)
	}
}

func TestLoadParsesConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := []byte(`
server:
  url: "https://pulse.test/api/"
  api_key: "secret"
  timeout: 10
agent:
  hostname: "pulse-host"
  version: "dev"
intervals:
  collect: 1
  send: 60
logging:
  level: "info"
  format: "text"
collectors:
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
`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.URL != "https://pulse.test/api/" {
		t.Errorf("Server.URL = %q", cfg.Server.URL)
	}
	if cfg.Agent.Hostname != "pulse-host" {
		t.Errorf("Agent.Hostname = %q", cfg.Agent.Hostname)
	}
	if !cfg.Collectors.CPU.Enabled {
		t.Error("Collectors.CPU.Enabled = false")
	}
	if !cfg.Collectors.CPU.PerCPU {
		t.Error("Collectors.CPU.PerCPU = false")
	}
}

func TestLoadRejectsInvalidEnabledDiskInterval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := []byte(`
server: { url: "https://pulse.test/api/", timeout: 10 }
intervals: { collect: 1, send: 60 }
collectors:
  disk: { enabled: true, interval: 0 }
`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want invalid disk interval error")
	}
	if !strings.Contains(err.Error(), "collectors.disk.interval") {
		t.Fatalf("Load() error = %q, want disk interval reference", err)
	}
}

func TestLoadRejectsInvalidEnabledNetworkInterval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := []byte(`
server: { url: "https://pulse.test/api/", timeout: 10 }
intervals: { collect: 1, send: 60 }
collectors:
  network: { enabled: true, interval: 0 }
`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want invalid network interval error")
	}
	if !strings.Contains(err.Error(), "collectors.network.interval") {
		t.Fatalf("Load() error = %q, want network interval reference", err)
	}
}

func TestLoadRejectsInvalidEnabledSystemInterval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := []byte(`
server: { url: "https://pulse.test/api/", timeout: 10 }
intervals: { collect: 1, send: 60 }
collectors:
  system: { enabled: true, interval: 0 }
`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want invalid system interval error")
	}
	if !strings.Contains(err.Error(), "collectors.system.interval") {
		t.Fatalf("Load() error = %q, want system interval reference", err)
	}
}
