# Security Notes

## Deployment boundary

Run OMID-IRAN BERMUDA only on infrastructure whose current terms expressly permit VPN/proxy workloads. Do not use this project to disguise the service purpose or bypass provider restrictions.

## Secrets

- Use a unique `ADMIN_TOKEN` with at least 32 random characters.
- Subscription tokens and generated VLESS UUIDs/Trojan passwords are bearer credentials.
- State JSON and Xray config files are written with restrictive permissions, but the host, Docker volume and backup destination must still be protected.
- Backups include user credentials and private Gateway route tokens. Encrypt backups before off-host storage and never commit them to source control.
- The admin token is kept in browser memory for the current page session; do not paste it into screenshots or shared terminals.

## Network boundary

- Xray listeners and StatsService bind to `127.0.0.1` inside the application container.
- Publish only the Go Gateway/admin port. Do not publish Xray's internal ports or StatsService port.
- Terminate HTTPS at a trusted ingress/reverse proxy or configure direct TLS on the Go server.
- Set `PUBLIC_BASE_URL` to the canonical public HTTPS origin when TLS terminates upstream.
- Set `TRUSTED_PROXY_CIDRS` only to the actual immediate proxy ranges. Forwarded-IP headers are ignored unless the socket peer belongs to one of those ranges.
- Keep the admin/Gateway port private behind the intended ingress. Do not expose a plain-HTTP admin panel to the public internet.

## Restore and recovery

- Restore replaces all current users and groups. Take a backup first and verify the backup source.
- The Xray supervisor validates updated configuration before stopping a currently running process.
- Xray auto-restart can repeatedly attempt a bad binary/configuration; inspect the service logs and disable auto-recovery while troubleshooting.

## Reporting

For a security issue in this project, provide a minimal reproduction without real subscription tokens, UUIDs, passwords, admin tokens or user data.
