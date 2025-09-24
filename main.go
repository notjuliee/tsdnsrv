package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"server/internal/config"
	"server/internal/dnsserver"
	"server/internal/mapping"

	"tailscale.com/tsnet"
)

func main() {
	configPath := flag.String("config", "", "path to JSON configuration file")
	flag.Parse()

	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "missing required --config path to JSON configuration file")
		os.Exit(2)
	}

	if len(flag.Args()) > 0 {
		fmt.Fprintf(os.Stderr, "unexpected positional arguments: %v\n", flag.Args())
		os.Exit(2)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(2)
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)
	ctx := context.Background()

	logger.InfoContext(ctx, "starting tsdnsrv", slog.String("config", cfg.ConfigPath), slog.String("hostname", cfg.Hostname), slog.String("map_file", cfg.MapFile), slog.String("state_dir", cfg.StateDir), slog.Bool("ephemeral", cfg.Ephemeral), slog.String("listen_addr", cfg.ListenAddr))

	store, err := mapping.LoadFile(cfg.MapFile)
	if err != nil {
		logger.ErrorContext(ctx, "failed to load mapping file", slog.String("path", cfg.MapFile), slog.Any("error", err))
		os.Exit(2)
	}

	logMappingMetadata(ctx, logger, store)

	if cfg.AuthKey == "" {
		logger.InfoContext(ctx, "no tailscale auth key configured; waiting for interactive login")
	}

	tsServer := &tsnet.Server{
		Hostname:   cfg.Hostname,
		AuthKey:    cfg.AuthKey,
		Ephemeral:  cfg.Ephemeral,
		ControlURL: cfg.ControlURL,
	}
	if cfg.StateDir != "" {
		tsServer.Dir = cfg.StateDir
	}
	tsServer.Logf = func(format string, args ...any) {
		logger.Debug(fmt.Sprintf(format, args...))
	}
	tsServer.UserLogf = func(format string, args ...any) {
		logger.InfoContext(ctx, fmt.Sprintf(format, args...))
	}

	status, err := tsServer.Up(ctx)
	if err != nil {
		logger.ErrorContext(ctx, "failed to connect to tailscale", slog.Any("error", err))
		os.Exit(1)
	}

	listenIP, listenPort, autoIP, err := resolveTailnetListenAddr(cfg.ListenAddr, status.TailscaleIPs)
	if err != nil {
		logger.ErrorContext(ctx, "invalid listen address", slog.String("listen_addr", cfg.ListenAddr), slog.Any("error", err))
		if closeErr := tsServer.Close(); closeErr != nil {
			logger.WarnContext(ctx, "tsnet close error", slog.Any("error", closeErr))
		}
		os.Exit(2)
	}

	listenAddr := net.JoinHostPort(listenIP, listenPort)
	logger.InfoContext(ctx, "resolved tailscale listener", slog.String("listen_addr", listenAddr), slog.Bool("auto_ip", autoIP))

	udpConn, err := tsServer.ListenPacket("udp", listenAddr)
	if err != nil {
		logger.ErrorContext(ctx, "failed to listen on tsnet UDP", slog.String("listen_addr", listenAddr), slog.Any("error", err))
		tsServer.Close()
		os.Exit(1)
	}

	tcpListener, err := tsServer.Listen("tcp", listenAddr)
	if err != nil {
		udpConn.Close()
		logger.ErrorContext(ctx, "failed to listen on tsnet TCP", slog.String("listen_addr", listenAddr), slog.Any("error", err))
		tsServer.Close()
		os.Exit(1)
	}

	dnsSrv, err := dnsserver.NewWithListeners(udpConn, tcpListener, store, logger)
	if err != nil {
		udpConn.Close()
		tcpListener.Close()
		logger.ErrorContext(ctx, "failed to construct DNS server", slog.Any("error", err))
		tsServer.Close()
		os.Exit(2)
	}

	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	defer func() {
		if err := tsServer.Close(); err != nil {
			logger.WarnContext(ctx, "tsnet close error", slog.Any("error", err))
		}
	}()

	reloadCh := make(chan os.Signal, 1)
	signal.Notify(reloadCh, syscall.SIGHUP)
	defer signal.Stop(reloadCh)

	go func() {
		for {
			select {
			case <-sigCtx.Done():
				return
			case <-reloadCh:
				logger.InfoContext(sigCtx, "reload requested", slog.String("map_file", cfg.MapFile))
				newStore, err := mapping.LoadFile(cfg.MapFile)
				if err != nil {
					logger.ErrorContext(sigCtx, "reload failed", slog.Any("error", err))
					continue
				}
				if err := dnsSrv.UpdateStore(newStore); err != nil {
					logger.ErrorContext(sigCtx, "store swap failed", slog.Any("error", err))
					continue
				}
				logMappingMetadata(sigCtx, logger, newStore)
			}
		}
	}()

	logger.InfoContext(sigCtx, "dns server listening", slog.String("listen_addr", listenAddr), slog.Bool("auto_ip", autoIP))

	if err := dnsSrv.Serve(sigCtx); err != nil && !errors.Is(err, context.Canceled) {
		logger.ErrorContext(ctx, "dns server exited with error", slog.Any("error", err))
		os.Exit(1)
	}

	logger.InfoContext(ctx, "dns server stopped")
}

