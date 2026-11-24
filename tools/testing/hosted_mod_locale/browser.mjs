import { chromium } from "playwright";
import assert from "node:assert/strict";
import { readFileSync, writeFileSync, existsSync } from "node:fs";
import { spawn } from "node:child_process";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { run } from "./mod-flow.mjs";

const [folder, result, standalone] = process.argv.slice(2);
const runtime = JSON.parse(readFileSync(join(folder, "runtime.json"), "utf8"));
const front = new URL(runtime.frontendOrigin);
const api = new URL(runtime.apiOrigin);
const embedded = new URL(process.env.NEXT_PUBLIC_API_BASE_URL);
assert.equal(front.origin, "http://127.0.0.1:3531");
assert.equal(api.hostname, "127.0.0.1");
assert.equal(embedded.origin, "https://api.example.test");
assert.equal(runtime.localeCases.length, 32);
assert.equal(runtime.revisionCases.length, 0);
const auth = JSON.parse(readFileSync(runtime.authPath, "utf8"));
const evidence = {
  build: readFileSync(join(standalone, ".next/BUILD_ID"), "utf8").trim(),
  steps: [], requests: [], phase: "started"
};
const save = () => writeFileSync(result, JSON.stringify(evidence, null, 2));
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
const next = spawn(process.execPath, ["--import", fileURLToPath(new URL("./ssr-local.mjs", import.meta.url)), join(standalone, "server.js")], {
  cwd: standalone, stdio: "ignore",
  env: { ...process.env, PORT: front.port, HOSTNAME: front.hostname, NODE_ENV: "production", MCMODS_LIVE_DESTINATION: api.origin, MCMODS_LIVE_EMBEDDED: embedded.href }
});
let browser;
async function openActor(token) {
  const context = await browser.newContext({ locale: "en-US", serviceWorkers: "block" });
  await context.addCookies([
    { name: "mcmods-ui-locale", value: "en-US", url: front.origin },
    { name: "mcmods_session", value: token, domain: api.hostname, path: "/api/", httpOnly: true, sameSite: "Lax" }
  ]);
  await context.route(url => url.origin !== front.origin, async route => {
    const req = route.request();
    const url = new URL(req.url());
    if (url.origin !== embedded.origin || url.pathname === "/api/v1/realtime/events") return route.abort();
    try {
      const headers = await req.allHeaders();
      for (const name of ["cookie", "host", "connection", "content-length"]) delete headers[name];
      headers.origin = front.origin;
      const res = await context.request.fetch(api.origin + url.pathname + url.search, {
        method: req.method(), headers, data: req.postDataBuffer() ?? undefined, maxRedirects: 0, timeout: 30000
      });
      if (req.method() === "POST" && url.pathname === "/api/v1/mods") {
        evidence.requests.push({ method: "POST", path: url.pathname, status: res.status() });
      }
      await route.fulfill({ response: res });
    } catch {
      await route.abort().catch(() => {});
    }
  });
  const page = await context.newPage();
  page.setDefaultTimeout(12000);
  return { page, close: () => context.close() };
}
async function expectWrite(page, pattern, method, action) {
  const [response] = await Promise.all([
    page.waitForResponse(r => pattern.test(new URL(r.url()).pathname) && r.request().method() === method),
    action()
  ]);
  return response;
}
try {
  save();
  let ready = false;
  for (let i = 0; i < 50; ++i) {
    if (next.exitCode !== null) throw new Error("Next startup failed");
    try {
      if ((await fetch(front.origin + "/login", { signal: AbortSignal.timeout(800) })).ok) { ready = true; break; }
    } catch {}
    await sleep(200);
  }
  assert(ready, "Next readiness failed");
  browser = await chromium.launch({ headless: true });
  await run({ runtime, folder, front, auth, openActor, expectWrite, save, sleep, evidence, assert, join, readFileSync, writeFileSync, existsSync });
  evidence.phase = "passed";
} catch (error) {
  evidence.phase = "failed";
  // Playwright request diagnostics can contain cookies. Save only the public operation and type.
  evidence.failure = { kind: error.name, operation: /^(locator\.[a-zA-Z]+|page\.[a-zA-Z]+|browserType\.launch):/.exec(String(error.message))?.[1] ?? "fixture assertion or local connection" };
  process.exitCode = 1;
} finally {
  save();
  if (browser) await browser.close();
  next.kill("SIGTERM");
}
