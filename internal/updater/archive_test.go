package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractTarGzipReleaseArchive(t *testing.T) {
	packageName := "pulse-agent_0.10.0_linux_amd64"
	archive := buildTarGzipArchive(t, map[string][]byte{
		packageName + "/pulse-agent":         []byte("binary"),
		packageName + "/config.example.yaml": []byte("configured: false\n"),
		packageName + "/ignored.txt":         []byte("ignored"),
	})
	archivePath := filepath.Join(t.TempDir(), packageName+".tar.gz")
	if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	destination := t.TempDir()

	extracted, err := extractReleaseArchive(
		archivePath,
		destination,
		packageName,
		"pulse-agent",
	)
	if err != nil {
		t.Fatalf("extractReleaseArchive() error = %v", err)
	}
	assertFileContents(t, extracted.binary, "binary")
	assertFileContents(t, extracted.configTemplate, "configured: false\n")
}

func TestExtractZipReleaseArchive(t *testing.T) {
	packageName := "pulse-agent_0.10.0_windows_amd64"
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for name, contents := range map[string]string{
		packageName + "/pulse-agent.exe":     "binary",
		packageName + "/config.example.yaml": "configured: false\n",
	} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if _, err := entry.Write([]byte(contents)); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	archivePath := filepath.Join(t.TempDir(), packageName+".zip")
	if err := os.WriteFile(archivePath, archive.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	extracted, err := extractReleaseArchive(
		archivePath,
		t.TempDir(),
		packageName,
		"pulse-agent.exe",
	)
	if err != nil {
		t.Fatalf("extractReleaseArchive() error = %v", err)
	}
	assertFileContents(t, extracted.binary, "binary")
}

func buildTarGzipArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	for name, contents := range files {
		header := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(contents)),
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("WriteHeader() error = %v", err)
		}
		if _, err := tarWriter.Write(contents); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("tar Close() error = %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("gzip Close() error = %v", err)
	}
	return archive.Bytes()
}

func assertFileContents(t *testing.T, path string, want string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if string(contents) != want {
		t.Fatalf("contents of %q = %q, want %q", path, contents, want)
	}
}
