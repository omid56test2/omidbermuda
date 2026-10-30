# Verification Report — OMID-IRAN BERMUDA 1.0.2

Verification date: 2026-10-30

## Completed in the current build session

- `go test -race ./...` — passed on Linux AMD64.
- `go vet ./...` — passed.
- Linux AMD64 static build — passed.
- Windows AMD64 cross-build — passed.
- `node --check web/bermuda-qr.js` — passed.
- Inline admin-page JavaScript syntax check — passed.
- Xray JSON fixtures and version metadata — parsed and validated.
- Local HTTP smoke test — passed: auth, user creation, three built-in inbounds, subscription output, QR asset, backup/restore, runtime metrics, generated Xray structure and private Gateway route matching.

The local Go toolchain used for this session was Go 1.23.2. The CI and Docker builder are pinned to Go 1.27.3. Go 1.27.3 was released on 2026-10-19; Xray v26.10.21 is the pinned runtime used by this release. See the official Go release history and Xray release page.

## Not verified in this session

- Docker image build/run — Docker is unavailable in this environment. The GitHub Actions Docker/Xray integration job is configured but must pass after you push the repository.
- Native Windows execution — the Windows executable cross-compiles successfully, but this Linux environment cannot perform a native PowerShell launch test.
- Real Xray client handshake and sustained traffic accounting — requires the official Xray binary, a compatible public TLS ingress and a host/network that permits the workload.
- Remote provider deployment — no cloud provider deployment was performed.

## Operational limitations

- Traffic is sampled every 15 seconds using Xray StatsService. A hard host/process failure between successful samples may lose the uncollected interval.
- IP limit counts distinct source IPs with active Gateway streams/requests, not device fingerprints.
- Connection limit counts concurrent Gateway requests/streams; XHTTP request concurrency is not an exact device count.
- Per-user speed limit is an aggregate Gateway byte pacing ceiling, not carrier-grade QoS.
- Only the fixed three Bermuda-style listeners are supported; custom listener creation is intentionally disabled.
- Subscriptions use private `/connect/{route-token}` paths; older-stage subscriptions must be refreshed.
- Use this service only on infrastructure whose current terms explicitly permit VPN/proxy workloads.
