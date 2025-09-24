package mapping

import (
	"bufio"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultTTLSeconds = 300
)

// Record represents a single DNS resource record sourced from the mapping file.
type Record struct {
	Name string
	Addr netip.Addr
	TTL  uint32
}

// Store is an immutable collection of parsed records keyed by hostname.
type Store struct {
	records     map[string]entry
	source      string
	loadedAt    time.Time
	lineCount   int
	recordCount int
}

type entry struct {
	ipv4 []Record
	ipv6 []Record
}

// Metadata returns the file path, load time, and counts for logging without exposing internals.
func (s *Store) Metadata() (source string, loadedAt time.Time, lines int, records int) {
	if s == nil {
		return "", time.Time{}, 0, 0
	}
	return s.source, s.loadedAt, s.lineCount, s.recordCount
}

// IPv4 returns a copy of all IPv4 records for the given hostname.
func (s *Store) IPv4(name string) []Record {
	return s.lookup(name).ipv4
}

// IPv6 returns a copy of all IPv6 records for the given hostname.
func (s *Store) IPv6(name string) []Record {
	return s.lookup(name).ipv6
}

// Exists returns true if the given hostname has any records defined.
func (s *Store) Exists(name string) bool {
	if s == nil {
		return false
	}
	key, err := normalizeHostname(name)
	if err != nil {
		return false
	}
	_, ok := s.records[key]
	return ok
}

func (s *Store) lookup(name string) entry {
	if s == nil {
		return entry{}
	}
	key, err := normalizeHostname(name)
	if err != nil {
		return entry{}
	}
	rec := s.records[key]
	out := entry{
		ipv4: make([]Record, len(rec.ipv4)),
		ipv6: make([]Record, len(rec.ipv6)),
	}
	copy(out.ipv4, rec.ipv4)
	copy(out.ipv6, rec.ipv6)
	return out
}

// LoadFile reads and parses the mapping file into an immutable Store.
func LoadFile(path string) (*Store, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open mapping file: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	records := make(map[string]entry)
	lineNumber := 0
	recordCount := 0

	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		parsed, ok, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNumber, err)
		}
		if !ok {
			continue
		}

		host, addr, ttl, err := parsed.toRecord()
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNumber, err)
		}

		existing := records[host]
		if addr.Is4() {
			existing.ipv4 = append(existing.ipv4, Record{Name: host, Addr: addr, TTL: ttl})
		} else {
			existing.ipv6 = append(existing.ipv6, Record{Name: host, Addr: addr, TTL: ttl})
		}
		records[host] = existing
		recordCount++
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan mapping file: %w", err)
	}

	store := &Store{
		records:     records,
		source:      path,
		loadedAt:    time.Now().UTC(),
		lineCount:   lineNumber,
		recordCount: recordCount,
	}
	return store, nil
}

type parsedLine struct {
	hostname string
	ip       string
	ttl      string
}

func parseLine(line string) (parsedLine, bool, error) {
	cleaned := stripComment(line)
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return parsedLine{}, false, nil
	}

	fields := strings.Fields(cleaned)
	if len(fields) < 2 {
		return parsedLine{}, false, fmt.Errorf("expected at least hostname and IP, got %d field(s)", len(fields))
	}

	result := parsedLine{
		hostname: fields[0],
		ip:       fields[1],
	}
	if len(fields) >= 3 {
		result.ttl = fields[2]
	}
	return result, true, nil
}

func (p parsedLine) toRecord() (string, netip.Addr, uint32, error) {
	host, err := normalizeHostname(p.hostname)
	if err != nil {
		return "", netip.Addr{}, 0, err
	}

	addr, err := netip.ParseAddr(p.ip)
	if err != nil {
		return "", netip.Addr{}, 0, fmt.Errorf("invalid IP address %q", p.ip)
	}

	ttl := uint64(defaultTTLSeconds)
	if p.ttl != "" {
		parsed, err := strconv.ParseUint(p.ttl, 10, 32)
		if err != nil {
			return "", netip.Addr{}, 0, fmt.Errorf("invalid TTL %q", p.ttl)
		}
		ttl = parsed
	}

	return host, addr, uint32(ttl), nil
}

func stripComment(line string) string {
	if idx := strings.IndexRune(line, '#'); idx >= 0 {
		return line[:idx]
	}
	return line
}

func normalizeHostname(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", errors.New("hostname is empty")
	}

	trimmed = strings.TrimSuffix(trimmed, ".")
	lowered := strings.ToLower(trimmed)

	if len(lowered) == 0 {
		return "", errors.New("hostname is empty")
	}
	if len(lowered) > 253 {
		return "", fmt.Errorf("hostname %q exceeds 253 characters", lowered)
	}

	labels := strings.Split(lowered, ".")
	for _, label := range labels {
		if err := validateLabel(label); err != nil {
			return "", err
		}
	}

	return lowered, nil
}

func validateLabel(label string) error {
	if label == "" {
		return errors.New("hostname contains empty label")
	}
	if len(label) > 63 {
		return fmt.Errorf("hostname label %q exceeds 63 characters", label)
	}

	for i, r := range label {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '-' {
			// allow hyphen but not at start/end
			if i == 0 || i == len(label)-1 {
				return fmt.Errorf("label %q cannot start or end with a hyphen", label)
			}
			continue
		}
		return fmt.Errorf("unsupported character %q in hostname label %q", r, label)
	}

	return nil
}
