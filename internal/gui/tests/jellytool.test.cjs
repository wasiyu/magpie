const assert = require("node:assert/strict");
const { test } = require("node:test");
const fs = require("node:fs/promises");
const path = require("node:path");
const { chromium, webkit } = require("playwright");

const url = process.env.JELLYTOOL_TEST_URL;
for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of (process.env.JELLYTOOL_TEST_LANG ? [process.env.JELLYTOOL_TEST_LANG] : ["en", "zh", "zh-TW", "ja", "de"])) {
    test(`${engine} ${lang}: independent Jellytool supplier, Agents and settings`, { skip: !url }, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const context = await browser.newContext({ viewport: { width: 1100, height: 760 }, reducedMotion: "reduce" });
      const page = await context.newPage();
      page.setDefaultTimeout(7000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [], requests = [], failures = [];
      t.after(() => { if (errors.length || failures.length) console.log({ errors, failures, requests }); });
      page.on("pageerror", (e) => errors.push(e.stack));
      page.on("response", (r) => { if (r.status() >= 400) failures.push(`${r.status()} ${r.url()}`); });
      page.on("request", (r) => { if (new URL(r.url()).pathname.startsWith("/api/")) requests.push(new URL(r.url()).pathname); });
      await context.request.post(url + "/api/settings", { data: { lang, theme: "light" } });
      await page.route("**/wails/runtime.js", (r) => r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" }));
      await context.request.post(url + "/api/provider/save", { data: { id: "jellytoken", models: [], noModels: true } });
      await page.goto(url + "/?view=providers");
      await page.waitForFunction(() => document.querySelector("#view-providers")?.hidden === false && document.body.textContent.includes("Jellytoken"));
      assert.equal(await page.title(), "Jellytool");
      assert.equal(await page.locator('link[rel="icon"]').getAttribute("href"), "icons/jellytoken.png");
      assert(await page.locator(".brand .logo img").evaluate((img) => img.complete && img.naturalWidth === 256));
      assert.deepEqual(await page.evaluate(() => [...document.body.childNodes].filter((node) => node.nodeType === Node.TEXT_NODE).map((node) => node.textContent.trim()).filter(Boolean)), []);
      assert.deepEqual(await page.locator("#nav button:visible").evaluateAll((bs) => bs.map((b) => b.dataset.view).sort()), ["agents", "providers"]);
      const guide = page.locator("#jellyGuide");
      await guide.waitFor();
      assert.equal(await guide.locator("ol li").count(), 3);
      assert.equal(await guide.locator('[aria-current="step"] b').textContent(), await page.evaluate(() => t("2. Choose models and save")));
      await guide.locator("summary").click();
      assert(await guide.locator("details").evaluate((d) => d.open));
      await page.evaluate(() => renderProviders());
      assert(await guide.locator("details").evaluate((d) => d.open), "refresh keeps help open");
      assert((await guide.textContent()).includes("models.json"));
      if (process.env.ARTIFACT_DIR) {
        await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
        await page.screenshot({ animations: "disabled", path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-guide.png`) });
      }
      await guide.locator('[data-action="supplier"]').click();
      const editor = page.locator("#modal .editor");
      await editor.locator(".models").waitFor();
      assert(await editor.locator(".jelly-editor-guide").isVisible());
      assert.equal(await editor.locator(".jelly-rec-group,.jelly-model-tabs").count(), 0, "recommendations belong on Quick Start");
      await editor.locator(".mchip.pick").first().waitFor();
      if (process.env.ARTIFACT_DIR) {
        await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
        await page.screenshot({ animations: "disabled", path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-supplier.png`) });
      }
      assert.equal(await editor.locator(".advanced, .bar > .danger, .mfoot input").count(), 0);
      await editor.locator(".mchip.pick").filter({ hasText: "vendor/text.v1:exact" }).click();
      const saved = page.waitForResponse((r) => new URL(r.url()).pathname === "/api/provider/save");
      await editor.locator(".bar > .primary").click();
      assert.equal((await saved).status(), 200);
      await page.waitForFunction(() => document.querySelector("#modal").hidden);
      let supplier = (await (await context.request.get(url + "/api/providers")).json()).providers[0];
      assert.deepEqual(supplier.chosen, ["vendor/text.v1:exact"]);
      assert.equal(await guide.locator('[aria-current="step"] b').textContent(), await page.evaluate(() => t("3. Connect WorkBuddy")));
      await guide.locator('[data-action="agents"]').click();
      await page.waitForFunction(() => !document.querySelector("#view-agents").hidden);
      await page.locator('#nav button[data-view="providers"]').click();
      const toggle = page.waitForResponse((r) => new URL(r.url()).pathname === "/api/provider/off", { timeout: 20000 });
      await page.locator('#providers .pswitch').click();
      assert.equal((await toggle).status(), 200);
      await page.locator("#foldOff").click();
      const enabled = page.waitForResponse((r) => new URL(r.url()).pathname === "/api/provider/on", { timeout: 20000 });
      await page.locator('#offProviders .pswitch').click();
      assert.equal((await enabled).status(), 200);
      supplier = (await (await context.request.get(url + "/api/providers")).json()).providers[0];
      assert.deepEqual(supplier.chosen, ["vendor/text.v1:exact"]);
      await page.locator('.row.provider[data-id="jellytoken"] .who').click();
      // UI-only prompt fixture: no Key is submitted. The real bundled host's
      // APIKey/models/fetch contract is exercised by TestJellytoolActiveAdapter.
      await page.route("**/api/plugin-signin/prompt", (route) => {
        assert.equal(route.request().postDataJSON().provider, "jellytoken");
        return route.fulfill({ contentType: "application/json", body: '{"prompt":null,"inputs":{}}' });
      });
      await editor.locator(".acc button").first().click();
      await page.locator('.plugin-ask input[type="password"]').waitFor();
      await page.locator('.plugin-ask button.text').last().click();
      await page.keyboard.press("Escape");
      await page.setViewportSize({ width: 720, height: 760 });
      await page.locator('#nav button[data-view="agents"]').click();
      await page.waitForFunction(() => !document.querySelector("#view-agents").hidden);
      await page.locator("#prefs").click();
      await page.locator("#themeSegs button").first().waitFor();
      if (process.env.ARTIFACT_DIR) await page.screenshot({ animations: "disabled", path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-settings.png`) });
      assert.equal(await page.locator(".om-only:visible").count(), 0);
      assert.deepEqual(await page.locator("#setTabs button:not([hidden])").evaluateAll((bs) => bs.map((b) => b.id).sort()), ["setTab-about", "setTab-general"]);
      await page.evaluate(() => show("plugins"));
      await page.waitForFunction(() => !document.querySelector("#view-providers").hidden);
      await page.evaluate(() => show("routing"));
      assert.equal(await page.locator("#view-providers").evaluate((e) => e.hidden), false);
      const initial = await (await context.request.get(url + "/api/providers")).json();
      await page.route("**/api/providers*", (route) => route.fulfill({ json: { ...initial, providers: [] } }));
      await page.goto(url + "/?view=providers");
      await page.locator("#addSheet .tile").waitFor();
      assert.deepEqual(await page.locator("#addSheet .tile").allTextContents(), ["Jellytoken"]);
      assert.equal(await guide.locator('[aria-current="step"] b').textContent(), await page.evaluate(() => t("1. Add your key")));
      assert(await guide.evaluate((g) => g.scrollWidth <= g.clientWidth + 1), "guide fits narrow window");
      await guide.locator('[data-action="supplier"]').click();
      await page.locator('#addSheet .plugin-ask input[type="password"]').waitFor();
      for (const path of requests) assert(!/^\/api\/(plugins|signin|accounts|usage|sessions|upstream|update|sync|importapps|fonts|discovery)/.test(path), `forbidden background request: ${path}`);
      assert.deepEqual(failures, []);
      assert.deepEqual(errors, []);
    });
  }
}
