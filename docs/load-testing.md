# Load-test gate

`scripts/load-test.mjs` is a zero-dependency Node.js HTTP load generator. It refuses non-loopback targets unless `MCMODS_LOAD_ALLOW_REMOTE=1` is explicitly set for an authorized disposable environment. Comment writes require `MCMODS_LOAD_ENABLE_MUTATIONS=1`. An authenticated presence scenario may record session activity, and `MCMODS_LOAD_ENABLE_ADMIN_PREVIEW=1` enables cleanup previews. Supply synthetic credentials through the process environment.

## Response contract

Each generated request has an explicit response contract. Ordinary reads accept direct 2xx/3xx responses and record redirects at the selected endpoint. The `antiabuse` pool recognizes configured rejection codes on 403, 428, and 429, including `428 challenge_required`. The `crawlers` pool uses its own 403/429 allowlist. A missing or unknown rejection code fails the contract.

The report keeps raw `clientErrors`, `serverErrors`, status counts, and anti-abuse code counts, and adds:

- `successfulResponses`: contract-matching responses below 400;
- `expectedRejects`: contract-matching structured 4xx risk responses;
- `unexpectedResponses`: every HTTP response outside the selected request contract;
- `errorRate`: `(unexpectedResponses + networkErrors) / requests`, rounded to six decimal places;
- `contract.failureReasons`: the exact conditions that make the process exit nonzero.

The process exits nonzero for any unexpected HTTP response, network failure, missing required structured code, insufficient successful-response volume, or insufficient expected risk-rejection volume. An interrupted run retains the partial JSON and exits nonzero with `contract.interrupted` set. The relevant settings are:

- `MCMODS_LOAD_MIN_SUCCESSFUL_RESPONSES` (default `1`);
- `MCMODS_LOAD_MIN_RISK_REJECTS` (default `1` for `antiabuse`, otherwise `0`);
- `MCMODS_LOAD_REQUIRED_CODES` (comma-separated exact response codes).

For an anti-abuse gate, set required codes to the behavior under test instead of accepting any 4xx:

```powershell
$env:MCMODS_LOAD_SCENARIO='antiabuse'
$env:MCMODS_LOAD_ENABLE_MUTATIONS='1'
$env:MCMODS_LOAD_REQUIRED_CODES='rate_limited,duplicate_content'
$env:MCMODS_LOAD_MIN_SUCCESSFUL_RESPONSES='1'
$env:MCMODS_LOAD_MIN_RISK_REJECTS='2'
node scripts/load-test.mjs
```

Request pools are built once before the workers start. The catalog pool samples a Mod and Plugin offset page and the first server page. To measure server continuation, use the `nextCursor` returned for the same filters and sort in a separate test.

## Query-plan evidence

Run the psql scripts against the current schema in a verified disposable dataset. Both check the database name and require an existing internal user. The general script also requires an activity action ID and a catalog offset.

```powershell
psql -X $env:DATABASE_URL -v expected_database=isolated_mcmods -v representative_user_id=42 -v catalog_offset=10000 -v representative_action_id=3 -f scripts/query-analysis.sql
psql -X $env:DATABASE_URL -v expected_database=isolated_mcmods -v representative_user_id=42 -v representative_action=comment.create -f scripts/anti-abuse-query-analysis.sql
```

For a reference load of at least 100k catalog rows and 1M activity rows, repeat with a high-cardinality user, a typical user, and a filter with no matching rows. Run shallow and deep offsets and record the data distribution and cache conditions. The `EXPLAIN (ANALYZE, BUFFERS)` statements measure database operations in isolation. Catalog plans focus on sorting, filtering, and pagination in PostgreSQL. Measure association hydration, HTTP, Typesense, and live Redis presence separately. The presence plan reads the candidates for pruning persisted authenticated-session snapshots.

The activity-retention DELETE actually runs inside a transaction followed by `ROLLBACK`. The outbox selection takes row locks for the duration of the statement. Use a database owned by this validation run. A small schema smoke test can check that the plans execute and that rollback preserves rows.

The JSON files already under `load-results` and `test-results/load` are immutable historical samples produced before this contract existed. In particular, their two-decimal `errorRate` may show zero despite real failures, and their aggregate 4xx count may include expected rejections. They remain useful only with the accompanying human interpretation and must not be promoted to an automated pass result or rewritten to simulate a new run.
