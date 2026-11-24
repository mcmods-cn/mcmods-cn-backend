"""Select existing Go cases and check their go test -json terminal events."""
import argparse
import json
import re
import sys
from collections import defaultdict, deque


# These tests either connect to the owned endpoints or start and stop a real
# server. The durable-server restart case controls its own in-process broker.
CASES = {
    "httpapi": (
        "TestAuditMSG001And003RealRedisTwoHTTPInstancesColdRestart",
        "TestAuditMSG003OptionalRealRedisKeepsReadinessAndDurableAdmission",
        "TestOCT03PresenceMountedHTTPRejectsClientIdentityAndSourceBudgetBypass",
        "TestRBACCacheHonorsNaturalGrantAndDenyExpirationIntegration",
        "TestSecurityVersionCommittedPropagationFailureAcrossReplicasIntegration",
        "TestSecurityVersionDelayedRequestCannotAuthorizeNextRequestIntegration",
        "TestSecurityVersionDelayedWriterAndColdRedisIntegration",
        "TestAuditP2NEXT002RealTypesenseStaleIndexHTTPPostgresIntegration",
        "TestAuditP2RPH003RealTypesenseImportedAndIndependentSourceVisibilityIntegration",
        "TestOCT03CreatorCatalogRealTypesenseCursorIntegration",
    ),
    "querycache": (
        "TestOCT03PresenceRealRedisAtomicCardinalityPruningAndFallback",
        "TestOCT03PresenceRealRedisSharesSixtyAdmissionsAndKeepsOutageBudget",
        "TestUnreadDeltaKeepsGlobalInvalidationWithRealRedis",
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

ASSERTION_SITE = re.compile(r"^\s*([A-Za-z0-9_]+_test\.go:\d+): (.*)")
REALTIME_STATUS = re.compile(r"^(/api/v1/realtime/events|/ready|/live)=(\d{3}) want(\d{3}):")


def verify(events, expected, receipt):
    packages = {package for package, _ in expected}
    started = set()
    ended = set()
    completed_packages = set()
    issues = []
    diagnostics = defaultdict(lambda: deque(maxlen=4))
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
        if action == "output":
            match = ASSERTION_SITE.match(event.get("Output", ""))
            if match:
                entry = {"source": match[1]}
                # Keep literal paths and numbers from the realtime fixture.
                # Its HTTP response and all other free-form text stay private.
                status = REALTIME_STATUS.match(match[2])
                if status:
                    entry.update(path=status[1], observed=int(status[2]), expected=int(status[3]))
                diagnostics[case].append(entry)
        if action in ("pass", "fail", "skip"):
            result = {"package": package, "test": name, "action": action, "seconds": event.get("Elapsed")}
            if action == "fail" and diagnostics[case]:
                result["test_locations"] = list(diagnostics[case])
            print(json.dumps(result), file=receipt)
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
    delayed = "TestSecurityVersionDelayedRequestCannotAuthorizeNextRequestIntegration"
    required = expected | {("mcmods-cn-backend/internal/httpapi", presence + "/" + mode)
                           for mode in ("shared-true", "shared-false")}
    required.update(("mcmods-cn-backend/internal/httpapi", delayed + "/" + kind)
                    for kind in ("permission", "rbac", "project-acl", "auth"))
    issues = verify((json.loads(line) for line in sys.stdin), required, sys.stdout)
    if issues:
        raise SystemExit("\n".join(issues))
    print(f"Verified {len(expected)} required top-level cases; every selected test and subtest passed.", file=sys.stderr)


if __name__ == "__main__":
    main()
