# Anti-abuse administration errors

Administration reads and writes retain the existing `security.anti-abuse.read`, `security.anti-abuse.write` and sensitive-field permissions. Event listing returns HTTP 500 if a stored row cannot be decoded or its cursor fails; it does not omit failed rows and report an empty or partial successful result.

Event review, user risk-state updates, restriction creation/lifting and bot-rule deletion return HTTP 500 on storage failure. HTTP 404 is reserved for a missing user, event, active restriction or bot rule. Clients should preserve the operation input after a server error and allow an explicit retry. Cache invalidation and success feedback run only after the database operation succeeds. Existing payloads, permission boundaries and successful responses are unchanged.

The regression tests use a closed lazy pool for failed writes and a deliberately broken PostgreSQL projection for row decoding. These are controlled failure tests; they do not assert that production rows are malformed or that the production anti-abuse service has been inspected.
