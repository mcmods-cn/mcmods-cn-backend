// Continue the owned vanilla game journey from the job already submitted by browser.mjs.
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFile, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { dirname, isAbsolute, join, resolve } from "node:path";
import { setTimeout as delay } from "node:timers/promises";

const file = process.env.MCMODS_AUDIT_JOURNEY_RUNTIME;
const frontend = process.env.MCMODS_AUDIT_FRONTEND_REPO;
assert(file && isAbsolute(file) && frontend && isAbsolute(frontend));
const directory = dirname(file);
const readJSON = async path => JSON.parse(await readFile(path, "utf8"));
const runtime = await readJSON(file);
assert.equal(runtime.state, "READY");
assert.equal(runtime.namespace, "minecraft");
assert.equal(resolve(dirname(runtime.authPath)), resolve(directory));
for (const origin of [runtime.apiOrigin, runtime.frontendOrigin, runtime.ossOrigin]) {
  const url = new URL(origin);
  assert(url.protocol === "http:" && url.hostname === "127.0.0.1" && url.origin === origin);
}
const auth = await readJSON(runtime.authPath);
const transfer = await readJSON(join(directory, "multipart-browser-result.json"));
assert.match(transfer.jobId, /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/);
assert.equal(transfer.sha256, runtime.sampleSha256);
const requireFrontend = createRequire(join(frontend, "package.json"));
const { chromium } = requireFrontend("playwright");
const { unzipSync } = requireFrontend("fflate");
const sharp = requireFrontend("sharp");
const archive = await readFile(runtime.samplePath);
assert.equal(createHash("sha256").update(archive).digest("hex"), runtime.sampleSha256);
const recipePath = "recipes/jei/recipes/minecraft/crafting.json";
const meshPath = "block_entities/models/minecraft/acacia_hanging_sign/default.mesh.json";
const sources = unzipSync(archive, { filter: entry => [recipePath, meshPath, "registries/items.json"].includes(entry.name) });
const fromSource = path => JSON.parse(new TextDecoder().decode(sources[path]));
const expected = fromSource(recipePath).recipes.find(recipe => recipe.recipe_id === "minecraft:torch");
const torch = fromSource("registries/items.json").entries.find(entry => entry.id === "minecraft:torch");
const mesh = fromSource(meshPath);
assert(expected && torch && mesh);
const browser = await chromium.launch({ headless: process.env.MCMODS_AUDIT_HEADED !== "1" });
const report = {
  sourceCommits: Object.fromEntries(Object.entries(runtime.repoRevisions).map(([name, value]) => [name, value.commit])),
  sampleSha256: runtime.sampleSha256, sampleBytes: archive.length, browser: browser.version(),
  jobId: transfer.jobId, headed: process.env.MCMODS_AUDIT_HEADED === "1", statuses: [], transfer: { confirmed: transfer.confirmed, resumed: transfer.sentAfter, completion: transfer.statuses },
};
const pageErrors = [];
const api = runtime.apiOrigin;
async function context(token) {
  const actor = await browser.newContext({ locale: "en-US", serviceWorkers: "block" });
  await actor.addCookies([{ name: "mcmods-ui-locale", value: "en-US", url: runtime.frontendOrigin }]);
  if (token) await actor.addCookies([{ name: "mcmods_session", value: token, url: api, httpOnly: true, sameSite: "Lax" }]);
  await actor.route(url => ![api,runtime.frontendOrigin,runtime.ossOrigin].includes(url.origin), route => route.abort());
  actor.on("page", page => page.on("pageerror", error => pageErrors.push(error.name)));
  return actor;
}
async function get(actor, path) {
  const response = await actor.request.get(api + path, { timeout: 60_000 });
  assert.equal(response.status(), 200, "GET " + path.split("?")[0] + " returned " + response.status());
  const envelope = await response.json();
  assert(envelope.data);
  return envelope.data;
}
function sourceCandidates(binding) {
  return binding.alternatives.map(candidate => [candidate.resource_location, candidate.amount,
    binding.chance_available === true, binding.chance ?? null, binding.chance_percent ?? null]);
}
try {
  const creator = await context(auth.creatorToken);
  const reviewer = await context(auth.reviewerToken);
  const anonymous = await context();
  const identities = await Promise.all([creator, reviewer].map(actor => get(actor, "/api/v1/auth/me")));
  assert.notEqual(identities[0].id, identities[1].id);
  for (const identity of identities) assert(!identity.permissionRules?.some(rule => rule.allow && ["*", "admin.*"].includes(rule.code)));
  const jobPath = "/api/v1/mods/" + encodeURIComponent(runtime.projectSiteId) + "/export-imports/" + transfer.jobId;
  const start = Date.now();
  let job;
  while (Date.now() - start < 30 * 60_000) {
    job = await get(creator, jobPath);
    if (report.statuses.at(-1)?.status !== job.status) report.statuses.push({ status: job.status, observedAt: new Date().toISOString() });
    if (["ready", "partial", "failed", "confirmation_required", "cancelled"].includes(job.status)) break;
    await delay(4000);
  }
  assert.equal(job?.status, "ready", "Actual worker state " + job?.status + ", code " + (job?.errorCode || "none"));
  const state = await readJSON(file);
  const revision = state.importRevisions?.find(item => item.jobId === transfer.jobId);
  const decisionFile = join(directory, "vanilla-review-decision.json");
  let decision;
  try {
    decision = await readJSON(decisionFile);
  } catch (err) {
    if (err.code !== "ENOENT") throw err;
  }
  if (!decision) {
    const queue = await get(reviewer, "/api/v1/reviews/content?limit=100");
    const item = queue.items.find(item => item.source === "export" && item.projectId === runtime.projectPublicId);
    assert(item && (!revision || item.id === revision.id), "The review queue must contain this job's revision");
    const path = "/api/v1/export-revisions/" + encodeURIComponent(item.id);
    const pending = await get(reviewer, path);
    assert.equal(pending.package.sha256, runtime.sampleSha256);
    assert.equal(pending.targetVersionPublicId, runtime.targetVersionPublicId);
    assert.equal(pending.namespace, "minecraft");
    assert.equal(pending.canReview, true);
    assert.equal(pending.previousRevisionId, "");
    assert(pending.counts.resources >= 1255);
    assert.deepEqual(pending.resourceChanges, {added: pending.counts.resources, changed: 0, removed: 0});
    assert.equal((await anonymous.request.get(api + path)).status(), 403);
    const page = await reviewer.newPage();
    await page.goto(runtime.frontendOrigin + "/reviews");
    const card = page.locator("article").filter({ has: page.locator('a[href*="importRevisionId=' + item.id + '"]') });
    const reviewed = page.waitForResponse(response => response.request().method() === "PATCH" && new URL(response.url()).pathname === new URL(item.reviewUrl, api).pathname, { timeout: 300_000 });
    await card.getByRole("button", { name: "Approve", exact: true }).click();
    assert.equal((await reviewed).status(), 200);
    decision = { jobId: transfer.jobId, revisionId: item.id, sha256: pending.package.sha256, pendingAnonymous: 403, status: 200 };
    await writeFile(decisionFile, JSON.stringify(decision), { flag: "wx", mode: 0o600 });
  }
  assert.equal(decision.jobId, transfer.jobId);
  assert.equal(decision.sha256, runtime.sampleSha256);
  assert.equal(decision.status, 200);
  assert.equal(decision.pendingAnonymous, 403);
  const revisionPath = "/api/v1/export-revisions/" + encodeURIComponent(decision.revisionId);
  const active = await get(anonymous, revisionPath);
  assert.equal(active.isActive, true);
  assert.equal(active.package.sha256, runtime.sampleSha256);
  assert.equal(active.targetVersionPublicId, runtime.targetVersionPublicId);
  report.revisionId = active.id;
  report.independentReview = { pendingAnonymous: 403, decision: 200, active: true, counts: active.counts };
  // Address the item by its known game ID. Its actual published recipe link avoids assuming
  // that a particular production recipe lands on the first page of a 2,084-recipe catalog.
  const entry = await get(anonymous, revisionPath + "/entry-detail?registry=items&locale=en_us&objectId=minecraft%3Atorch");
  const resource = await get(anonymous, "/api/v1/mods/" + runtime.projectSiteId + "/content-resources/" + entry.publicId);
  assert.equal(resource.canonicalId, torch.id);
  const linked = entry.recipes.find(recipe => recipe.recipeId === expected.recipe_id && recipe.revisionId === active.id);
  assert(linked && linked.entityId && entry.publicId);
  const rendered = await get(anonymous, "/api/v1/catalog/recipes/" + encodeURIComponent(linked.entityId) + "/render");
  assert.equal(rendered.revisionId, active.id);
  assert.equal(rendered.layout.source_mod_id, expected.source_mod_id);
  assert.equal(rendered.layout.source_mod_version, expected.source_mod_version);
  assert.deepEqual(rendered.layout.slots.map(slot => slot.slot_id).sort(), expected.bindings.map(binding => binding.slot_id).sort());
  for (const binding of expected.bindings) {
    const slot = rendered.layout.slots.find(value => value.slot_id === binding.slot_id);
    assert.equal(slot.ingredient_present, binding.ingredient_present);
    assert.equal(slot.semantic_role, binding.semantic_role);
    assert.deepEqual(slot.alternatives.map(candidate => [candidate.id, candidate.amount, candidate.chance_available, candidate.chance, candidate.chance_percent]), sourceCandidates(binding));
  }
  const sign = await get(anonymous, revisionPath + "/entry-detail?registry=blocks&locale=en_us&objectId=minecraft%3Aacacia_hanging_sign");
  assert.equal(sign.blockEntityModel.blockId, "minecraft:acacia_hanging_sign");
  const variant = sign.blockEntityModel.variants.find(variant => variant.meshPath === meshPath);
  assert(variant && variant.vertexCount === mesh.vertex_count && variant.quadCount === mesh.quad_count);
  // PostgreSQL jsonb emits numeric zero for both 0.0 and -0.0, including face normals.
  let negativeZeros = 0, sourceNumbers = 0;
  const expectedFaces = JSON.parse(JSON.stringify(mesh.faces, (_field, value) => {
    if (typeof value === "number") {
      assert(Number.isFinite(value), "Game mesh coordinates must be finite");
      if (Object.is(value, -0)) negativeZeros++;
      sourceNumbers++;
    }
    return value;
  }));
  assert.deepEqual(variant.mesh.faces, expectedFaces);
  report.model = { block: sign.blockEntityModel.blockId, publicId: sign.publicId, mesh: meshPath, vertices: variant.vertexCount, quads: variant.quadCount, comparedNumbers: sourceNumbers, sourceNegativeZeros: negativeZeros, facesEqual: true };
  const publicPage = await anonymous.newPage();
  await publicPage.goto(runtime.frontendOrigin + "/mods/" + runtime.projectSiteId + "/resources/" + entry.publicId + "?version=" + runtime.targetVersionPublicId);
  await publicPage.getByRole("heading", { name: /^(Torch|minecraft:torch)$/i, exact: true }).waitFor({ timeout: 60_000 });
  await publicPage.screenshot({ path: join(directory, "vanilla-public-item.png") });
  const recipePage = await anonymous.newPage();
  const limited = recipePage.waitForResponse(response => {
    const url = new URL(response.url());
    return url.origin === api && ["/api/v1/auth/me", "/api/v1/site/config"].includes(url.pathname) && response.status() === 429;
  }, {timeout: 60_000}).catch(() => null);
  await recipePage.goto(runtime.frontendOrigin + "/recipe-types?recipePublicId=" + linked.entityId);
  const card = recipePage.locator("#recipe-" + linked.entityId);
  const rateResponse = await Promise.race([card.waitFor({timeout: 60_000}).then(() => null), limited]);
  if (rateResponse) {
    const retryAfter = Number(rateResponse.headers()["retry-after"]);
    const error = await rateResponse.json();
    assert.equal(error.code, "crawler_rate_limited");
    assert(Number.isSafeInteger(retryAfter) && retryAfter > 0 && retryAfter <= 120);
    report.readCooldown = {status: 429, code: error.code, retryAfterSeconds: retryAfter};
    await delay((retryAfter + 1) * 1000);
    await recipePage.reload();
  }
  await card.waitFor({timeout: 60_000});
  const image = card.locator('img[src*="torch.png"]').first();
  await image.waitFor({ state: "attached", timeout: 60_000 });
  await image.scrollIntoViewIfNeeded();
  const src = await image.evaluate(async img => { await img.decode(); return img.src; });
  const assetURL = new URL(src);
  assert.equal(assetURL.origin, api);
  const path = assetURL.searchParams.get("path");
  assert(path);
  const original = unzipSync(archive, { filter: entry => entry.name === path })[path];
  assert(original);
  const response = await anonymous.request.get(src);
  assert.equal(response.status(), 200);
  const [before, after] = await Promise.all([Buffer.from(original), await response.body()].map(bytes => sharp(bytes).ensureAlpha().raw().toBuffer({ resolveWithObject: true })));
  assert.equal(before.info.width, after.info.width);
  assert.equal(before.info.height, after.info.height);
  assert.deepEqual(before.data, after.data);
  await recipePage.screenshot({ path: join(directory, "vanilla-public-recipe.png") });
  assert.deepEqual(pageErrors, []);
  report.recipe = { id: expected.recipe_id, publicId: linked.entityId, itemPublicId: entry.publicId, slots: expected.bindings.length,
    ingredients: expected.bindings.filter(binding => binding.ingredient_present).map(binding => ({ role: binding.semantic_role, alternatives: sourceCandidates(binding) })),
    image: { path, width: before.info.width, height: before.info.height, decodedRGBAEqual: true } };
  await writeFile(join(directory, "vanilla-published.json"), JSON.stringify(report, null, 2) + "\n", { flag: "wx", mode: 0o600 });
} finally {
  await browser.close();
}
