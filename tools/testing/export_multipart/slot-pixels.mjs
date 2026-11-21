// The vanilla shaped-crafting category uses JEI's standard item renderer for these three slots.
import assert from "node:assert/strict";

const vanillaTorch = [
  { id: "input_1", icons: ["coal", "charcoal"], native: { x: 23, y: 5, width: 16, height: 16 } },
  { id: "input_4", icons: ["stick"], native: { x: 23, y: 23, width: 16, height: 16 } },
  { id: "output_0", icons: ["torch"], native: { x: 99, y: 23, width: 16, height: 16 } },
];

function checkRectangle(observed, source, scale) {
  for (const key of ["x", "y", "width", "height"]) {
    assert(Math.abs(observed[key] - source[key] * scale) <= 1, key + " differs from the game rectangle");
  }
}

// Compare two actual browser screenshots. Hiding one image for the second capture keeps
// the JEI background and amount label intact. Count changes at that sprite's opaque texels
// so that an unrelated animated ingredient elsewhere on the template cannot move the bounds.
function drawnPixels(shown, hidden, original, visual) {
  assert.deepEqual(shown.info, hidden.info);
  const source = original.info;
  const scale = Math.min(visual.width / source.width, visual.height / source.height);
  const left = visual.x + (visual.width - source.width * scale) / 2;
  const top = visual.y + (visual.height - source.height * scale) / 2;
  const right = Math.min(shown.info.width, Math.ceil(left + source.width * scale));
  const bottom = Math.min(shown.info.height, Math.ceil(top + source.height * scale));
  let changedPixels = 0;
  let originalOpaquePixels = 0;
  let minX = Infinity, minY = Infinity, maxX = -1, maxY = -1;

  for (let y = Math.max(0, Math.floor(top)); y < bottom; y++) {
    for (let x = Math.max(0, Math.floor(left)); x < right; x++) {
      const sourceX = Math.floor((x + 0.5 - left) / scale);
      const sourceY = Math.floor((y + 0.5 - top) / scale);
      if (sourceX < 0 || sourceY < 0 || sourceX >= source.width || sourceY >= source.height) continue;
      const alpha = original.data[(sourceY * source.width + sourceX) * 4 + 3];
      if (alpha < 128) continue;

      originalOpaquePixels++;
      const pixel = (y * shown.info.width + x) * 4;
      const changed = [0, 1, 2].some(channel => Math.abs(shown.data[pixel + channel] - hidden.data[pixel + channel]) > 3);
      if (!changed) continue;
      changedPixels++;
      minX = Math.min(minX, x);
      minY = Math.min(minY, y);
      maxX = Math.max(maxX, x);
      maxY = Math.max(maxY, y);
    }
  }
  assert(changedPixels >= 12 && changedPixels >= originalOpaquePixels / 2, "The public recipe must visibly paint the game texture");
  return { x: minX, y: minY, width: maxX - minX + 1, height: maxY - minY + 1, changedPixels, originalOpaquePixels };
}

async function rectanglesInCanvas(img) {
  await img.decode();
  const holder = img.closest(".group.absolute");
  if (!holder?.parentElement) throw new Error("Missing JEI hit target or its template");
  const canvas = holder.parentElement.getBoundingClientRect();
  const locate = node => {
    const box = node.getBoundingClientRect();
    return { x: box.x - canvas.x, y: box.y - canvas.y, width: box.width, height: box.height };
  };
  return { src: img.currentSrc, canvasWidth: canvas.width, canvasHeight: canvas.height, hit: locate(holder), visual: locate(img) };
}

export async function verifyVanillaTorchSlots(card, rendered, document, originalFile, sharp) {
  const template = document.templates.find(item => item.template_id === rendered.layout.template_id);
  assert(template?.background && !template.background_contains_ingredients);
  const canvas = card.locator('img[src*="template_"]').first().locator("..");
  const rawPixels = bytes => sharp(bytes).ensureAlpha().raw().toBuffer({ resolveWithObject: true });
  const capture = { animations: "disabled", scale: "css" };
  const results = [];

  for (const target of vanillaTorch) {
    const declared = template.slots.find(slot => slot.slot_id === target.id);
    const slot = rendered.layout.slots.find(slot => slot.slot_id === target.id);
    assert.deepEqual(declared.rect, target.native);
    assert.deepEqual(slot.rect, declared.rect);
    assert.deepEqual(slot.visual_rect, declared.visual_rect);
    const selectors = target.icons.map(icon => 'img[src*="' + icon + '.png"]');
    const image = canvas.locator(selectors.join(",")).first();
    let result;

    // Coal and charcoal rotate normally. If the URL changes between the captures,
    // measure the new frame without pausing the animation or changing the application.
    for (let attempt = 0; attempt < 3 && !result; attempt++) {
      await image.waitFor({ state: "attached" });
      const measured = await image.evaluate(rectanglesInCanvas);
      const scale = measured.canvasWidth / document.canvas.width;
      assert.equal(measured.canvasHeight / document.canvas.height, scale);
      checkRectangle(measured.hit, slot.rect, scale);
      checkRectangle(measured.visual, slot.visual_rect, scale);
      const path = new URL(measured.src).searchParams.get("path");
      assert(path && target.icons.some(icon => path.endsWith("/" + icon + ".png")));

      let shown, hidden;
      await image.evaluate(node => node.setAttribute("data-game-pixel-probe", ""));
      try {
        shown = await canvas.screenshot(capture);
        hidden = await canvas.screenshot({ ...capture, style: "img[data-game-pixel-probe] { visibility: hidden !important; }" });
      } finally {
        await image.evaluate(node => node.removeAttribute("data-game-pixel-probe"));
      }
      if (await image.evaluate(node => node.currentSrc) !== measured.src) continue;
      const [before, without, source] = await Promise.all([rawPixels(shown), rawPixels(hidden), rawPixels(originalFile(path))]);
      const drawn = drawnPixels(before, without, source, measured.visual);
      const hit = measured.hit;

      // Only these native vanilla item cells are expected to contain the painted pixels.
      // Other renderers can have independent offsets or extents for their visual rectangle.
      assert(drawn.x >= hit.x - 1 && drawn.y >= hit.y - 1 &&
        drawn.x + drawn.width <= hit.x + hit.width + 1 && drawn.y + drawn.height <= hit.y + hit.height + 1,
        target.id + " painted outside its native item cell");
      result = {
        id: target.id, sourceRect: declared.rect, sourceVisualRect: declared.visual_rect,
        actualHit: hit, actualImage: measured.visual, canvasScale: scale, sourcePNG: path, actualPainted: drawn,
      };
    }
    assert(result, "A stable source sprite must be visible between tag rotations");
    results.push(result);
  }
  return results;
}
