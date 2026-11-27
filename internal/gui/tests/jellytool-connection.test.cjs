// Jellytoken setup, editable connection settings and draft-only diagnostics.
// Real page scripts; all API requests are isolated fixtures, never live keys.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test, after } = require("node:test");
const { chromium, webkit } = require("playwright");
const assets = path.resolve(process.env.JELLYTOOL_CONNECTION_ASSETS || path.join(__dirname, "../assets"));
const defaultModelConfig = require("../assets/jellytool-models.json");
const official = "https://aiservice.jellytoken.com/v1";

// One browser per engine, shared by every case in this file. Launching
// WebKit (or Chromium) takes seconds and is what makes the suite spawn a
// visible app over and over; a new context per case keeps the isolation
// (its own cookies, storage and routing) at milliseconds per case. A case
// that needs its browser to behave differently passes `launch` and gets
// its own, still closed when the case ends.
const launchers = {};
function launch(engine) {
  if (!launchers[engine]) {
    launchers[engine] = engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" });
  }
  return launchers[engine];
}
for (const engine of ["chromium", "webkit"]) after(async () => (await launchers[engine])?.close());
async function newPage(engine, options) {
  const context = await (await launch(engine)).newContext(options);
  const page = await context.newPage();
  page.setDefaultTimeout(6000);
  page.setDefaultNavigationTimeout(60000);
  return { context, page };
}
async function closePage(context) { await context.close(); }

