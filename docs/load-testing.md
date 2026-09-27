# Load-test gate

`scripts/load-test.mjs` is a zero-dependency Node.js HTTP load generator. It refuses non-loopback targets unless `MCMODS_LOAD_ALLOW_REMOTE=1` is explicitly set for an authorized disposable environment. Mutation scenarios additionally require `MCMODS_LOAD_ENABLE_MUTATIONS=1` and test credentials supplied only through the process environment.

## Response contract

Each generated request has an explicit response contract. Ordinary reads accept only 2xx/3xx. The `antiabuse` and `crawlers` pools separately enumerate the structured 403/412/429 response codes that count as expected risk rejections; an allowed status with an unknown or missing risk code is still unexpected.

The report keeps raw `clientErrors`, `serverErrors`, status counts, and anti-abuse code counts, and adds:

- `successfulResponses`: contract-matching responses below 400;
- `expectedRejects`: contract-matching structured 4xx risk responses;
- `unexpectedResponses`: every HTTP response outside the selected request contract;
- `errorRate`: `(unexpectedResponses + networkErrors) / requests`, rounded to six decimal places;
- `contract.failureReasons`: the exact conditions that make the process exit nonzero.

The process exits nonzero for any unexpected HTTP response, network failure, missing required structured code, insufficient successful-response volume, or insufficient expected risk-rejection volume. The relevant settings are:

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

Request pools and the weighted mixed pool are constructed once per process, before workers start, so the load generator does not rebuild all arrays on every request.

## Query-plan evidence

The SQL files are psql scripts and refuse to choose an arbitrary first user:

```powershell
psql $env:DATABASE_URL -v representative_user_id=42 -v catalog_offset=10000 -v representative_action_id=3 -f scripts/query-analysis.sql
psql $env:DATABASE_URL -v representative_user_id=42 -v representative_action=comment.create -f scripts/anti-abuse-query-analysis.sql
```

Choose a deliberately high-cardinality user and a deep catalog offset from an isolated dataset containing at least 100k catalog rows and 1M activity rows. Every selected statement uses `EXPLAIN (ANALYZE, BUFFERS)`; the activity-retention DELETE executes inside an explicit transaction followed by `ROLLBACK`.

The JSON files already under `load-results` and `test-results/load` are immutable historical samples produced before this contract existed. In particular, their two-decimal `errorRate` may show zero despite real failures, and their aggregate 4xx count may include expected rejections. They remain useful only with the accompanying human interpretation and must not be promoted to an automated pass result or rewritten to simulate a new run.
