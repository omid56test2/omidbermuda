# OMID-IRAN BERMUDA 1.0.2

Self-hosted Go control plane for a fixed Bermuda-style Xray runtime. The application manages users, groups, per-user subscriptions, traffic accounting, Gateway policies, backups and Xray lifecycle. The previous Python OMID-IRAN source is intentionally not included in this release. Keep any old deployment snapshot in a separate private archive; it is not part of this runtime.

> Run this workload only on infrastructure whose acceptable-use terms explicitly permit VPN/proxy services. This project does not attempt to conceal its purpose from hosting providers or evade provider enforcement.

## License status

No open-source license is granted by this repository at this time. See [LICENSE-STATUS.md](LICENSE-STATUS.md). Before making this repository public or redistributing Bermuda-derived UI/assets, verify that you own those materials or have permission to redistribute them.

## Runtime architecture

- Go HTTP control plane and admin UI (`cmd/server`, `internal/managed`).
- Xray process supervisor (`internal/supervisor`) with config preflight validation before starting Xray.
- Three immutable Xray listeners, all bound to loopback inside the application container:

| Listener | Protocol | Transport | Local address | Upstream path |
|---|---|---|---|---|
| `bermuda-vless-xhttp` | VLESS | XHTTP `auto` | `127.0.0.1:18443` | `/api/v1/sync` |
| `bermuda-vless-ws` | VLESS | WebSocket | `127.0.0.1:18444` | `/api/v1/live` |
| `bermuda-trojan-ws` | Trojan | WebSocket | `127.0.0.1:18445` | `/api/v1/gateway` |

The Go Gateway accepts only private `/connect/{route-token}` paths and proxies to the matching fixed loopback listener. It does not expose a catch-all proxy or arbitrary destination URL. TLS is expected to terminate at a trusted HTTPS ingress/reverse proxy or may be terminated directly by Go using `TLS_CERT_FILE` and `TLS_KEY_FILE`.

## Features

### User and group management

- Create, edit, activate/deactivate and delete users.
- Optional groups; users may remain ungrouped.
- Built-in inbound assignments are read-only and can be selected per user.
- Per-user independent subscription token and per-config credentials.
- Expiry, traffic quota, source-IP limit, concurrent Gateway request/stream limit and aggregate byte-rate ceiling.
- Zero means unlimited for expiry, quota and numerical limits.

### Subscriptions

- `GET /sub/{token}` returns the graphic Persian subscription page to browsers and Base64 text to clients.
- `GET /sub/base64/{token}` returns Base64 text.
- `GET /sub/raw/{token}` returns newline-separated client URIs for diagnostics.
- QR codes are generated locally in the browser; private URIs are not sent to third-party QR services.
- Subscription tokens are credentials. Treat them like passwords.

### Xray and traffic operations

- Xray config is generated atomically with restrictive file permissions.
- `xray run -test -config ...` is executed before Xray is started/restarted.
- `StatsService` is loopback-only at `127.0.0.1:10085`; Go polls every 15 seconds.
- Traffic deltas are persisted. Users that exhaust their quota are removed from the generated Xray client list and Xray is refreshed.
- Gateway IP and concurrent-stream policies are applied per user. WebSocket counts for the lifetime of the stream; XHTTP's concurrent request/stream count is not a physical device count.
- Speed pacing is an aggregate Gateway byte-rate ceiling, not carrier-grade QoS.
- Optional autorecovery with exponential backoff; an explicit admin Stop disables automatic recovery until Start is requested.

### Monitoring and recovery

- `GET /health` is a liveness endpoint.
- `GET /ready` is ready only while Xray is reported running.
- `GET /admin/system` reports runtime, Gateway, Xray and usage collector status.
- `GET /admin/usage` reports StatsService collector status.
- `GET /admin/backup` exports all managed state, including credentials and private route tokens.
- `POST /admin/restore` replaces the current managed state from a validated version-1 backup (maximum 10 MiB), regenerates Xray config and refreshes routes.

Backups contain working connection credentials. Encrypt them at rest, restrict access and never commit them to source control.

## Quick start with Docker Compose

1. Copy `.env.example` to `.env`.
2. Generate an admin secret: `openssl rand -hex 32`.
3. Put that value in `ADMIN_TOKEN` and set `PUBLIC_BASE_URL` to the public HTTPS origin users will connect to (no trailing slash).
4. Keep `HOST_BIND=127.0.0.1` when a reverse proxy runs on the same host. Configure that reverse proxy to terminate TLS and forward HTTP/WebSocket traffic to `127.0.0.1:8080`. If the proxy is in another container, use a private Docker network instead of binding the admin/Gateway port publicly.
5. Start the stack:

```bash
docker compose --env-file .env -f docker-compose.example.yml up -d --build
```

Check status:

```bash
docker compose -f docker-compose.example.yml ps
docker compose -f docker-compose.example.yml logs -f omid-iran-bermuda
```

The Dockerfile builds the Go application and copies the official Xray executable from `ghcr.io/xtls/xray-core:26.10.21`. Xray is configured to auto-start and auto-recover in the container. Only port `8080` is published by Compose; Xray's internal listener and StatsService ports are not published.

## Windows local mode (without Docker)

