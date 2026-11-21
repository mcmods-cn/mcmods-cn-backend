// Invoked by TestRealGameMultipartResumeIntegration. All interfaces, cookies
// and signed URLs are restricted to the fixture's ephemeral loopback services.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { access, link, readFile, unlink, writeFile } from "node:fs/promises";
import { randomUUID } from "node:crypto";
import { dirname, isAbsolute, join, resolve } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { pathToFileURL } from "node:url";

async function configuration() {
  if (process.argv[2] === "--live-fixture") {
    const path = process.env.MCMODS_AUDIT_JOURNEY_RUNTIME;
    const frontend = process.env.MCMODS_AUDIT_FRONTEND_REPO;
    assert(path && isAbsolute(path) && frontend && isAbsolute(frontend));
    const runtime = JSON.parse(await readFile(path, "utf8"));
    const directory = dirname(path);
    assert.equal(runtime.state, "READY");
    assert.equal(dirname(resolve(runtime.authPath)), resolve(directory));
    const auth = JSON.parse(await readFile(runtime.authPath, "utf8"));
    return {
      archive: runtime.samplePath, sha256: runtime.sampleSha256, frontend, frontendOrigin: runtime.frontendOrigin,
      api: process.env.MCMODS_AUDIT_BROWSER_API_ORIGIN || runtime.apiOrigin, oss: runtime.ossOrigin,
      site: runtime.projectSiteId, version: runtime.targetVersionPublicId, actor: auth.creatorToken,
      existingFrontend: true, handoffDir: directory,
    };
  }
  let input = "";
  for await (const chunk of process.stdin) input += chunk;
  return JSON.parse(input);
}
async function newPrivateReceipt(path, data) {
  const temporary = path + "." + randomUUID();
  await writeFile(temporary, JSON.stringify(data), { flag: "wx", mode: 0o600 });
  try {
    await link(temporary, path);
  } finally {
    await unlink(temporary);
  }
}

const cfg = await configuration();
const origins = [cfg.api, cfg.oss, cfg.frontendOrigin];
for (const origin of origins) {
  const url = new URL(origin);
  assert(url.protocol === "http:" && url.hostname === "127.0.0.1" && url.origin === origin);
}
const { chromium } = await import(pathToFileURL(join(cfg.frontend, "node_modules/playwright/index.mjs")).href);
const next = cfg.existingFrontend ? undefined : spawn(process.execPath, [
  join(cfg.frontend, "node_modules/next/dist/bin/next"), "dev", "--hostname", "127.0.0.1", "--port", new URL(cfg.frontendOrigin).port,
], {
  cwd: cfg.frontend, stdio: "ignore", detached: true,
  env: { ...process.env, NEXT_PUBLIC_API_BASE_URL: cfg.api, NEXT_PUBLIC_SITE_URL: cfg.frontendOrigin,
    NEXT_PUBLIC_YGGDRASIL_API_ROOT: cfg.api + "/api/yggdrasil/", NEXT_PUBLIC_CSP_CONNECT_ORIGINS: cfg.oss,
    NEXT_PUBLIC_CSP_IMAGE_ORIGINS: cfg.oss, NEXT_TELEMETRY_DISABLED: "1" },
});
const stopped = next ? once(next, "exit") : Promise.resolve();
let terminated = false;
function stop() {
  if (!next || terminated) return;
  terminated = true;
  try {
    process.kill(-next.pid, "SIGTERM");
  } catch (err) {
    if (err.code !== "ESRCH") throw err;
  }
}
process.once("exit", stop);
let browser;
let context;
const abandonRequests = Promise.withResolvers();
const allowJob = Promise.withResolvers();
for (const signal of ["SIGTERM", "SIGINT"]) {
  process.once(signal, async () => { stop(); await browser?.close(); process.exit(1); });
}
let step = "start frontend";
const failures = [];