function fixture(lang, theme) {
  const w = { connected: false, wired: false, key: "", baseURL: official, api: "openai", chosen: [], posts: [] };
  const row = () => ({ id: "jellytoken", name: "Jellytoken", icon: "jellytoken", host: "fixture.invalid", chat: "plugin://jellytoken/v1", ready: true, off: !!w.off,
    key: { set: false }, keyList: [], models: (w.models || ["m1", "m2"]).map((id) => ({ id, name: id, on: w.chosen.includes(id) })), chosen: w.chosen, noModels: !w.chosen.length, agents: [], fallback: [],
    account: { agent: "jellytoken", agentName: "Jellytoken", agentIcon: "jellytoken", user: "fixture", signedOut: !!w.signedOut, logins: [{ user: "fixture", active: true, on: true }] } });
  const providers = () => ({ providers: [...(w.connected ? [row()] : []), ...(w.extraProviders || [])], presets: [], excluded: [], plugins: [{ id: "jellytoken", pid: "jellytoken", name: "Jellytoken", icon: "jellytoken", single: true, methods: [{ type: "api", label: "API key" }] }], gateway: { running: true, window: true } });
  const state = () => ({ agents: [...(w.wired ? [{ id: "codex", name: "Codex", icon: "generic", path: "/fixture/codex", wired: true, fields: [{ key: "model", value: "jellytoken/m1", options: [{ value: "jellytoken/m1", ref: "jellytoken/m1", label: "m1" }] }] }] : []), ...(w.extraAgents || [])], profiles: [], served: w.chosen.length + (w.extraProviders || []).reduce((n, p) => n + p.chosen.length, 0), settings: { lang, theme } });
  const config = () => ({ baseURL: w.baseURL, api: w.api, signedIn: w.connected });
  w.serve = async (route) => {
    const req = route.request(), u = new URL(req.url());
    const json = (body, status = 200) => route.fulfill({ json: body, status });
    if (u.pathname === "/jellytool-model-config.js") return route.fulfill({ contentType: "text/javascript", body: `window.jellytoolModelConfig = ${JSON.stringify(w.modelConfig ?? defaultModelConfig)};` });
    if (u.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs=${JSON.stringify({ lang, theme, web: false, jellytool: true })};` });
    if (u.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window={};" });
    if (u.pathname === "/api/state") { if (w.stateWait) await w.stateWait; return json(state()); }
    if (u.pathname === "/api/providers") {
      const snapshot = providers();
      w.providerReads = (w.providerReads || 0) + 1;
      if (w.providerWait) await w.providerWait;
      if (w.providerFailure) { w.providerFailure--; return json({ error: "fixture setup unavailable" }, 503); }
      return json(snapshot);
    }
    // The window saying it is in use (update/using, sent on input) or that it
    // went from view (page-away) is not a save of the page's own.
    if (req.method() === "POST" && (u.pathname === "/api/update/using" || u.pathname === "/api/page-away")) return json({});
    if (req.method() === "POST") {
      const body = req.postDataJSON();
      w.posts.push({ path: u.pathname, body });
      if (u.pathname === "/api/jellytool/key" || u.pathname === "/api/jellytool/config") {
        if (body.key === "bad-key") return json({ error: "invalid fixture key" }, 400);
        if (w.keyWait) await w.keyWait;
        w.connected = true; w.key = body.key || w.key;
        w.baseURL = body.baseURL || w.baseURL; w.api = body.api || w.api;
        return json(config());
      }
      if (u.pathname === "/api/jellytool/detect") return json({ results: (body.models?.length ? body.models : ["m1"]).flatMap((model) => ["openai", "responses", "anthropic", "gemini", "decide"].map((protocol) => ({ protocol, model, base: body.baseURL, ok: protocol === "responses", status: protocol === "responses" ? 200 : 404, error: protocol === "responses" ? "" : "HTTP 404" }))) });
      if (u.pathname === "/api/provider/save") { if (w.saveFailure) return json({ error: "fixture save failed" }, 503); w.chosen = body.models || []; return json(providers()); }
    }
    if (u.pathname === "/api/jellytool/config") return json(config());
    if (u.pathname === "/api/jellytool/models") { if (w.modelsWait) await w.modelsWait; return json({ models: (w.catalogModels ?? w.models ?? []).map((id) => ({ id, outputs: ["text"] })), catalogError: !!w.catalogError }); }
    if (u.pathname === "/api/usage/quotas") return json([]);
    if (u.pathname === "/api/groups") return json({ groups: [] });
    if (u.pathname === "/api/plugins") return json({ plugins: [] });
    if (u.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, u.pathname === "/" ? "index.html" : u.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".png": "image/png", ".svg": "image/svg+xml" }[path.extname(file)];
    try {
      let body = await fs.readFile(file);
      if (u.pathname === "/") body = Buffer.from(body.toString().replace("</head>", '<link rel="stylesheet" href="jellytool.css"></head>').replace('<script src="start.js"></script>', '<script src="jellytool-model-config.js"></script><script src="jellytool.js"></script><script src="start.js"></script>'));
      return route.fulfill({ body, contentType });
    } catch { return route.fulfill({ status: 404, body: "" }); }
  };
  return w;
}

async function readable(page) {
  const failures = await page.locator("#quickstart").evaluate((root) => {
    const rgb = (s) => { const p = s.match(/[\d.]+/g)?.map(Number) || []; return [p[0] || 0, p[1] || 0, p[2] || 0, p[3] ?? 1]; };
    const over = (a, b) => a.slice(0, 3).map((n, i) => n * a[3] + b[i] * (1 - a[3])).concat(1);
    const bg = (el) => { const chain = []; for (let x = el; x; x = x.parentElement) chain.unshift(x); return chain.reduce((c, e) => over(rgb(getComputedStyle(e).backgroundColor), c), [255, 255, 255, 1]); };
    const light = (c) => c.slice(0, 3).map((n) => n / 255).map((n) => n <= .04045 ? n / 12.92 : ((n + .055) / 1.055) ** 2.4).reduce((sum, n, i) => sum + n * [.2126, .7152, .0722][i], 0);
    const checks = [...root.querySelectorAll(".guide-h,.qs-tagline,.guide-title,.guide-say,.qs-label,.qs-description,.guide-n,.jelly-rec-head h4,.jelly-rec-group > p,.jelly-model-count,.jelly-model-purpose,.jelly-model-recommended,.qs-model-summary li,.qs-image-section-title,.qs-image-section-description,.qs-image-example code,.mchip.pick,.qs-usage-tabs .opt,.text:not(:disabled)")].filter((e) => e.textContent.trim() && e.getClientRects().length);
    const result = [];
    for (const e of checks) {
      const background = bg(e), color = over(rgb(getComputedStyle(e).color), background);
      const l = [light(color), light(background)].sort((a, b) => b - a), contrast = (l[0] + .05) / (l[1] + .05);
      if (contrast < 4.5) result.push(`${e.className}: contrast ${contrast.toFixed(2)}`);
      if (e.tagName === "BUTTON" && e.scrollWidth > e.clientWidth + 1) result.push(`${e.textContent}: clipped button`);
    }
    const input = root.querySelector(".jelly-guide-key:not(:disabled)");
    if (input) {
      const l = [light(rgb(getComputedStyle(input, "::placeholder").color)), light(bg(input))].sort((a, b) => b - a);
      if ((l[0] + .05) / (l[1] + .05) < 4.5) result.push("placeholder contrast");
    }
    if (document.scrollingElement.scrollWidth > innerWidth + 1 || root.scrollWidth > root.clientWidth + 1) result.push("horizontal overflow");
    return result;
  });
  assert.deepEqual(failures, [], "text, inputs and actions remain readable without clipping");
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) for (const width of [1100, 420]) for (const theme of ["light", "dark"]) {
    test(`${engine} ${lang} ${width} ${theme}: Jellytoken key guidance and editable connection drafts`, async (t) => {
      const { context, page } = await newPage(engine, { viewport: { width, height: 850 }, reducedMotion: "reduce" });
      t.after(() => closePage(context));
      const errors = []; page.on("pageerror", (e) => errors.push(e.message));
      const w = fixture(lang, theme); await page.route("**/*", w.serve);
      let releaseProviders;
      if (lang === "en" && width === 1100 && theme === "light") {
        w.providerWait = new Promise((resolve) => { releaseProviders = resolve; }); w.providerFailure = 1;
      }
      await page.goto("http://magpie.test/");
      const guide = page.locator("#quickstart");
      if (releaseProviders) {
        await guide.locator('.qs-loading[aria-busy="true"]').waitFor();
        assert.equal(await guide.locator(".jelly-guide-key").count(), 0, "unknown configuration shows a skeleton instead of an incorrect key form");
        releaseProviders();
        await guide.locator('.guide-check[role="alert"]').waitFor();
        await guide.locator('.guide-check button').click();
      }
      await guide.locator(".jelly-guide-key").waitFor();
      const tr = (s) => page.evaluate((s) => t(s), s);
      assert.deepEqual(await guide.locator('.guide-step').evaluateAll((es) => es.map((e) => e.dataset.step)), ["source", "models", "agent", "usage", "images"], "usage and optional image setup stay available");
      const usage = guide.locator('[data-step="usage"]');
      assert.equal(await usage.locator('.guide-tick').textContent(), "4");
      assert.equal(await usage.locator('.guide-title').textContent(), await tr("Use models in your agent"));
      assert.equal(await usage.locator('select,[role="tab"],.qs-usage-steps').count(), 0, "unconnected Agents have no usage guides");
      assert.equal(await usage.getByText(await tr("Connect an agent in step 3 to see how to use its models."), { exact: true }).isVisible(), true);
      assert.equal(await guide.locator('.is-current').getAttribute("data-step"), "source");
      assert.equal(await guide.locator(".primary").count(), 1, "only the next unfinished step has a primary action");
      assert.equal(await guide.locator('[data-step="source"] .qs-copy .guide-say').first().textContent(), await tr("A subscription you already pay for, a provider, or an API key"), "the original provider guidance stays visible beside the Jellytoken form");
      assert.equal(await guide.locator('[data-step="agent"] .guide-say').textContent(), await tr("Install Claude Code, Codex, Gemini CLI, OpenCode… and magpie will list them here."), "the original agent guidance stays visible");
      assert.equal(await guide.locator('[data-step="agent"]').evaluate((e) => e.classList.contains("qs-original")), true, "agent connection keeps the original step layout");
      assert.equal(await guide.locator('label[for="jelly-guide-key"]').isVisible(), true, "a visible label accompanies the key input");
      const layout = await guide.locator('[data-step="source"] .guide-body').evaluate((e) => {
        const copy = e.querySelector(".qs-copy").getBoundingClientRect(), actions = e.querySelector(".qs-actions").getBoundingClientRect();
        return { copy: { x: copy.x, bottom: copy.bottom }, actions: { x: actions.x, y: actions.y } };
      });
      if (width > 700) assert(layout.actions.x > layout.copy.x + 100, "desktop copy and action columns align");
      else assert(Math.abs(layout.actions.x - layout.copy.x) < 2 && layout.actions.y >= layout.copy.bottom, "narrow layout explicitly stacks its action area");
      await readable(page);
      if (process.env.ARTIFACT_DIR && lang === "zh") {
        await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
        await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-${theme}-key-guide.png`) });
      }
      // Connection settings are available even before the first key exists.
      await guide.getByRole("button", { name: await tr("Connection settings"), exact: true }).click();
      await page.locator(".jelly-connect-editor .jelly-key").waitFor();
      assert.equal(await page.locator(".jelly-connect-editor .base-apis button").count(), 5);
      await page.locator(".jelly-connect-editor .bar").getByRole("button", { name: await tr("Cancel"), exact: true }).click();
      await page.locator('#nav [data-view="quickstart"]').click();
      await guide.getByRole("button", { name: await tr("Create a Jellytoken API key ↗"), exact: true }).click();
      assert(w.posts.some((p) => p.path === "/api/open" && p.body.url === "https://www.jellytoken.com/api-keys"));
      await guide.getByRole("button", { name: await tr("Verify and connect"), exact: true }).click();
      await guide.getByText(await tr("Paste your API key first"), { exact: true }).waitFor();
      assert.equal(w.posts.some((p) => p.path === "/api/jellytool/key"), false, "empty submission stays in the form");
      // Background reads retain the reader's input, visibility preference,
      // selection and focus, including while the key is being verified.
      await guide.locator(".jelly-guide-key").fill("fixture-key");
      await guide.locator(".qs-key-eye").click();
      await guide.locator(".jelly-guide-key").focus();
      await guide.locator(".jelly-guide-key").evaluate((e) => e.setSelectionRange(2, 6));
      const composing = await guide.locator(".jelly-guide-key").elementHandle();
      await composing.evaluate((e) => e.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true })));
      await page.evaluate(() => load(true));
      assert.equal(await composing.evaluate((e) => e.isConnected), true, "a background read retains the field during IME composition");
      await composing.evaluate((e) => e.dispatchEvent(new CompositionEvent("compositionend", { bubbles: true })));
      assert.deepEqual(await guide.locator(".jelly-guide-key").evaluate((e) => [e.value, e.type, e.selectionStart, e.selectionEnd, document.activeElement === e]), ["fixture-key", "text", 2, 6, true]);
      await guide.locator(".jelly-guide-key").fill("bad-key");
      await guide.locator(".jelly-guide-key").press("Enter");
      await guide.getByText("invalid fixture key", { exact: true }).waitFor();
      assert.equal(await guide.locator(".jelly-guide-key").getAttribute("aria-invalid"), "true");
      assert.equal(await guide.locator(".guide-result").getAttribute("role"), "alert");
      await readable(page);
      assert.equal(w.connected, false);
      let releaseKey;
      w.keyWait = new Promise((resolve) => { releaseKey = resolve; });
      await guide.locator(".jelly-guide-key").fill("valid-key");
      await guide.locator(".jelly-guide-key").press("Enter");
      await guide.locator('.qs-key-form[aria-busy="true"]').waitFor();
      await page.evaluate(() => load(true));
      assert.equal(await guide.locator(".jelly-guide-key").isDisabled(), true);
      assert.equal(await guide.locator('button[type="submit"]').isDisabled(), true);
      releaseKey(); w.keyWait = null;
      await guide.locator('[data-step="source"].done').waitFor();
      assert.equal(await guide.locator('[data-step="source"] .qs-copy .guide-say').first().textContent(), "Jellytoken", "the connected provider remains visible in the original step copy");
      assert.equal(await guide.locator('[data-step="source"] #jelly-guide-saved-key').inputValue(), "••••••••••••", "a saved key is shown only as a mask");
      assert.equal(await guide.locator('[data-step="source"] #jelly-guide-saved-key').isDisabled(), true, "the saved key is read-only until replacement is requested");
      await guide.getByRole("button", { name: await tr("Replace API key"), exact: true }).click();
      assert.equal(await guide.locator('[data-step="source"] #jelly-guide-key').isDisabled(), false, "replacement opens a second key field for editing");
      assert.equal(await guide.locator('.is-current').getAttribute("data-step"), "models");
      assert.equal(await guide.locator('[data-step="models"].done').count(), 0, "connecting a key selects no model");
      assert.equal(await guide.getByRole("button", { name: await tr("Connection settings"), exact: true }).isEnabled(), true);
      const open = async () => {
        await guide.locator('.qs-settings').click();
        await page.locator("#modal .jelly-connection").waitFor();
        await page.waitForFunction(() => document.querySelector(".jelly-base-url")?.value);
      };
      await open();
      const editor = page.locator("#modal .editor"), fields = editor.locator(".jelly-connection");
      assert.equal(await fields.locator(".base-apis button").count(), 5);
      assert.equal(await fields.locator(".jelly-base-url").inputValue(), official);
      await fields.locator(".jelly-base-url").fill("https://draft.invalid/prefix/v1");
      await fields.locator(".jelly-key").fill("probe-key");
      const detect = fields.getByRole("button", { name: await tr("Detect APIs"), exact: true });
      await detect.click(); await fields.locator(".jelly-apply").waitFor();
      const probe = w.posts.findLast((p) => p.path === "/api/jellytool/detect");
      assert.equal(probe.body.baseURL, "https://draft.invalid/prefix/v1", "detection must use the typed Base URL"); assert.equal(probe.body.key, "probe-key");
      assert.equal(w.baseURL, official); assert.equal(w.key, "valid-key");
      await fields.locator(".jelly-apply").click();
      assert.equal(await page.evaluate(() => draft.jellytoken.api), "responses");
      assert.equal(w.api, "openai", "applying detection results only changes the draft");
      await page.keyboard.press("Escape");
      await page.locator("dialog.action-confirm button.primary").click();
      await page.locator("#modal").waitFor({ state: "hidden" });
      assert.equal(w.baseURL, official); assert.equal(w.key, "valid-key");
      await page.locator('#nav [data-view="quickstart"]').click();
      await open();
      assert.equal(await fields.locator(".jelly-base-url").inputValue(), official);
      await fields.locator(".jelly-base-url").fill("https://saved.invalid/v1");
      await fields.locator('.base-apis [data-api="responses"]').click();
      await fields.locator(".jelly-key").fill("bad-key");
      const save = editor.locator(".bar > .primary");
      await save.click(); await editor.getByText("invalid fixture key", { exact: true }).waitFor();
      assert.equal(w.key, "valid-key"); assert.equal(w.baseURL, official, "a failed replacement preserves the working connection");
      await fields.locator(".jelly-key").fill("new-key");
      const allModels = editor.locator('.jelly-model-tabs [data-tab="all"]');
      if (await allModels.count()) await allModels.click();
      await editor.locator(".mchip.pick").filter({ hasText: "m1" }).first().click();
      await fields.getByRole("button", { name: await tr("Each picked model"), exact: true }).click();
      await fields.locator(".jelly-apply").waitFor();
      assert.deepEqual(w.posts.findLast((p) => p.path === "/api/jellytool/detect").body.models, ["m1"]);
      await save.click(); await page.locator("#modal").waitFor({ state: "hidden" });
      assert.equal(w.key, "new-key"); assert.equal(w.baseURL, "https://saved.invalid/v1"); assert.equal(w.api, "responses");
      assert.deepEqual(w.chosen, ["m1"]);
      await page.locator('#nav [data-view="quickstart"]').click();
      assert.equal(await guide.locator('[data-step="models"].done').count(), 1);
      assert.equal(await guide.locator('[data-step="routing"]').count(), 0);
      w.wired = true; await page.evaluate(() => load(true));
      assert.deepEqual(await guide.locator(".guide-step.done .guide-number").allTextContents(), ["1", "2", "3"], "completion keeps the original step numbers");
      assert.equal(await guide.locator(".guide-step.done .guide-done-mark").count(), 3, "completion adds a separate status mark beside every step number");
      assert.equal(await guide.locator('[data-step="agent"] .guide-agent.connected').count(), 1, "connected Agents stay visible in the original step");
      assert.equal(await guide.locator('[data-step="agent"] .guide-agent.connected').isDisabled(), false, "connected Agent chips can be opened to disconnect from quick start");
      assert.equal(await guide.locator('[data-step="agent"] .qs-original-title .guide-open').count(), 1, "the Agents page action stays aligned with the step title");
      const tabs = usage.getByRole("tab"), selectedTab = () => usage.getByRole("tab", { selected: true });
      assert.deepEqual(await tabs.allTextContents(), ["Codex"], "only the connected Agent has a usage tab");
      assert.equal(await selectedTab().textContent(), "Codex");
      assert.equal(await usage.getByRole("tablist", { name: await tr("Agent to use") }).count(), 1);
      assert.equal(await usage.locator('select').count(), 0, "usage switches through tabs");
      assert.equal(await guide.locator('.guide-n').textContent(), await page.evaluate(() => t("{n} of {m} setup steps done", { n: 3, m: 3 })), "progress explicitly counts configuration, not reading the guide");
      const usageWrites = w.posts.length;
      const connected = [["claude", "Claude Code"], ["opencode", "OpenCode"], ["pi", "Pi"], ["gemini", "Gemini CLI"], ["workbuddy", "WorkBuddy"], ["vscode", "VS Code"]];
      w.extraAgents = connected.map(([id, name]) => ({ id, name, icon: "generic", path: "/fixture/" + id, wired: true, fields: [] }));
      await page.evaluate(() => load(true));
      assert.deepEqual(await tabs.allTextContents(), ["Codex", ...connected.map((a) => a[1])]);
      if (process.env.ARTIFACT_DIR && lang === "zh") {
        await require("./reader.cjs").reach(page, usage.locator("summary"));
        await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-${theme}-usage-tabs.png`) });
      }
      for (const [id, marker] of [["claude", "/model"], ["codex", "/model"], ["opencode", "/models"], ["pi", "/model"], ["gemini", "Gemini CLI"], ["workbuddy", "WorkBuddy"], ["vscode", "VS Code"]]) {
        await usage.locator(`[role="tab"][data-usage-agent="${id}"]`).click();
        assert.equal(await selectedTab().getAttribute("data-usage-agent"), id);
        assert.equal(await usage.getByRole("tabpanel").getAttribute("aria-labelledby"), await selectedTab().getAttribute("id"));
        assert.match(await usage.locator('.qs-usage-steps').textContent(), new RegExp(marker));
        assert.equal(await usage.locator('.qs-usage-steps > li').count(), 3, "each Agent explains reopening, model selection and starting a conversation");
        assert.equal(await usage.evaluate((e) => e.classList.contains("done")), false, "reading instructions is not treated as verified usage");
        await readable(page);
      }
      assert.equal(w.posts.length, usageWrites, "viewing guides neither configures Agents nor sends model requests");
      await tabs.first().focus();
      await page.keyboard.press("End");
      assert.equal(await selectedTab().getAttribute("data-usage-agent"), "vscode");
      await page.keyboard.press("ArrowRight");
      assert.equal(await selectedTab().getAttribute("data-usage-agent"), "codex", "keyboard navigation wraps to the first connected Agent");
      await page.keyboard.press("ArrowLeft");
      assert.equal(await selectedTab().getAttribute("data-usage-agent"), "vscode");
      await page.keyboard.press("Home");
      assert.equal(await selectedTab().getAttribute("data-usage-agent"), "codex");
      await usage.getByRole("tab", { name: "WorkBuddy", exact: true }).click();
      await usage.locator('summary').click();
      await selectedTab().focus();
      w.chosen = ["m1", "m2"]; await page.evaluate(() => load(true));
      assert.equal(await selectedTab().getAttribute("data-usage-agent"), "workbuddy", "a changed setup preserves the selected usage guide");
      assert.equal(await usage.locator('details').evaluate((e) => e.open), true, "troubleshooting stays expanded after a redraw");
      assert.equal(await selectedTab().evaluate((e) => document.activeElement === e), true, "redraw restores the selected tab's focus");
      w.chosen = ["m1"]; await page.evaluate(() => load(true));
      w.extraAgents = [{ id: "zcode", name: "ZCode", icon: "generic", menu: true, wired: true, fields: [], models: { listed: 1, shown: 1, names: ["m1"] } }, { id: "opencode", name: "OpenCode", icon: "generic", path: "/fixture/opencode", fields: [] }, { id: "cursor", name: "Cursor", icon: "generic", fields: [] }];
      await page.evaluate(() => load(true));
      assert.deepEqual(await tabs.allTextContents(), ["Codex", "ZCode"], "installed but unconnected Agents have no usage tab");
      assert.equal(await selectedTab().textContent(), "Codex", "disconnecting the selected Agent falls back to a connected one");
      await usage.getByRole("tab", { name: "ZCode", exact: true }).click();
      assert.match(await usage.locator('.qs-usage-steps').textContent(), /ZCode/);
      w.wired = false; w.extraAgents = []; await page.evaluate(() => load(true));
      assert.equal(await usage.locator('[role="tab"],.qs-usage-steps,details').count(), 0, "disconnecting every Agent removes stale guides and tabs");
      w.wired = true;
      w.extraAgents = []; await page.evaluate(() => load(true));
      assert.equal(await guide.locator(".is-current,.primary").count(), 0, "completed setup keeps actions without claiming another required task");
      assert.equal(await guide.locator('[data-step="source"] .qs-settings').count(), 1, "key replacement and endpoint settings share one clear action");
      await readable(page);
      if (lang === "en" && width === 1100 && theme === "light") {
        await page.setViewportSize({ width: 850, height: 850 });
        await page.evaluate(() => { navPick = "side"; layoutNav(); });
        await readable(page);
        const stacked = await guide.locator('[data-step="source"] .guide-body').evaluate((e) => {
          const a = e.querySelector(".qs-copy").getBoundingClientRect(), b = e.querySelector(".qs-actions").getBoundingClientRect();
          return Math.abs(a.x - b.x) < 2 && b.y >= a.bottom;
        });
        assert.equal(stacked, true, "columns also collapse when a saved sidebar reduces the available width");
      }
      if (process.env.ARTIFACT_DIR && lang === "zh") {
        await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
        await page.screenshot({ animations: "disabled", path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-${theme}-complete.png`) });
        await open(); await page.screenshot({ animations: "disabled", path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-${theme}-connection.png`) });
        await page.keyboard.press("Escape"); await page.locator("#modal").waitFor({ state: "hidden" });
        await page.locator('#nav [data-view="quickstart"]').click();
      }

      // Reopening reads real configuration, rather than reusing completed
      // steps. Saved picks and an Agent's wiring survive signing out.
      const done = () => guide.locator('.guide-step.done').evaluateAll((es) => es.map((e) => e.dataset.step));
      const settled = () => page.waitForFunction(() => document.querySelector("#quickstart")?.getAttribute("aria-busy") !== "true");
      const revisit = async () => {
        await page.locator('#nav [data-view="agents"]').click();
        await page.locator('#nav [data-view="quickstart"]').click();
        await settled();
      };
      await settled();
      const writes = w.posts.length, reads = w.providerReads;
      w.signedOut = true; await revisit();
      assert(w.providerReads > reads, "entering Quick Start rechecks provider state");
      assert.deepEqual(await done(), ["agent"], "signed-out provider and its stale served count cannot complete setup");
      assert.deepEqual(w.chosen, ["m1"], "checking does not erase saved model picks");
      assert.equal(await guide.locator('.jelly-guide-key').isVisible(), true, "signed-out setup can be configured again");
      w.signedOut = false; w.off = true; await revisit();
      assert.deepEqual(await done(), ["agent"], "disabled provider is not a usable source");
      w.off = false; w.connected = false; await revisit();
      assert.deepEqual(await done(), ["agent"], "removed provider does not inherit cached completion");
      w.connected = true; w.chosen = []; await revisit();
      assert.deepEqual(await done(), ["source", "agent"], "cleared model selection is read on entry");
      w.chosen = ["m1"]; w.wired = false; await revisit();
      assert.deepEqual(await done(), ["source", "models"], "disconnected Agent is read on entry");
      w.wired = true; await revisit();
      assert.deepEqual(await done(), ["source", "models", "agent"]);
      w.providerFailure = 1; await revisit();
      await guide.locator('.guide-check[role="alert"]').waitFor();
      assert.deepEqual(await done(), [], "failed verification is unknown, not a cached all-clear");
      assert.equal(await usage.locator('[role="tab"],.qs-usage-steps').count(), 0, "failed setup reads do not present cached usage guides as connected");
      await readable(page);
      w.signedOut = true;
      await guide.locator('.guide-check button').click(); await settled();
      assert.deepEqual(await done(), ["agent"], "retry rechecks the current configuration");
      w.signedOut = false; await page.evaluate(() => load(true)); await settled();
      assert.deepEqual(await done(), ["source", "models", "agent"], "window focus refresh also rereads provider status");

      // A response from a visit already left must not overwrite a later visit.
      await page.locator('#nav [data-view="agents"]').click();
      let releaseOld;
      w.providerWait = new Promise((resolve) => { releaseOld = resolve; });
      const beforeOld = w.providerReads;
      await page.locator('#nav [data-view="quickstart"]').click();
      while (w.providerReads === beforeOld) await page.waitForTimeout(10);
      await page.locator('#nav [data-view="agents"]').click();
      w.providerWait = null; w.signedOut = true;
      await page.locator('#nav [data-view="quickstart"]').click(); await settled();
      const oldResponse = page.waitForResponse((r) => new URL(r.url()).pathname === "/api/providers");
      releaseOld(); await (await oldResponse).finished();
      await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      assert.deepEqual(await done(), ["agent"], "late response cannot restore checks from an earlier visit");
      assert.equal(await page.evaluate(() => view), "quickstart");
      assert.equal(w.posts.length, writes, "entry checks only read; they do not change provider, model or Agent configuration");
      assert.deepEqual(errors, []);
    });
  }
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) for (const width of [1100, 420]) for (const theme of ["light", "dark"]) {
    test(`${engine} ${lang} ${width} ${theme}: Quick Start expands three model categories in place`, async (t) => {
      const { context, page } = await newPage(engine, { viewport: { width, height: 850 }, reducedMotion: "reduce" });
      t.after(() => closePage(context));
      const w = fixture(lang, theme), errors = [];
      w.connected = true;
      let releaseMetadata; w.modelsWait = new Promise((resolve) => { releaseMetadata = resolve; });
      w.models = ["gpt-6.1-astra", "gpt-6-astra", "gpt-6.1-sol", "kimi-k3.1-pro", "kimi-k3.1-pro-highspeed", "qwen3.8-max", "glm-5.3", "glm-5.3-prime", "gpt-6.1-terra", "glm-5.3-flash", "glm-5.3-flashx", "gpt-6-luna", "MiniMax-M3.1-Flash", "minimax-m2.7", "minimax-m2.7-highspeed", "doubao-seed-2-1-pro", "custom-model", "custom-extra", "claude-opus-5", "test-model", ...Array.from({ length: 16 }, (_, i) => `extra-model-${i}`)];
      w.chosen = ["custom-model"];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", w.serve);
      await page.goto("http://jellytool.fixture/?view=quickstart");
      const guide = page.locator("#quickstart"), choose = guide.locator('[data-step="models"] .guide-go');
      const savedModels = guide.locator('.qs-model-summary .qs-model-summary-name');
      const savedModelTiers = guide.locator('.qs-model-summary .qs-model-tier');
      const tierOrder = { "gpt-6.1-astra": 0, "gpt-6.1-sol": 1, "gpt-6.1-terra": 2, "gpt-6-luna": 2 };
      const savedOrder = (ids) => ids.map((id, index) => ({ id, index })).sort((a, b) => (tierOrder[a.id] ?? 3) - (tierOrder[b.id] ?? 3) || a.index - b.index).map(({ id }) => id);
      await choose.waitFor();
      await page.waitForFunction(() => document.querySelector("#quickstart")?.getAttribute("aria-busy") === "false");
      assert.deepEqual(await savedModels.allTextContents(), ["custom-model"], "the collapsed model step shows saved selections on entry");
      assert.deepEqual(await savedModelTiers.allTextContents(), [await page.evaluate(() => t("Other"))], "saved custom models show the other tier");
      const savedAtModelStep = async (ids) => {
        await page.waitForFunction(() => document.querySelector("#quickstart")?.getAttribute("aria-busy") !== "true");
        assert.deepEqual(await savedModels.allTextContents(), savedOrder(ids), "saved models remain visible in task-tier order after the picker closes");
        assert.equal(await choose.evaluate((b) => document.activeElement === b), true, "saving returns keyboard focus to Choose models");
        const position = await choose.evaluate((b) => {
          const step = b.closest('.guide-step'), v = b.closest('.view');
          return { top: step.getBoundingClientRect().top - v.getBoundingClientRect().top, remaining: v.scrollHeight - v.clientHeight - v.scrollTop, height: v.clientHeight };
        });
        assert(position.top >= -1 && position.top < position.height / 3 && (position.top <= 2 || position.remaining <= 1), `saving locates the model step as high as the view allows (${JSON.stringify(position)})`);
        await readable(page);
        if (process.env.ARTIFACT_DIR && lang === "zh" && ids.length === 4) {
          await page.screenshot({ animations: "disabled", path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-${theme}-saved-models.png`) });
        }
      };
      await choose.click();
      const picker = guide.locator(".qs-model-picker"), groups = picker.locator(".jelly-rec-group");
      assert.equal(await picker.count(), 1, "Choose models expands the picker inside Quick Start");
      await groups.last().waitFor();
      assert.equal(await groups.count(), 3, "all three recommendation categories expand on Quick Start");
      assert.equal(await picker.getByRole("tab").count(), 2, "model selection offers Recommendations and Selected tabs");
      const browseMore = picker.locator(".jelly-choose-all");
      assert.equal(await browseMore.isVisible(), true, "Browse more models is visible");
      assert.equal(await browseMore.evaluate((b) => b.scrollWidth <= b.clientWidth + 1), true, "Browse more models has room for its full label");
      assert.equal(await picker.locator(".jelly-model-purpose").count(), 9, "every recommendation card explains its purpose");
      assert.equal(await picker.locator(".jelly-category-symbol .mask").count(), 3, "categories use illustrated line icons instead of text symbols");
      assert.equal(await picker.locator(".jelly-model-check svg").count(), 9, "card selectors use the app’s vector check mark");
      assert.equal(await picker.locator(".jelly-category-symbol").evaluateAll((ss) => ss.every((s) => s.getAttribute("aria-hidden") === "true")), true);
      assert.equal(await picker.locator('[data-model="kimi-k3.1-pro"] .jelly-model-purpose').textContent(), await page.evaluate(() => t("Combines native vision and a million-token context with coding, long-document synthesis, financial research and modeling.")), "card copy is condensed from model Basic information");
      assert(await picker.locator(".jelly-model-card").first().evaluate((b) => b.getBoundingClientRect().height <= 120), "cards keep concise descriptions with room for the prominent recommendation badge");
      assert.equal(await picker.locator("details, .jelly-model-detail").count(), 0, "model selection omits redundant details and evidence");
      const bulkLabels = await page.evaluate(() => [t("Select available models"), t("Clear")]);
      assert.deepEqual(await groups.locator("button.action").allTextContents(), Array(3).fill(bulkLabels[0]), "each category has one state-dependent bulk action");
      assert.equal(await page.evaluate(() => view), "quickstart");
      assert.equal(await page.locator("#modal").isVisible(), false, "model selection does not open connection settings");
      assert.equal(await choose.getAttribute("aria-expanded"), "true");
      const tr = (s) => page.evaluate((s) => t(s), s);
      assert.deepEqual(await groups.locator("h4").allTextContents(), await Promise.all(["Solving complex problems", "Everyday work and coding", "Simple tasks"].map(tr)));
      const ids = (group) => group.locator("[data-model]").evaluateAll((bs) => bs.map((b) => b.dataset.model));
      assert.deepEqual(await ids(groups.nth(0)), ["gpt-6.1-astra", "glm-5.3", "qwen3.8-max", "doubao-seed-2-1-pro", "kimi-k3.1-pro"]);
      assert.equal(await picker.locator('[data-model="gpt-6-astra"]').count(), 0, "the Astra card follows the newest version the catalog lists");
      assert.deepEqual(await ids(groups.nth(1)), ["gpt-6.1-sol", "glm-5.3-flash"]);
      assert.deepEqual(await ids(groups.nth(2)), ["gpt-6.1-terra", "gpt-6-luna"]);
      assert.equal(await picker.locator(".jelly-model-recommended").count(), 4, "each fixed GPT model has a recommendation badge");
      assert.equal(await picker.locator('[data-model^="MiniMax-"],[data-model^="minimax-"]').count(), 0, "MiniMax is absent from recommendations");
      assert.equal(await picker.locator('.jelly-model-content').evaluate((e) => e.scrollHeight <= e.clientHeight + 1), true, "recommendations are expanded without a clipped inner scroll box");
      const workhorse = picker.locator('[data-model="gpt-6.1-sol"]');
      const cardBeforeMetadata = await workhorse.elementHandle();
      await workhorse.focus(); await page.keyboard.down("Space");
      const metadataResponse = page.waitForResponse((r) => new URL(r.url()).pathname === "/api/jellytool/models");
      releaseMetadata(); await (await metadataResponse).finished();
      await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      assert.equal(await cardBeforeMetadata.evaluate((b) => b.isConnected), true, "late metadata cannot detach a card during a click");
      await page.keyboard.up("Space");
      assert.equal(await workhorse.getAttribute("aria-pressed"), "true", "selection completes while metadata arrives");
      await workhorse.focus(); await page.keyboard.press("Space"); await page.keyboard.press("Space");
      assert.equal(await workhorse.getAttribute("aria-pressed"), "true");
      assert.equal(await workhorse.evaluate((b) => document.activeElement === b), true, "keyboard selection keeps focus");
      await page.evaluate(() => load(true));
      assert.equal(await workhorse.getAttribute("aria-pressed"), "true", "background refresh retains the model draft");
      const containedActions = async () => page.locator("#quickstart").evaluate((root) => {
        const controls = [root.querySelector('[data-step="source"] .guide-go'), root.querySelector('[data-step="models"] .guide-go'), root.querySelector('[data-step="agent"] .guide-go')].filter(Boolean);
        return controls.every((b) => {
          const r = b.getBoundingClientRect(), body = b.closest(".guide-body").getBoundingClientRect();
          return r.left >= body.left - 1 && r.right <= body.right + 1;
        });
      });
      assert.equal(await containedActions(), true, "configuration actions stay inside their step while expanded");
      assert.equal(await choose.textContent(), await tr("Collapse models"));
      await choose.click();
      assert.equal(await picker.count(), 0, "the same button collapses the model panel");
      assert.equal(await choose.getAttribute("aria-expanded"), "false");
      assert.equal(await choose.textContent(), await tr("Choose models"));
      assert.equal(await containedActions(), true, "configuration actions stay inside their step while collapsed");
      await choose.click();
      assert.equal(await workhorse.getAttribute("aria-pressed"), "true", "reopening preserves unsaved model choices");
      assert.deepEqual(w.posts, [], "collapse and reopen never save or discard the draft");
      const bulk = (index, label) => groups.nth(index).getByRole("button", { name: label, exact: true });
      const all = bulkLabels[0], none = bulkLabels[1];
      assert.equal(await bulk(0, none).count(), 0, "an empty category offers only Select all");
      await picker.locator('[data-model="gpt-6.1-astra"]').click();
      await bulk(1, all).focus(); await page.keyboard.press("Enter");
      assert.equal(await groups.nth(1).locator('[aria-pressed="true"]').count(), 2, "Select all selects every model in this category without duplicates");
      assert.equal(await bulk(1, all).count(), 0, "the same control changes to Select none after selecting all");
      assert.equal(await groups.nth(1).locator("button.action").count(), 1);
      assert.equal(await bulk(1, none).evaluate((b) => document.activeElement === b), true, "the bulk toggle keeps keyboard focus when its label changes");
      await page.keyboard.press("Enter");
      assert.equal(await groups.nth(1).locator('[aria-pressed="true"]').count(), 0, "Select none clears the whole category");
      assert.equal(await picker.locator('[data-model="gpt-6.1-astra"]').getAttribute("aria-pressed"), "true", "clearing one category keeps other categories selected");
      assert.equal(await picker.locator(".jelly-model-count").textContent(), await page.evaluate(() => t("{n} selected", { n: 2 })), "directory-only selections survive clearing a recommendation category");
      assert.equal(await bulk(1, none).count(), 0, "the same control changes back to Select all after clearing");
      assert.equal(await groups.nth(1).locator("button.action").count(), 1);
      assert.equal(await bulk(1, all).evaluate((b) => document.activeElement === b), true, "the bulk toggle keeps keyboard focus after clearing");
      await picker.locator('[data-model="gpt-6.1-astra"]').click();
      await bulk(1, all).click();
      assert.equal(await picker.locator(".jelly-model-count").textContent(), await page.evaluate(() => t("{n} selected", { n: 3 })), "selection count includes saved and newly selected models");
      assert.deepEqual(w.chosen, ["custom-model"], "recommendations only edit the local draft");
      assert.deepEqual(w.posts, [], "opening and choosing do not write configuration");
      await picker.getByRole("tab", { name: await tr("Recommendations"), exact: true }).focus();
      await page.keyboard.press("End");
      assert.equal(await picker.getByRole("tab", { name: await tr("Selected"), exact: true }).getAttribute("aria-selected"), "true", "tabs support keyboard switching");
      assert.deepEqual(await ids(picker), ["custom-model", "gpt-6.1-sol", "glm-5.3-flash"], "Selected includes saved directory models and recommendation picks");
      await picker.locator('[data-model="glm-5.3-flash"]').click();
      assert.equal(await picker.locator('[data-model="glm-5.3-flash"]').count(), 0, "Selected permits removing a pick");
      await picker.getByRole("tab", { name: await tr("Recommendations"), exact: true }).click();
      await bulk(1, all).click();
      await readable(page);
      if (process.env.ARTIFACT_DIR && lang === "zh") {
        await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
        await page.screenshot({ animations: "disabled", fullPage: true, path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-${theme}-model-categories.png`) });
      }
      await picker.locator(".qs-model-actions").getByRole("button", { name: await tr("Cancel"), exact: true }).click();
      await picker.waitFor({ state: "detached" });
      assert.equal(await picker.count(), 0, "Cancel closes the unsaved picker");
      await choose.click();
      await picker.locator('[data-model="gpt-6.1-sol"]').click();
      // Reopening checks catalog access asynchronously; save the visible pick.
      await picker.locator('[data-model="gpt-6.1-sol"][aria-pressed="true"]').waitFor();
      w.saveFailure = true;
      await picker.locator(".qs-model-actions button").last().click();
      try { await picker.getByText("fixture save failed", { exact: true }).waitFor(); }
      catch (e) { console.error("save failure state", JSON.stringify({ posts: w.posts, ui: await picker.textContent(), view: await page.evaluate(() => view) })); throw e; }
      assert.deepEqual(w.chosen, ["custom-model"]);
      assert.equal(await picker.locator('[data-model="gpt-6.1-sol"]').getAttribute("aria-pressed"), "true", "failed Save retains the draft for retry");
      assert.deepEqual(await savedModels.allTextContents(), ["custom-model"], "a failed Save leaves the saved summary unchanged");
      w.saveFailure = false;
      let releaseSavedState; w.stateWait = new Promise((resolve) => { releaseSavedState = resolve; });
      const savedStateRead = page.waitForRequest((r) => new URL(r.url()).pathname === "/api/state");
      await picker.locator(".qs-model-actions button").last().click(); await savedStateRead;
      assert.equal(await picker.count(), 1, "Save keeps the current picker busy until the persisted state is read");
      assert.equal(await picker.locator("[data-model]").first().isDisabled(), true);
      assert.deepEqual(await savedModels.allTextContents(), ["custom-model"], "the summary waits for the authoritative saved state");
      releaseSavedState(); w.stateWait = null;
      await picker.waitFor({ state: "detached" });
      assert.deepEqual(w.chosen, ["custom-model", "gpt-6.1-sol"]);
      assert.deepEqual(w.posts.at(-1), { path: "/api/provider/save", body: { id: "jellytoken", models: ["custom-model", "gpt-6.1-sol"], noModels: false, fallback: [] } });
      await savedAtModelStep(w.chosen);
      assert.deepEqual(await savedModelTiers.allTextContents(), [await tr("Everyday coding"), await tr("Other")], "saved models keep their task tiers visible in order");
      await choose.click();
      await picker.locator('[data-model="gpt-6.1-astra"]').click();
      await picker.locator('[data-model="gpt-6.1-astra"][aria-pressed="true"]').waitFor();
      const writesBeforeMore = w.posts.length;
      await picker.locator(".jelly-choose-all").press("Enter");
      const editor = page.locator("#modal .editor");
      await editor.locator(".models").waitFor();
      assert.equal(await editor.locator(".mserves,.mnames,.mfoot,.jelly-connection").count(), 0, "guided directory exposes only model selection");
      assert.equal(await page.evaluate(() => view), "providers", "more models opens the supplier directory");
      assert.equal(await editor.locator(".jelly-rec-group,.jelly-model-tabs").count(), 0, "supplier editor has no recommendation view");
      assert.deepEqual(await page.evaluate(() => draft.chosen), ["custom-model", "gpt-6.1-sol", "gpt-6.1-astra"], "more models carries the unsaved recommendation draft");
      assert.equal(w.posts.length, writesBeforeMore, "opening the directory does not save");
      await editor.locator(".mbulk input").fill("custom-extra");
      assert.equal(await editor.locator(".mchip.pick").count(), 1, "complete directory supports filtering");
      await editor.locator(".mchip.pick").filter({ hasText: "custom-extra" }).first().click();
      await editor.locator(".jelly-back-recommendations").click();
      await page.locator("#modal").waitFor({ state: "hidden" });
      assert.equal(await page.evaluate(() => view), "quickstart");
      await picker.getByRole("tab", { name: await tr("Selected"), exact: true }).click();
      assert.deepEqual(await ids(picker), ["custom-model", "gpt-6.1-sol", "gpt-6.1-astra", "custom-extra"], "returning from the directory keeps all unsaved picks");
      assert.equal(w.posts.length, writesBeforeMore, "returning and switching tabs do not save");
      await picker.locator(".jelly-choose-all").press("Enter");
      await editor.locator(".models").waitFor();
      assert.deepEqual(await page.evaluate(() => draft.chosen), ["custom-model", "gpt-6.1-sol", "gpt-6.1-astra", "custom-extra"], "reopening the directory retains the same selection");
      w.saveFailure = true;
      await editor.locator(".bar > .primary").click();
      await editor.getByText("fixture save failed", { exact: true }).waitFor();
      assert.deepEqual(w.chosen, ["custom-model", "gpt-6.1-sol"], "failed directory Save leaves persisted choices alone");
      w.saveFailure = false;
      await editor.locator(".bar > .primary").click();
      await page.locator("#modal").waitFor({ state: "hidden" });
      assert.deepEqual(w.chosen, ["custom-model", "gpt-6.1-sol", "gpt-6.1-astra", "custom-extra"], "supplier Save includes recommendations and extra models");
      assert.equal(await page.evaluate(() => view), "quickstart", "Save from the guided directory returns to Quick Start");
      assert.equal(await picker.count(), 0, "saving completes the selection session");
      await savedAtModelStep(w.chosen);
      // A background read must update names even when the selection count stays the same.
      w.chosen = ["custom-model", "gpt-6.1-sol", "gpt-6.1-astra", "extra-model-0"];
      await page.evaluate(() => refreshQuickstart());
      assert.deepEqual(await savedModels.allTextContents(), savedOrder(w.chosen), "equal-sized saved selections refresh the summary in task-tier order");
      w.chosen = ["custom-model", "gpt-6.1-sol", "gpt-6.1-astra", "custom-extra"];
      await page.evaluate(() => refreshQuickstart());
      await choose.click(); await picker.locator(".jelly-choose-all").press("Enter");
      await editor.locator(".mchip.pick").filter({ hasText: "custom-extra" }).first().click();
      const writesBeforeCancel = w.posts.length;
      await editor.locator(".bar").getByRole("button", { name: await tr("Cancel"), exact: true }).click();
      await page.locator("dialog.action-confirm button.primary").click();
      await page.locator("#modal").waitFor({ state: "hidden" });
      assert.equal(await page.evaluate(() => view), "quickstart", "discarding returns to Quick Start");
      assert.equal(w.posts.length, writesBeforeCancel, "Cancel never saves");
      assert.deepEqual(w.chosen, ["custom-model", "gpt-6.1-sol", "gpt-6.1-astra", "custom-extra"], "Cancel restores the previously saved selection");
      await choose.click(); await picker.locator(".jelly-choose-all").press("Enter");
      await editor.locator(".models").waitFor();
      await page.keyboard.press("Escape");
      await page.locator("#modal").waitFor({ state: "hidden" });
      assert.equal(await page.evaluate(() => view), "quickstart", "Escape closes the guided directory through the same cancellation flow");
      await guide.locator(".qs-settings").click();
      await editor.locator(".jelly-connection").waitFor();
      assert.equal(await editor.locator(".jelly-back-recommendations").count(), 0, "direct supplier entry keeps the original editor");
      assert.equal(await editor.locator(".mserves").count(), 1, "direct supplier entry retains its serving modes");
      await editor.locator(".bar > .primary").click();
      await page.locator("#modal").waitFor({ state: "hidden" });
      assert.equal(await page.evaluate(() => view), "providers", "direct supplier Save stays on Providers");
      w.chosen = ["gpt-6.1-sol"];
      await page.locator('#nav [data-view="quickstart"]').click();
      await page.waitForFunction(() => document.querySelector("#quickstart")?.getAttribute("aria-busy") !== "true");
      await choose.click();
      await picker.locator('[data-model="gpt-6.1-sol"]').click();
      await picker.getByRole("tab", { name: await tr("Selected"), exact: true }).click();
      assert.equal(await picker.locator("[data-model]").count(), 0);
      await picker.getByText(await tr("No models selected yet."), { exact: true }).waitFor();
      await picker.locator(".qs-model-actions button").last().click();
      await picker.waitFor({ state: "detached" });
      assert.equal(w.posts.at(-1).body.noModels, true, "explicitly clearing all models saves an empty selection");
      assert.deepEqual(w.chosen, []);
      await savedAtModelStep([]);
      assert.equal(await guide.locator('.qs-model-summary').count(), 0, "clearing the selection removes the saved model list");
      await choose.focus(); await page.keyboard.press("Enter");
      await browseMore.waitFor();
      // The shell restores programmatic scrolls during a click hold. Scroll as a
      // reader would so this also verifies the action inside the visible view.
      await page.mouse.move(width - 40, 600);
      for (let attempt = 0; attempt < 4; attempt++) {
        const delta = await browseMore.evaluate((b) => {
          const v = b.closest(".view"), r = b.getBoundingClientRect(), bounds = v.getBoundingClientRect();
          return r.top < bounds.top || r.bottom > bounds.bottom ? r.top - bounds.top - v.clientHeight / 2 : 0;
        });
        if (!delta) break;
        await page.mouse.wheel(0, delta);
        await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      }
      assert.equal(await browseMore.evaluate((b) => {
        const r = b.getBoundingClientRect(), range = document.createRange(); range.selectNodeContents(b);
        const text = range.getBoundingClientRect();
        if (b.scrollWidth > b.clientWidth + 1 || text.left < r.left - 1 || text.right > r.right + 1 || text.top < r.top - 1 || text.bottom > r.bottom + 1) return false;
        for (let e = b.parentElement; e; e = e.parentElement) {
          const style = getComputedStyle(e), bounds = e.getBoundingClientRect();
          if (style.overflowX !== "visible" && (r.left < bounds.left - 1 || r.right > bounds.right + 1)) return false;
          if (style.overflowY !== "visible" && (r.top < bounds.top - 1 || r.bottom > bounds.bottom + 1)) return false;
        }
        return true;
      }), true, "Browse more models displays its entire label and arrow inside the container");
      if (process.env.ARTIFACT_DIR && lang === "zh") {
        await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
        await page.screenshot({ animations: "disabled", path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-${theme}-browse-more.png`) });
      }
      assert(w.posts.every((p) => p.path === "/api/provider/save"), "no Agent edits or inference calls");
      assert.deepEqual(errors, []);
    });
  }
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    test(`${engine} ${lang}: fixed GPT recommendations require model access before selection`, async (t) => {
      const { context, page } = await newPage(engine, { viewport: { width: 1100, height: 850 }, reducedMotion: "reduce" });
      t.after(() => closePage(context));
      const w = fixture(lang, "light"), errors = [];
      w.connected = true;
      w.catalogModels = ["glm-5.3", "MiniMax-M3.1-Flash", "custom-model"];
      w.models = [...w.catalogModels, "gpt-6-astra", "gpt-6.1-sol", "gpt-6.1-terra", "gpt-6-luna"];
      w.chosen = ["custom-model", "MiniMax-M3.1-Flash"];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", w.serve);
      await page.goto("http://jellytool.fixture/?view=quickstart");
      await page.locator('#quickstart [data-step="models"] .guide-go').click();
      const picker = page.locator(".qs-model-picker");
      const notice = await page.evaluate(() => t("Please enable model access in Jellytoken first."));
      // The supplier editor lists older GPT IDs the key's catalog doesn't; once
      // the catalog is read the cards are redrawn as the configured fallbacks.
      await picker.locator('[data-kind="complex"] [data-model="gpt-6.2-astra"]').waitFor();
      assert.equal(await picker.locator('[data-model="gpt-6-astra"], [data-model="gpt-6.1-sol"]').count(), 0, "cards drawn from the supplier list are replaced");
      for (const [id, tier] of [["gpt-6.2-astra", "complex"], ["gpt-6.2-sol", "daily"], ["gpt-6.1-terra", "simple"], ["gpt-6-luna", "simple"]]) {
        const card = picker.locator(`[data-kind="${tier}"] [data-model="${id}"]`);
        assert.equal(await card.count(), 1, "GPT stays visible without access");
        assert.equal((await card.locator(".jelly-model-recommended").textContent()).endsWith(await page.evaluate(() => t("Recommended"))), true);
        await card.click();
        assert.equal(await card.getAttribute("aria-pressed"), "false");
        assert.equal(await page.locator("#status").textContent(), notice);
        assert.equal(await page.locator("#status").isVisible(), true, "permission toast is visible");
      }
      // Price metadata cannot remove the fixed recommendations on redraw.
      await picker.locator('button[data-category="complex"]').click();
      assert.equal(await picker.locator('[data-model="glm-5.3"]').getAttribute("aria-pressed"), "true");
      assert.equal(await picker.locator('[data-model="gpt-6.2-astra"]').getAttribute("aria-pressed"), "false");
      assert.equal(await picker.locator('button[data-category="complex"]').textContent(), await page.evaluate(() => t("Clear")), "partial access does not claim every displayed model is selected");
      await picker.locator('button[data-category="complex"]').click();
      assert.equal(await picker.locator('button[data-category="complex"]').textContent(), await page.evaluate(() => t("Select available models")));
      await picker.locator('button[data-category="complex"]').click();
      await picker.locator('button[data-category="simple"]').click();
      assert.equal(await picker.locator('[data-kind="simple"] [aria-pressed="true"]').count(), 0);
      assert.equal(await page.locator("#status").textContent(), notice);
      await picker.locator('[data-tab="selected"]').click();
      assert.deepEqual(await picker.locator('[data-model]').evaluateAll((bs) => bs.map((b) => b.dataset.model)), ["custom-model", "MiniMax-M3.1-Flash", "glm-5.3"], "existing MiniMax picks survive removal from recommendations");
      await picker.locator(".qs-model-actions button").last().click();
      await picker.waitFor({ state: "detached" });
      assert.deepEqual(w.chosen, ["custom-model", "MiniMax-M3.1-Flash", "glm-5.3"], "unauthorized GPT models never enter the saved selection");
      assert.deepEqual(errors, []);
    });
  }
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const failed of [false, true]) {
    test(`${engine}: GPT selection uses the API catalog, failed=${failed}`, async (t) => {
      const { context, page } = await newPage(engine, { reducedMotion: "reduce" });
      t.after(() => closePage(context));
      const w = fixture("zh", "light"); w.connected = true;
      w.models = ["custom-model"];
      w.catalogModels = ["custom-model", "gpt-6-astra", "gpt-6.1-sol", "gpt-6.1-terra", "gpt-6-luna"];
      w.catalogError = failed;
      let release; w.modelsWait = new Promise((resolve) => { release = resolve; });
      await page.route("**/*", w.serve);
      await page.goto("http://jellytool.fixture/?view=quickstart");
      await page.locator('#quickstart [data-step="models"] .guide-go').click();
      const picker = page.locator(".qs-model-picker");
      await picker.locator('button[data-category="simple"]').click();
      assert.equal(await picker.locator('[data-kind="simple"] [aria-pressed="true"]').count(), 0, "bulk selection waits for the API result");
      release();
      if (failed) {
        await page.waitForFunction(() => document.querySelector("#status").textContent === t("Could not verify model access. Refresh the model list and try again."));
        assert.equal(await picker.locator('[aria-pressed="true"]').count(), 0);
      } else {
        await page.waitForFunction(() => document.querySelector('[data-model="gpt-6-luna"]').getAttribute("aria-pressed") === "true");
        assert.equal(await picker.locator('[data-model="gpt-6.1-terra"]').getAttribute("aria-pressed"), "true", "both simple GPT roles are selected from the API catalog");
        await picker.locator('[data-model="gpt-6.1-sol"]').click();
        assert.equal(await picker.locator('[data-model="gpt-6.1-sol"]').getAttribute("aria-pressed"), "true", "missing supplier editor IDs cannot deny API-authorized models");
        await picker.locator('button[data-category="simple"]').click();
        assert.equal(await picker.locator('[data-kind="simple"] [aria-pressed="true"]').count(), 0, "the next bulk click clears the selected simple models");
      }
      assert.deepEqual(w.posts, [], "selection remains a local draft");
    });
  }
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) for (const lang of ["en", "zh"]) {
  test(`${engine} ${lang}: Quick Start lists only Jellytoken's saved models`, async (t) => {
    const { context, page } = await newPage(engine, { reducedMotion: "reduce" });
    t.after(() => closePage(context));
    const w = fixture(lang, "light"), errors = []; w.connected = true; w.chosen = ["m1"];
    const other = (id, chosen) => ({ id, name: id, ready: true, models: chosen.map((m) => ({ id: m, name: m, on: true })), chosen, agents: [], fallback: [], account: { user: "fixture", logins: [{ user: "fixture", active: true, on: true }] } });
    w.extraProviders = [other("sub2api-claude", ["claude-opus-5-5", "claude-sonnet-5-5"]), other("sub-anti", ["gemini-3.8-flash"])];
    page.on("pageerror", (e) => errors.push(e.message));
    await page.route("**/*", w.serve);
    await page.goto("http://jellytool.fixture/?view=quickstart");
    const step = page.locator('#quickstart [data-step="models"]');
    await step.locator(".qs-model-summary").waitFor();
    assert.deepEqual(await step.locator(".qs-model-summary .qs-model-summary-name").allTextContents(), ["m1"], "another provider's picks stay out of the Jellytoken summary");
    assert.equal(await step.locator(".guide-say").first().textContent(), await page.evaluate(() => t("{n} models selected", { n: 1 })), "the count is Jellytoken's, not every provider's");
    assert.deepEqual(errors, []);
  });
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: recommendation configuration changes display on reload without granting access`, async (t) => {
    const { context, page } = await newPage(engine, { reducedMotion: "reduce" });
    t.after(() => closePage(context));
    const w = fixture("zh", "light"); w.connected = true; w.models = ["custom-model"];
    w.modelConfig = { version: 2, tiers: { complex: [], daily: [{ id: "gpt-custom", alwaysShow: true, recommended: true, name: "Configured GPT" }], simple: [] } };
    await page.route("**/*", w.serve);
    await page.goto("http://jellytool.fixture/?view=quickstart");
    const choose = page.locator('#quickstart [data-step="models"] .guide-go');
    await choose.click();
    const picker = page.locator(".qs-model-picker");
    assert.equal(await picker.locator('[data-kind="daily"] [data-model="gpt-custom"]').count(), 1);
    assert.equal(await picker.locator('[data-model^="gpt-6"]').count(), 0);
    await picker.locator('[data-model="gpt-custom"]').click();
    assert.equal(await picker.locator('[data-model="gpt-custom"]').getAttribute("aria-pressed"), "false");
    assert.equal(await page.locator("#status").textContent(), await page.evaluate(() => t("Please enable model access in Jellytoken first.")));
    w.modelConfig = { version: 1, tiers: { complex: [{ id: "custom-model", recommended: true, name: "My model" }], daily: [], simple: [] } };
    await page.reload(); await choose.click();
    assert.equal(await picker.locator('[data-kind="complex"] [data-model="custom-model"]').count(), 1);
    assert.equal(await picker.locator('[data-model="gpt-custom"]').count(), 0, "a reload reads the edited configuration");
    await picker.locator('button[data-category="complex"]').click();
    assert.equal(await picker.locator('[data-model="custom-model"]').getAttribute("aria-pressed"), "true");
    assert.deepEqual(w.posts, [], "reloading recommendation config never writes provider choices");
  });
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    test(`${engine} ${lang}: DeepSeek recommendations display and select dated model IDs`, async (t) => {
      const { context, page } = await newPage(engine, { reducedMotion: "reduce" });
      t.after(() => closePage(context));
      const w = fixture(lang, "light"); w.connected = true;
      w.models = ["deepseek-v4-pro", "deepseek-v4-pro-0813", "deepseek-v4.1-flash", "deepseek-v4.1-flash-1017"];
      await page.route("**/*", w.serve);
      await page.goto("http://jellytool.fixture/?view=quickstart");
      await page.locator('#quickstart [data-step="models"] .guide-go').click();
      const picker = page.locator(".qs-model-picker");
      for (const [tier, id] of [["complex", "deepseek-v4-pro-0813"], ["daily", "deepseek-v4.1-flash-1017"]]) {
        const card = picker.locator(`[data-kind="${tier}"] [data-model="${id}"]`);
        await card.click();
        assert.equal(await card.getAttribute("aria-pressed"), "true");
      }
      assert.equal(await picker.locator('[data-model="deepseek-v4-pro"], [data-model="deepseek-v4.1-flash"]').count(), 0);
      assert.deepEqual(w.posts, [], "selecting dated recommendations stays a draft");
    });
  }
}

// Saved protocols remain explicit; Restore default opts into Responses.
for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    test(`${engine} ${lang}: Responses default preserves saved protocol until Save`, async (t) => {
      const { context, page } = await newPage(engine, { viewport: { width: 1100, height: 850 } });
      t.after(() => closePage(context));
      const w = fixture(lang, "light"); w.connected = true; w.key = "fixture-key"; w.chosen = ["m1"];
      await page.route("**/*", w.serve);
      await page.goto("http://jellytool.fixture/?view=providers");
      await page.locator('#providers .row.provider[data-id="jellytoken"]').waitFor();
      await page.evaluate(() => openProvider("jellytoken"));
      const editor = page.locator("#modal .editor"), fields = editor.locator(".jelly-connection");
      await fields.locator('[data-api="openai"][aria-pressed="true"]').waitFor();
      const reset = await page.evaluate(() => t("Restore default"));
      await fields.getByRole("button", { name: reset, exact: true }).click();
      assert.equal(await fields.locator('[data-api="responses"]').getAttribute("aria-pressed"), "true");
      assert.equal(w.api, "openai", "the saved protocol is unchanged until Save");
      await editor.locator(".bar .primary").click();
      await page.locator("#modal").waitFor({ state: "hidden" });
      assert.equal(w.api, "responses");
      assert.equal(w.key, "fixture-key");
      assert.deepEqual(w.chosen, ["m1"]);
      assert(w.posts.some((p) => p.path === "/api/jellytool/config" && p.body.api === "responses"));
    });
  }
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    test(`${engine} ${lang}: optional image setup checks installation and guides agents`, async (t) => {
      const { context, page } = await newPage(engine, { viewport: { width: 420, height: 850 }, reducedMotion: "reduce" });
      t.after(() => closePage(context));
      const w = fixture(lang, "light"), errors = [], writes = [];
      page.on("pageerror", (e) => errors.push(e.message));
      const server = (name = "jellytool-image", agents = ["codex"]) => ({ name, transport: "stdio", command: "/Applications/Jellytool.app/Contents/MacOS/jellytool", args: ["mcp", "image"], agents });
      const lib = { agents: [{ id: "codex", name: "Codex", icon: "generic", mcp: "/fixture/config.toml" }], servers: [server()], foundServers: [], foundSkills: [], skills: [], projects: [], problems: [], instructions: { agents: [], sets: [] } };
      let reads = 0, failed = false;
      await page.route("**/*", w.serve);
      await page.route("**/api/library**", async (route) => {
        const u = new URL(route.request().url()), body = route.request().method() === "POST" ? route.request().postDataJSON() : null;
        if (u.pathname === "/api/library") { reads++; return route.fulfill({ status: failed ? 503 : 200, json: failed ? { error: "fixture library unavailable" } : lib }); }
        if (u.pathname === "/api/library/mcp/check") return route.fulfill({ json: { servers: {} } });
        if (u.pathname === "/api/library/market/servers") return route.fulfill({ json: { items: [{ id: "jellytool-image", name: "jellytool-image", title: "Jellytool Image", transport: "stdio", runs: "jellytool", featured: true, optIn: false, have: lib.servers[0]?.name || "" }] } });
        if (body) writes.push({ path: u.pathname, body });
        if (u.pathname === "/api/library/market/server") lib.servers = [server("jellytool-image", body.agents)];
        return route.fulfill({ json: body ? lib : { items: [] } });
      });
      await page.route("**/api/plugins/builtin", async (route) => {
        writes.push({ path: "/api/plugins/builtin", body: route.request().postDataJSON() });
        return route.fulfill({ json: [{ id: "library", name: "Library", on: true, chosen: true }] });
      });
      await page.goto("http://jellytool.fixture/?view=quickstart");
      const images = page.locator('#quickstart [data-step="images"]'), check = images.locator(".qs-image-check"), result = images.locator(".qs-image-result");
      await images.waitFor();
      assert.equal(await images.locator(".guide-number").textContent(), "5");
      assert.equal(await images.locator(".guide-title").textContent(), await page.evaluate(() => t("Image generation setup (optional)")));
      assert.equal(await images.locator(".qs-image-section").count(), 3, "image guidance is split into model, MCP and prompt sections");
      assert.deepEqual(await images.locator(".qs-image-section-title").allTextContents(), await page.evaluate(() => [t("Image model"), t("MCP tool"), t("Prompt examples")]));
      assert.equal(reads, 0, "optional setup does not read the library until requested");
      assert((await images.textContent()).includes("WorkBuddy"));
      assert((await images.textContent()).includes("generate_image"));
      assert((await images.textContent()).includes("generation_result"));
      assert((await images.textContent()).includes("assets/seaside.png"));
      const dimensions = await images.locator(".qs-image-size").textContent();
      for (const size of ["1:1", "16:9", "9:16", "1024x1024", "1536x1024", "1024x1536"]) assert(dimensions.includes(size), `size guidance includes ${size}`);
      const pixelExample = images.locator(".qs-image-example:not(.qs-image-model-example)").filter({ hasText: '"1536x1024"' });
      const copied = page.waitForRequest((r) => new URL(r.url()).pathname === "/api/copy");
      await pixelExample.locator(".copy").click();
      assert.equal((await copied).postDataJSON().text, await pixelExample.locator("code").textContent(), "copy preserves the requested pixel resolution");
      const modelHelp = images.locator(".qs-image-model-help"), modelExample = images.locator(".qs-image-model-example");
      assert.equal(await modelHelp.count(), 1);
      assert((await modelHelp.textContent()).includes("gpt-image-2.5-flare"));
      assert((await modelHelp.textContent()).includes("generate_image"));
      const modelCopied = page.waitForRequest((r) => new URL(r.url()).pathname === "/api/copy");
      await modelExample.locator(".copy").click();
      assert.equal((await modelCopied).postDataJSON().text, await modelExample.locator("code").textContent(), "copy preserves the explicit-model instructions");
      assert((await modelExample.textContent()).includes("gpt-image-2.5-flare"));
      assert.equal(await images.locator(".qs-image-example").evaluateAll((es) => es.every((e) => /MCP/i.test(e.textContent))), true, "copyable prompts identify jellytool-image as an MCP tool");
      await check.click();
      await result.locator(".qs-image-agents").waitFor();
      assert((await result.textContent()).includes("Codex"));
      assert.deepEqual(writes, [], "checking an installed server never changes configuration");
      await result.locator(".qs-image-agents").click();
      await page.locator('#view-library .lib-server').waitFor();
      assert.equal(await page.locator("#modal .mk-sheet").count(), 0, "an installed server opens its existing configuration");
      await page.evaluate(() => show("quickstart"));
      lib.servers = [{ ...server("magpie-image", []), command: "C:\\Tools\\magpie-windows-amd64.exe" }];
      await check.click();
      await page.waitForFunction(() => document.querySelector(".qs-image-result")?.textContent.includes("magpie-image"));
      assert.deepEqual(writes, [], "a legacy installation is kept without duplicating it");
      lib.servers = [{ ...server(), problems: { codex: "fixture write refused" } }];
      await check.click();
      await result.locator(".err").waitFor();
      assert(!(await result.textContent()).includes("Codex"), "a failed write is not reported as configured");
      lib.servers = []; lib.foundServers = [{ server: server("magpie-image") }];
      await check.click();
      await page.waitForFunction(() => document.querySelector(".qs-image-result")?.textContent.includes("magpie-image"));
      assert.equal(await result.locator(".qs-image-agents").textContent(), await page.evaluate(() => t("Open the Library")));
      failed = true; await check.click();
      await result.locator(".err").waitFor();
      assert.equal(await result.locator(".qs-image-agents").count(), 0, "a failed read offers no installation based on unknown state");
      failed = false; lib.foundServers = [];
      await page.evaluate(() => featuresOff.add("library"));
      await check.click(); await result.locator(".qs-image-enable").click();
      await result.locator(".qs-image-agents").waitFor();
      assert.deepEqual(writes, [{ path: "/api/plugins/builtin", body: { id: "library", on: true } }]);
      for (const width of [420, 1100]) { await page.setViewportSize({ width, height: 850 }); await readable(page); }
      if (process.env.ARTIFACT_DIR && lang === "zh") {
        await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
        await images.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-optional-images.png`) });
      }
      await result.locator(".qs-image-agents").click();
      const sheet = page.locator("#modal .mk-sheet"); await sheet.waitFor();
      assert.equal(await sheet.locator('.lib-ag[data-agent="codex"]').count(), 1);
      await sheet.locator('.lib-ag[data-agent="codex"]').click();
      await sheet.locator('.lib-ag[data-agent="codex"]').click();
      await sheet.locator(".bar .primary").click();
      await sheet.waitFor({ state: "hidden" });
      assert.deepEqual(writes.at(-1), { path: "/api/library/market/server", body: { id: "jellytool-image", values: {}, agents: ["codex"] } });
      const prefs = { lang, theme: "light", redactWords: [], workbuddyCheckins: [], lanURLs: [], visionModels: [], imageGen: "off", imageGenModels: [{ id: "jellytoken/image", name: "Fixture Image", provider: "jellytoken", providerName: "Jellytoken" }], fx: { rate: 7.2 }, builtins: [] };
      await page.route("**/api/settings", (route) => {
        if (route.request().method() === "POST") { const body = route.request().postDataJSON(); writes.push({ path: "/api/settings", body }); Object.assign(prefs, body); }
        return route.fulfill({ json: prefs });
      });
      await page.evaluate(() => show("quickstart"));
      await images.locator(".qs-image-model").click();
      await page.locator('#setPage-models:not([hidden]) #imageGenRow').waitFor();
      await page.locator("#imageGenRow .rt-cond").click();
      await page.locator('#pop #list').getByText("Fixture Image", { exact: true }).click();
      await page.waitForFunction(() => state.settings?.imageGen === "jellytoken/image");
      assert.equal(writes.at(-1).body.imageGen, "jellytoken/image", "the existing model setting enables image generation");
      assert.deepEqual(errors, []);
    });
  }
}
