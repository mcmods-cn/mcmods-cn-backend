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
and retained `55006` behavior using controlled components. The exact
`41f9a914` Python source and tests passed all 11 controlled components on
2026-11-01 (unittest 0.012s, external wall 0.137s), followed by an independent
review. That execution verifies the mock transport layer.

A separate source03 harness (`40276b32`) then directly exercised five unchanged
V2 ownership functions from `41f9a914` (byte-identical to `c86db80f`) against a
new isolated PostgreSQL 18.6 instance. All 10 cases passed in 14.291s, including
real `55006`, preservation of the held connection and sentinel, and ordinary
DROP with confirmed absence after that connection closed normally. Its 71
recorded driver commands do not include every native SQL or held-session
subprocess. Native SQL stdout/stderr were not separately retained, and the
last server-log snapshot precedes the final parent-state assertions. The
report preserves these raw-evidence limits and the earlier parent-startup
failure; the old bootstrap file remains held locally rather than adopted.

This establishes the five-function real PostgreSQL layer. That source03
function-level scope did not execute full V2 `prepare`/`run`/`cleanup` with
PostgreSQL, Redis and NATS; its lifecycle result is `NOT_RUN` for that scope;
the separate Go-helper evidence does not substitute for that lifecycle.
Existing V1 guard and own-process tests stay unchanged. See the audit delivery's
`t002-v2-five-functions-real-pg-verification-resume.json` and
`t002-v2-five-functions-real-pg-closure-addendum.json`, together with
`MCMods_T002_V2_真实PG五函数与原始环境失败增量_2026-11-01.zip`
(SHA256 `d5e9e6abff5a658432953ac110c3b9bca308300c46425b9c3f92e2641edd29d0`).

An operator can later use the same `prepare`, `run`, `cleanup` or `all` actions
with the new versioned path and a new private scope. The normal owned parent
environment is still required; the tool does not borrow another service's
credentials or change production roles. Old scopes are not valid V2 inputs.
No claim is made that the catalog read and DROP are atomic against a separate
actor renaming/replacing databases concurrently.
