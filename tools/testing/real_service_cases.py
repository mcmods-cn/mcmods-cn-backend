"""Select existing Go cases and check their go test -json terminal events."""
import argparse
import json
import sys


# These tests either connect to the owned endpoints or start and stop a real
# server. The durable-server restart case controls its own in-process broker.
CASES = {
    "httpapi": (
        "TestAuditMSG001And003RealRedisTwoHTTPInstancesColdRestart",
        "TestAuditMSG003OptionalRealRedisKeepsReadinessAndDurableAdmission",
        "TestOCT03PresenceMountedHTTPRejectsClientIdentityAndSourceBudgetBypass",
        "TestRBACCacheHonorsNaturalGrantAndDenyExpirationIntegration",
        "TestAuditP2NEXT002RealTypesenseStaleIndexHTTPPostgresIntegration",
        "TestAuditP2RPH003RealTypesenseImportedAndIndependentSourceVisibilityIntegration",
        "TestOCT03CreatorCatalogRealTypesenseCursorIntegration",
    ),
    "querycache": (
        "TestOCT03PresenceRealRedisAtomicCardinalityPruningAndFallback",
        "TestOCT03PresenceRealRedisSharesSixtyAdmissionsAndKeepsOutageBudget",
    ),
    "queue": (
        "TestClosingQueueCancelsRunningTaskIntegration",
        "TestCoreOnlyPeerConsumesTasksExclusivelyFromPostgres",
        "TestCandidateRollbackRetainsPeerAcknowledgedMessages",
        "TestDeadLetterDatabaseRecoverySurvivesWorkerRestartIntegration",
        "TestDeadLetterIsRetainedByBrokerWhenDatabaseSinkFailsIntegration",
        "TestDurablePublishRequiresBrokerConfirmation",
        "TestFullStreamPreservesOutboxUntilCapacityReturns",
        "TestJetStreamDurableConsumerResumesBacklogAfterRestartIntegration",
        "TestJetStreamExplicitAckRedeliveryAndDeduplicationIntegration",
        "TestJetStreamLongHandlerAndRetryDelayIntegration",
        "TestJetStreamReconnectsAfterServerRestartIntegration",
        "TestPreprovisionedStreamRetainsItsLimitsAndReportsUsage",
        "TestTaskMaxConcurrentIsTheAuthoritativeExecutionLimitIntegration",
        "TestTimedOutTaskCanPersistDeadLetterIntegration",
        "TestTransactionalOutboxRollbackAndDispatchIntegration",
        "TestUnsupportedDeadLetterDoesNotStarveRecoveryIntegration",
    ),
    "searchindex": (
        "TestAuditGRR02RealTypesenseRebuildRemovesLegacyNonapprovedTermsIntegration",
        "TestRealTypesenseImportsAndFindsLocalizedTextIntegration",
    ),
}


def verify(events, expected, receipt):
    packages = {package for package, _ in expected}
    started = set()
    ended = set()
    completed_packages = set()
    issues = []
    for event in events:
        action, package, name = event.get("Action"), event.get("Package"), event.get("Test")
        if package not in packages:
            continue
        if name is None:
            if action == "pass":
                completed_packages.add(package)
            elif action in ("fail", "skip"):
                issues.append(f"{package}: {action}")
            continue
        case = (package, name)
        if (package, name.split("/", 1)[0]) not in expected:
            continue
        if action == "run":
            started.add(case)
        if action in ("pass", "fail", "skip"):
            print(json.dumps({"package": package, "test": name, "action": action,
                              "seconds": event.get("Elapsed")}), file=receipt)
            if case not in started or case in ended or action != "pass":
                issues.append(f"{package}/{name}: {action} (started={case in started}, duplicate={case in ended})")
            ended.add(case)
    for package, name in sorted((started | expected) - ended):
        issues.append(f"{package}/{name}: missing terminal event")
    for package in sorted(packages - completed_packages):
        issues.append(f"{package}: missing package pass")
    return issues


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("pattern", "verify"))
    args = parser.parse_args()
    expected = {("mcmods-cn-backend/internal/" + package, name)
                for package, names in CASES.items() for name in names}
    if args.command == "pattern":
        print("^(" + "|".join(sorted(name for _, name in expected)) + ")$")
        return
    # The same HTTP case compares both admission paths. A missing shared
    # subtest must not silently turn the live Redis check into a local-only run.
    presence = "TestOCT03PresenceMountedHTTPRejectsClientIdentityAndSourceBudgetBypass"
    required = expected | {("mcmods-cn-backend/internal/httpapi", presence + "/" + mode)
                           for mode in ("shared-true", "shared-false")}
    issues = verify((json.loads(line) for line in sys.stdin), required, sys.stdout)
    if issues:
        raise SystemExit("\n".join(issues))
    print(f"Verified {len(expected)} required top-level cases; every selected test and subtest passed.", file=sys.stderr)


if __name__ == "__main__":
    main()