try {
  const deadline = Date.now() + 90_000;
  let ready = false;
  while (Date.now() < deadline && (!next || next.exitCode === null)) {
    try {
      const response = await fetch(cfg.frontendOrigin + "/login", { signal: AbortSignal.timeout(3000) });
      await response.body?.cancel();
      if (response.ok) { ready = true; break; }
    } catch { /* Retry while the isolated dev compiler starts. */ }
    await delay(150);
  }
  assert(ready, "the isolated frontend did not start");
  browser = await chromium.launch({ headless: true });
  context = await browser.newContext({ locale: "en-US", serviceWorkers: "block" });
  await context.route(url => /^https?:$/.test(url.protocol) && !origins.includes(url.origin), route => route.abort());
  await context.addCookies([
    { name: "mcmods-ui-locale", value: "en-US", url: cfg.frontendOrigin },
    { name: "mcmods_session", value: cfg.actor, url: cfg.api, httpOnly: true, sameSite: "Lax" },
  ]);
  const who = await context.request.get(cfg.api + "/api/v1/auth/me");
  assert.equal(who.status(), 200);
  const { data: actor } = await who.json();
  assert(!actor.permissionRules.some(rule => rule.allow && ["*", "admin.*", "admin.access"].includes(rule.code)));
  const page = await context.newPage();
  page.setDefaultTimeout(40_000);
  page.on("response", response => {
    const url = new URL(response.url());
    if (response.status() >= 400 && failures.length < 12) {
      if (url.origin === cfg.oss) failures.push("OSS part " + url.searchParams.get("partNumber") + " HTTP " + response.status());
      else if (url.origin === cfg.api) failures.push("API " + url.pathname.split("/").at(-1) + " HTTP " + response.status());
    }
  });
  const root = "/api/v1/mods/" + cfg.site + "/export-imports";
  const path = cfg.frontendOrigin + "/mods/" + cfg.site + "/data/edit?import=exporter&version=" + cfg.version;
  if (cfg.handoffDir) {
    // Let the live fixture inspect the active file and charged quota before
    // the real worker can consume the source. Never synthesize the job POST.
    await context.route(url => url.origin === cfg.api && url.pathname === root, async route => {
      if (route.request().method() === "POST" && !(await allowJob.promise)) return route.abort("aborted");
      await route.continue();
    });
  }
  const puts = [[], []];
  let phase = 0;
  const interrupted = [];
  // Hold selected first-attempt PUTs before they reach the provider. Leaving
  // the page disconnects those requests; resumed PUTs use native HTTP and XHR.
  await context.route(url => url.origin === cfg.oss && url.searchParams.has("uploadId"), async route => {
    const request = route.request();
    const part = Number(new URL(request.url()).searchParams.get("partNumber"));
    if (request.method() === "PUT" && phase === 0 && part !== 1 && part !== 3) {
      interrupted.push(part);
      await abandonRequests.promise;
      try { await route.abort("aborted"); } catch { /* The previous document closed its XHR. */ }
      return;
    }
    await route.continue();
  });
  let presigns = 0;
  let resumes = 0;
  page.on("request", request => {
    const url = new URL(request.url());
    if (url.origin === cfg.api && request.method() === "POST") {
      if (url.pathname === root + "/uploads/presign") presigns++;
      if (url.pathname === root + "/uploads/resume") resumes++;
    }
    if (request.method() === "PUT" && url.origin === cfg.oss && url.searchParams.has("uploadId")) {
      puts[phase].push(Number(url.searchParams.get("partNumber")));
    }
  });
  const onPost = suffix => page.waitForResponse(r => r.url() === cfg.api + root + suffix && r.request().method() === "POST", { timeout: cfg.handoffDir && !suffix ? 180_000 : 40_000 });
  step = "open the real editor";
  await page.goto(path);
  // The editor shows its drop zone while it reads IndexedDB. A user can only
  // select the file after that check enables the input.
  const input = page.locator('input[type="file"][accept=".zip,application/zip"]:visible:enabled').first();
  await input.waitFor();

  step = "hash the file and get presigned parts";
  const presigned = onPost("/uploads/presign");
  await input.setInputFiles(cfg.archive);
  const firstResponse = await presigned;
  assert.equal(firstResponse.status(), 200);
  const { data: first } = await firstResponse.json();
  assert.equal(first.method, "MULTIPART");
  assert.equal(first.sha256, cfg.sha256);
  assert.equal(first.multipart.partSize, 8 * 1024 * 1024);

  step = "wait for the user's first and third parts to persist";
  const checkpointDeadline = Date.now() + 45_000;
  let confirmed;
  while (!confirmed && Date.now() < checkpointDeadline) {
    confirmed = await page.evaluate(async ({ actor, uploadID, size }) => {
      const database = await new Promise((resolve, reject) => {
        const req = indexedDB.open("mcmods-cn-import-uploads");
        req.onsuccess = () => resolve(req.result); req.onerror = () => reject(req.error);
      });
      try {
        const tx = database.transaction(["tasks", "files"]);
        const values = await Promise.all(["tasks", "files"].map(name => new Promise((resolve, reject) => {
          const req = tx.objectStore(name).getAll(); req.onsuccess = () => resolve(req.result); req.onerror = () => reject(req.error);
        })));
        const task = values[0].find(value => value.subjectId === actor && value.ticket?.multipart?.uploadId === uploadID);
        const localFile = values[1].find(value => value.key === task?.key)?.file;
        return task?.completedPartNumbers?.join(",") === "1,3" && localFile?.size === size ? task.completedPartNumbers : false;
      } finally { database.close(); }
    }, { actor: actor.id, uploadID: first.multipart.uploadId, size: first.sizeBytes });
    if (!confirmed) await delay(50);
  }
  assert.deepEqual(confirmed, [1, 3], "two nonconsecutive parts and the original File must be durable before navigation");
  assert(interrupted.length > 0);
  await page.goto("about:blank");
  phase = 1;
  abandonRequests.resolve();
  step = "reopen the editor and resume the same upload";
  const resumed = onPost("/uploads/resume");
  const completed = onPost("/uploads/complete");
  const createdJob = onPost("");
  void createdJob.catch(() => {});
  // Do not allow an unobserved rejected wait to escape error cleanup below.
  void completed.catch(() => {});
  await page.goto(path);
  const resumeResponse = await resumed;
  assert.equal(resumeResponse.status(), 200);
  const { data: ticket } = await resumeResponse.json();
  assert.equal(ticket.multipart.uploadId, first.multipart.uploadId);
  assert.equal(ticket.objectKey, first.objectKey);
  assert.equal(ticket.sha256, cfg.sha256);
  step = "complete and confirm the exact same upload again";
  const completion = await completed;
  assert.equal(completion.status(), 201);
  const { data: record } = await completion.json();
  const repeat = await context.request.post(cfg.api + root + "/uploads/complete", {
    headers: { Origin: cfg.frontendOrigin }, data: completion.request().postDataJSON(),
  });
  assert.equal(repeat.status(), 200);
  assert.equal((await repeat.json()).data.id, record.id);
  assert.equal(presigns, 1);
  assert.equal(resumes, 1);
  assert.deepEqual(puts[1].sort((a, b) => a - b), first.multipart.parts.map(p => p.partNumber).filter(n => !confirmed.includes(n)));
  const facts = {
    fileId: record.id, uploadId: first.multipart.uploadId, objectKey: first.objectKey, sha256: first.sha256,
    sizeBytes: first.sizeBytes, confirmed, interrupted, sentAfter: puts[1], presigns, resumes,
    statuses: [completion.status(), repeat.status()], browser: browser.version(),
  };
  if (cfg.handoffDir) {
    step = "wait for the live fixture to check the active binding and quota";
    await newPrivateReceipt(join(cfg.handoffDir, "multipart-binding-ready.json"), facts);
    const limit = Date.now() + 120_000;
    let checked = false;
    while (Date.now() < limit) {
      try { await access(join(cfg.handoffDir, "multipart-binding-checked")); checked = true; break; }
      catch (err) { if (err.code !== "ENOENT") throw err; }
      await delay(100);
    }
    assert(checked, "the fixture did not acknowledge the active-binding snapshot");
    allowJob.resolve(true);
  }
  step = "observe the original page enqueue the import";
  const created = await createdJob;
  assert.equal(created.status(), 202);
  const { data: job } = await created.json();
  assert(job.id);
  const result = { ...facts, jobId: job.id };
  if (cfg.handoffDir) await newPrivateReceipt(join(cfg.handoffDir, "multipart-browser-result.json"), result);
  process.stdout.write(JSON.stringify(result));
} catch (err) {
  // Signed transport details and the private fixture token must not be logged.
  console.error(step + ": " + String(err.message).split("\n")[0].replace(/https?:\/\/[^\s]+/g, "[fixture URL]"), failures);
  process.exitCode = 1;
} finally {
  abandonRequests.resolve();
  allowJob.resolve(false);
  await context?.unrouteAll({ behavior: "wait" });
  await browser?.close();
  stop();
  await stopped;
}
