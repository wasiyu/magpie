// Jellytool's full Providers page: official subscriptions start in Turned off,
// and Jellytoken stays first in the list and Add sheet with the app's own PNG.
// Local fixtures only; both engines, every language, wide and narrow windows.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const defaultModelConfig = require("../assets/jellytool-models.json");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(process.env.JELLYTOOL_PROVIDER_ASSETS || path.join(__dirname, "../assets"));
const row = (id, name, extra = {}) => ({ id, name, icon: "generic", host: "fixture.invalid", models: [], agents: [], key: { set: false }, ...extra });
const rows = [row("relay", "Relay"), row("jellytoken", "Jellytoken", { icon: "jellytoken", ready: true, account: { agent: "jellytoken", agentName: "Jellytoken", user: "fixture", logins: [] } }), row("codex", "Codex", { off: true })];
const plugins = ["Alpha", "Jellytoken", "Zulu"].map((name) => ({
  id: name.toLowerCase(), pid: name.toLowerCase(), name, icon: name === "Jellytoken" ? "jellytoken" : "generic",
  spec: "fixture-" + name.toLowerCase(), methods: [{ type: "api", label: "API key" }], single: true,
}));

function server(lang, jellytool = true) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/jellytool-model-config.js") return route.fulfill({ contentType: "text/javascript", body: `window.jellytoolModelConfig = ${JSON.stringify(defaultModelConfig)};` });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = ${JSON.stringify({ lang, theme: "light", web: false, jellytool })};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light", guideHidden: true } });
    if (url.pathname === "/api/providers") return json({ providers: rows, plugins, presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/jellytool/config") return json({ baseURL: "https://aiservice.jellytoken.com/v1", api: "openai", signedIn: true });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/plugin-signin/prompt") return json({ prompt: null, inputs: {} });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try {
      let body = await fs.readFile(file);
      if (url.pathname === "/" && jellytool) body = Buffer.from(body.toString()
        .replace('</head>', '<link rel="stylesheet" href="jellytool.css"></head>')
        .replace('<script src="start.js"></script>', '<script src="jellytool-model-config.js"></script><script src="jellytool.js"></script><script src="start.js"></script>'));
      await route.fulfill({ body, contentType });
    }
    catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: Jellytool supplier priority and icon`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of (process.env.JELLYTOOL_TEST_LANG ? [process.env.JELLYTOOL_TEST_LANG] : ["en", "zh", "zh-TW", "ja", "de"])) for (const width of [1100, 420]) {
      await t.test(`${lang} ${width}`, async () => {
        const context = await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang));
        await page.goto("http://magpie.test/?view=providers");
        await page.locator('#providers .row[data-id="jellytoken"]').waitFor();
        assert.deepEqual(await page.locator("#providers .row.provider").evaluateAll((rs) => rs.map((r) => r.dataset.id)), ["jellytoken", "relay"]);
        assert(await page.locator("#offProviders").isHidden(), "official subscriptions start folded under Turned off");
        assert.deepEqual(await page.evaluate(() => providers.providers.map((p) => p.id)), ["relay", "jellytoken", "codex"], "display priority preserves routing order");
        await page.waitForFunction(() => document.querySelector('#providers [data-id="jellytoken"] .ic img')?.naturalWidth === 256);
        await page.locator("#addProvider").click();
        const sheet = page.locator("#addSheet");
        await sheet.locator(".tile").first().waitFor();
        assert.equal(await sheet.locator(".kind b").first().textContent(), await page.evaluate(() => t("From plugins")));
        assert.deepEqual(await sheet.locator(".grid").first().locator(".n").allTextContents(), ["Jellytoken", "Alpha", "Zulu"]);
        assert.equal(await sheet.locator(".tile").first().locator("img").getAttribute("src"), "icons/jellytoken.png");
        assert(await sheet.evaluate((e) => e.scrollWidth <= e.clientWidth + 1), "sheet fits window");
        if (process.env.ARTIFACT_DIR && lang === "zh") {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${width}-jellytool-providers.png`) });
        }
        await sheet.locator(".find").fill("plugin");
        assert.equal(await sheet.locator(".tile .n").first().textContent(), "Jellytoken");
        await sheet.locator(".find").fill("codex");
        assert.equal(await sheet.locator('.tile[data-pick="Jellytoken"]').count(), 0, "pin still respects search");
        await sheet.locator(".find").fill("");
        await sheet.locator(".tile").first().click();
        await page.locator('#modal .jelly-connection input[type="password"]').waitFor();
        assert.equal(await page.locator("#modal .jelly-connection .base-apis button").count(), 5, "the Jellytoken tile opens the same complete connection editor");
        assert.deepEqual(errors, []);
        await context.close();
      });
    }
    const page = await browser.newPage();
    await page.route("**/*", server("en", false));
    await page.goto("http://magpie.test/?view=providers");
    await page.locator("#addProvider").click();
    assert.equal(await page.locator("#providers .row.provider").first().getAttribute("data-id"), "relay", "magpie keeps its provider order");
    assert.equal(await page.locator("#addSheet .kind b").first().textContent(), "Subscriptions", "magpie keeps its Add sheet order");
    await page.close();
  });
}
