import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import http from "node:http";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);
const scriptPath = fileURLToPath(new URL("./load-test.mjs", import.meta.url));

async function withLoadServer(handler, callback) {
  const server = http.createServer((request, response) => {
    setTimeout(() => handler(request, response), 5);
  });
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  try {
    const address = server.address();
    return await callback(`http://127.0.0.1:${address.port}`);
  } finally {
    await new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
  }
}

async function runLoad(baseURL, extraEnvironment = {}, onStart) {
  const environment = Object.fromEntries(
    Object.entries(process.env).filter(([name]) => !name.startsWith("MCMODS_LOAD_")),
  );
  try {
    const execution = execFileAsync(process.execPath, [scriptPath], {
      env: {
        ...environment,
        MCMODS_LOAD_BASE_URL: baseURL,
        MCMODS_LOAD_STAGES: "1x1",
        MCMODS_LOAD_TIMEOUT_MS: "500",
        MCMODS_LOAD_MIN_SUCCESSFUL_RESPONSES: "1",
        ...extraEnvironment,
      },
      windowsHide: true,
      maxBuffer: 2 * 1024 * 1024,
    });
    onStart?.(execution.child);
    const result = await execution;
    return { code: 0, report: JSON.parse(result.stdout), stderr: result.stderr };
  } catch (error) {
    return {
      code: error.code,
      report: error.stdout ? JSON.parse(error.stdout) : null,
      stderr: error.stderr ?? "",
    };
  }
}

function failureMessage(result) {
  return result.report?.contract.failureReasons.join(", ") || result.stderr;
}

test("CLI accepts only responses in the selected request contract", async () => {
  await withLoadServer((_request, response) => {
    response.writeHead(200, { "Content-Type": "application/json" });
    response.end(`{"ok":true}`);
  }, async (baseURL) => {
    const result = await runLoad(baseURL, { MCMODS_LOAD_SCENARIO: "infrastructure" });
    assert.equal(result.code, 0, failureMessage(result));
    assert.ok(result.report.expectedResponses > 0);
    assert.equal(result.report.unexpectedResponses, 0);
    assert.equal(result.report.errorRate, 0);
  });

  await withLoadServer((_request, response) => {
    response.writeHead(429, { "Content-Type": "application/json" });
    response.end(`{"code":"rate_limited"}`);
  }, async (baseURL) => {
    const result = await runLoad(baseURL, { MCMODS_LOAD_SCENARIO: "infrastructure" });
    assert.equal(result.code, 1);
    assert.ok(result.report.unexpectedResponses > 0);
    assert.equal(result.report.expectedRejects, 0);
    assert.equal(result.report.errorRate, 1);
  });
});

test("CLI measures the redirect response from the selected server", async () => {
  let redirectDestinationRequests = 0;
  await withLoadServer((request, response) => {
    if (request.url === "/live") {
      response.writeHead(302, { Location: "/next" });
    } else {
      redirectDestinationRequests++;
      response.writeHead(200);
    }
    response.end();
  }, async (baseURL) => {
    const result = await runLoad(baseURL, { MCMODS_LOAD_SCENARIO: "infrastructure" });
    assert.equal(result.code, 0, failureMessage(result));
    assert.ok(result.report.statuses[302] > 0);
    assert.equal(redirectDestinationRequests, 0);
  });
});

test("CLI reports interrupted stages as incomplete after successful responses", {
  skip: process.platform === "win32" ? "Windows child-process termination does not deliver POSIX SIGINT" : false,
}, async () => {
  let requests = 0;
  let child;
  let signaled = false;
  await withLoadServer((_request, response) => {
    if (++requests === 2) signaled = child.kill("SIGINT");
    response.writeHead(200, { "Content-Type": "application/json" });
    response.end('{"ok":true}');
  }, async (baseURL) => {
    const result = await runLoad(baseURL, {
      MCMODS_LOAD_SCENARIO: "infrastructure",
      MCMODS_LOAD_STAGES: "1x1,1x1",
    }, (childProcess) => { child = childProcess; });
    assert.ok(signaled);
    assert.ok(result.report.successfulResponses > 0);
    assert.equal(result.code, 1);
    assert.equal(result.report.contract.interrupted, true);
    assert.deepEqual(result.report.contract.failureReasons, ["load test interrupted before all stages completed"]);
  });
});

