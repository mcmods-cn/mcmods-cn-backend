#!/usr/bin/env node

import { writeFile } from "node:fs/promises";
import { performance } from "node:perf_hooks";

import { classifyLoadResponse, summarizeLoadOutcomes } from "./load-test-lib.mjs";

const baseURL = new URL(process.env.MCMODS_LOAD_BASE_URL ?? "http://127.0.0.1:8080");
const localHosts = new Set(["127.0.0.1", "localhost", "::1", "[::1]"]);
if (!localHosts.has(baseURL.hostname) && process.env.MCMODS_LOAD_ALLOW_REMOTE !== "1") {
  throw new Error("Refusing to load-test a non-local host. Set MCMODS_LOAD_ALLOW_REMOTE=1 only for an authorized test environment.");
}

const stages = parseStages(process.env.MCMODS_LOAD_STAGES ?? "2x10,5x15,10x20,20x10,5x15");
const scenarioName = process.env.MCMODS_LOAD_SCENARIO ?? "mixed";
const token = process.env.MCMODS_LOAD_TOKEN ?? "";
const authCookie = process.env.MCMODS_LOAD_COOKIE ?? "";
const adminToken = process.env.MCMODS_LOAD_ADMIN_TOKEN ?? "";
const enableAdminPreview = process.env.MCMODS_LOAD_ENABLE_ADMIN_PREVIEW === "1";
const enableMutations = process.env.MCMODS_LOAD_ENABLE_MUTATIONS === "1";
const userID = process.env.MCMODS_LOAD_USER_ID ?? "";
const cardUserIDs = splitValues(process.env.MCMODS_LOAD_CARD_USER_IDS ?? userID);
const commentTargetType = process.env.MCMODS_LOAD_COMMENT_TARGET_TYPE ?? "";
const commentTargetID = process.env.MCMODS_LOAD_COMMENT_TARGET_ID ?? "";
const mutationCommentID = process.env.MCMODS_LOAD_COMMENT_ID ?? "";
const fixedCommentIdempotencyKey = process.env.MCMODS_LOAD_COMMENT_IDEMPOTENCY_KEY ?? "";
const botToken = process.env.MCMODS_LOAD_BOT_TOKEN ?? "";
const outputPath = process.env.MCMODS_LOAD_OUTPUT ?? "";
const requestTimeoutMS = boundedNumber(process.env.MCMODS_LOAD_TIMEOUT_MS, 10_000, 500, 120_000);
const includeReadinessProbe = process.env.MCMODS_LOAD_INCLUDE_READY === "1";
const minimumSuccessfulResponses = boundedNumber(process.env.MCMODS_LOAD_MIN_SUCCESSFUL_RESPONSES, 1, 1, 1_000_000_000);
const minimumRiskRejects = boundedNumber(process.env.MCMODS_LOAD_MIN_RISK_REJECTS, scenarioName === "antiabuse" ? 1 : 0, 0, 1_000_000_000);
const requiredCodes = splitValues(process.env.MCMODS_LOAD_REQUIRED_CODES ?? "");

const results = [];
const outcomes = [];
const statusCounts = new Map();
const scenarioCounts = new Map();
const antiAbuseCodes = new Map();
let networkErrors = 0;
let completed = 0;
let stopped = false;
const startedAt = new Date();
const started = performance.now();
const requestPools = createRequestPools();
const selectedRequests = scenarioName === "mixed" ? createMixedRequests(requestPools) : requestPools[scenarioName];
if (!selectedRequests?.length) throw new Error(`Scenario ${scenarioName} has no runnable requests with the supplied configuration.`);

process.on("SIGINT", () => { stopped = true; });

for (const stage of stages) {
  if (stopped) break;
  const deadline = performance.now() + stage.seconds * 1_000;
  const workers = Array.from({ length: stage.concurrency }, (_, index) => worker(index, deadline));
  await Promise.all(workers);
}

