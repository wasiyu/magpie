// Recommendations belong to Quick Start; the supplier retains its full directory.
// JELLYTOOL_TEST_URL must be the isolated Jellytool fixture, never a live gateway.
const assert = require("node:assert/strict");
const { test } = require("node:test");
const fs = require("node:fs/promises");
const path = require("node:path");
const { chromium, webkit } = require("playwright");
const url = process.env.JELLYTOOL_TEST_URL;
const first = "vendor/text.v1:exact", second = "vendor/other.v2:exact";
for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of (process.env.JELLYTOOL_TEST_LANG ? [process.env.JELLYTOOL_TEST_LANG] : ["en", "zh", "zh-TW", "ja", "de"])) {
    test(`${engine} ${lang}: recommendations open the supplier directory without saving`, { skip: !url }, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const context = await browser.newContext({ viewport: { width: 720, height: 760 }, reducedMotion: "reduce" });
      await context.request.post(url + "/api/settings", { data: { lang, theme: "light" } });
      await context.request.post(url + "/api/provider/save", { data: { id: "jellytoken", models: [first] } });
      const page = await context.newPage(), mutations = [], errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      page.on("request", (r) => { if (r.method() === "POST" && new URL(r.url()).pathname.startsWith("/api/")) mutations.push(new URL(r.url()).pathname); });
      await page.route("**/wails/runtime.js", (r) => r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" }));
      await page.goto(url + "/?view=quickstart");
      await page.locator('#quickstart [data-step="models"] .guide-go').click();
      const picker = page.locator("#quickstart .qs-model-picker");
      assert.equal(await picker.locator(".jelly-rec-group").count(), 3);
      await picker.locator(".jelly-choose-all").click();
      const editor = page.locator("#modal .editor");
      await editor.locator(".models").waitFor();
      assert.equal(await editor.locator(".jelly-rec-group,.jelly-model-tabs").count(), 0);
      assert.deepEqual(await page.evaluate(() => draft.chosen), [first]);
      assert.deepEqual(mutations, [], "opening more models changes no configuration");
      await editor.locator(".mchip.pick").filter({ hasText: second }).first().click();
      assert.deepEqual(mutations, [], "directory selection is still a draft");
      if (process.env.ARTIFACT_DIR) {
        await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
        await page.screenshot({ animations: "disabled", path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-more-models.png`) });
      }
      await editor.locator(".bar > .primary").click();
      await page.waitForFunction(() => document.querySelector("#modal").hidden);
      const supplier = (await (await context.request.get(url + "/api/providers")).json()).providers.find((p) => p.id === "jellytoken");
      assert.deepEqual(supplier.chosen, [first, second]);
      assert.deepEqual(mutations, ["/api/provider/save"], "supplier Save is the only write");
      assert.deepEqual(errors, []);
    });
  }
}
