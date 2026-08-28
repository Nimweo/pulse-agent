package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMergeTemplateAddsMissingSettingsAndPreservesValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	existing := []byte(`configured: true
server:
  base_url: "https://custom.test/api/"
collectors:
  cpu:
    enabled: false
`)
	if err := os.WriteFile(path, existing, 0o640); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	template := []byte(`configured: false
server:
  base_url: "https://pulse.test/api/"
  timeout: 10
collectors:
  cpu:
    enabled: true
    per_cpu: false
  process:
    enabled: false
updates:
  enabled: false
  interval: "24h"
`)

	changed, err := MergeTemplate(path, template)
	if err != nil {
		t.Fatalf("MergeTemplate() error = %v", err)
	}
	if !changed {
		t.Fatal("MergeTemplate() changed = false, want true")
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(contents, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	server := got["server"].(map[string]any)
	if server["base_url"] != "https://custom.test/api/" || server["timeout"] != 10 {
		t.Fatalf("server = %#v", server)
	}
	collectors := got["collectors"].(map[string]any)
	cpu := collectors["cpu"].(map[string]any)
	if cpu["enabled"] != false || cpu["per_cpu"] != false {
		t.Fatalf("cpu collector = %#v", cpu)
	}
	if _, ok := collectors["process"]; !ok {
		t.Fatal("process collector was not added")
	}
	if _, ok := got["updates"]; !ok {
		t.Fatal("update settings were not added")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o640 {
		t.Errorf("configuration mode = %o, want 640", info.Mode().Perm())
	}

	changed, err = MergeTemplate(path, template)
	if err != nil {
		t.Fatalf("second MergeTemplate() error = %v", err)
	}
	if changed {
		t.Fatal("second MergeTemplate() changed = true, want false")
	}
}

func TestMergeTemplateRejectsNonMappingConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("- invalid\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := MergeTemplate(path, []byte("configured: false\n"))
	if err == nil {
		t.Fatal("MergeTemplate() error = nil, want root mapping error")
	}
}