func newLogger(level string) *slog.Logger {
	var slogLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		slogLevel = slog.LevelDebug
	case "info":
		slogLevel = slog.LevelInfo
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:     slogLevel,
		AddSource: true,
	})

	return slog.New(handler)
}

func resolveTailnetListenAddr(addr string, tailscaleIPs []netip.Addr) (string, string, bool, error) {
	ip, port, ipProvided, err := parseListenAddr(addr)
	if err != nil {
		return "", "", false, err
	}
	if !ipProvided {
		ip, err = selectTailnetIP(tailscaleIPs)
		if err != nil {
			return "", "", false, err
		}
	}
	if !ip.IsValid() {
		return "", "", false, errors.New("resolved listen IP is invalid")
	}
	return ip.String(), port, !ipProvided, nil
}

func parseListenAddr(addr string) (netip.Addr, string, bool, error) {
	addr = strings.TrimSpace(addr)
	var ip netip.Addr
	if addr == "" {
		return ip, "", false, errors.New("empty address")
	}

	var port string
	ipProvided := false
	if strings.Count(addr, ":") == 0 {
		port = addr
	} else {
		host, portPart, err := net.SplitHostPort(addr)
		if err != nil {
			return ip, "", false, err
		}
		host = strings.TrimSpace(host)
		if host != "" {
			parsedIP, parseErr := netip.ParseAddr(host)
			if parseErr != nil {
				return ip, "", false, fmt.Errorf("invalid listen host %q: %w", host, parseErr)
			}
			ip = parsedIP
			ipProvided = true
		}
		port = portPart
	}

	if port = strings.TrimSpace(port); port == "" {
		return ip, "", ipProvided, errors.New("missing port")
	}

	normalizedPort, err := normalizePort(port)
	if err != nil {
		return ip, "", ipProvided, err
	}

	return ip, normalizedPort, ipProvided, nil
}

func normalizePort(port string) (string, error) {
	portNum, err := net.LookupPort("udp", port)
	if err != nil {
		return "", fmt.Errorf("invalid port %q: %w", port, err)
	}
	return strconv.Itoa(portNum), nil
}

func selectTailnetIP(addrs []netip.Addr) (netip.Addr, error) {
	var fallback netip.Addr
	for _, addr := range addrs {
		if !addr.IsValid() {
			continue
		}
		if addr.Is4() {
			return addr, nil
		}
		if !fallback.IsValid() {
			fallback = addr
		}
	}
	if fallback.IsValid() {
		return fallback, nil
	}
	return netip.Addr{}, errors.New("no tailscale IPs available")
}

func logMappingMetadata(ctx context.Context, logger *slog.Logger, store *mapping.Store) {
	if store == nil {
		logger.WarnContext(ctx, "mapping store unavailable for logging")
		return
	}
	source, loadedAt, lines, records := store.Metadata()
	logger.InfoContext(ctx, "mapping file loaded", slog.String("source", source), slog.Time("loaded_at", loadedAt), slog.Int("lines", lines), slog.Int("records", records))
}
