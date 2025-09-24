package dnsserver

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/miekg/dns"
	"log/slog"

	"server/internal/mapping"
)

type stubAddr struct{}

func (stubAddr) Network() string { return "udp" }
func (stubAddr) String() string  { return "127.0.0.1:12345" }

type recorder struct {
	msg *dns.Msg
}

func (r *recorder) LocalAddr() net.Addr       { return stubAddr{} }
func (r *recorder) RemoteAddr() net.Addr      { return stubAddr{} }
func (r *recorder) WriteMsg(m *dns.Msg) error { r.msg = m; return nil }
func (r *recorder) Write([]byte) (int, error) { return 0, nil }
func (r *recorder) Close() error              { return nil }
func (r *recorder) TsigStatus() error         { return nil }
func (r *recorder) TsigTimersOnly(bool)       {}
func (r *recorder) Hijack()                   {}

func TestHandleDNS_ARecord(t *testing.T) {
	dir := t.TempDir()
	mapPath := filepath.Join(dir, "hosts.txt")
	contents := "example.internal 192.0.2.1 60\n"
	if err := os.WriteFile(mapPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("failed to write map file: %v", err)
	}

	store, err := mapping.LoadFile(mapPath)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	srv := &Server{logger: logger}
	srv.store.Store(store)

	req := new(dns.Msg)
	req.SetQuestion("example.internal.", dns.TypeA)

	rec := &recorder{}
	srv.handleDNS(rec, req)

	if rec.msg == nil {
		t.Fatalf("no response written")
	}
	if rec.msg.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected success rcode, got %s", dns.RcodeToString[rec.msg.Rcode])
	}
	if len(rec.msg.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(rec.msg.Answer))
	}
	if a, ok := rec.msg.Answer[0].(*dns.A); !ok || !a.A.Equal(net.IPv4(192, 0, 2, 1)) {
		t.Fatalf("unexpected answer: %#v", rec.msg.Answer[0])
	}
}

func TestHandleDNS_WildcardARecord(t *testing.T) {
	dir := t.TempDir()
	mapPath := filepath.Join(dir, "hosts.txt")
	contents := "*.internal 192.0.2.50 90\n"
	if err := os.WriteFile(mapPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("failed to write map file: %v", err)
	}

	store, err := mapping.LoadFile(mapPath)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	srv := &Server{logger: logger}
	srv.store.Store(store)

	req := new(dns.Msg)
	req.SetQuestion("svc.internal.", dns.TypeA)

	rec := &recorder{}
	srv.handleDNS(rec, req)

	if rec.msg == nil {
		t.Fatalf("no response written")
	}
	if rec.msg.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected success rcode, got %s", dns.RcodeToString[rec.msg.Rcode])
	}
	if len(rec.msg.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(rec.msg.Answer))
	}
	a, ok := rec.msg.Answer[0].(*dns.A)
	if !ok {
		t.Fatalf("unexpected answer type: %#v", rec.msg.Answer[0])
	}
	if a.Hdr.Name != "svc.internal." {
		t.Fatalf("answer name = %q, want svc.internal.", a.Hdr.Name)
	}
	if a.Hdr.Ttl != 90 {
		t.Fatalf("answer TTL = %d, want 90", a.Hdr.Ttl)
	}
	if !a.A.Equal(net.IPv4(192, 0, 2, 50)) {
		t.Fatalf("unexpected answer IP: %v", a.A)
	}
}

func TestHandleDNS_NXDOMAIN(t *testing.T) {
	dir := t.TempDir()
	mapPath := filepath.Join(dir, "hosts.txt")
	if err := os.WriteFile(mapPath, []byte(""), 0o600); err != nil {
		t.Fatalf("failed to write map file: %v", err)
	}

	store, err := mapping.LoadFile(mapPath)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	srv := &Server{logger: logger}
	srv.store.Store(store)

	req := new(dns.Msg)
	req.SetQuestion("missing.internal.", dns.TypeA)

	rec := &recorder{}
	srv.handleDNS(rec, req)

	if rec.msg == nil {
		t.Fatalf("no response written")
	}
	if rec.msg.Rcode != dns.RcodeNameError {
		t.Fatalf("expected NXDOMAIN, got %s", dns.RcodeToString[rec.msg.Rcode])
	}
	if len(rec.msg.Answer) != 0 {
		t.Fatalf("expected no answers, got %d", len(rec.msg.Answer))
	}
}

