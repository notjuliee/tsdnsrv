package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_BasicJSON(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	contents := `{
        "hostname": "dns-node",
        "authKey": "tskey-123",
        "stateDir": "/var/lib/tsdnsrv",
        "mapFile": "/etc/tsdnsrv/hosts.txt",
        "listenAddress": "0.0.0.0:53",
        "logLevel": "WARNING",
        "controlURL": "https://control.example.com",
        "ephemeral": false
    }`
	if err := os.WriteFile(cfgPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.ConfigPath != cfgPath {
		t.Errorf("ConfigPath = %q, want %q", cfg.ConfigPath, cfgPath)
	}
	if cfg.Hostname != "dns-node" {
		t.Errorf("Hostname = %q, want dns-node", cfg.Hostname)
	}
	if cfg.AuthKey != "tskey-123" {
		t.Errorf("AuthKey = %q, want tskey-123", cfg.AuthKey)
	}
	if cfg.StateDir != "/var/lib/tsdnsrv" {
		t.Errorf("StateDir = %q, want /var/lib/tsdnsrv", cfg.StateDir)
	}
	if cfg.MapFile != "/etc/tsdnsrv/hosts.txt" {
		t.Errorf("MapFile = %q, want /etc/tsdnsrv/hosts.txt", cfg.MapFile)
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("LogLevel = %q, want warn", cfg.LogLevel)
	}
	if cfg.ListenAddr != "0.0.0.0:53" {
		t.Errorf("ListenAddr = %q, want 0.0.0.0:53", cfg.ListenAddr)
	}
	if cfg.ControlURL != "https://control.example.com" {
		t.Errorf("ControlURL = %q, want https://control.example.com", cfg.ControlURL)
	}
	if cfg.Ephemeral {
		t.Errorf("Ephemeral = true, want false")
	}
}

func TestLoad_DefaultsAndRelativePaths(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")

	if err := os.WriteFile(cfgPath, []byte(`{
        "hostname": "dns-node",
        "stateDir": "state",
        "mapFile": "hosts.txt"
    }`), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if !strings.HasSuffix(cfg.StateDir, "/state") {
		t.Errorf("StateDir = %q, expected to end with /state", cfg.StateDir)
	}
	if filepath.Dir(cfg.StateDir) != dir {
		t.Errorf("StateDir directory = %q, want %q", filepath.Dir(cfg.StateDir), dir)
	}
	if !strings.HasSuffix(cfg.MapFile, "/hosts.txt") {
		t.Errorf("MapFile = %q, expected to end with /hosts.txt", cfg.MapFile)
	}
	if filepath.Dir(cfg.MapFile) != dir {
		t.Errorf("MapFile directory = %q, want %q", filepath.Dir(cfg.MapFile), dir)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel default = %q, want info", cfg.LogLevel)
	}
	if cfg.Ephemeral {
		t.Errorf("Ephemeral default = true, want false")
	}
	if cfg.ListenAddr != ":53" {
		t.Errorf("ListenAddr default = %q, want :53", cfg.ListenAddr)
	}
}

func TestLoad_InvalidLogLevel(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")

	if err := os.WriteFile(cfgPath, []byte(`{
        "hostname": "dns-node",
        "authKey": "tskey",
        "mapFile": "hosts.txt",
        "logLevel": "chatty"
    }`), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for invalid log level, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported log level") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_MissingRequired(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")

	if err := os.WriteFile(cfgPath, []byte(`{"hostname": "dns-node"}`), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for missing values, got nil")
	}
	if !strings.Contains(err.Error(), "mapFile is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_EphemeralRequiresAuthKey(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")

	if err := os.WriteFile(cfgPath, []byte(`{
        "hostname": "dns-node",
        "mapFile": "hosts.txt",
        "ephemeral": true
    }`), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for ephemeral without auth key, got nil")
	}
	if !strings.Contains(err.Error(), "ephemeral nodes require an authKey") {
		t.Fatalf("unexpected error: %v", err)
	}
}
