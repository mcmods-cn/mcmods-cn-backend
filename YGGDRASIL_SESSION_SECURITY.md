# Yggdrasil texture session authorization

Launcher texture PUT and DELETE authenticate the active token, enabled launcher
account, active owner and selected player profile before processing a request.
This preflight check is also repeated inside the write transaction: after the
user row, the transaction locks the launcher account, player profile and token
in that order. Refresh uses the same account/profile/token ordering.

The transaction requires that the token is still active, unexpired and bound to
the same user and profile. Expiry uses the database wall clock after waiting for
locks. A token revoked while an upload body is being read cannot equip a texture.
Credential rotation, launcher disabling, profile deletion and token revocation
serialize with the protected write. A session invalidated before the transaction
rechecks it receives the existing Yggdrasil 401 `UnauthorizedException` response;
database failures receive 503. A write that acquired these locks first may finish
before a concurrent revocation commits.

No schema, cookie, route or DTO migration is required. Deploy the backend change
normally; it is compatible with the existing frontend. The focused regression
uses PostgreSQL and the repository's owned HTTP OSS provider with synthetic
images and credentials. It verifies revocation between preflight and multipart
processing; it does not contact production storage or a real launcher service.

Skin metadata revisions read review configuration through their existing write
transaction. They do not acquire a second pool connection while holding the
asset and user locks. A PostgreSQL regression verifies successful persistence
with a one-connection pool; the review policy itself remains unchanged.
