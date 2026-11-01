# CurseForge modpack job pagination and manifest evidence

TP-003 and TP-004 originally required full HTTP/PostgreSQL with a local provider.
An official key and a live CurseForge project are separate validation layers.
Earlier fixes already paginate within20pages/1000records and the configured
total timeout, distinguish provider failure/repeated pages/budget exhaustion/no
legal release, and reject unsupported manifests and invalid references. Earlier
archive-importer function tests did not demonstrate the HTTP-created job's
stored terminal state, creator-only result and absence of publication.

The private `runModMetadataImportWithProviderClients` core accepts separate API
and archive client factories. The existing API-only factory signature stays
unchanged. Production entry points always bind the original
`newProviderHTTPClient` and `newProviderDownloadHTTPClient`; the six simple
project and Mod branches retain their original bodies. The original direct
modpack importer and archive downloader signatures also keep their secure
defaults. This is explicit dependency construction, with no runtime setting,
environment/context/Server-field switch, exported test endpoint, global
transport/DNS modification, credential-origin relaxation or TLS bypass.

The named regressions create real jobs through an ordinary user's HTTP API,
execute the complete Worker business body and read the actual owner endpoint
and PostgreSQL rows. Strict factories permit only the declared CurseForge API
and archive authorities, connecting them to two owned TLS peers with verified
certificate chains. Only the API peer receives the synthetic shared key. Every
page index, request count, factory argument and TLS response is asserted. The
archive bytes are generated once per case, served unchanged, and their SHA256
is recorded. Production redirect/DNS/address policies are tested separately;
this controlled layer makes no official-network or real-queue delivery claim.

The pagination matrix covers a legal release only on page2, exact page2HTTP429,
a real supported5s deadline, a repeated page, all nonrelease files, a legal
record1000 and an unread record1001 budget exhaustion. No retry enlarges the
20page or1000record bound. The manifest matrix covers valid version1, the
original simultaneous wrong type/negative version, individual unsupported
types/versions, missing Minecraft/loaders, an empty loader and invalid or
conflicting project/file references. Failures require the exact stored error,
`failed`/SQLNULL result and no usable owner result. Success requires the selected
file ID, provider references, required-side flags and canonical
`technology`/`Forge`/`1.20.1` result fields. All cases preserve the full
business/outbox snapshot after job creation and keep the anonymous modpack
catalog empty; a preview never substitutes for independent publication.

The first fixture lacked the ordinary peer's base `modpack.create` capability,
so the route correctly returned403 before the intended foreign-owner404. The
peer now has that exact capability and the404 ownership assertion remains.
Later positive-field observations corrected source category `tech` to the
current canonical `technology` and producer loader `forge` to the consumer's
canonical `Forge`. The private cloned RoundTripper also forwards the original
bounded cancellable Context without the deprecated `Request.Cancel` channel;
the first draft masked its deadline as a legacy client-cancel error. The exact
DeadlineExceeded oracle and production timeout remain. Original whole-run
failures and each correction are retained in the task evidence.

Run the named actual layer in an authorized owned loopback test environment:

```sh
go test -race ./internal/httpapi -run '^TestAuditTP00[34]ActualHTTPPostgresWorker' -count=1 -v -timeout=6m
```

Set the explicit owned PostgreSQL integration variables. The optional
`MCMODS_AUDIT_PG_LOCK` test-only setting serializes fixture setup/drop; do not
also take that same flock outside the command. A project `db_suite` batch takes
its own outer mutex with this optional test variable unset. Preserve the final
command, immutable commit, first failures and actual terminal results.

No schema, API wire format, frontend or exporter deployment order changes.
Reverting this dependency refactor retains the earlier defect fixes; reverting
the original pagination/manifest validation would reopen them. Source ZIPs are
temporary and removed by the importer, so retained old task previews generally
cannot prove their original manifest discriminator. A historic failed task or
completed preview is only a candidate until its exact original source is
correlated. No production scan, automatic task replay, content deletion or
repair was performed. New ordinary uploads and independent review remain the
supported revalidation path; do not infer historic publication was safe from a
new passing preview.
