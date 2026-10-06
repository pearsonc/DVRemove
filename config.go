package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// TranscodeConfig holds GPU transcoding settings for Profile 5 files.
type TranscodeConfig struct {
	Encoder     string `yaml:"encoder"`      // "auto", "nvenc", "vaapi", "software"
	Quality     int    `yaml:"quality"`      // CRF/CQ value: 18-28, default 20
	Preset      string `yaml:"preset"`       // slow/medium/fast
	VAAPIDevice string `yaml:"vaapi_device"` // VAAPI render device, default: /dev/dri/renderD128
}

// ParallelConfig holds parallel processing settings.
type ParallelConfig struct {
	MaxWorkers int `yaml:"max_workers"` // Number of concurrent conversions (1-8)
}

// Config holds application configuration.
type Config struct {
	InputDir  string          `yaml:"input_dir"`
	OutputDir string          `yaml:"output_dir"`
	LogDir    string          `yaml:"log_dir"`
	Transcode TranscodeConfig `yaml:"transcode"`
	Parallel  ParallelConfig  `yaml:"parallel"`
}

// LoadConfig reads configuration from a YAML file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	cfg.setDefaults()

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

func (c *Config) setDefaults() {
	if c.Transcode.Encoder == "" {
		c.Transcode.Encoder = "auto"
	}
	if c.Transcode.Quality == 0 {
		c.Transcode.Quality = 20
	}
	if c.Transcode.Preset == "" {
		c.Transcode.Preset = "slow"
	}
	if c.Transcode.VAAPIDevice == "" {
		c.Transcode.VAAPIDevice = "/dev/dri/renderD128"
	}
	if c.Parallel.MaxWorkers == 0 {
		c.Parallel.MaxWorkers = 1
	}
}

func (c *Config) validate() error {
	if c.InputDir == "" {
		return fmt.Errorf("input_dir is required")
	}
	if c.OutputDir == "" {
		return fmt.Errorf("output_dir is required")
	}
	if c.LogDir == "" {
		return fmt.Errorf("log_dir is required")
	}

	// Verify directories exist
	for _, dir := range []string{c.InputDir, c.OutputDir, c.LogDir} {
		info, err := os.Stat(dir)
		if err != nil {
			return fmt.Errorf("directory %s does not exist: %w", dir, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", dir)
		}
	}

	// Validate transcode config
	validEncoders := map[string]bool{"auto": true, "nvenc": true, "vaapi": true, "software": true}
	if !validEncoders[c.Transcode.Encoder] {
		return fmt.Errorf("invalid encoder %q, must be one of: auto, nvenc, vaapi, software", c.Transcode.Encoder)
	}
	if c.Transcode.Quality < 0 || c.Transcode.Quality > 51 {
		return fmt.Errorf("quality must be between 0 and 51, got %d", c.Transcode.Quality)
	}
	validPresets := map[string]bool{"slow": true, "medium": true, "fast": true}
	if !validPresets[c.Transcode.Preset] {
		return fmt.Errorf("invalid preset %q, must be one of: slow, medium, fast", c.Transcode.Preset)
	}

	if c.Parallel.MaxWorkers < 1 || c.Parallel.MaxWorkers > 8 {
		return fmt.Errorf("parallel.max_workers must be between 1 and 8, got %d", c.Parallel.MaxWorkers)
	}

	return nil
}
