# Research Notes: tsnet Integration

## tsnet.Server Quick Facts
- Package: `tailscale.com/tsnet` (available in the main Tailscale repo).
- Core flow:
  1. Create `tsnet.Server` struct with options (Hostname, AuthKey, Ephemeral, Dir, ControlURL, Logf).
  2. Call `server.Listen` / `ListenPacket` for TCP/UDP after `server.Start` or implicitly on first listen.
  3. Close with `server.Close()` to flush state and unregister ephemeral nodes.
- Authentication:
  - `AuthKey` must be generated with `--ephemeral=true` for ephemeral nodes; persistent nodes require reusable keys without ephemeral flag.
  - When `Ephemeral` is `true`, node state is not stored; otherwise tsnet caches credentials in `Dir`.
- State directory:
  - Defaults to `os.UserCacheDir()/tsnet-<hostname>` if `Dir` is empty.
  - Needs write access; ensure we expose flag `--state-dir` to make location explicit.
- Privileged ports:
  - tsnet proxies connections via userspace Tailscale stack; binding `:53` works without root.
  - Host OS firewalls do not apply; access is limited to Tailnet peers.

## Required Dependencies
- `tailscale.com/tsnet` (from go modules; requires Go toolchain able to fetch module).
- `github.com/miekg/dns` for DNS message parsing/serving.
- Go stdlib packages: `flag`, `log/slog` or `log`, `net`, `os`, `context`, `sync/atomic`, `time`, `strings`, `net/netip` for IP parsing.

## Outstanding Questions
- Should we expose control over `ControlURL` for enterprise/self-hosted tailnets? (Default Tailscale control plane likely fine but flag could help.)
- Need to confirm logging preference (structured `slog` vs simple).
- Determine TTL default (maybe 60s or 300s) and whether to allow per-entry override.

## Next Checks
- Inspect `tsnet` sample code (e.g. tailscaled's `tsnet/tsnet.go`) for reference patterns.
- Decide concurrency pattern for hot reload (atomic pointer or RWMutex).
- Validate that binding `:53` via tsnet works on both UDP and TCP by writing integration test after adding minimal scaffolding.
