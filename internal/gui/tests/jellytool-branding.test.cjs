const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const { test } = require("node:test");
const defaultModelConfig = require("../assets/jellytool-models.json");
const { chromium, webkit } = require("playwright");
const { settled } = require("./reader.cjs");

const assets = path.resolve(process.env.JELLYTOOL_BRAND_ASSETS || path.join(__dirname, "../assets"));
const langs = ["en", "zh", "zh-TW", "ja", "de"];
const attributions = { en: "Based on magpie", zh: "基于 magpie", "zh-TW": "基於 magpie", ja: "magpie をベースに開発", de: "Basiert auf magpie" };
const root = path.resolve(__dirname, "../../..");
const licenseText = fs.readFileSync(path.join(root, "LICENSE"), "utf8");
const noticesText = fs.readFileSync(path.join(root, "THIRD_PARTY_NOTICES"), "utf8");

test("Jellytool brands translated templates without rewriting user data or protocol names", () => {
  const ctx = vm.createContext({ window: { bootPrefs: { jellytool: true } } });
  vm.runInContext(fs.readFileSync(path.join(assets, "i18n.js"), "utf8"), ctx);
  vm.runInContext("var magpie = { strings(dict) { for (const [lang, words] of Object.entries(dict)) Object.assign(I18N[lang], words); } };", ctx);
  for (const id of ["library", "routing", "usage", "sessions", "redact", "otel"]) {
    vm.runInContext(fs.readFileSync(path.join(assets, `plugins/${id}/i18n.js`), "utf8"), ctx);
  }
  const say = (key, vars) => vm.runInContext(`t(${JSON.stringify(key)}, ${JSON.stringify(vars)})`, ctx);
  for (const lang of langs) {
    vm.runInContext(`locale = ${JSON.stringify(lang)}`, ctx);
    for (const key of ["Disconnect from magpie", "Plugins extend magpie", "Set by magpie", "Empty magpie's trash?", "magpie masked these on this computer", "Little Magpie's notebook", "Give agents Magpie Image…"]) {
      const text = say(key);
      assert(text.includes("Jellytoken助手"), `${lang}: ${key}: ${text}`);
      assert(!/magpie|小鹊|小鵲/i.test(text), text);
    }
    const text = say("{agent} is connected to magpie", { agent: "my-magpie-agent" });
    assert(text.includes("my-magpie-agent") && text.includes("Jellytoken助手"), "user-supplied names stay literal");
    const literal = say("{agent} is connected to magpie", { agent: "magpie" });
    assert(literal.includes("magpie") && literal.includes("Jellytoken助手"), "an exact old-brand user name stays literal too");
    assert.equal(say("Jellytoken助手's key"), say("magpie's key"), "branded provider labels still translate");
    assert(say("Reported by the app that made it, through POST /v1/magpie/usage/requests: the call went straight to its vendor, not through magpie.").includes("/v1/magpie/usage/requests"));
    assert(!say("Written for magpie by its community, in github.com/magpie-community/plugins").includes("Jellytoken助手"));
    assert(!say("Before connecting, back up WorkBuddy’s models.json. Existing magpie and other provider entries are kept. A model ID already owned by another provider must be removed from this selection.").includes("Jellytoken助手"));
    assert(say("{sent} is a routing group of the remote magpie, and it routed the request to {served}: the group picking one of its models, not the vendor swapping the model.").includes("magpie"));
    const notice = vm.runInContext(`tNotice("magpie couldn't ask Custom magpie to quit. Quit it yourself, then open it again.")`, ctx);
    assert(notice.includes("Jellytoken助手") && notice.includes("Custom magpie"), "notices brand the template before filling its agent slot");
  }
  assert.equal(say("unknown plugin text about magpie"), "unknown plugin text about magpie");
  vm.runInContext('window.bootPrefs.jellytool = false; locale = "en"', ctx);
  assert.equal(say("Disconnect from magpie"), "Disconnect from magpie");
});

