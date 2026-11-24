# Anti-abuse administration errors

Administration reads and writes retain the existing `security.anti-abuse.read`, `security.anti-abuse.write` and sensitive-field permissions. Event listing returns HTTP 500 if a stored row cannot be decoded or its cursor fails; it does not omit failed rows and report an empty or partial successful result.

Event review, user risk-state updates, restriction creation/lifting and bot-rule deletion return HTTP 500 on storage failure. HTTP 404 is reserved for a missing user, event, active restriction or bot rule. Clients should preserve the operation input after a server error and allow an explicit retry. Cache invalidation and success feedback run only after the database operation succeeds. Existing payloads, permission boundaries and successful responses are unchanged.

The regression tests use a closed lazy pool for failed writes and a deliberately broken PostgreSQL projection for row decoding. These are controlled failure tests; they do not assert that production rows are malformed or that the production anti-abuse service has been inspected.

## Timed rules and challenges

Each protected write checks the current account, risk state and matching user, IP and device restrictions in one primary PostgreSQL query. Every instance sees changes committed before its query, including natural rule starts and ends. Failure to read this decision returns HTTP 503 with `anti_abuse_state_unavailable`. Redis and local counters still enforce rate limits.

Crawler rules retain their precedence after caching. A matching temporary rule checks the PostgreSQL clock once per request. A permanent rule or an unmatched rule needs no extra clock query. Configure Turnstile with the final verification URL. Redirects produce the existing provider-unavailable result. A successful provider response can consume a challenge only before its database deadline.

Upgrade API instances together to enforce the same policy on all protected writes. Older cached account profiles and crawler rules expire under their existing TTLs.