func TestServe_StartStop(t *testing.T) {
	dir := t.TempDir()
	mapPath := filepath.Join(dir, "hosts.txt")
	if err := os.WriteFile(mapPath, []byte("example.internal 192.0.2.1\n"), 0o600); err != nil {
		t.Fatalf("failed to write map file: %v", err)
	}

	// Some sandboxes disallow binding sockets; detect and skip if so.
	probe, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("skipping Serve_StartStop: unable to bind UDP socket: %v", err)
	}
	probe.Close()

	store, err := mapping.LoadFile(mapPath)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))

	udpConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create UDP listener: %v", err)
	}
	defer udpConn.Close()

	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create TCP listener: %v", err)
	}
	defer tcpListener.Close()

	srv, err := NewWithListeners(udpConn, tcpListener, store, logger)
	if err != nil {
		t.Fatalf("NewWithListeners returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ctx)
	}()

	// Allow server to start and then cancel context.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != context.Canceled && err != nil {
			t.Fatalf("Serve returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not exit in time")
	}
}

func TestNewWithListenersValidation(t *testing.T) {
	dir := t.TempDir()
	mapPath := filepath.Join(dir, "hosts.txt")
	if err := os.WriteFile(mapPath, []byte("example.internal 192.0.2.1\n"), 0o600); err != nil {
		t.Fatalf("failed to write map file: %v", err)
	}

	store, err := mapping.LoadFile(mapPath)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))

	if _, err := NewWithListeners(nil, nil, store, logger); err == nil {
		t.Fatalf("expected error when listeners missing")
	}

	udpConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("skipping NewWithListeners validation: unable to bind UDP socket: %v", err)
	}
	defer udpConn.Close()

	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		udpConn.Close()
		t.Skipf("skipping NewWithListeners validation: unable to bind TCP socket: %v", err)
	}
	defer tcpListener.Close()

	srv, err := NewWithListeners(udpConn, tcpListener, store, logger)
	if err != nil {
		t.Fatalf("NewWithListeners returned error: %v", err)
	}

	if srv == nil {
		t.Fatalf("expected server instance")
	}
}

func TestUpdateStoreSwapsRecords(t *testing.T) {
	dir := t.TempDir()
	initialPath := filepath.Join(dir, "hosts1.txt")
	updatedPath := filepath.Join(dir, "hosts2.txt")

	if err := os.WriteFile(initialPath, []byte("example.internal 192.0.2.1\n"), 0o600); err != nil {
		t.Fatalf("failed to write initial map: %v", err)
	}
	if err := os.WriteFile(updatedPath, []byte("example.internal 198.51.100.2\n"), 0o600); err != nil {
		t.Fatalf("failed to write updated map: %v", err)
	}

	initialStore, err := mapping.LoadFile(initialPath)
	if err != nil {
		t.Fatalf("LoadFile initial: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	srv := &Server{logger: logger}
	srv.store.Store(initialStore)

	req := new(dns.Msg)
	req.SetQuestion("example.internal.", dns.TypeA)

	initalRec := &recorder{}
	srv.handleDNS(initalRec, req)
	if initalRec.msg == nil || len(initalRec.msg.Answer) != 1 {
		t.Fatalf("expected initial answer, got %#v", initalRec.msg)
	}
	if a, ok := initalRec.msg.Answer[0].(*dns.A); !ok || !a.A.Equal(net.IPv4(192, 0, 2, 1)) {
		t.Fatalf("initial answer mismatch: %#v", initalRec.msg.Answer[0])
	}

	updatedStore, err := mapping.LoadFile(updatedPath)
	if err != nil {
		t.Fatalf("LoadFile updated: %v", err)
	}

	if err := srv.UpdateStore(updatedStore); err != nil {
		t.Fatalf("UpdateStore: %v", err)
	}

	updatedRec := &recorder{}
	srv.handleDNS(updatedRec, req)
	if updatedRec.msg == nil || len(updatedRec.msg.Answer) != 1 {
		t.Fatalf("expected updated answer, got %#v", updatedRec.msg)
	}
	if a, ok := updatedRec.msg.Answer[0].(*dns.A); !ok || !a.A.Equal(net.IPv4(198, 51, 100, 2)) {
		t.Fatalf("updated answer mismatch: %#v", updatedRec.msg.Answer[0])
	}
}
