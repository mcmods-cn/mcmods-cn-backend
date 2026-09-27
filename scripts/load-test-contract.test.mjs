import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  classifyLoadResponse,
  loadFailureRate,
  summarizeLoadOutcomes,
} from "./load-test-lib.mjs";

test("small real failure ratios never round to zero", () => {
  assert.equal(loadFailureRate(23, 14_613), 0.001574);
  assert.equal(loadFailureRate(29, 15_245), 0.001902);
  assert.equal(loadFailureRate(0, 15_245), 0);
});

test("expected anti-abuse rejection is distinct from unexpected HTTP failure", () => {
  const request = {
    name: "spam-exact",
    expectedStatuses: [201, 403, 412, 429],
    expectedRiskCodes: ["duplicate_content", "challenge_required", "rate_limited"],
  };
  assert.deepEqual(classifyLoadResponse(request, 429, "rate_limited"), {
    expected: true,
    expectedReject: true,
  });
  assert.deepEqual(classifyLoadResponse(request, 429, "unknown_code"), {
    expected: false,
    expectedReject: false,
  });
  assert.deepEqual(classifyLoadResponse(request, 500, "rate_limited"), {
    expected: false,
    expectedReject: false,
  });
});

test("scenario contract enforces unexpected responses, minimum volume, and required risk codes", () => {
  const summary = summarizeLoadOutcomes({
    outcomes: [
      { expected: true, expectedReject: false, code: "allowed" },
      { expected: true, expectedReject: true, code: "rate_limited" },
      { expected: false, expectedReject: false, code: "unexpected" },
    ],
    networkErrors: 1,
    completed: 4,
    minimumSuccessfulResponses: 3,
    minimumRiskRejects: 2,
    requiredCodes: ["rate_limited", "challenge_required"],
  });
  assert.equal(summary.expectedResponses, 2);
  assert.equal(summary.expectedRejects, 1);
  assert.equal(summary.successfulResponses, 1);
  assert.equal(summary.unexpectedResponses, 1);
  assert.equal(summary.failureRate, 0.5);
  assert.deepEqual(summary.missingRequiredCodes, ["challenge_required"]);
  assert.equal(summary.failureReasons.length, 5);
});

test("request pools are built once and SQL plans require explicit representative inputs", async () => {
  const loadScript = await readFile(new URL("./load-test.mjs", import.meta.url), "utf8");
  assert.match(loadScript, /const requestPools = createRequestPools\(\);/);
  assert.doesNotMatch(loadScript, /function chooseRequest[\s\S]*const pools =/);

  const queryAnalysis = await readFile(new URL("./query-analysis.sql", import.meta.url), "utf8");
  assert.doesNotMatch(queryAnalysis, /select id from users order by id limit 1/i);
  assert.match(queryAnalysis, /representative_user_id/);
  assert.match(queryAnalysis, /catalog_offset/);
  assert.match(queryAnalysis, /explain \(analyze, buffers/gi);

  const antiAbuseAnalysis = await readFile(new URL("./anti-abuse-query-analysis.sql", import.meta.url), "utf8");
  assert.doesNotMatch(antiAbuseAnalysis, /buffers false/i);
  assert.equal((antiAbuseAnalysis.match(/explain \(analyze, buffers/gi) ?? []).length, 3);
  assert.match(antiAbuseAnalysis, /representative_user_id/);

  const documentation = await readFile(new URL("../docs/load-testing.md", import.meta.url), "utf8");
  for (const required of [
    "MCMODS_LOAD_MIN_SUCCESSFUL_RESPONSES",
    "MCMODS_LOAD_MIN_RISK_REJECTS",
    "MCMODS_LOAD_REQUIRED_CODES",
    "immutable historical samples",
    "EXPLAIN (ANALYZE, BUFFERS)",
  ]) {
    assert.ok(documentation.includes(required), `load-test documentation is missing ${required}`);
  }
});
