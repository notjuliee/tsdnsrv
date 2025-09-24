# tsdnsrv Usage Guide

This guide walks through building the service, preparing configuration, and operating the Tailnet DNS responder.

## Prerequisites
- Go 1.25.1 or newer
- A Tailnet where you can add new devices (interactive login or auth key)
- (Optional) A Tailscale auth key (`tskey-...`) if you prefer headless logins

## Build the Binary
```bash
go build ./...
go build -o tsdnsrv ./
```

If the sandbox blocks the default Go caches, set `GOCACHE`/`GOMODCACHE` to workspace paths before running commands, for example:
```bash
GOCACHE=$(pwd)/.gocache GOMODCACHE=$(pwd)/.gomodcache go build -o tsdnsrv ./
```

## Prepare the Mapping File
Create a text file where each non-empty line contains:
```
<hostname> <ip-address> [ttl-seconds]
```

Rules:
- Lines starting with `#` or blank lines are ignored.
- Hostnames are case-insensitive and treated as FQDNs (a trailing dot is optional).
- IPv4 and IPv6 addresses are supported.
- TTL is optional; defaults to 300 seconds when omitted.

Example (`examples/hosts.example.txt`):
```
web.dev.internal 100.64.0.10 60
db.dev.internal 100.64.0.11
ipv6.dev.internal fd7a:115c:a1e0::1 120
```

## Create the Configuration File
Place a JSON file similar to the following (see `examples/config.example.json`):
```json
{
  "hostname": "tsdnsrv-dev",
  "stateDir": "./state",
  "mapFile": "./hosts.example.txt",
  "listenAddress": ":53",
  "logLevel": "info",
  "controlURL": "",
  "ephemeral": false
}
```

Field reference:
- `hostname` (**required**): Node name that will appear inside your Tailnet.
- `authKey`: Tailscale auth key for headless startup. Leave empty to receive an interactive login link at runtime.
- `stateDir`: Directory where tsnet stores persistent state (ignored when `ephemeral` is `true`). Relative paths resolve against the config file location.
- `mapFile` (**required**): Path to the mapping file described above. Relative paths resolve against the config file location.
- `listenAddress`: Only the port portion is used after tsnet starts. Defaults to `:53`.
- `logLevel`: One of `debug`, `info`, `warn`, or `error` (`warning` normalizes to `warn`).
- `controlURL`: Optional custom control plane URL for non-default Tailnets.
- `ephemeral`: Defaults to `false`. Set to `true` only when supplying an auth key that grants ephemeral access.

## Run the Service
Execute the binary with the configuration file:
```bash
./tsdnsrv --config /path/to/config.json
```

Key behaviors:
- On startup the service joins your Tailnet, binds UDP/TCP listeners for the configured port (default 53), and logs mapping metadata.
- Logs emit to stderr in `slog` text format. Adjust verbosity via `logLevel` in the config.
- Use `Ctrl+C`, `SIGINT`, or `SIGTERM` to shut the server down cleanly.

## Reload Records
Send `SIGHUP` to the process to reload the mapping file without a restart:
```bash
kill -HUP <pid-of-tsdnsrv>
```
Successful reloads log updated metadata; errors leave the previous store active.

## Validate DNS Responses
1. Confirm the Tailnet-assigned IP address for the tsdnsrv node (`tailscale ip -4` / `tailscale ip -6`).
2. From another Tailnet node, query the server:
   ```bash
   dig @100.64.0.5 web.dev.internal A
   dig @fd7a:115c:a1e0::5 ipv6.dev.internal AAAA
   ```
   Replace the IP with the address from step 1.

NXDOMAIN is returned for names not present in the mapping file; only static A/AAAA records are currently supported.

## Troubleshooting
- **Auth errors**: Verify the auth key scope or complete the interactive login link in the logs. Ensure `ephemeral` is `false` when running without an auth key.
- **tsnet state issues**: Ensure the process can write to `stateDir` (or omit it for the default cache location). Remove stale state when switching from persistent to ephemeral nodes.
- **DNS clients cannot resolve names**: Double-check the mapping file format and reload with `SIGHUP`. Remember that lookups are case-insensitive but require exact labels.
- **Port conflicts**: tsnet listens inside the Tailnet namespace, so host port conflicts are unlikely; however, you can change `listenAddress` to another port if needed and update clients accordingly.

For development, run `go test ./...` (adding the `GOCACHE`/`GOMODCACHE` overrides if the environment restricts default cache locations).
