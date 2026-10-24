# Third-party import corrections (2026-10-24)

This batch covers TP-001/002/003/004/005/006/008, DGR-003 and the SQL root cause of CMP-007. TP-007's image component and origin handling belong to the frontend batch.

Public metadata imports accept GitHub only when its response explicitly declares `private:false`. Modrinth accepts the public direct-link states `approved`, `archived`, and `unlisted`; moderation, private and unknown states are refused before requesting versions, teams or README. Stored GitHub/Modrinth results are rechecked at read time; nonpublic sources return 403 and an upstream outage returns 503 without disclosing the result. This does not grant user-specific private-source authorization. Provider HTTP errors retain their status, not a potentially private response document.

Mod compatibility is derived from each public version's loader/version facts. Project-level summary arrays no longer invent relations. CurseForge file enumeration has a 20-page/1000-record cap and a total time budget equal to the configured request timeout; provider failures, repeated/inconsistent pages, budget exhaustion and a fully searched set with no eligible release are distinct errors. CurseForge manifests must declare `minecraftModpack` version 1 and valid Minecraft/loader/project/file references. Modpack categories use their own supported registry.

Binary downloads use uncredentialed HTTPS clients with exact provider CDN families. The DNS/IP guard applies on initial connection and each redirect. Only an independently configured API base can grant its exact origin the existing local fixture exception; the download URL cannot grant this exception. GitHub release CDN redirects are permitted without forwarding Authorization, API keys or cookies. Other-provider, private, userinfo, downgrade, alternate-port and redirect-loop targets remain denied. Existing bounded streaming and hash checks remain active.

Imported simple-project collection fields serialize as arrays, including the non-addon parent normalization path. Existing completed results are normalized at the read boundary without changing stored records or dropping unknown extension fields. Mixed modpack associations provide explicit PostgreSQL types for unresolved metadata; no placeholder Mod is inserted.

## Verification scope

New named tests live in `internal/httpapi/audit_thirdparty_contract_test.go` and `audit_thirdparty_http_integration_test.go`. The former uses real HTTP with controlled provider fixtures plus unit/serialization and scripted redirect assertions. The latter uses a real API server and PostgreSQL, ordinary creators and independent reviewers. Existing `TestTEST035ModpackImportWorkerBoundariesAndFinalStatesIntegration` verifies the actual business Worker against PostgreSQL and a controlled TLS provider. These tests do not establish real upstream provider behavior, real browser operation of all eleven editor combinations, or full release acceptance.

Activate the environment's Go toolchain, configure an owned PostgreSQL database and run:

```sh
source /workspace/.cloud-setup/activate.sh
MCMODS_RUN_DB_INTEGRATION=1 go test ./internal/httpapi \
  -run 'Test(TP00|DGR003|AuditCMP007|AuditTP001|TEST035ModpackImportWorkerBoundariesAndFinalStatesIntegration|ModImport|ExternalMetadata|Provider|Modrinth|CurseForge|SimpleProjectDraft)' \
  -count=1 -v
```

No production data repair was executed. Previously stored provider results are candidates for a scoped privacy review, not proof of a leak. The current read gate does not retract data already downloaded or prove that historically imported/published content was public at collection time. Preserve audit records and diagnose by public job ID, provider, timestamp and result byte count before considering any authorized remediation.
