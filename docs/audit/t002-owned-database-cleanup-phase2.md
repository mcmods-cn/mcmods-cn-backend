# T-002: remaining isolated Go test databases

This test-only follow-up covers the eleven other Go helpers which create a
disposable database and previously dropped it with `WITH (FORCE)`. It is based
on the separate TEST018 ownership candidate. Neither batch is dynamically
verified at the time of this commit. The original 748e full PostgreSQL run and
its failures remain separate evidence.

Each helper retains its original name prefix, signature, caller contexts,
migration and pool configuration. In particular, TEST043 keeps six maximum
connections, and TEST033 still returns its database name and closes its
independent pools through the original cleanup registrations. All business
assertions and worker/channel cleanup are unchanged.

After its own CREATE succeeds, the helper records the actual database OID and
creator owner, persists a cryptographically random COMMENT nonce, and confirms
the complete tuple. Incomplete creation proof leaves the database for scoped
diagnosis. Cleanup first closes only its own pool within the existing
30-second cleanup bound, then rereads and compares the name/OID/owner/nonce,
performs an ordinary DROP, and confirms absence. A mismatch, read failure,
unclosed pool or live connection is an explicit cleanup failure. No backend
termination, forced removal or adoption by current name is permitted.

The HTTP API, progression and search packages each have private `*_test.go`
guards. They introduce no dependency into product runtime code. Component
negatives cover identity and scope mismatches and a controlled blocked closer;
the opt-in PostgreSQL negative creates a new owned substitute, checks exact
`55006` with a held connection, verifies the same PID and sentinel data remain,
then closes that connection normally before removing the database. None of
these tests needs a foreign resource.

Validation order is the original TEST018 two-file candidate first, then the
new guard tests and every real caller of the eleven wrappers, under normal
and race builds with the original contexts and command budgets. Any exposed
connection leak must remain a recorded failure and be fixed in its specific
test cleanup; it cannot justify FORCE or skipped assertions. Formatting and
static byte comparisons alone are not execution results.

This batch does not change the Python live-acceptance cleanup tool or two
intentional cleanup sites holding their own freshly started process handles.
The Python tool has separate parent/container/runtime/namespace guards and
needs a separately versioned ownership change. Historical records without an
original OID/owner/nonce cannot be promoted into cleanup authority. This batch
also does not establish the cause of a storage `53100` failure, prove all
cleanup paths safe, or claim atomic protection against an external actor
renaming or replacing databases between catalog statements.
