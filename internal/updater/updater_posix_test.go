//go:build linux || darwin

package updater

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestClientApplyUpdatesBinaryAndMigratesConfiguration(t *testing.T) {
	client, archiveName, archive := testUpdateClient(t, "v0.10.0")
	target := filepath.Join(t.TempDir(), "pulse-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("configured: true\n"), 0o640); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	server := serveUpdateRelease(t, archiveName, archive, false)
	defer server.Close()
	client.latestReleaseURL = server.URL + "/latest"
	client.releaseBaseURL = server.URL

	result, err := client.Apply(context.Background(), "0.9.0", target, configPath)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !result.Updated || result.LatestVersion != "0.10.0" {
		t.Fatalf("Apply() result = %#v", result)
	}
	assertFileContents(t, target, testReleaseBinary)
	configContents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(configContents), "updates:") ||
		!strings.Contains(string(configContents), "configured: true") {
		t.Fatalf("migrated configuration = %q", configContents)
	}
}

func TestClientApplySkipsCurrentRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/latest" {
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"tag_name":"v0.9.0"}`))
	}))
	defer server.Close()
	client := NewClient()
	client.latestReleaseURL = server.URL + "/latest"

	result, err := client.Apply(context.Background(), "0.9.0", "unused", "")
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result.Updated || result.LatestVersion != "0.9.0" {
		t.Fatalf("Apply() result = %#v", result)
	}
}

func TestClientApplyRejectsInvalidChecksumWithoutReplacingBinary(t *testing.T) {
	client, archiveName, archive := testUpdateClient(t, "v0.10.0")
	target := filepath.Join(t.TempDir(), "pulse-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	server := serveUpdateRelease(t, archiveName, archive, true)
	defer server.Close()
	client.latestReleaseURL = server.URL + "/latest"
	client.releaseBaseURL = server.URL

	if _, err := client.Apply(context.Background(), "0.9.0", target, ""); err == nil {
		t.Fatal("Apply() error = nil, want checksum error")
	}
	assertFileContents(t, target, "old binary")
}

func TestClientApplyRejectsBinaryWithMismatchedVersion(t *testing.T) {
	client, archiveName, archive := testUpdateClientWithBinary(
		t,
		"v0.10.0",
		"#!/bin/sh\nprintf '0.11.0\\n'\n",
	)
	target := filepath.Join(t.TempDir(), "pulse-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	server := serveUpdateRelease(t, archiveName, archive, false)
	defer server.Close()
	client.latestReleaseURL = server.URL + "/latest"
	client.releaseBaseURL = server.URL

	if _, err := client.Apply(context.Background(), "0.9.0", target, ""); err == nil {
		t.Fatal("Apply() error = nil, want version mismatch error")
	}
	assertFileContents(t, target, "old binary")
}

func TestClientApplyRollsBackBinaryWhenConfigurationMigrationFails(t *testing.T) {
	client, archiveName, archive := testUpdateClient(t, "v0.10.0")
	target := filepath.Join(t.TempDir(), "pulse-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("- invalid\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	server := serveUpdateRelease(t, archiveName, archive, false)
	defer server.Close()
	client.latestReleaseURL = server.URL + "/latest"
	client.releaseBaseURL = server.URL

	if _, err := client.Apply(context.Background(), "0.9.0", target, configPath); err == nil {
		t.Fatal("Apply() error = nil, want migration error")
	}
	assertFileContents(t, target, "old binary")
}

func testUpdateClient(t *testing.T, tag string) (*Client, string, []byte) {
	t.Helper()
	return testUpdateClientWithBinary(t, tag, testReleaseBinary)
}

func testUpdateClientWithBinary(
	t *testing.T,
	tag string,
	binary string,
) (*Client, string, []byte) {
	t.Helper()
	client := NewClient()
	client.goos = runtime.GOOS
	client.goarch = runtime.GOARCH
	packageName, archiveName, binaryName, err := releasePackage(
		strings.TrimPrefix(tag, "v"),
		client.goos,
		client.goarch,
	)
	if err != nil {
		t.Fatalf("releasePackage() error = %v", err)
	}
	archive := buildTarGzipArchive(t, map[string][]byte{
		packageName + "/" + binaryName:       []byte(binary),
		packageName + "/config.example.yaml": []byte("configured: false\nupdates:\n  enabled: false\n  interval: \"24h\"\n"),
	})
	return client, archiveName, archive
}

const testReleaseBinary = "#!/bin/sh\nprintf '0.10.0\\n'\n"

func serveUpdateRelease(
	t *testing.T,
	archiveName string,
	archive []byte,
	invalidChecksum bool,
) *httptest.Server {
	t.Helper()
	digest := sha256.Sum256(archive)
	checksum := fmt.Sprintf("%x  %s\n", digest, archiveName)
	if invalidChecksum {
		checksum = fmt.Sprintf("%064d  %s\n", 0, archiveName)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			_, _ = w.Write([]byte(`{"tag_name":"v0.10.0"}`))
		case "/v0.10.0/checksums.txt":
			_, _ = w.Write([]byte(checksum))
		case "/v0.10.0/" + archiveName:
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
}
