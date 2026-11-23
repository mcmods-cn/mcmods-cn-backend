# Serial PostgreSQL regression batches

The ordinary `go test ./... -count=1` gate remains required. Explicit database
integration is a second gate: discover every top-level Test and Fuzz seed in
`go list ./...`, invoke each name exactly once, and fail if any batch fails.
Conditional skips are printed by Go; `invoked` is not a claim that skipped
tests passed. Required external/live fixtures must be validated separately.
When opting into [real Typesense tests](../../../docs/typesense-search.md), provide both the dedicated loopback service credentials and the absolute server executable path. The creator cursor test starts its own process. The Redis restart tests also take a separate `MCMODS_TEST_REDIS_BINARY` executable.
Package discovery parses only `go list` stdout. Module-download diagnostics
remain visible on stderr and cannot become test package names; a failed
`go list` stops the runner instead of using a partial list.

Use only an owned, fully initialized loopback test database. Initialize a new
empty database with `go run ./cmd/db-initialize` using the same configuration
as the API and set:

```text
APP_ENV=test
DATABASE_URL=postgres://postgres@127.0.0.1:5432/mcmods_test?sslmode=disable
MCMODS_TEST_DATABASE_URL=<exactly the same URL>
MCMODS_RUN_DB_INTEGRATION=1
MCMODS_RUN_ACTIVITY_LOAD=1
```

Use a single loopback endpoint with the host and any custom port in the URL authority,
and the database in its path. Query options can set TLS and other connection behavior.

```sh
go run ./tools/testing/db_suite
go run ./tools/testing/db_suite -race
```

The runner does not initialize/delete a database, change any fixture context,
raise a query budget, enable parallel DB fixtures, or use cached test results.
Each invocation retains Go's ten-minute package alarm, `-count=1`, `-p=1`,
and `-parallel=1`; only the number of independent fixtures accumulated under
one alarm is bounded (default 30). `-batch-size` may range from 1 to 100.
`MCMODS_GO_BINARY` can select an explicit installed Go executable.
