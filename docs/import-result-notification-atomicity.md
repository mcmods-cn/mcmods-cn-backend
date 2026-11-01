# Import result notification atomicity (CMP-003)

ZIP and embedded-icon import Workers now enqueue the existing result notification
inside the transaction that commits terminal success, promoted projections,
active artifacts and consumed-source tombstones. A notification-intent database
failure prevents terminal success from committing. The ordinary failure path
also commits failed state, failure log and failure notification together; a
stalled-job HTTP poll uses the same atomic failure boundary. Exhausted failure
persistence errors remain visible to the Worker caller.

The existing notification.direct event type, template keys, recipient and payload
semantics are preserved. No reward event or permission is added. Job row/run-token
and state predicates ensure repeated terminal attempts cannot enqueue another
intent. Once committed, redelivery carries the original outbox event ID, and the
existing NotificationWorker source-event deduplication creates one notification.
Notification intent persistence is not recipient receipt.

Every enqueue uses the caller's pgx transaction; it does not borrow another
connection while the terminal transaction holds one. The supported pool2/pool12,
lease cancellation and late-attempt behavior remain separate regression gates.
There is no schema migration or wire-format change. Update all import Worker
producers before relying on this guarantee; older producers retain the historical
post-commit failure window. Rolling back reopens that window and must not remove
existing intents, notifications or audit history.

The isolated PostgreSQL regressions inject exact XX001 constraints against
only the relevant notification template. They exercise actual import business
handlers, both source formats, ordinary source-owner retry and duplicate
notification consumers, plus failure/stalled persistence and recovery. Controlled
OSS and direct handler invocation are disclosed; these tests do not certify
real NATS delivery, browser success or production execution.

Historical terminal jobs without an observable result intent are candidates,
not proven missing notifications. Outbox or notification retention and account
removal can also remove those rows. Correlate immutable task attempts, retained
source events and delivery/retention records before any approved reconstruction.
No historical notification, experience, currency or balance backfill has been
performed. Normal shared objects and existing audit records remain untouched.
