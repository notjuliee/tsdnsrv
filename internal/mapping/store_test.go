package mapping

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFile_Basic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts.txt")
	data := strings.TrimSpace(`
# comment
example.com 192.0.2.1 120
www.example.com 2001:db8::1
example.com 192.0.2.2
`)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("failed to write temp mapping file: %v", err)
	}

	store, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}

	source, loadedAt, lines, records := store.Metadata()
	if source != path {
		t.Errorf("Metadata source = %q, want %q", source, path)
	}
	if loadedAt.IsZero() {
		t.Errorf("LoadedAt should be set")
	}
	if lines != 4 {
		t.Errorf("line count = %d, want 4", lines)
	}
	if records != 3 {
		t.Errorf("record count = %d, want 3", records)
	}

	ipv4 := store.IPv4("example.com")
	if len(ipv4) != 2 {
		t.Fatalf("example.com IPv4 count = %d, want 2", len(ipv4))
	}
	if ipv4[0].TTL != 120 {
		t.Errorf("first TTL = %d, want 120", ipv4[0].TTL)
	}
	if got := ipv4[0].Addr.String(); got != "192.0.2.1" {
		t.Errorf("first addr = %q, want 192.0.2.1", got)
	}
	if got := ipv4[1].Addr.String(); got != "192.0.2.2" {
		t.Errorf("second addr = %q, want 192.0.2.2", got)
	}

	ipv6 := store.IPv6("www.example.com.") // allow trailing dot
	if len(ipv6) != 1 {
		t.Fatalf("www.example.com IPv6 count = %d, want 1", len(ipv6))
	}
	if got := ipv6[0].Addr.String(); got != "2001:db8::1" {
		t.Errorf("ipv6 addr = %q, want 2001:db8::1", got)
	}
}

func TestLoadFile_InvalidLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.txt")
	data := "example.com\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("failed to write temp mapping file: %v", err)
	}

	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected error for missing IP, got nil")
	}
	if !strings.Contains(err.Error(), "expected at least hostname and IP") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestLoadFile_InvalidHostname(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad_host.txt")
	data := "-bad.example 192.0.2.1\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("failed to write temp mapping file: %v", err)
	}

	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected error for invalid hostname, got nil")
	}
	if !strings.Contains(err.Error(), "cannot start or end") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFile_ReturnsCopyOnLookup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "copy.txt")
	data := "example.com 192.0.2.1\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("failed to write temp mapping file: %v", err)
	}

	store, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}

	ipv4 := store.IPv4("example.com")
	if len(ipv4) != 1 {
		t.Fatalf("IPv4 len = %d, want 1", len(ipv4))
	}
	ipv4[0].Name = "mutated"

	ipv4Again := store.IPv4("example.com")
	if ipv4Again[0].Name == "mutated" {
		t.Fatalf("Store returned mutated slice")
	}
}