For a ready-to-run Windows x64 package, download `OMID-IRAN-BERMUDA-v1.0.2-Windows-Local.zip` from the GitHub Releases page, extract it, and run `start-local.bat`. The first run downloads the official Xray Windows x64 archive and verifies its SHA-256 digest before installation. The local server binds only to `127.0.0.1:8000`; its admin token is generated locally and saved in `data-preview/admin-token.txt`. See [README.windows.fa.md](README.windows.fa.md) for Persian instructions.

The Windows binary is a release asset and is intentionally not tracked in Git.

## Manual run (without Docker)

Use Go 1.23 or newer and install a compatible Xray binary. Set `XRAY_BIN` to its path.

```bash
cp .env.example .env
# Set ADMIN_TOKEN and PUBLIC_BASE_URL in your shell/environment.
export ADMIN_TOKEN="$(openssl rand -hex 32)"
export PUBLIC_BASE_URL="https://vpn.example.com"
export DATA_FILE="./data/omid_state.json"
export XRAY_CONFIG="./data/xray-generated.json"
export XRAY_BIN="/usr/local/bin/xray"
export XRAY_AUTOSTART=true
export XRAY_AUTORESTART=true

go test -race ./...
go vet ./...
go run ./cmd/server
```

Open the admin panel at the server origin, enter the `ADMIN_TOKEN` and manage users. Do not expose the admin panel over plain HTTP on an untrusted network.

## Environment variables

| Variable | Description | Default |
|---|---|---|
| `PORT` | Go Gateway/admin HTTP port | `8080` |
| `LISTEN_ADDR` | Exact application bind address; normally omit for container use | `:<PORT>` |
| `HOST_BIND` / `HOST_PORT` | Host-side Docker Compose bind address and port | `127.0.0.1` / `8080` |
| `ADMIN_TOKEN` | Management API Bearer token; minimum 32 characters | required |
| `DATA_FILE` | Persistent user/group JSON state | `./data/omid_state.json` |
| `PUBLIC_BASE_URL` | Canonical public origin used in client links | request host fallback for local development |
| `XRAY_BIN` | Xray executable path | empty outside Docker |
| `XRAY_CONFIG` | Generated Xray config path | `./data/xray-generated.json` |
| `XRAY_AUTOSTART` | Start Xray at application boot | `false` outside Docker |
| `XRAY_AUTORESTART` | Recover after unexpected Xray exit | `false` outside Docker |
| `TRUSTED_PROXY_CIDRS` | Exact CIDRs of trusted ingress proxies only | empty |
| `TLS_CERT_FILE` / `TLS_KEY_FILE` | Optional direct TLS cert/key for the Go server | empty |

Do not set `TRUSTED_PROXY_CIDRS` to broad internet ranges. Forwarded client-IP headers are used only when the immediate peer belongs to a configured trusted proxy CIDR. When TLS is terminated upstream, set `PUBLIC_BASE_URL` explicitly to the canonical HTTPS origin; arbitrary `X-Forwarded-Proto` headers are not trusted to choose link security.

## Management API

All management endpoints below require `Authorization: Bearer <ADMIN_TOKEN>`:

- `GET /api/inbounds` — list the three immutable listeners.
- `POST /api/inbounds`, `PATCH /api/inbounds/{id}`, `DELETE /api/inbounds/{id}` — always 403; built-in listeners cannot be added/edited/deleted.
- `GET|POST /api/users`, `PATCH|DELETE /api/users/{id}` — user management.
- `GET|POST /api/groups`, `PATCH|DELETE /api/groups/{id}` — group management.
- `POST /admin/xray/rebuild` — rebuild config and restart Xray if running.
- `GET /admin/xray`, `POST /admin/xray/start`, `POST /admin/xray/stop` — Xray lifecycle.
- `GET /admin/usage`, `GET /admin/system` — usage and system monitoring.
- `GET /admin/backup`, `POST /admin/restore` — protected backup/restore.

Compatibility aliases `/api/subs` are retained for older terminology.

## Verification

Run the repository verification suite:

```bash
./scripts/verify.sh
```

Run a local HTTP smoke test (builds and starts a temporary app, creates a test user, checks subscriptions, QR asset, backup/restore and Xray JSON structure):

```bash
./scripts/smoke.sh
```

Run the real Docker + Xray integration test after building the image:

```bash
docker build -t omid-iran-bermuda:1.0.2 .
./scripts/docker-smoke.sh
```

The GitHub Actions workflow runs Go, HTTP and Docker/Xray integration checks. A separate tag-triggered workflow builds Linux and Windows release assets and publishes a GitHub Release. The tag workflow runs only after you push a version tag such as `v1.0.2`. The local HTTP smoke test does not establish that a remote hosting provider permits this workload.

## Upgrade note

Stage 5 introduced private `/connect/{route-token}` subscription paths. Users migrating from earlier stages must refresh/re-import their subscriptions after deploying this release.

## Native Windows run (without Docker)

The release includes a Windows AMD64 executable and `start-local.bat`. Extract the archive and double-click `start-local.bat`; it downloads and SHA-256-verifies the official Xray Windows x64 release on first run, generates a persistent admin token, and binds the app to `127.0.0.1:8000`. See `README.windows.fa.md` for Persian instructions and manual PowerShell variables. No Python venv, pip, or Go installation is required for the prebuilt release.
