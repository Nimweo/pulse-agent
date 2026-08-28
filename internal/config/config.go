package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/nimweo/pulse-agent/internal/model"
	"gopkg.in/yaml.v3"
)

const (
	DefaultPath = "configs/config.yaml"
	ExamplePath = "configs/config.example.yaml"
)

func Load(path string) (*model.Config, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf(
				"configuration file %q does not exist; create it by copying %q, then update its settings",
				path,
				ExamplePath,
			)
		}

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
	if c.Server.URL == "" {
		return errors.New("server.url is required")
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
	if c.Collectors.System.Enabled && c.Collectors.System.Interval <= 0 {
		return errors.New("collectors.system.interval must be greater than zero when system collection is enabled")
	}
	if c.Collectors.Disk.Enabled && c.Collectors.Disk.Interval <= 0 {
		return errors.New("collectors.disk.interval must be greater than zero when disk collection is enabled")
	}
	if c.Collectors.Network.Enabled && c.Collectors.Network.Interval <= 0 {
		return errors.New("collectors.network.interval must be greater than zero when network collection is enabled")
	}

	return nil
}
