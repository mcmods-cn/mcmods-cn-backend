# PostgreSQL load observer

`cmd/load-observer` samples the current database's rows in `pg_stat_activity` every 250 ms and emits one JSON report. It is read-only and does not reset, migrate, or seed the database.

## Connection configuration

The command uses the same configuration loader as the API:

1. A non-empty `DATABASE_URL` is the complete connection authority.
2. Otherwise `DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USER`, `DB_PASSWORD`, and `DB_SSLMODE` are composed into the connection string.

When `DATABASE_URL` is present, its path also supplies the effective database name used by the separate development-reset safety checks. `DB_RESET_CONFIRM` must therefore be `RESET <database-name-from-DATABASE_URL>`, not the ignored `DB_NAME` value. The observer itself never enables reset behavior.

## Observer settings

- `MCMODS_OBSERVER_SECONDS`: sampling duration from 1 through 3600 seconds; omitted, invalid, zero, or out-of-range values use 60 seconds.
- `MCMODS_OBSERVER_OUTPUT`: optional report file. The command writes it with mode `0600`; the parent directory must already exist.

PowerShell example:

```powershell
$env:MCMODS_OBSERVER_SECONDS='30'
$env:MCMODS_OBSERVER_OUTPUT='test-results/load/database-observer.json'
& 'D:\System\SDK\go\go1.26.4\bin\go.exe' run ./cmd/load-observer
```

The report is printed to stdout after any requested file has been written. Database connection failure, any failed/timed-out sample, JSON encoding failure, stdout failure, or an explicitly requested output-file failure makes the command exit nonzero. A report containing sampling errors may still be written for diagnosis, but must not be treated as a successful performance artifact.