const elapsedSeconds = Math.max(0.001, (performance.now() - started) / 1_000);
const sorted = results.toSorted((left, right) => left - right);
const success = [...statusCounts.entries()].filter(([status]) => status >= 200 && status < 400).reduce((sum, [, count]) => sum + count, 0);
const clientErrors = [...statusCounts.entries()].filter(([status]) => status >= 400 && status < 500).reduce((sum, [, count]) => sum + count, 0);
const serverErrors = [...statusCounts.entries()].filter(([status]) => status >= 500).reduce((sum, [, count]) => sum + count, 0);
const outcomeSummary = summarizeLoadOutcomes({
  outcomes,
  networkErrors,
  completed,
  minimumSuccessfulResponses,
  minimumRiskRejects,
  requiredCodes,
});
const report = {
  generatedAt: new Date().toISOString(),
  startedAt: startedAt.toISOString(),
  baseURL: baseURL.origin,
  scenario: scenarioName,
  stages,
  elapsedSeconds: round(elapsedSeconds),
  requests: completed,
  success,
  clientErrors,
  serverErrors,
  networkErrors,
  expectedResponses: outcomeSummary.expectedResponses,
  expectedRejects: outcomeSummary.expectedRejects,
  successfulResponses: outcomeSummary.successfulResponses,
  unexpectedResponses: outcomeSummary.unexpectedResponses,
  errorRate: outcomeSummary.failureRate,
  requestsPerSecond: round(completed / elapsedSeconds),
  latencyMS: {
    average: round(sorted.reduce((sum, value) => sum + value, 0) / Math.max(1, sorted.length)),
    p50: percentile(sorted, 0.50),
    p90: percentile(sorted, 0.90),
    p95: percentile(sorted, 0.95),
    p99: percentile(sorted, 0.99),
    maximum: round(sorted.at(-1) ?? 0),
  },
  statuses: Object.fromEntries([...statusCounts.entries()].sort(([left], [right]) => left - right)),
  requestsByScenario: Object.fromEntries([...scenarioCounts.entries()].sort()),
  antiAbuseCodes: Object.fromEntries([...antiAbuseCodes.entries()].sort()),
  contract: {
    minimumSuccessfulResponses,
    minimumRiskRejects,
    requiredCodes,
    missingRequiredCodes: outcomeSummary.missingRequiredCodes,
    failureReasons: outcomeSummary.failureReasons,
  },
  notes: [
    "This client does not infer backend CPU, memory, DB connections, locks, or cache hit rate; capture those from the test environment telemetry.",
    "The script never calls the destructive cleanup execute endpoint. Activity cleanup load covers preview only.",
    "errorRate counts network failures and responses outside each request's explicit contract; expected anti-abuse/crawler rejections are reported separately.",
    enableMutations ? "Comment mutations were explicitly enabled; run only against disposable test data." : "Comment mutations were disabled (safe default).",
  ],
};

process.stdout.write(`${JSON.stringify(report, null, 2)}\n`);
if (outputPath) await writeFile(outputPath, `${JSON.stringify(report, null, 2)}\n`, "utf8");
if (outcomeSummary.failureReasons.length) process.exitCode = 1;

async function worker(workerID, deadline) {
  let sequence = workerID;
  while (!stopped && performance.now() < deadline) {
    const request = chooseRequest(sequence++);
    if (!request) {
      await delay(100);
      continue;
    }
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), requestTimeoutMS);
    const requestStarted = performance.now();
    try {
      const body = request.bodyFactory ? request.bodyFactory(workerID, sequence) : request.body;
      const response = await fetch(new URL(request.path, baseURL), {
        method: request.method ?? "GET",
        headers: {
          ...(request.token ? { Authorization: `Bearer ${request.token}` } : {}),
          ...(authCookie ? { Cookie: authCookie } : {}),
          ...(body ? { "Content-Type": "application/json" } : {}),
          ...(request.headers ?? {}),
          "X-Client-ID": request.clientID ?? `mcmods-load-${workerID % 8}`,
          "X-Load-Test": "mcmods-local-load-test",
        },
        body: body ? JSON.stringify(body) : undefined,
        signal: controller.signal,
      });
      const responseBody = await response.text();
      let code = "";
      try {
        const parsedCode = JSON.parse(responseBody)?.code;
        if (typeof parsedCode === "string" && parsedCode) {
          code = parsedCode;
          antiAbuseCodes.set(code, (antiAbuseCodes.get(code) ?? 0) + 1);
        }
      } catch {}
      statusCounts.set(response.status, (statusCounts.get(response.status) ?? 0) + 1);
      outcomes.push({
        name: request.name,
        status: response.status,
        code,
        ...classifyLoadResponse(request, response.status, code),
      });
    } catch {
      networkErrors++;
    } finally {
      clearTimeout(timer);
      results.push(performance.now() - requestStarted);
      completed++;
      scenarioCounts.set(request.name, (scenarioCounts.get(request.name) ?? 0) + 1);
    }
  }
}

function createRequestPools() {
  return {
    catalogs: catalogRequests(),
    cards: cardRequests(),
    comments: commentRequests(),
    presence: presenceRequests(),
    statistics: statisticsRequests(),
    activity: activityRequests(),
    antiabuse: antiAbuseRequests(),
    crawlers: crawlerRequests(),
    infrastructure: infrastructureRequests(),
  };
}

function createMixedRequests(pools) {
  return interleave([
    repeat(pools.catalogs, 6),
    repeat(pools.cards, 2),
    repeat(pools.comments, 2),
    pools.presence,
    pools.statistics,
    pools.activity,
  ]);
}

