package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/nimweo/pulse-agent/internal/config"
)

const (
	githubLatestReleaseURL = "https://api.github.com/repos/Nimweo/pulse-agent/releases/latest"
	githubReleaseBaseURL   = "https://github.com/Nimweo/pulse-agent/releases/download"
	maxReleaseMetadataSize = 2 << 20
	maxChecksumsSize       = 1 << 20
	maxArchiveSize         = 256 << 20
)

type Result struct {
	CurrentVersion string
	LatestVersion  string
	Updated        bool
}

type Client struct {
	latestReleaseURL string
	releaseBaseURL   string
	goos             string
	goarch           string
	http             *http.Client
}

type githubRelease struct {
	TagName string `json:"tag_name"`
}

func NewClient() *Client {
	return &Client{
		latestReleaseURL: githubLatestReleaseURL,
		releaseBaseURL:   githubReleaseBaseURL,
		goos:             runtime.GOOS,
		goarch:           runtime.GOARCH,
		http:             &http.Client{Timeout: 5 * time.Minute},
	}
}

func (c *Client) Apply(
	ctx context.Context,
	currentVersion string,
	targetPath string,
	configPath string,
) (Result, error) {
	result := Result{CurrentVersion: currentVersion}
	current, err := parseVersion(currentVersion)
	if err != nil {
		return result, fmt.Errorf("parse current version: %w", err)
	}

	latestTag, err := c.latestVersion(ctx)
	if err != nil {
		return result, err
	}
	latest, err := parseVersion(latestTag)
	if err != nil {
		return result, fmt.Errorf("parse latest release version: %w", err)
	}
	result.LatestVersion = latest.String()
	if latest.Compare(current) <= 0 {
		return result, nil
	}
	if !selfUpdateSupported(c.goos) {
		return result, fmt.Errorf("automatic binary replacement is not supported on %s", c.goos)
	}

	packageName, archiveName, binaryName, err := releasePackage(
		latest.String(),
		c.goos,
		c.goarch,
	)
	if err != nil {
		return result, err
	}
	releaseURL := strings.TrimRight(c.releaseBaseURL, "/") + "/v" + latest.String()

	temporaryDirectory, err := os.MkdirTemp("", "pulse-agent-update-*")
	if err != nil {
		return result, fmt.Errorf("create update directory: %w", err)
	}
	defer os.RemoveAll(temporaryDirectory)

	checksums, err := c.downloadBytes(
		ctx,
		releaseURL+"/checksums.txt",
		maxChecksumsSize,
	)
	if err != nil {
		return result, fmt.Errorf("download release checksums: %w", err)
	}
	expectedChecksum, err := checksumForArchive(checksums, archiveName)
	if err != nil {
		return result, err
	}

	archivePath := filepath.Join(temporaryDirectory, archiveName)
	if err := c.downloadFile(
		ctx,
		releaseURL+"/"+archiveName,
		archivePath,
		maxArchiveSize,
	); err != nil {
		return result, fmt.Errorf("download release archive: %w", err)
	}
	if err := verifyFileChecksum(archivePath, expectedChecksum); err != nil {
		return result, err
	}

	extracted, err := extractReleaseArchive(
		archivePath,
		temporaryDirectory,
		packageName,
		binaryName,
	)
	if err != nil {
		return result, err
	}

	migrateConfig := func() error {
		if configPath == "" {
			return nil
		}
		template, err := os.ReadFile(extracted.configTemplate)
		if err != nil {
			return fmt.Errorf("read release configuration template: %w", err)
		}
		if _, err := config.MergeTemplate(configPath, template); err != nil {
			return fmt.Errorf("migrate configuration: %w", err)
		}
		return nil
	}
	if err := installBinary(
		ctx,
		targetPath,
		extracted.binary,
		latest.String(),
		migrateConfig,
	); err != nil {
		return result, err
	}

	result.Updated = true
	return result, nil
}

func (c *Client) latestVersion(ctx context.Context) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.latestReleaseURL, nil)
	if err != nil {
		return "", fmt.Errorf("create latest release request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "pulse-agent-updater")

	response, err := c.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("request latest release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, response.Body)
		return "", fmt.Errorf("latest release endpoint returned %s", response.Status)
	}
	if response.ContentLength > maxReleaseMetadataSize {
		return "", errors.New("latest release response exceeds the size limit")
	}

	contents, err := io.ReadAll(io.LimitReader(response.Body, maxReleaseMetadataSize+1))
	if err != nil {
		return "", fmt.Errorf("read latest release response: %w", err)
	}
	if len(contents) > maxReleaseMetadataSize {
		return "", errors.New("latest release response exceeds the size limit")
	}
	var release githubRelease
	if err := json.Unmarshal(contents, &release); err != nil {
		return "", fmt.Errorf("decode latest release response: %w", err)
	}
	if strings.TrimSpace(release.TagName) == "" {
		return "", errors.New("latest release response does not contain a tag")
	}
	return release.TagName, nil
}

func (c *Client) downloadBytes(ctx context.Context, url string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "pulse-agent-updater")

	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil, fmt.Errorf("download endpoint returned %s", response.Status)
	}
	if response.ContentLength > limit {
		return nil, fmt.Errorf("download exceeds the %d-byte limit", limit)
	}

	contents, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(contents)) > limit {
		return nil, fmt.Errorf("download exceeds the %d-byte limit", limit)
	}
	return contents, nil
}

func (c *Client) downloadFile(
	ctx context.Context,
	url string,
	path string,
	limit int64,
) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "pulse-agent-updater")

	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, response.Body)
		return fmt.Errorf("download endpoint returned %s", response.Status)
	}
	if response.ContentLength > limit {
		return fmt.Errorf("download exceeds the %d-byte limit", limit)
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, io.LimitReader(response.Body, limit+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > limit {
		return fmt.Errorf("download exceeds the %d-byte limit", limit)
	}
	return nil
}

func checksumForArchive(contents []byte, archiveName string) ([]byte, error) {
	for _, line := range strings.Split(string(contents), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.TrimPrefix(fields[1], "*") != archiveName {
			continue
		}
		checksum, err := hex.DecodeString(fields[0])
		if err != nil || len(checksum) != sha256.Size {
			return nil, fmt.Errorf("invalid checksum for %s", archiveName)
		}
		return checksum, nil
	}
	return nil, fmt.Errorf("checksum not found for %s", archiveName)
}

func verifyFileChecksum(path string, expected []byte) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open release archive for verification: %w", err)
	}
	defer file.Close()

	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return fmt.Errorf("hash release archive: %w", err)
	}
	if !equalBytes(digest.Sum(nil), expected) {
		return errors.New("release archive checksum verification failed")
	}
	return nil
}

func equalBytes(left []byte, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var difference byte
	for index := range left {
		difference |= left[index] ^ right[index]
	}
	return difference == 0
}
