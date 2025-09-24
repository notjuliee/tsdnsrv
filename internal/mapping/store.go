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
	wildcards   map[string]entry
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
	if _, ok := s.records[key]; ok {
		return true
	}
	_, ok := s.lookupWildcard(key)
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
	if rec, ok := s.records[key]; ok {
		return copyEntry(rec)
	}
	if rec, ok := s.lookupWildcard(key); ok {
		out := copyEntry(rec)
		for i := range out.ipv4 {
			out.ipv4[i].Name = key
		}
		for i := range out.ipv6 {
			out.ipv6[i].Name = key
		}
		return out
	}
	return entry{}
}

func (s *Store) lookupWildcard(key string) (entry, bool) {
	if s == nil || len(s.wildcards) == 0 {
		return entry{}, false
	}
	labels := strings.Split(key, ".")
	if len(labels) < 2 {
		return entry{}, false
	}
	suffix := strings.Join(labels[1:], ".")
	rec, ok := s.wildcards[suffix]
	return rec, ok
}

func copyEntry(src entry) entry {
	out := entry{
		ipv4: make([]Record, len(src.ipv4)),
		ipv6: make([]Record, len(src.ipv6)),
	}
	copy(out.ipv4, src.ipv4)
	copy(out.ipv6, src.ipv6)
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
	wildcards := make(map[string]entry)
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

		spec, err := parsed.toRecord()
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNumber, err)
		}

		rec := Record{Name: spec.host, Addr: spec.addr, TTL: spec.ttl}

		if spec.wildcard {
			existing := wildcards[spec.suffix]
			if spec.addr.Is4() {
				existing.ipv4 = append(existing.ipv4, rec)
			} else {
				existing.ipv6 = append(existing.ipv6, rec)
			}
			wildcards[spec.suffix] = existing
		} else {
			existing := records[spec.host]
			if spec.addr.Is4() {
				existing.ipv4 = append(existing.ipv4, rec)
			} else {
				existing.ipv6 = append(existing.ipv6, rec)
			}
			records[spec.host] = existing
		}
		recordCount++
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan mapping file: %w", err)
	}

	store := &Store{
		records:     records,
		wildcards:   wildcards,
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

type recordSpec struct {
	host     string
	addr     netip.Addr
	ttl      uint32
	wildcard bool
	suffix   string
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

func (p parsedLine) toRecord() (recordSpec, error) {
	host, wildcard, suffix, err := normalizeMappingHostname(p.hostname)
	if err != nil {
		return recordSpec{}, err
	}

	addr, err := netip.ParseAddr(p.ip)
	if err != nil {
		return recordSpec{}, fmt.Errorf("invalid IP address %q", p.ip)
	}

	ttl := uint64(defaultTTLSeconds)
	if p.ttl != "" {
		parsed, err := strconv.ParseUint(p.ttl, 10, 32)
		if err != nil {
			return recordSpec{}, fmt.Errorf("invalid TTL %q", p.ttl)
		}
		ttl = parsed
	}

	return recordSpec{
		host:     host,
		addr:     addr,
		ttl:      uint32(ttl),
		wildcard: wildcard,
		suffix:   suffix,
	}, nil
}

func stripComment(line string) string {
	if idx := strings.IndexRune(line, '#'); idx >= 0 {
		return line[:idx]
	}
	return line
}

func normalizeMappingHostname(name string) (normalized string, wildcard bool, suffix string, err error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", false, "", errors.New("hostname is empty")
	}

	trimmed = strings.TrimSuffix(trimmed, ".")
	lowered := strings.ToLower(trimmed)

	if strings.HasPrefix(lowered, "*.") {
		if strings.Count(lowered, "*") > 1 {
			return "", false, "", errors.New("wildcard hostname may contain only one '*' label")
		}
		suffixPart := strings.TrimPrefix(lowered, "*.")
		if suffixPart == "" {
			return "", false, "", errors.New("wildcard hostname must include a suffix")
		}

		normalizedSuffix, err := normalizeHostname(suffixPart)
		if err != nil {
			return "", false, "", err
		}
		return "*." + normalizedSuffix, true, normalizedSuffix, nil
	}

	if strings.Contains(lowered, "*") {
		return "", false, "", errors.New("wildcard '*' is only supported as the entire left-most label")
	}

	host, err := normalizeHostname(trimmed)
	if err != nil {
		return "", false, "", err
	}
	return host, false, "", nil
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
