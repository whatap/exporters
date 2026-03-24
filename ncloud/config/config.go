package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds all exporter configuration.
type Config struct {
	NCloud   NCloudConfig      `yaml:"ncloud"`
	Exporter ExporterConfig    `yaml:"exporter"`
	Namespaces []NamespaceConfig `yaml:"namespaces"`
}

// NCloudConfig holds NCloud API credentials.
type NCloudConfig struct {
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
}

// ExporterConfig holds exporter server settings.
type ExporterConfig struct {
	ListenAddress  string `yaml:"listen_address"`
	MetricsPath    string `yaml:"metrics_path"`
	ScrapeInterval int    `yaml:"scrape_interval"`
}

// NamespaceConfig defines a monitored NCloud namespace.
type NamespaceConfig struct {
	Name    string   `yaml:"name"`
	Enabled bool     `yaml:"enabled"`
	Regions []string `yaml:"regions"`
}

// Load reads and parses a YAML config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	cfg := &Config{
		Exporter: ExporterConfig{
			ListenAddress:  ":9850",
			MetricsPath:    "/metrics",
			ScrapeInterval: 300,
		},
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Override from environment variables if set
	if v := os.Getenv("NCLOUD_ACCESS_KEY"); v != "" {
		cfg.NCloud.AccessKey = v
	}
	if v := os.Getenv("NCLOUD_SECRET_KEY"); v != "" {
		cfg.NCloud.SecretKey = v
	}

	if cfg.NCloud.AccessKey == "" || cfg.NCloud.SecretKey == "" {
		return nil, fmt.Errorf("ncloud access_key and secret_key are required")
	}

	return cfg, nil
}
