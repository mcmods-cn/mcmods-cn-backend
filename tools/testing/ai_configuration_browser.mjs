import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFile, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { isAbsolute, join } from "node:path";

const [directory, frontend] = process.argv.slice(2);
assert.ok(directory && frontend && isAbsolute(directory) && isAbsolute(frontend), "pass the absolute fixture directory and frontend checkout");
const runtime = JSON.parse(await readFile(join(directory, "runtime.json"), "utf8"));
const sessions = JSON.parse(await readFile(join(directory, "private.json"), "utf8"));
const configPath = "/api/v1/admin/ai/config";
assert.equal(runtime.frontendOrigin, "http://127.0.0.1:3329");
const api = new URL(runtime.apiOrigin);
assert.equal(api.protocol, "http:");
assert.equal(api.hostname, "127.0.0.1");
assert.equal(api.origin, runtime.apiOrigin);
const { chromium } = createRequire(join(frontend, "package.json"))("playwright");
const browser = await chromium.launch();
let complete = false;

function configResponse(response, method) {
  return response.url() === api.origin + configPath && response.request().method() === method;
}

async function openProviders(actor, viewport) {
  const context = await browser.newContext({ viewport, colorScheme: actor === "reader" ? "dark" : "light" });
  await context.addCookies([
    { name: runtime.authCookieName, value: sessions[actor], domain: api.hostname, path: "/api/", httpOnly: true, sameSite: "Lax" },
    { name: "mcmods-ui-locale", value: "en-US", url: runtime.frontendOrigin },
  ]);
  const page = await context.newPage();
  let writes = 0;
  page.on("request", (request) => {
    if (request.url() === api.origin + configPath && request.method() === "PUT") writes++;
  });
  const origins = new Set([runtime.frontendOrigin, api.origin]);
  await context.route((url) => (url.protocol === "http:" || url.protocol === "https:") && !origins.has(url.origin),
    (route) => route.abort());
  await page.goto(runtime.frontendOrigin + "/admin");
  const group = page.locator('[data-admin-group="ai"]');
  await group.waitFor({ state: "attached" });
  const navToggle = page.locator('button[aria-controls="admin-navigation"]');
  if (!(await group.isVisible()) && await navToggle.isVisible()) {
    await navToggle.click();
  }
  const target = page.locator('[data-admin-panel="ai-providers"]');
  if (!(await target.isVisible())) {
    await group.click();
  }
  const read = page.waitForResponse((response) => configResponse(response, "GET"));
  await target.click();
  const response = await read;
  assert.equal(response.status(), 200);
  const { data } = await response.json();
  assert.match(data.revision, /^[a-f\d]{64}$/);
  await page.getByLabel("Description", { exact: true }).first().waitFor();
  return { page, data, writes: () => writes };
}

async function save({ page }, expected) {
  const saved = page.waitForResponse((response) => configResponse(response, "PUT"));
  await page.getByRole("button", { name: "Save AI config", exact: true }).click();
  const response = await saved;
  assert.equal(response.status(), expected);
  return response.json();
}

try {
  const reader = await openProviders("reader", { width: 390, height: 844 });
  const readerSave = reader.page.getByRole("button", { name: "Save AI config", exact: true });
  assert.equal(await reader.page.getByLabel("Description", { exact: true }).first().isEditable(), false);
  assert.ok(!(await readerSave.count()) || await readerSave.isDisabled());
  assert.equal(reader.writes(), 0);
  await reader.page.screenshot({ path: join(directory, "reader-mobile.png") });

  const editorA = await openProviders("editorA", { width: 1440, height: 900 });
  const editorB = await openProviders("editorB", { width: 1440, height: 900 });
  assert.equal(editorA.data.revision, editorB.data.revision);
  const aNotes = editorA.page.getByLabel("Description", { exact: true }).nth(0);
  const bNotes = editorB.page.getByLabel("Description", { exact: true }).nth(1);
  await aNotes.fill("Round two browser edit A");
  await bNotes.fill("Round two browser edit B");
  const first = await save(editorA, 200);
  assert.equal(first.data.providers[0].notes, "Round two browser edit A");
  const conflict = await save(editorB, 409);
  assert.equal(conflict.code, "AI_CONFIG_CONFLICT");
  assert.equal(await bNotes.inputValue(), "Round two browser edit B");
  await editorB.page.screenshot({ path: join(directory, "conflict-desktop.png") });
  await editorB.page.getByRole("button", { name: "Compare with the latest settings", exact: true }).click();
  await editorB.page.getByRole("button", { name: "Apply my edited fields to the latest settings", exact: true }).click();
  assert.equal(await editorB.page.getByLabel("Description", { exact: true }).nth(0).inputValue(), "Round two browser edit A");
  const second = await save(editorB, 200);
  assert.deepEqual(second.data.providers.slice(0, 2).map((provider) => provider.notes),
    ["Round two browser edit A", "Round two browser edit B"]);
  assert.deepEqual([reader.writes(), editorA.writes(), editorB.writes()], [0, 1, 2]);
  const result = {
    frontend: execFileSync("git", ["-C", frontend, "rev-parse", "HEAD"], { encoding: "utf8" }).trim(),
    browser: browser.version(),
    readerWrites: reader.writes(),
    saveStatuses: [200, 409, 200],
    draftPreserved: true,
    bothFieldsRetained: true,
  };
  await writeFile(join(directory, "browser-result.json"), JSON.stringify(result, null, 2) + "\n", { flag: "wx", mode: 0o600 });
  await writeFile(join(directory, "done.flag"), "", { flag: "wx", mode: 0o600 });
  complete = true;
  console.log("AI configuration browser checks passed");
} finally {
  await browser.close();
  if (!complete) {
    await writeFile(join(directory, "stop.flag"), "", { flag: "wx", mode: 0o600 });
  }
}
