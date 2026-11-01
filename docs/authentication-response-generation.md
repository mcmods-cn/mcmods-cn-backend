# Authentication responses and current browser sessions

Public endpoints with optional authentication return the existing
`X-MCMods-Auth-State: invalid` signal for an invalid submitted credential and
serve the request as a guest. They do not write or expire `mcmods_session`.
A response issued before a subsequent login can arrive afterwards; a browser
applies its `Set-Cookie` header before JavaScript can check the request's
authentication generation. A deletion from that earlier response would erase
the new session even if the client correctly ignores the old invalid signal.

The frontend must still check the request's authentication generation before
acting on invalid signals or 401 responses. A genuinely invalid current session
still receives the invalid signal on public reads and 401 on protected reads.
Explicit logout continues to revoke the current server session and expire its
cookie. Temporary authentication-storage errors remain 503 and do not expire
the cookie. No session or authorization check is relaxed.

`TestAuditDGR002LateOptionalInvalidCookieCannotDeleteNewLoginHTTPPostgresIntegration`
uses the real HTTP routes, PostgreSQL and a cookie jar. An owned forwarding
proxy holds the complete response headers for a revoked account A, performs
an ordinary password login for B, then releases A's response. B must remain
the current authenticated user; explicit B logout must still end its session.
This checks transport semantics independently of the browser generation tests.
