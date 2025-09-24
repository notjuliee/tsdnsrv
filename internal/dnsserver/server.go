package dnsserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"

	"server/internal/mapping"
)

// Server provides UDP and TCP DNS listeners bound to a configurable address.
type Server struct {
	addr        string
	store       atomic.Pointer[mapping.Store]
	logger      *slog.Logger
	udpConn     net.PacketConn
	tcpListener net.Listener
}

// NewWithListeners constructs a DNS server configured with pre-bound UDP/TCP endpoints.
func NewWithListeners(udpConn net.PacketConn, tcpListener net.Listener, store *mapping.Store, logger *slog.Logger) (*Server, error) {
	if udpConn == nil {
		return nil, errors.New("udp packet connection must be provided")
	}
	if tcpListener == nil {
		return nil, errors.New("tcp listener must be provided")
	}

	srv, err := newBase(store, logger)
	if err != nil {
		return nil, err
	}

	srv.udpConn = udpConn
	srv.tcpListener = tcpListener
	return srv, nil
}

// UpdateStore swaps the active mapping store used to answer DNS queries.
func (s *Server) UpdateStore(store *mapping.Store) error {
	if store == nil {
		return errors.New("mapping store must be provided")
	}
	s.store.Store(store)
	return nil
}

func newBase(store *mapping.Store, logger *slog.Logger) (*Server, error) {
	if store == nil {
		return nil, errors.New("mapping store must be provided")
	}
	if logger == nil {
		logger = slog.Default()
	}

	srv := &Server{
		logger: logger,
	}
	srv.store.Store(store)
	return srv, nil
}

// Serve starts UDP and TCP DNS listeners and blocks until context cancellation or error.
func (s *Server) Serve(ctx context.Context) error {
	mux := dns.NewServeMux()
	mux.HandleFunc(".", s.handleDNS)

	udpServer := &dns.Server{Handler: mux, Net: "udp"}
	tcpServer := &dns.Server{Handler: mux, Net: "tcp"}

	var udpActivate bool
	var tcpActivate bool

	switch {
	case s.udpConn != nil:
		udpServer.PacketConn = s.udpConn
		udpServer.Addr = s.udpConn.LocalAddr().String()
		udpActivate = true
	case s.addr != "":
		udpServer.Addr = s.addr
	default:
		return errors.New("no UDP endpoint configured")
	}

	switch {
	case s.tcpListener != nil:
		tcpServer.Listener = s.tcpListener
		tcpServer.Addr = s.tcpListener.Addr().String()
		tcpActivate = true
	case s.addr != "":
		tcpServer.Addr = s.addr
	default:
		return errors.New("no TCP endpoint configured")
	}

	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	start := func(name string, srv *dns.Server, activate bool) {
		go func() {
			defer wg.Done()
			var err error
			if activate {
				err = srv.ActivateAndServe()
			} else {
				err = srv.ListenAndServe()
			}
			if err != nil {
				selErr := fmt.Errorf("%s listener: %w", name, err)
				select {
				case errCh <- selErr:
				default:
				}
			}
		}()
	}

	start("udp", udpServer, udpActivate)
	start("tcp", tcpServer, tcpActivate)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	shutdown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := udpServer.ShutdownContext(shutdownCtx); err != nil {
			s.logger.Warn("UDP shutdown error", slog.Any("error", err))
		}
		if err := tcpServer.ShutdownContext(shutdownCtx); err != nil {
			s.logger.Warn("TCP shutdown error", slog.Any("error", err))
		}
	}

	select {
	case <-ctx.Done():
		shutdown()
		<-done
		return ctx.Err()
	case err := <-errCh:
		shutdown()
		<-done
		return err
	case <-done:
		return nil
	}
}

func (s *Server) handleDNS(w dns.ResponseWriter, r *dns.Msg) {
	start := time.Now()

	response := new(dns.Msg)
	response.SetReply(r)
	response.Authoritative = true

	store := s.store.Load()
	if store == nil {
		// This shouldn't happen in normal operation, but as a fallback, treat as empty.
		store = &mapping.Store{}
	}

	if len(r.Question) == 0 {
		response.Rcode = dns.RcodeFormatError
		s.writeResponse(w, response, start, r.Question)
		return
	}

	// Assume success unless we determine otherwise.
	response.Rcode = dns.RcodeSuccess

	// We only answer one question per query.
	q := r.Question[0]
	name := strings.TrimSuffix(strings.ToLower(q.Name), ".")

	if !store.Exists(name) {
		response.Rcode = dns.RcodeNameError
		s.writeResponse(w, response, start, r.Question)
		return
	}

	switch q.Qtype {
	case dns.TypeA:
		records := store.IPv4(name)
		for _, rec := range records {
			if ip := ipFromRecord(rec, false); ip != nil {
				rr := &dns.A{
					Hdr: dns.RR_Header{
						Name:   dns.Fqdn(rec.Name),
						Rrtype: dns.TypeA,
						Class:  dns.ClassINET,
						Ttl:    rec.TTL,
					},
					A: ip,
				}
				response.Answer = append(response.Answer, rr)
			}
		}
	case dns.TypeAAAA:
		records := store.IPv6(name)
		for _, rec := range records {
			if ip := ipFromRecord(rec, true); ip != nil {
				rr := &dns.AAAA{
					Hdr: dns.RR_Header{
						Name:   dns.Fqdn(rec.Name),
						Rrtype: dns.TypeAAAA,
						Class:  dns.ClassINET,
						Ttl:    rec.TTL,
					},
					AAAA: ip,
				}
				response.Answer = append(response.Answer, rr)
			}
		}
	default:
		// For unsupported qtypes where the name *does* exist, we return NOERROR with an empty answer.
		// This signals that the name is valid, but the requested record type is not available.
	}

	s.writeResponse(w, response, start, r.Question)
}

func (s *Server) writeResponse(w dns.ResponseWriter, response *dns.Msg, start time.Time, questions []dns.Question) {
	if err := w.WriteMsg(response); err != nil {
		s.logger.Error("failed to write DNS response", slog.Any("error", err))
	}

	s.logger.Debug(
		"dns query",
		slog.String("remote", w.RemoteAddr().String()),
		slog.String("questions", formatQuestions(questions)),
		slog.String("rcode", dns.RcodeToString[response.Rcode]),
		slog.Int("answers", len(response.Answer)),
		slog.Duration("duration", time.Since(start)),
	)
}

func ipFromRecord(rec mapping.Record, isIPv6 bool) net.IP {
	if !rec.Addr.IsValid() || rec.Addr.Is4() == isIPv6 {
		return nil
	}
	return rec.Addr.AsSlice()
}

func formatQuestions(qs []dns.Question) string {
	if len(qs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(qs))
	for _, q := range qs {
		parts = append(parts, fmt.Sprintf("%s %s", q.Name, dns.TypeToString[q.Qtype]))
	}
	return strings.Join(parts, ", ")
}
