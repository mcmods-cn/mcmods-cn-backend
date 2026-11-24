# Anti-abuse administration errors

Administration reads and writes retain the existing `security.anti-abuse.read`, `security.anti-abuse.write` and sensitive-field permissions. Event listing returns HTTP 500 if a stored row cannot be decoded or its cursor fails; it does not omit failed rows and report an empty or partial successful result.

Event review, user risk-state updates, restriction creation/lifting and bot-rule deletion return HTTP 500 on storage failure. HTTP 404 is reserved for a missing user, event, active restriction or bot rule. Clients should preserve the operation input after a server error and allow an explicit retry. Bot-rule changes invalidate their shared cache after the database write succeeds. Account restrictions take effect on the next protected write across instances.

The regression tests use a closed lazy pool for failed writes and a deliberately broken PostgreSQL projection for row decoding. These are controlled failure tests; they do not assert that production rows are malformed or that the production anti-abuse service has been inspected.

## Timed rules and challenges

Each protected write checks the current account, risk state and matching user, IP and device restrictions in one primary PostgreSQL query. Every instance sees changes committed before its query, including natural rule starts and ends. Failure to read this decision returns HTTP 503 with `anti_abuse_state_unavailable`. Active user, IP and device restrictions have separate indexes. The IP and device indexes omit empty identifiers and lifted rules. Redis and local counters still enforce rate limits.

Crawler rules retain their precedence after caching. A matching temporary rule checks the PostgreSQL clock once per request. A permanent rule or an unmatched rule needs no extra clock query. Configure Turnstile with the final verification URL. Redirects produce the existing provider-unavailable result. A successful provider response can consume a challenge only before its database deadline.

Upgrade API instances together to enforce the same policy on all protected writes. Older cached account profiles and crawler rules expire under their existing TTLs.

On PostgreSQL 18.6, a warm synthetic fixture used 100,000 restrictions across 1,000 accounts, with 80% account, 10% IP and 10% device rules. Every seventh rule was lifted. A generic plan for the protected write's policy query took a median 50.306 ms and hit 2,181 shared blocks for a common match before adding the device index. The indexed run took 0.779 ms and hit 209 blocks. For a missing rule the same measurements were 35.587 ms and 2,181 blocks, then 0.046 ms and 13 blocks. Each median uses five runs following one warmup. The partial index contained 8,572 entries and occupied 80 KiB. These figures cover SQL execution on the fixture. The installed-schema regression is `MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/antiabuse -run '^TestAntiAbuseQueryPlans$' -count=1`.
