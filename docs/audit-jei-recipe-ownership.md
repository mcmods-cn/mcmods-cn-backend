# F-002: JEI technical categories and recipe source ownership

A canonical `auditfixture:*` recipe can legitimately use `minecraft:crafting`.
The category and template retain their technical identities. Their observation
revision belongs to the admitted recipe source, derived from the index's
`source_mod_id`, `source_mod_id_source`, canonical flag and recipe ID. A canonical
recipe uses its recipe-ID namespace; a generated/noncanonical recipe uses its
category namespace. Unknown or contradictory provenance fails explicitly.

`exportRecipeImportScope` is constructed after the existing Mod identity,
Minecraft/loader, target version and original-file hash gates. It receives only
those admitted namespaces. Each dependency observation gets a temporary map for
its recipe helper; the original namespace map and the strict resource/tag/document
helpers are unchanged. Binding ownership and template references must match the
index and declared collections. Cross-project or undeclared source namespaces
are not granted import authority by referring to a shared category.

Both declared categories v6 and v7 remain supported. V7 template-only categories
retain scoped template observations and explicitly omitted recipe bodies. They
do not fabricate recipes. Canonical editor templates and slots are updated only
by their own import source, with manual publication protected. Other sources keep
their independent imported snapshots and recipe rendering without replacing the
existing canonical editor definition.

No SQL schema migration is introduced by this change. Existing failed jobs can
use the normal authorized retry path, subject to current creator/permission and
source-file rules. This task does not automatically retry production jobs or
rewrite historical archives. Source/category namespace differences are expected;
do not use that difference alone as a stock-data corruption diagnostic. A recipe
whose retained `owner_mod_id` conflicts with its source revision merits a read-only
provenance review; do not automatically transfer ownership or choose a canonical
template winner without retained history and an explicit product decision.

The committed small golden archives are unchanged outputs from actual Minecraft
1.20.1 / Fabric 0.16.14 / JEI 15.20.0.133, running exporter source
`806214802c1e3447a9895fa5311e73f3be6edfcc` in the real Gradle client with OpenGL.
The full sample SHA256 is
`059b1c56921a3f70b934bd577fd940486982da03b24d2f6dca7c0efcfb5af2eb`.
The separate template-only sample came from exporter `a3b0f66`; its SHA256 is
`95bc42266d526b8b91a23c13da8af4cb75c207b3fa73280ac7463b139893b431`.
The original package bytes are checked before tests read them; counterexamples
and multi-owner data are controlled in-memory/DB fixtures, not claims of more
actual game exports.

Validation uses an explicitly owned loopback PostgreSQL database. Configure both
`DATABASE_URL` and `MCMODS_TEST_DATABASE_URL` to that database and set
`MCMODS_RUN_DB_INTEGRATION=1`, then run:

```sh
go test -race ./internal/httpapi -run '^Test(AuditRecipeScope|ExportRevisionRequiresExactNamespace|ImportQueuesRejectUnknownNamespaces|AuditFixJEIV7ProducerProjectionWorkerIntegration|AuditFixGRR01ConcurrentWorkersProgressAtSupportedPoolSizesIntegration)' -count=1 -timeout=8m -v
```

The actual-game integration test uploads the original bytes through mounted HTTP,
uses PostgreSQL and calls the actual business Worker directly, verifies ready,
keeps pending content anonymous-inaccessible, rejects author approval, approves
with an independent reviewer, and verifies anonymous metadata/assets plus the
quantity-two output. It also verifies that another source cannot replace the
canonical template or slot geometry while its own observation persists. This
proves that integration layer; the browser, real JetStream delivery and subscribed
Worker journey is a separately recorded execution.