function server(lang, jellytool) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/jellytool-model-config.js") return route.fulfill({ contentType: "text/javascript", body: `window.jellytoolModelConfig = ${JSON.stringify(defaultModelConfig)};` });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = ${JSON.stringify({ jellytool, lang, theme: "light", web: false })}` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [{ id: "codex", name: "Codex", icon: "generic", path: "/fixture/codex", wired: true, fields: [{ key: "model", label: "model", value: "relay/model", options: [{ value: "relay/model", label: "model", ref: "relay/model", note: "Relay · via magpie" }] }] }], profiles: [], settings: { lang, theme: "light", guideHidden: true, dir: "/fixture/jellytool", gateway: "http://127.0.0.1:3435" } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/notices") return json({ text: noticesText });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    let body = fs.readFileSync(file);
    if (url.pathname === "/" && jellytool) body = Buffer.from(body.toString()
      .replace(/<span class="logo"[^>]*>.*?<\/span>/s, '<span class="logo" aria-hidden="true"><img src="icons/jellytoken.png" width="22" height="22" alt=""></span>')
      .replace('</head>', '<link rel="stylesheet" href="jellytool.css"></head>')
      .replace('<script src="start.js">', '<script src="jellytool-model-config.js"></script><script src="jellytool.js"></script><script src="start.js">'));
    await route.fulfill({ body, contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: the native Jellytool sidebar brand aligns with navigation`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const page = await browser.newPage({ viewport: { width: 1100, height: 760 }, reducedMotion: "reduce" });
    const errors = []; page.on("pageerror", (e) => errors.push(e.message));
    await page.route("**/*", server("zh", true));
    await page.goto("http://jellytool.test/?view=agents");
    await page.locator('.row.agent[data-id="codex"]').waitFor();
    await page.evaluate(() => { document.body.classList.add("mac"); navPick = "side"; layoutNav(); });
    for (const theme of ["light", "dark"]) for (const width of [168, 212, 320]) {
      await page.evaluate(({ theme, width }) => { document.documentElement.dataset.theme = theme; sideKeep(width); }, { theme, width });
      const box = await page.evaluate(() => {
        const head = document.querySelector(".side-top").getBoundingClientRect();
        const brand = document.querySelector(".side-top .brand").getBoundingClientRect();
        const logo = document.querySelector(".side-top .logo img").getBoundingClientRect();
        const toggle = document.querySelector(".side-top #sideToggle")?.getBoundingClientRect();
        const nav = document.querySelector('#nav button[data-view="quickstart"] .nav-ic').getBoundingClientRect();
        return { aligned: Math.abs(logo.left - nav.left) < 1, fits: brand.right <= head.right - 12 && brand.top >= head.top && brand.bottom <= head.bottom,
          centered: Math.abs(brand.top + brand.height / 2 - head.top - head.height / 2) < 1,
          toggleFits: !!toggle && toggle.left >= brand.right + 8 && toggle.right <= head.right - 12 && Math.abs(toggle.top + toggle.height / 2 - head.top - head.height / 2) < 1,
          font: parseFloat(getComputedStyle(document.querySelector(".side-top .brand")).fontSize), logo: [logo.width, logo.height] };
      });
      assert.equal(box.aligned, true, `${theme} ${width}: brand and navigation share a left edge`);
      assert.equal(box.fits && box.centered, true, `${theme} ${width}: brand fits and is vertically centered`);
      assert(box.font <= 14, "sidebar wordmark stays subordinate to the page title");
      assert.deepEqual(box.logo, [22, 22], "the existing Jellytoken icon keeps its size and proportions");
      assert.equal(box.toggleFits, true, `${theme} ${width}: collapse control fits to the right of Jellytool`);
      assert.equal(await page.locator('#sideToggle').count(), 1, "the same toggle is moved rather than duplicated");
      if (process.env.ARTIFACT_DIR && width === 212) {
        fs.mkdirSync(process.env.ARTIFACT_DIR, { recursive: true });
        await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${theme}-brand-header.png`) });
      }
    }
    const toggle = page.locator('#sideToggle');
    await toggle.focus(); await page.keyboard.press("Enter");
    await page.locator('.top #sideToggle').waitFor();
    assert.equal(await toggle.getAttribute('aria-pressed'), "false");
    assert.equal(await toggle.getAttribute('aria-label'), await page.evaluate(() => t("Show sidebar")));
    assert.equal(await toggle.evaluate((e) => document.activeElement === e), true, "collapsing retains keyboard focus on the moved control");
    await page.keyboard.press("Enter");
    await page.locator('.side-top #sideToggle').waitFor();
    assert.equal(await toggle.getAttribute('aria-pressed'), "true");
    assert.equal(await toggle.getAttribute('aria-label'), await page.evaluate(() => t("Hide sidebar")));
    assert.equal(await toggle.evaluate((e) => document.activeElement === e), true, "expanding retains keyboard focus on the moved control");
    await toggle.click();
    await page.locator('.top #sideToggle').waitFor();
    assert.equal(await page.evaluate(() => localStorage.getItem('magpie.navLayout')), "top");
    await page.reload();
    await page.locator('.row.agent[data-id="codex"]').waitFor();
    await page.locator('.top #sideToggle').waitFor();
    assert.equal(await page.locator('.top .brand').isVisible(), true, "switching to horizontal navigation retains the brand");
    await toggle.click(); await page.locator('.side-top #sideToggle').waitFor();
    assert.deepEqual(errors, []);
  });

  test(`${engine}: Jellytool branding in Agents, settings and About`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of langs) for (const width of [1100, 420]) {
      await t.test(`${lang} ${width}`, async () => {
        const context = await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" });
        try {
          const page = await context.newPage(), errors = [];
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", server(lang, true));
          await page.goto("http://jellytool.test/?view=agents");
          await page.locator('.row.agent[data-id="codex"]').waitFor();
          assert.equal(await page.title(), "Jellytoken助手");
          assert.equal(await page.locator('.side-top #sideToggle').count(), width === 1100 ? 1 : 0);
          assert.equal(await page.locator('#sideToggle').getAttribute('aria-label'), await page.evaluate((key) => t(key), width === 1100 ? "Hide sidebar" : "Show sidebar"));
          assert((await page.locator("#view-agents").innerText()).includes("Jellytoken助手"));
          assert(!/magpie/i.test(await page.locator("#view-agents").innerText()));
          assert((await page.locator(".ag-conn").getAttribute("aria-label")).includes("Jellytoken助手"));
          await page.locator("#prefs").click();
          await page.evaluate(() => setSetTab("about"));
          await page.waitForFunction(() => document.querySelector("#about")?.textContent.includes("www.jellytoken.com"));
          assert((await page.locator("#about").innerText()).includes("www.jellytoken.com"));
          assert(!await page.locator('#about .discord').count(), "upstream community is not presented as Jellytool support");
          assert.equal(await page.locator("#about .github").count(), 0, "no upstream repository button");
          const licenses = page.locator("#about .row.pref").filter({ hasText: attributions[lang] });
          assert.equal(await licenses.count(), 1, "attribution shares the existing licenses row");
          await licenses.getByRole("button", { name: await page.evaluate(() => t("View")), exact: true }).click();
          await page.locator(".notices-text").waitFor({ state: "visible" });
          const text = await page.locator(".notices-text").textContent();
          assert.equal(text, noticesText);
          assert(text.includes(licenseText), "the original copyright and complete MIT license are visible");
          await page.locator(".notices").getByRole("button", { name: await page.evaluate(() => t("Done")), exact: true }).click();
          await page.locator("#modal").waitFor({ state: "hidden" });
          assert.deepEqual(errors, []);
          if (process.env.ARTIFACT_DIR && lang === "zh") {
            fs.mkdirSync(process.env.ARTIFACT_DIR, { recursive: true });
            await settled(page);
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${width}-branding.png`) });
          }
        } finally { await context.close(); }
      });
    }
  });
  test(`${engine}: magpie retains its community links`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const page = await browser.newPage();
    await page.route("**/*", server("en", false));
    await page.goto("http://magpie.test/?view=settings&tab=about");
    await page.locator("#about .github").waitFor();
    assert.equal(await page.locator("#about .github").getAttribute("title"), "github.com/yetone/magpie");
    assert.equal(await page.locator("#about .discord").count(), 1);
    assert(!(await page.locator("#about").innerText()).includes("Based on magpie"));
  });
}
