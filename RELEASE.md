# Publishing a GitHub Release

1. Review `LICENSE-STATUS.md` and confirm that you have permission to publish every included file and asset. Do not assume that the absence of a license means the code is reusable by others.
2. Make sure `.env`, `data/`, `data-preview/`, `xray/`, backups and user credentials are not staged.
3. Commit the source tree and push it to your GitHub repository.
4. Create and push the release tag:

```bash
git tag -a v1.0.1 -m "OMID-IRAN BERMUDA 1.0.1"
git push origin v1.0.1
```

5. The `Release` GitHub Actions workflow builds Linux AMD64 and Windows AMD64 binaries, creates a Windows local-run bundle, generates SHA-256 checksums, and publishes a GitHub Release.
6. Wait for both the `CI` and `Release` workflows to finish. Do not announce the release until the Docker/Xray integration job is green.

The workflow does not deploy the application to a hosting provider.