function chooseRequest(sequence) {
  return selectedRequests[sequence % selectedRequests.length];
}

function infrastructureRequests() {
  // Readiness probes touch dependencies and are intentionally rare. Liveness
  // is the only endpoint suitable for high-frequency process probing.
  const requests = Array.from({ length: 64 }, () => ({ name: "liveness", path: "/live" }));
  if (includeReadinessProbe) requests.push({ name: "readiness", path: "/ready" });
  if (token || authCookie) {
    requests.push(
      { name: "unread-summary", path: "/api/v1/me/unread-summary", token },
      { name: "message-conversations", path: "/api/v1/messages/conversations", token },
    );
  }
  if (token) requests.push({ name: "presence", path: "/api/v1/site/presence", method: "POST", token, body: { visitorId: "mcmods-infrastructure-load" } });
  return requests;
}

function catalogRequests() {
  const page = 2;
  return [
    { name: "mods-heat", path: `/api/v1/mods?sort=heat&limit=20&offset=${(page - 1) * 20}`, token },
    { name: "mods-version-heat", path: "/api/v1/mods?sort=heat&version=1.20.1&limit=20", token },
    { name: "plugins-heat", path: `/api/v1/content-projects/plugin?sort=heat&limit=20&offset=${(page - 1) * 20}`, token },
    { name: "maps-updated", path: "/api/v1/content-projects/map?sort=updated&limit=20", token },
    { name: "servers-heat", path: `/api/v1/servers?sort=heat&limit=20&page=${page}`, token },
    { name: "tutorials", path: "/api/v1/community/posts?kind=tutorial&sort=latest&limit=20&offset=0", token },
  ];
}

function cardRequests() {
  const requests = cardUserIDs.map((id) => ({ name: "user-card", path: `/api/v1/users/${encodeURIComponent(id)}/card`, token }));
  if (token) requests.push({ name: "message-conversations", path: "/api/v1/messages/conversations", token });
  return requests;
}

function commentRequests() {
  const requests = [];
  if (commentTargetType && commentTargetID) {
    requests.push({ name: "comments-list-latest", path: `/api/v1/comment-targets/${encodeURIComponent(commentTargetType)}/${encodeURIComponent(commentTargetID)}/comments?sort=latest&limit=50`, token });
    requests.push({ name: "comments-list-hot", path: `/api/v1/comment-targets/${encodeURIComponent(commentTargetType)}/${encodeURIComponent(commentTargetID)}/comments?sort=hot&limit=50`, token });
    if (enableMutations && token) requests.push({
      name: "comment-create",
      path: `/api/v1/comment-targets/${encodeURIComponent(commentTargetType)}/${encodeURIComponent(commentTargetID)}/comments`,
      method: "POST",
      token,
      bodyFactory: (workerID, sequence) => ({
        body: `[mcmods-load-test] worker=${workerID} sequence=${sequence}`,
        idempotencyKey: fixedCommentIdempotencyKey || `mcmods-load-${workerID}-${sequence}-${startedAt.getTime()}`,
      }),
    });
  }
  if (mutationCommentID) {
    requests.push({ name: "comment-replies", path: `/api/v1/comments/${encodeURIComponent(mutationCommentID)}/replies`, token });
    requests.push({ name: "comment-thread", path: `/api/v1/comments/${encodeURIComponent(mutationCommentID)}/thread`, token });
  }
  if (enableMutations && token && mutationCommentID) {
    requests.push({ name: "comment-edit", path: `/api/v1/comments/${encodeURIComponent(mutationCommentID)}`, method: "PATCH", token,
      bodyFactory: (workerID, sequence) => ({ body: `[mcmods-load-test-edit] worker=${workerID} sequence=${sequence}` }) });
    requests.push({ name: "comment-react", path: `/api/v1/comments/${encodeURIComponent(mutationCommentID)}/reaction`, method: "PUT", token,
      body: { reaction: "thumbs_up" } });
  }
  return requests;
}

function presenceRequests() {
  if (!token) return [];
  return [{ name: "presence", path: "/api/v1/site/presence", method: "POST", token, body: { visitorId: "mcmods-load-test-browser-session" } }];
}

function statisticsRequests() {
  if (!token) return [];
  return ["all", "7d", "30d", "90d", "1y"].map((range) => ({ name: `statistics-${range}`, path: `/api/v1/users/me/statistics?range=${range}`, token }));
}

