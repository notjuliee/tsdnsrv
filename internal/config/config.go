package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultLogLevel = "info"

// Config captures runtime settings for the DNS service loaded from JSON.
type Config struct {
	ConfigPath string
	Hostname   string
	AuthKey    string
	StateDir   string
	MapFile    string
	LogLevel   string
	ControlURL string
	Ephemeral  bool
	ListenAddr string
}

// fileConfig mirrors the JSON structure while allowing optional values.
type fileConfig struct {
	Hostname   string `json:"hostname"`
	AuthKey    string `json:"authKey"`
	StateDir   string `json:"stateDir"`
	MapFile    string `json:"mapFile"`
	LogLevel   string `json:"logLevel"`
	ControlURL string `json:"controlURL"`
	Ephemeral  *bool  `json:"ephemeral"`
	ListenAddr string `json:"listenAddress"`
}

// Load reads and validates configuration from the provided JSON file path.
func Load(path string) (Config, error) {
	sanitized := strings.TrimSpace(path)
	if sanitized == "" {
		return Config{}, errors.New("config file path is required")
	}

	data, err := os.ReadFile(sanitized)
	if err != nil {
		return Config{}, fmt.Errorf("read config file: %w", err)
	}

	cfg, err := parseConfig(data)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", sanitized, err)
	}

	cfg.ConfigPath = sanitized

	if cfg.MapFile != "" && !filepath.IsAbs(cfg.MapFile) {
		cfg.MapFile = filepath.Clean(filepath.Join(filepath.Dir(sanitized), cfg.MapFile))
	}
	if cfg.StateDir != "" && !filepath.IsAbs(cfg.StateDir) {
		cfg.StateDir = filepath.Clean(filepath.Join(filepath.Dir(sanitized), cfg.StateDir))
	}

	if err := cfg.validate(); err != nil {
		return Config{}, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

func parseConfig(data []byte) (Config, error) {
	var fileCfg fileConfig
	if err := json.Unmarshal(data, &fileCfg); err != nil {
		return Config{}, fmt.Errorf("decode json: %w", err)
	}

	cfg := Config{
		Hostname:   strings.TrimSpace(fileCfg.Hostname),
		AuthKey:    strings.TrimSpace(fileCfg.AuthKey),
		StateDir:   strings.TrimSpace(fileCfg.StateDir),
		MapFile:    strings.TrimSpace(fileCfg.MapFile),
		LogLevel:   strings.TrimSpace(fileCfg.LogLevel),
		ControlURL: strings.TrimSpace(fileCfg.ControlURL),
		Ephemeral:  false,
	}

	if fileCfg.Ephemeral != nil {
		cfg.Ephemeral = *fileCfg.Ephemeral
	}

	if cfg.LogLevel == "" {
		cfg.LogLevel = defaultLogLevel
	}

	cfg.ListenAddr = strings.TrimSpace(fileCfg.ListenAddr)
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":53"
	}

	return cfg, nil
}

func (c *Config) validate() error {
	if c.MapFile == "" {
		return errors.New("mapFile is required")
	}
	if c.Hostname == "" {
		return errors.New("hostname is required")
	}
	if c.ListenAddr == "" {
		return errors.New("listenAddress is required")
	}

	if c.AuthKey == "" && c.Ephemeral {
		return errors.New("ephemeral nodes require an authKey; disable ephemeral for interactive login")
	}

	c.LogLevel = strings.ToLower(c.LogLevel)
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
		return nil
	case "warning":
		c.LogLevel = "warn"
		return nil
	default:
		return fmt.Errorf("unsupported log level %q", c.LogLevel)
	}
}
