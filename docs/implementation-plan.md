# Implementation Plan: Tailscale-backed DNS Server

## Overview
This plan converts the high-level concept into concrete tasks needed to ship a Go service that joins a Tailnet via the `tailscale.com/tsnet` package and serves DNS records derived from a static mapping file.

## Deliverable Definition
- Go binary (`tsdnsrv`) that:
- Authenticates to Tailnet via auth key or interactive login (persistent node by default, configurable persistence).
  - Listens on TCP/UDP port 53 on the Tailnet interface and answers queries from a hostname→IP mapping file.
  - Supports A/AAAA records (with straightforward extensibility for additional record types).
  - Provides CLI options or env vars to configure auth, hostname, state directory, mapping file path, and logging level.
  - Gracefully shuts down on SIGINT/SIGTERM and emits useful logs.
- Tests covering mapping parser and DNS response logic.
- README section describing prerequisites, configuration, and validation steps.

## Work Streams & Tasks

### 1. Foundations & Research
- Validate `tsnet` usage for binding privileged ports; confirm auth key requirements (ephemeral vs persistent).
- Decide default filesystem locations (state dir, cache) and configuration precedence order (flags > env > defaults).
- Evaluate DNS library options; choose `github.com/miekg/dns` for maturity and flexibility.

### 2. Configuration & CLI
- Introduce structured configuration (`Config` struct) and command-line parsing using `flag` package.
- Support flags/env for: `--hostname`, `--auth-key`, `--state-dir`, `--map-file`, `--log-level`, `--ephemeral` (listener port derived from `listenAddress`, host component ignored once tsnet is active).
- Implement validation for required files and string formats; ensure secrets can be passed via env to avoid shell history exposure.

### 3. Mapping File Management
- Implement parser that loads mapping file to in-memory store:
  - Ignore blank lines and `#` comments.
  - Validate hostnames (case-insensitive, canonical FQDN) and IPv4/IPv6 addresses.
  - Support optional TTL column with sensible default.
- Provide atomic reload helper returning new immutable map plus metadata (timestamp, entry count).
- Add unit tests leveraging table-driven cases for malformed lines, duplicates, IPv6 support.

### 4. Tailscale Integration Layer
- Instantiate `tsnet.Server` with configuration-derived options (hostname, auth key, directories, ephemerality, log hook).
- Start the server and obtain listeners:
  - UDP: `PacketConn` for port 53 via `ListenPacket`.
  - TCP: `Listener` for port 53 via `Listen`.
- Ensure context-based cancellation and deferred shutdown (`srv.Close()`), capturing errors for logging.

### 5. DNS Handling
- Integrate `miekg/dns` server components:
  - Register handler for A/AAAA queries; return NXDOMAIN for unknown names.
  - Compose responses with correct TTL and answer sections; handle case-insensitive lookups.
  - Ensure both UDP and TCP servers share the same handler/mapping store (with concurrency-safe access).
- Add logging around query handling (query name, type, response outcome) with throttled verbosity.

### 6. Lifecycle & Observability
- Add signal handling for clean shutdown (close DNS servers, wait for goroutines).
- Optional: implement SIGHUP-triggered reload using `signal.Notify`, swapping mapping store atomically.
- Provide minimal health logging (startup summary, reload success/failures) and structured log levels.

### 7. Testing & Validation
- Unit tests for configuration parsing defaults/overrides.
- Unit tests for mapping parser and DNS handler using in-memory `dns.Msg` objects.
- Manual validation script/instructions: run binary with auth key, verify Tailnet node visibility, query DNS from another node (`dig @<tailscale-ip> host`).

### 8. Documentation & Polish
- Update README with build/run instructions, flags, mapping format, operational notes.
- Document limitations (no recursion, static records) and suggestions for production hardening.
- Consider packaging (systemd unit example) if time allows.

## Execution Timeline (Sequential Milestones)
1. Foundations & Research
2. Configuration & CLI + Mapping Parser (build baseline binary that loads config/mappings)
3. Tailscale integration (join network, stub DNS handler returning hard-coded response)
4. DNS handling completion + tests
5. Lifecycle improvements (reload, shutdown)
6. Docs & polish

## Risks & Mitigations
- **Tailscale auth complications**: Provide clear instructions for auth key scope; support dry-run mode without connecting.
- **Port 53 binding issues**: Validate early; if restriction arises, expose configurable port with documentation.
- **Mapping file errors causing downtime**: Validate on load; reject file with descriptive error before swapping active map.
- **Limited testability without Tailnet**: Keep core logic testable offline; mark integration tests optional.

## Acceptance Criteria
- Service successfully joins Tailnet and responds to `dig` queries for configured hostnames with appropriate IP addresses.
- Configuration errors result in actionable messages and non-zero exit codes.
- Unit tests pass via `go test ./...`.
- Documentation enables another engineer to deploy the service with minimal guidance.

## Immediate Action Items
1. Research and document concrete `tsnet` setup requirements (auth key scopes, state dir behavior, port binding capabilities).
2. Prototype configuration struct and flag parsing scaffolding.
3. Implement mapping parser with tests.

Work proceeds to Action Item 1 next.
