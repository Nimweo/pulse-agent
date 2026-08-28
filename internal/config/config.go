package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

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

	return nil
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