test("anti-abuse CLI treats only structured configured rejections as expected", async () => {
  await withLoadServer((request, response) => {
    if (request.method === "GET") {
      response.writeHead(200, { "Content-Type": "application/json" });
      response.end(`{"ok":true}`);
      return;
    }
    response.writeHead(429, { "Content-Type": "application/json" });
    response.end(`{"code":"rate_limited"}`);
  }, async (baseURL) => {
    const result = await runLoad(baseURL, {
      MCMODS_LOAD_SCENARIO: "antiabuse",
      MCMODS_LOAD_STAGES: "4x1",
      MCMODS_LOAD_ENABLE_MUTATIONS: "1",
      MCMODS_LOAD_TOKEN: "test-token",
      MCMODS_LOAD_COMMENT_TARGET_TYPE: "mod",
      MCMODS_LOAD_COMMENT_TARGET_ID: "testmod01",
      MCMODS_LOAD_REQUIRED_CODES: "rate_limited",
      MCMODS_LOAD_MIN_RISK_REJECTS: "1",
    });
    assert.equal(result.code, 0, failureMessage(result));
    assert.ok(result.report.expectedRejects > 0);
    assert.equal(result.report.unexpectedResponses, 0);
    assert.equal(result.report.errorRate, 0);
    assert.deepEqual(result.report.contract.missingRequiredCodes, []);
  });
});

test("catalog CLI requests the first server page using its cursor contract", async () => {
  let serverRequests = 0;
  await withLoadServer((request, response) => {
    const url = new URL(request.url, "http://127.0.0.1");
    let status = 200;
    if (url.pathname === "/api/v1/servers") {
      serverRequests++;
      if (url.searchParams.has("page") || url.searchParams.has("offset")) status = 400;
    }
    response.writeHead(status, { "Content-Type": "application/json" });
    response.end('{"items":[],"hasMore":false,"nextCursor":""}');
  }, async (baseURL) => {
    const result = await runLoad(baseURL, {
      MCMODS_LOAD_SCENARIO: "catalogs",
      MCMODS_LOAD_STAGES: "6x1",
    });
    assert.ok(serverRequests > 0);
    assert.equal(result.code, 0, failureMessage(result));
    assert.equal(result.report.unexpectedResponses, 0);
  });
});

test("anti-abuse CLI records a structured 428 challenge as an expected rejection", async () => {
  await withLoadServer((request, response) => {
    const challenge = request.method === "POST";
    response.writeHead(challenge ? 428 : 200, { "Content-Type": "application/json" });
    response.end(challenge ? '{"code":"challenge_required"}' : '{"ok":true}');
  }, async (baseURL) => {
    const result = await runLoad(baseURL, {
      MCMODS_LOAD_SCENARIO: "antiabuse",
      MCMODS_LOAD_STAGES: "4x1",
      MCMODS_LOAD_ENABLE_MUTATIONS: "1",
      MCMODS_LOAD_TOKEN: "test-token",
      MCMODS_LOAD_COMMENT_TARGET_TYPE: "mod",
      MCMODS_LOAD_COMMENT_TARGET_ID: "testmod01",
      MCMODS_LOAD_REQUIRED_CODES: "challenge_required",
    });
    assert.equal(result.code, 0, failureMessage(result));
    assert.ok(result.report.statuses[428] > 0);
    assert.equal(result.report.statuses[428], result.report.expectedRejects);
    assert.equal(result.report.unexpectedResponses, 0);
  });
});
