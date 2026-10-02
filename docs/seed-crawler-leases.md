# Seed crawler lease lifecycle

Each claimed run has a random `lease_owner`. Claims use a shared PostgreSQL
transaction lock and check the configured global concurrency limit. Renewal,
actor assignment, failure persistence and completion must still match the same
running run and owner. An expired run can be recovered with a new attempt;
the old worker cannot publish over the new owner.

The heartbeat renews the lease while provider work runs. A lost owner or a
database renewal error cancels the run and remains observable when the
heartbeat stops. Cancellation of the parent request also remains an error.
Normal completion stops the heartbeat with a distinct internal cancellation
cause: cancellation of an in-flight renewal by that stop is not a failed run.
This prevents successful provider work from being unnecessarily requeued.
An already observed database failure or ownership loss is never cleared by
normal shutdown.

No API or schema change is required. Deploy the worker change with the backend;
there is no data migration. Provider requests can still have reached an external
service before a crash, so this lease protocol does not guarantee exactly one
external execution or charge.

Run the regression tests against an explicitly configured disposable PostgreSQL
database, with `MCMODS_RUN_DB_INTEGRATION=1` and `MCMODS_TEST_DATABASE_URL` set:

```sh
go test -race ./internal/httpapi \
  -run '^(TestOCT02SeedCrawler.*Heartbeat.*|TestTEST020SeedCrawlerWorkerRetriesDegradesHonorsConcurrencyAndOwnsLeasesIntegration)$' \
  -count=1 -timeout=5m
```

The new tests use temporary tables and a single connection. A query tracer
controls cancellation at the real renewal boundary; constraint and ownership
failures use actual PostgreSQL results. The broader lifecycle test uses a
local HTTP provider fixture and disables AI requests. These tests do not call
real providers or establish production health or real translation quality.
