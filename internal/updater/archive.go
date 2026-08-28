package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	maxBinarySize         = 128 << 20
	maxConfigTemplateSize = 2 << 20
)

type extractedRelease struct {
	binary         string
	configTemplate string
}

func extractReleaseArchive(
	archivePath string,
	destination string,
	packageName string,
	binaryName string,
) (extractedRelease, error) {
	wantedBinary := path.Join(packageName, binaryName)
	wantedConfig := path.Join(packageName, "config.example.yaml")
	result := extractedRelease{
		binary:         filepath.Join(destination, "release-binary"),
		configTemplate: filepath.Join(destination, "release-config.example.yaml"),
	}

	var err error
	switch {
	case strings.HasSuffix(archivePath, ".tar.gz"):
		err = extractTarGzipFiles(
			archivePath,
			wantedBinary,
			wantedConfig,
			result,
		)
	case strings.HasSuffix(archivePath, ".zip"):
		err = extractZipFiles(
			archivePath,
			wantedBinary,
			wantedConfig,
			result,
		)
	default:
		err = fmt.Errorf("unsupported release archive: %s", filepath.Base(archivePath))
	}
	if err != nil {
		return extractedRelease{}, err
	}
	if _, err := os.Stat(result.binary); err != nil {
		return extractedRelease{}, fmt.Errorf("release archive does not contain %s: %w", binaryName, err)
	}
	if _, err := os.Stat(result.configTemplate); err != nil {
		return extractedRelease{}, fmt.Errorf("release archive does not contain config.example.yaml: %w", err)
	}
	if err := os.Chmod(result.binary, 0o755); err != nil {
		return extractedRelease{}, fmt.Errorf("make release binary executable: %w", err)
	}
	return result, nil
}

func extractTarGzipFiles(
	archivePath string,
	wantedBinary string,
	wantedConfig string,
	destination extractedRelease,
) error {
	archive, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open release archive: %w", err)
	}
	defer archive.Close()

	gzipReader, err := gzip.NewReader(archive)
	if err != nil {
		return fmt.Errorf("open compressed release archive: %w", err)
	}
	defer gzipReader.Close()

	reader := tar.NewReader(gzipReader)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read release archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}

		name := path.Clean(strings.ReplaceAll(header.Name, "\\", "/"))
		switch name {
		case wantedBinary:
			if err := writeArchiveFile(destination.binary, reader, header.Size, maxBinarySize); err != nil {
				return fmt.Errorf("extract release binary: %w", err)
			}
		case wantedConfig:
			if err := writeArchiveFile(
				destination.configTemplate,
				reader,
				header.Size,
				maxConfigTemplateSize,
			); err != nil {
				return fmt.Errorf("extract configuration template: %w", err)
			}
		}
	}
	return nil
}

func extractZipFiles(
	archivePath string,
	wantedBinary string,
	wantedConfig string,
	destination extractedRelease,
) error {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open release archive: %w", err)
	}
	defer archive.Close()

	for _, entry := range archive.File {
		name := path.Clean(strings.ReplaceAll(entry.Name, "\\", "/"))
		if name != wantedBinary && name != wantedConfig {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			return fmt.Errorf("open %s in release archive: %w", name, err)
		}
		destinationPath := destination.binary
		limit := int64(maxBinarySize)
		if name == wantedConfig {
			destinationPath = destination.configTemplate
			limit = maxConfigTemplateSize
		}
		extractErr := writeArchiveFile(
			destinationPath,
			reader,
			int64(entry.UncompressedSize64),
			limit,
		)
		closeErr := reader.Close()
		if extractErr != nil {
			return fmt.Errorf("extract %s: %w", name, extractErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close %s in release archive: %w", name, closeErr)
		}
	}
	return nil
}

func writeArchiveFile(destination string, source io.Reader, size int64, limit int64) error {
	if size < 0 || size > limit {
		return fmt.Errorf("archive entry exceeds the %d-byte limit", limit)
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, io.LimitReader(source, limit+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written != size || written > limit {
		return fmt.Errorf("archive entry size mismatch")
	}
	return nil
}