function activityRequests() {
  if (!adminToken) return [];
  const requests = [
    { name: "admin-logs", path: "/api/v1/admin/logs?kind=user&limit=50", token: adminToken },
    { name: "activity-retention-config", path: "/api/v1/admin/activity-logs/retention", token: adminToken },
    { name: "activity-ingestion-status", path: "/api/v1/admin/activity-logs/ingestion", token: adminToken },
    { name: "anti-abuse-overview", path: "/api/v1/admin/anti-abuse/overview", token: adminToken },
    { name: "anti-abuse-events", path: "/api/v1/admin/anti-abuse/events?limit=100", token: adminToken },
  ];
  if (enableAdminPreview) requests.push({
      name: "activity-cleanup-preview",
      path: "/api/v1/admin/activity-logs/cleanup/preview",
      method: "POST",
      token: adminToken,
      body: { actions: ["view"], from: new Date(Date.now() - 30 * 86_400_000).toISOString() },
  });
  return requests;
}

function antiAbuseRequests() {
  if (!enableMutations || !token || !commentTargetType || !commentTargetID) return [];
  const path = `/api/v1/comment-targets/${encodeURIComponent(commentTargetType)}/${encodeURIComponent(commentTargetID)}/comments`;
  const replayKey = fixedCommentIdempotencyKey || `replay-${startedAt.getTime()}`;
  const expectedStatuses = [200, 201, 202, 403, 412, 429];
  const expectedRiskCodes = [
    "action_restricted",
    "challenge_required",
    "duplicate_content",
    "pending_review_limit",
    "rate_limited",
    "request_denied",
    "temporarily_blocked",
  ];
  return [
    { name: "spam-exact", path, method: "POST", token, expectedStatuses, expectedRiskCodes,
      bodyFactory: (workerID, sequence) => ({ body: "[mcmods-anti-abuse-load] exact duplicate", idempotencyKey: `exact-${workerID}-${sequence}-${startedAt.getTime()}` }) },
    { name: "spam-near", path, method: "POST", token, expectedStatuses, expectedRiskCodes,
      bodyFactory: (workerID, sequence) => ({ body: `[mcmods-anti-abuse-load] repeated template number ${sequence % 5}`, idempotencyKey: `near-${workerID}-${sequence}-${startedAt.getTime()}` }) },
    { name: "idempotent-replay", path, method: "POST", token, expectedStatuses, expectedRiskCodes,
      body: { body: "[mcmods-anti-abuse-load] idempotent replay", idempotencyKey: replayKey }, headers: { "Idempotency-Key": replayKey } },
    { name: "normal-comment-read", path: `${path}?sort=latest&limit=20`, token },
  ];
}

function crawlerRequests() {
  const expectedStatuses = [200, 403, 429];
  const expectedRiskCodes = ["crawler_rate_limited", "rate_limited", "request_denied", "temporarily_blocked"];
  const requests = [
    { name: "unknown-crawler-catalog", path: "/api/v1/mods?limit=20", expectedStatuses, expectedRiskCodes,
      headers: { "User-Agent": "MCModsResearchCrawler/1.0" } },
    { name: "spoofed-googlebot", path: "/api/v1/community/posts?kind=tutorial&limit=20", expectedStatuses, expectedRiskCodes,
      headers: { "User-Agent": "Googlebot/2.1" } },
    { name: "crawler-search", path: "/api/v1/search?q=minecraft", expectedStatuses, expectedRiskCodes,
      headers: { "User-Agent": "ExampleSpider/1.0" } },
  ];
  if (botToken) requests.push({ name: "allowed-readonly-bot", path: "/api/v1/mods?limit=20", headers: { "User-Agent": "MCModsAllowedIndexer/1.0", "X-MCMods-Bot-Token": botToken } });
  return requests;
}

function parseStages(value) {
  const parsed = value.split(",").map((part) => {
    const match = part.trim().match(/^(\d+)x(\d+)$/);
    if (!match) throw new Error(`Invalid load stage ${part}; expected concurrency x seconds, for example 10x30.`);
    return { concurrency: boundedNumber(match[1], 1, 1, 500), seconds: boundedNumber(match[2], 1, 1, 3600) };
  });
  if (!parsed.length) throw new Error("At least one load stage is required.");
  return parsed;
}

function splitValues(value) {
  return [...new Set(value.split(",").map((item) => item.trim()).filter(Boolean))];
}

function repeat(values, count) {
  return Array.from({ length: count }, () => values).flat();
}

function interleave(groups) {
  const maximumLength = Math.max(0, ...groups.map((group) => group.length));
  const values = [];
  for (let index = 0; index < maximumLength; index++) {
    for (const group of groups) {
      if (group[index]) values.push(group[index]);
    }
  }
  return values;
}

function boundedNumber(value, fallback, minimum, maximum) {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? Math.min(maximum, Math.max(minimum, Math.trunc(parsed))) : fallback;
}

function percentile(values, fraction) {
  if (!values.length) return 0;
  return round(values[Math.min(values.length - 1, Math.ceil(values.length * fraction) - 1)]);
}

function round(value) {
  return Math.round(value * 100) / 100;
}

function delay(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}
