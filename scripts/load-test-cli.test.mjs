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

async function runLoad(baseURL, extraEnvironment = {}) {
  try {
    const result = await execFileAsync(process.execPath, [scriptPath], {
      env: {
        ...process.env,
        MCMODS_LOAD_BASE_URL: baseURL,
        MCMODS_LOAD_STAGES: "1x1",
        MCMODS_LOAD_TIMEOUT_MS: "500",
        MCMODS_LOAD_MIN_SUCCESSFUL_RESPONSES: "1",
        ...extraEnvironment,
      },
      windowsHide: true,
      maxBuffer: 2 * 1024 * 1024,
    });
    return { code: 0, report: JSON.parse(result.stdout), stderr: result.stderr };
  } catch (error) {
    return {
      code: error.code,
      report: error.stdout ? JSON.parse(error.stdout) : null,
      stderr: error.stderr ?? "",
    };
  }
}

test("CLI accepts only responses in the selected request contract", async () => {
  await withLoadServer((_request, response) => {
    response.writeHead(200, { "Content-Type": "application/json" });
    response.end(`{"ok":true}`);
  }, async (baseURL) => {
    const result = await runLoad(baseURL, { MCMODS_LOAD_SCENARIO: "infrastructure" });
    assert.equal(result.code, 0, result.stderr);
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
      MCMODS_LOAD_ENABLE_MUTATIONS: "1",
      MCMODS_LOAD_TOKEN: "test-token",
      MCMODS_LOAD_COMMENT_TARGET_TYPE: "mod",
      MCMODS_LOAD_COMMENT_TARGET_ID: "testmod01",
      MCMODS_LOAD_REQUIRED_CODES: "rate_limited",
      MCMODS_LOAD_MIN_RISK_REJECTS: "1",
    });
    assert.equal(result.code, 0, result.stderr);
    assert.ok(result.report.expectedRejects > 0);
    assert.equal(result.report.unexpectedResponses, 0);
    assert.equal(result.report.errorRate, 0);
    assert.deepEqual(result.report.contract.missingRequiredCodes, []);
  });
});
