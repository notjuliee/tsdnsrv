# Initial Plan: Tailscale-backed DNS Server

## Context
- Target binary should run headlessly and join a Tailnet as its own node using the Tailscale Go bindings (preferably `tailscale.com/tsnet`).
- The process must expose DNS service on port 53 within the Tailnet and answer queries based on a static hostname→IP mapping file.
- We have a minimal Go module (`main.go`, `go.mod`) as a starting point.

## Goals
- Embed and authenticate a Tailscale node created at runtime so the service appears as a host on the user's Tailnet.
- Listen on both UDP and TCP port 53 on that Tailscale interface.
- Serve A/AAAA (and optionally other record types) responses from a simple mapping file (`hostname ip` per line, comments allowed).
- Provide a CLI/config mechanism to point at the mapping file, auth key, tailnet name, logging verbosity, etc.
- Handle reloads (initial load at minimum; hot reload via SIGHUP or fsnotify is a stretch goal).

## Assumptions & Open Questions
- **Auth**: support both reusable auth keys and interactive login links; confirm whether to create ephemeral nodes when a key is provided or fall back to persistent state for link-based flows.
- **State storage**: `tsnet` writes state to disk; decide location (default temp dir vs configurable path).
- **DNS scope**: start with A records only; clarify if AAAA, CNAME, or reverse lookups are required.
- **Mapping file format**: assume whitespace-separated `hostname ip`; allow comments with `#`; need to validate IPs.
- **Port binding**: port 53 requires root on many systems; verify `tsnet` exposes virtual interface that bypasses host root requirement.
- **Error handling**: determine desired behavior on unknown hostnames (NXDOMAIN vs SERVFAIL).
- **Logging/metrics**: confirm if structured logging or metrics export is needed.

## High-Level Architecture
- **Entry point (`main.go`)**
  - Parse CLI flags/env (mapping file path, tailnet auth key or link login, hostname, log level).
  - Initialize `tsnet.Server` with appropriate options (hostname, optional auth key, logf, ephemerality, dir).
  - Start the server; obtain `net.Listener`/`net.PacketConn` for TCP/UDP port 53 via `Listen`/`ListenPacket` on the `tsnet.Server`.
- **Config & state package**
  - Define a struct to hold runtime options and validated mapping entries.
  - Provide helper to load/parse mapping file into in-memory map keyed by FQDN.
- **DNS handler package**
  - Use `github.com/miekg/dns` (preferred) or manual `net` implementation to parse queries and craft responses.
  - Implement handler that looks up the queried hostname, matches A/AAAA request type, and writes response with correct TTL.
- **Lifecycle management**
  - Wire shutdown: respond to SIGINT/SIGTERM, close listeners, stop `tsnet.Server`.
  - Optional file watcher or signal-triggered reload to refresh mapping without restart.

## Work Breakdown
1. **Research & dependencies**
   - Confirm Tailscale Go library usage patterns (`tsnet.Server` auth, state directory, listening on privileged ports).
   - Add third-party deps (`github.com/miekg/dns`) to `go.mod`.
2. **CLI & configuration parsing**
   - Introduce flags/env for mapping file path, Tailscale settings, logging verbosity, reload behavior.
   - Validate inputs (ensure mapping file exists/readable, hostname format, IP parsing).
3. **Mapping file loader**
   - Implement parser that reads the file, skips comments/blank lines, validates hostnames/IPs, builds in-memory lookup (case-insensitive hostnames).
   - Provide reload helper returning new map + timestamp for logging.
4. **Tailscale server wiring**
   - Instantiate `tsnet.Server` with user-specified hostname, optional auth key, control URL, directories, ephemerality flag.
   - Start server; acquire listeners for UDP and TCP on port 53, handling fallback if port unavailable.
5. **DNS serving layer**
   - Integrate `miekg/dns` server using the `PacketConn`/`Listener` from `tsnet` for UDP/TCP.
   - Implement handler to respond with configured records; return NXDOMAIN when absent.
6. **Hot reload (optional)**
   - Implement SIGHUP handler or fsnotify watcher to reread mapping file and swap the in-memory map atomically.
7. **Observability & polish**
   - Add structured logging (logrus/zap or stdlib) for startup info, queries, reloads, and errors.
   - Provide health endpoint or simple metrics hooks if needed.
8. **Testing strategy**
   - Unit tests for mapping parser and DNS handler logic using in-memory request/response.
   - Integration test stub that skips unless a `TAILSCALE_AUTHKEY` is provided.
   - Manual validation instructions (run binary, verify node appears in Tailnet, query DNS from another client).

## Deliverables
- Go binary capable of joining Tailnet and serving DNS based on mapping file.
- Configuration documentation covering required environment variables/flags and mapping file format.
- Tests for parser and DNS handler, plus manual validation notes.
- (Stretch) Optional hot reload capability and monitoring hooks.

## Immediate Next Steps
- Validate required auth/key workflow with the user (ephemeral vs persistent nodes, interactive login expectations).
- Flesh out CLI flag set and decide on configuration structure.
- Spike a minimal `tsnet` listener to confirm port 53 binding works without root.
