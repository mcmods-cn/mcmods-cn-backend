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

## Signing configuration across API instances

The administrator reads `GET /api/v1/admin/config/yggdrasil` before saving.
Send the returned `revision` with the editable fields in the PUT. A missing
revision returns 428. A competing save or rotation returns 409 and preserves
the stored settings. Reload before choosing which edits to submit. An empty
private-key field keeps the current signing key. Replacing it or requesting
rotation changes the public key. Neither administrator response includes the
private key.

Each launcher protocol request reads the current encrypted setting from the
primary database. All steps of that request use its selected configuration.
Each API process caches RSA parsing by the exact stored revision. The public
metadata response requires cache revalidation. An unreadable stored setting
returns 503 for the launcher's signed API and the configuration panel. Public
texture downloads continue to apply their independent asset visibility rules.

Before the first administrative save, enabled API instances in production
need the same `YGGDRASIL_PRIVATE_KEY_BASE64` and endpoint settings. Development
can use a process-local temporary key. All instances need the same settings
encryption key to read a saved configuration. Deploy the API and its admin UI
together. On a mixed-version rollout, keep signing configuration unchanged
until every API instance and admin client supports revisions. Existing client
metadata cached under the previous policy may last for its prior five-minute TTL.
