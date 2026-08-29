package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	configtemplate "github.com/nimweo/pulse-agent/configs"
	"github.com/nimweo/pulse-agent/internal/model"
	"gopkg.in/yaml.v3"
)

const EnvPath = "PULSE_CONFIG"

var ErrNotConfigured = errors.New("configuration is not marked as configured")

// ResolvePath returns the explicit path, the environment override, or the
// platform-specific user configuration path, in that order.
func ResolvePath(explicit string) (string, error) {
	path := strings.TrimSpace(explicit)
	if path == "" {
		path = strings.TrimSpace(os.Getenv(EnvPath))
	}

	if path == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("resolve user configuration directory: %w", err)
		}
		path = filepath.Join(configDir, "nimweo", "pulse-agent", "config.yaml")
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve configuration path %q: %w", path, err)
	}

	return absolutePath, nil
}

// Ensure creates a private configuration file from the embedded example when
// the target does not exist. It never overwrites an existing file.
func Ensure(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("inspect configuration file %q: %w", path, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, fmt.Errorf("create configuration directory for %q: %w", path, err)
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("create configuration file %q: %w", path, err)
	}

	if _, err := file.Write(configtemplate.Example); err != nil {
		_ = file.Close()
		return false, fmt.Errorf("write configuration file %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return false, fmt.Errorf("close configuration file %q: %w", path, err)
	}

	return true, nil
}

func Load(path string) (*model.Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open configuration file %q: %w", path, err)
	}
	defer file.Close()

	var cfg model.Config
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)

	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode configuration file %q: %w", path, err)
	}

	if err := validate(cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration in %q: %w", path, err)
	}

	return &cfg, nil
}

func validate(c model.Config) error {
	if !c.Configured {
		return ErrNotConfigured
	}
	if err := validateBaseURL(c.Server.BaseURL); err != nil {
		return err
	}
	if err := validateAPIEndpoint("health", c.Server.APIEndpoints.Health); err != nil {
		return err
	}
	if err := validateAPIEndpoint("ingest", c.Server.APIEndpoints.Ingest); err != nil {
		return err
	}
	if c.Server.Timeout <= 0 {
		return errors.New("server.timeout must be greater than zero")
	}
	if c.Intervals.Collect <= 0 {
		return errors.New("intervals.collect must be greater than zero")
	}
	if c.Intervals.Send <= 0 {
		return errors.New("intervals.send must be greater than zero")
	}
	if c.Transport.MaxRetries < 0 {
		return errors.New("transport.max_retries must not be negative")
	}
	if c.Transport.RetryBackoff < 0 {
		return errors.New("transport.retry_backoff must not be negative")
	}
	if c.Collectors.System.Enabled && c.Collectors.System.Interval <= 0 {
		return errors.New("collectors.system.interval must be greater than zero when system collection is enabled")
	}
	if c.Collectors.Disk.Enabled && c.Collectors.Disk.Interval <= 0 {
		return errors.New("collectors.disk.interval must be greater than zero when disk collection is enabled")
	}
	if c.Collectors.Network.Enabled && c.Collectors.Network.Interval <= 0 {
		return errors.New("collectors.network.interval must be greater than zero when network collection is enabled")
	}
	if c.Collectors.GPU.Enabled && c.Collectors.GPU.Interval <= 0 {
		return errors.New("collectors.gpu.interval must be greater than zero when GPU collection is enabled")
	}
	if c.Collectors.Process.Enabled && c.Collectors.Process.Interval <= 0 {
		return errors.New("collectors.process.interval must be greater than zero when process collection is enabled")
	}
	if c.Collectors.Process.TopCPU < 0 {
		return errors.New("collectors.process.top_cpu must not be negative")
	}
	if c.Collectors.Process.TopMemory < 0 {
		return errors.New("collectors.process.top_memory must not be negative")
	}
	for _, name := range c.Collectors.Process.MonitoredProcesses {
		if strings.TrimSpace(name) == "" {
			return errors.New("collectors.process.monitored_processes must not contain empty names")
		}
	}
	if c.Updates.Enabled && strings.TrimSpace(c.Updates.Interval) == "" {
		return errors.New("updates.interval is required when automatic updates are enabled")
	}
	if c.Updates.Interval != "" {
		if _, err := UpdateInterval(c.Updates.Interval); err != nil {
			return err
		}
	}

	return nil
}

func UpdateInterval(value string) (time.Duration, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1h":
		return time.Hour, nil
	case "6h":
		return 6 * time.Hour, nil
	case "12h":
		return 12 * time.Hour, nil
	case "24h":
		return 24 * time.Hour, nil
	case "weekly":
		return 7 * 24 * time.Hour, nil
	case "monthly":
		return 30 * 24 * time.Hour, nil
	default:
		return 0, errors.New(
			"updates.interval must be one of 1h, 6h, 12h, 24h, weekly, or monthly",
		)
	}
}

func validateBaseURL(rawURL string) error {
	if strings.TrimSpace(rawURL) == "" {
		return errors.New("server.base_url is required")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("server.base_url is invalid: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("server.base_url must be an absolute HTTP or HTTPS URL")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("server.base_url must not contain a query or fragment")
	}

	return nil
}

func validateAPIEndpoint(name string, rawEndpoint string) error {
	endpoint := strings.TrimSpace(rawEndpoint)
	if endpoint == "" {
		return nil
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("server.api_endpoints.%s must be a relative path without query or fragment", name)
	}
	if strings.HasPrefix(endpoint, "//") {
		return fmt.Errorf("server.api_endpoints.%s must not start with //", name)
	}
	return nil
}
