# Real export ZIP multipart recovery

This opt-in gate opens the actual project data editor in Chromium and uses the checkout's ordinary hashing, IndexedDB and OSS upload code. An isolated PostgreSQL schema supplies a scoped editor and a target version. The TEST037 loopback store speaks the OSS multipart protocol and keeps the archive bytes in memory.

Use a disposable frontend checkout with its documented Node version, `npm ci` and `npx playwright install chromium`. Set up the backend's isolated integration-test environment as described in its README. Supply the absolute paths to the frontend checkout and to a private game-produced ZIP, plus the SHA-256 recorded when the game produced it. The gate requires at least four production-size parts and fits the fixture's 64 MiB allowance.

```sh
MCMODS_RUN_DB_INTEGRATION=1 MCMODS_RUN_REAL_GAME_MULTIPART=1 \
MCMODS_AUDIT_FRONTEND_REPO=/absolute/path/to/isolated/frontend \
MCMODS_AUDIT_GAME_PACKAGE=/absolute/path/to/private/export.zip \
MCMODS_AUDIT_GAME_SHA256="$recorded_sha256" \
go test ./internal/httpapi -run '^TestRealGameMultipartResumeIntegration$' -count=1 -timeout=8m -v
```

The browser leaves after IndexedDB records parts 1 and 3, reopens the editor, transfers the remaining parts under the original upload ID and repeats the completion request. The `MULTIPART_BINDING` line records part numbers, both HTTP statuses, the hashes of both object bodies and the database binding, plus user and daily quotas. The standalone fixture accepts the original import request and stops before worker execution.

An owner of a running `TestAuditLiveGameJourneyFixture` can run the browser against its existing Next process to continue the *same* upload through its worker. Set `MCMODS_AUDIT_JOURNEY_RUNTIME` to that private fixture's runtime JSON, and set `MCMODS_AUDIT_FRONTEND_REPO` to its installed checkout. Use `node tools/testing/export_multipart/browser.mjs --live-fixture` with stdout redirected inside the fixture's owner-only directory. The fixture inspects the active binding and writes `multipart-binding-proof.json` before releasing the original job request. Keep the ZIP, runtime, credentials and identifying checkpoints private. The binding proof contains only counts, sizes, hashes and verification results.
