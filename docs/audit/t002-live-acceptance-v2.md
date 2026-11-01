# T-002: versioned live acceptance ownership

Use `tools/testing/live_acceptance_v2.py` for new live acceptance scopes. The
command arguments and existing parent/container/endpoint/runtime/namespace
guards are retained. V1 source and its known unittest execution were preserved
separately before this addition; neither the old tool nor an old execution
record is rewritten here.

V2 records the actual PostgreSQL database OID and creator owner immediately
after its own CREATE succeeds. It writes a fresh random COMMENT nonce and
confirms the complete tuple before setting `created=true`. The private atomic
receipt distinguishes `COMMENT_PENDING` from `CONFIRMED`; a partial creation
stays incomplete for scoped diagnosis.

A legacy scope without a confirmed original creation tuple is held before SQL
or namespace cleanup. V2 never fills that gap using the current database name
or adopts an old scope. Missing OID, owner or nonce needs separate read-only
diagnosis and explicit scoped handling, not a manufactured receipt.

Run and cleanup reread the actual identity through the already verified
parent. Cleanup preserves all existing stopped-runtime/run-id and exact
Redis/NATS namespace requirements, then checks database identity again after
namespace cleanup. It performs ordinary DROP without FORCE and confirms
absence. A live connection produces an ordinary PostgreSQL `55006` failure
(verbose psql diagnostics retain SQLSTATE); no backend termination fallback
exists. Failed removal does not mark the database removed or emit a success
receipt.

The added Python tests cover malformed/legacy receipts, original creation and
partial nonce failures, catalog mismatch, the final recheck, exact cleanup SQL
and retained `55006` behavior using controlled components. These are not a
real-service acceptance run. The exact `41f9a914` Python source and tests passed all 11 controlled components
on 2026-11-01 (unittest 0.012s, external wall 0.137s), followed by an independent
review. This verifies the mock transport layer. Real PostgreSQL and full V2
prepare/run/cleanup remain unexecuted; the separate Go-helper `55006` evidence
is not substituted for them. Existing V1 guard and own-process tests stay unchanged.

An operator can later use the same `prepare`, `run`, `cleanup` or `all` actions
with the new versioned path and a new private scope. The normal owned parent
environment is still required; the tool does not borrow another service's
credentials or change production roles. Old scopes are not valid V2 inputs.
No claim is made that the catalog read and DROP are atomic against a separate
actor renaming/replacing databases concurrently.
