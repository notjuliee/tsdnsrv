# Agent Handoff Notes

## Repository Snapshot
- Go module name: `server` (Go 1.25.1).
- Entry point `main.go` parses `--config`, loads JSON config, and logs startup plus mapping metadata (tsnet wiring still TODO).
- Directories of interest:
  - `docs/` – planning and research markdown.
  - `internal/config/` – JSON-backed configuration loader with tests.
  - `internal/mapping/` – hostname/IP mapping parser with tests.
  - `examples/` – sample JSON config and mapping files.

## Key Documents
- `docs/initial-plan.md`: High-level concept and architecture outline for the Tailscale-backed DNS server.
- `docs/implementation-plan.md`: Detailed execution plan with work streams, risks, and acceptance criteria.
- `docs/research-notes.md`: Summarized findings on `tsnet` usage, auth keys, state directories, and dependency selections.

## Implemented Code
- `internal/config/config.go`: Loads JSON config files, normalizes relative paths, enforces defaults (`Ephemeral=false`, `logLevel=info`, `listenAddress=:53`), and validates required fields.
- `internal/config/config_test.go`: Exercises JSON parsing, default handling, relative path resolution, listen address defaults, and validation errors.
- `internal/mapping/store.go`: Parses mapping files into immutable stores with TTL support; exposed via lookup helpers.
- `internal/mapping/store_test.go`: Covers parsing success, validation failures, and copy-on-read behavior.
- `internal/dnsserver/`: Provides a `Serve` wrapper around `github.com/miekg/dns` that responds with A/AAAA records sourced from the mapping store; includes unit tests with sandbox-aware skips.
- `main.go`: Consumes `--config`, initializes logging, loads mapping store, spins up the DNS server on the configured listen address, and waits for SIGINT/SIGTERM (tsnet integration still pending).

## Testing Notes
- Tests may require explicit cache/mod directories in sandboxed runs; use `GOCACHE=$(pwd)/.gocache GOMODCACHE=$(pwd)/.gomodcache go test ./...` if needed (cleanup may require elevated permissions).
- Current test status: passing for `internal/config`, `internal/mapping`, and `internal/dnsserver` packages (Serve test skips when sockets unavailable).

## Outstanding Work
1. Replace host-bound listeners with `tsnet.Server` integration so the DNS service answers on the Tailnet interface.
2. Add mapping reload support (signals or watcher) and ensure thread-safe store swaps.
3. Flesh out operational docs/README (config field reference, example flows, auth key handling) and note example files.
4. Consider observability (query logging shaping, metrics) and integration tests once Tailnet support exists.

## Environment & Constraints
- Sandbox: workspace-write, network restricted (module downloads may need approval if not cached).
- Approval policy: on-request; tests can run locally as long as they stay within workspace.
- Prefer using ripgrep (`rg`) for searches; avoid editing non-ASCII.
- Keep comments concise; respect existing user changes if present (currently none besides our additions).

## Helpful References
- `tailscale.com/tsnet` examples for listener setup and auth key handling.
- `github.com/miekg/dns` server patterns for shared handler between UDP/TCP.

This file should enable the next agent to continue with configuration wiring, mapping loader, and Tailscale/DNS integration steps per the implementation plan.
