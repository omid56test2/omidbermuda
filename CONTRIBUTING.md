# Contributing

1. Open an issue describing the bug or proposed change before large refactors.
2. Never include admin tokens, subscription links, UUIDs, Trojan passwords, backups, user data, or real domains in issues or pull requests.
3. Keep built-in Xray listener definitions immutable unless the design and migration path are discussed first.
4. Run the checks before opening a pull request:

```bash
./scripts/verify.sh
./scripts/smoke.sh
```

For Docker/Xray integration, build the image and run:

```bash
docker build -t omid-iran-bermuda:1.0.2 .
./scripts/docker-smoke.sh
```

5. Document any change to API routes, environment variables, backup format, subscription format, or persistence behavior.
6. Do not add evasion features intended to conceal the service from a hosting provider or bypass its acceptable-use enforcement.
