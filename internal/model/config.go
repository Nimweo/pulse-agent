package model

type Config struct {
	Configured bool             `yaml:"configured"`
	Server     ServerConfig     `yaml:"server"`
	Agent      AgentConfig      `yaml:"agent"`
	Intervals  IntervalsConfig  `yaml:"intervals"`
	Logging    LoggingConfig    `yaml:"logging"`
	Collectors CollectorsConfig `yaml:"collectors"`
	Transport  TransportConfig  `yaml:"transport"`
	Buffer     BufferConfig     `yaml:"buffer"`
}

type ServerConfig struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
	Timeout int    `yaml:"timeout"`
}

type AgentConfig struct {
	Hostname string `yaml:"hostname"`
}

type IntervalsConfig struct {
	Collect int `yaml:"collect"`
	Send    int `yaml:"send"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type CollectorConfig struct {
	Enabled  bool `yaml:"enabled"`
	Interval int  `yaml:"interval"`
}

type CPUCollectorConfig struct {
	Enabled  bool `yaml:"enabled"`
	Interval int  `yaml:"interval"`
	PerCPU   bool `yaml:"per_cpu"`
}

type CollectorsConfig struct {
	System  CollectorConfig    `yaml:"system"`
	Load    CollectorConfig    `yaml:"load"`
	CPU     CPUCollectorConfig `yaml:"cpu"`
	Memory  CollectorConfig    `yaml:"memory"`
	Disk    CollectorConfig    `yaml:"disk"`
	Network CollectorConfig    `yaml:"network"`
	GPU     CollectorConfig    `yaml:"gpu"`
}

type TransportConfig struct {
	Compression  bool `yaml:"compression"`
	MaxRetries   int  `yaml:"max_retries"`
	RetryBackoff int  `yaml:"retry_backoff"`
}

type BufferConfig struct {
	MaxSize          int  `yaml:"max_size"`
	DiskSpoolEnabled bool `yaml:"disk_spool_enabled"`
}
