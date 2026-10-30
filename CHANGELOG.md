# Changelog

## 1.0.2 — 2026-10-30

- Fixed Windows Xray bootstrapper parsing of the official `SHA2-256=` digest label.
- Added a shared PowerShell digest parser with SHA2-256, SHA-256 and SHA256 format support.
- Added Windows CI fixtures for digest parsing, UTF-8 BOM handling and rejection of missing SHA-256 values.

## 1.0.1 — 2026-10-30

- Prepared a GitHub-ready repository and tagged release workflow.
- Added a Windows x64 local-run package with a native process supervisor and a checksum-verified Xray setup script.
- Added native Windows CI tests and cross-platform release builds.
- Fixed Unix shell script executable permissions in the release archive.
- Changed the sample environment file to require an explicitly generated admin token and canonical public URL; no known or reusable secret is shipped.
- Added release asset packaging and SHA-256 checksums.

## 1.0.0 — 2026-10-30

- Finalized the Go control plane and fixed Bermuda-style Xray listener set.
- Added preflight validation (`xray run -test`) before starting Xray.
- Added official Xray runtime image as a pinned Docker build stage (26.10.21).
- Updated builder/runtime images to maintained releases (Go 1.27.3 and Alpine 3.24.3).
- Added private-by-default Docker Compose port binding and named persistent data volume.
- Added container readiness healthcheck against Xray process state.
- Added stricter browser security headers and stopped trusting arbitrary X-Forwarded-Proto headers.
- Reset Xray crash-recovery backoff after a stable run.
- Added release verification, local HTTP smoke tests and GitHub Actions container/Xray integration smoke test.

## 0.5.0 — Operations and backup

- Per-user Gateway routes, IP and concurrent request/stream limits, aggregate rate pacing.
- Runtime metrics, optional Xray auto-recovery, protected backup/restore.

## 0.4.0 — Usage and QR

- Xray StatsService collector, periodic traffic accounting, quota enforcement and local QR generation.

## 0.3.0 — Bermuda runtime model

- Three loopback Xray listeners, path-based Gateway, per-user subscriptions.

## 0.2.0 — Managed users and groups

- User, group and built-in inbound management APIs and admin UI.

## 0.1.0 — Private alpha

- Initial Go control plane and Xray process supervisor.
