// Run with Node's test runner and Playwright on the module path; see README.md.
// The window's Get started page keeps its steps and actions after completion. Unconfigured
// launches open it first; a completed launch keeps the user's last page. The
// compact panel still hides its card after completion or explicit dismissal.
// Renzexi01 on X (一进来就这么复杂): while the panel's card shows, the agents'
// list is folded under it, since its last step names the ones found here.
// Both engines, wide and narrow windows and the panel, every language; no backend.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = process.env.MAGPIE_GUIDE_ASSETS || path.resolve(__dirname, "../assets");
const shots = process.env.ARTIFACT_DIR;

// world is what the fake server knows, changed by the page's writes
function newWorld() {
  return { provider: false, served: 0, wired: false, hidden: false };
}

function agents(w) {
  // with no model served, the agents' pickers have none of magpie's
  const mine = w.served ? [{ value: "relay/m1", label: "m1", ref: "relay/m1", note: "Relay · via magpie" }] : [];
  const field = (value, own) => [{ key: "model", label: "model", value, options: [...own, ...mine] }];
  return [
    { id: "codex", name: "Codex", icon: "generic", path: "/fixture/codex", wired: w.wired, fields: field(w.wired ? "relay/m1" : "", [{ value: "gpt-5.4", label: "GPT-5.4" }]) },
    { id: "claude", name: "Claude Code", icon: "generic", path: "/fixture/claude", fields: field("", [{ value: "sonnet", label: "Sonnet" }]) },
    { id: "opencode", name: "OpenCode", icon: "generic", path: "/fixture/opencode", fields: field("", []) },
    // never connectable: not offered on the card
    { id: "cursor", name: "Cursor", icon: "generic", path: "/fixture/cursor", fields: [] },
    // its folder left behind with the CLI uninstalled (#2173): not offered
    { id: "hermes", name: "Hermes Agent", icon: "generic", path: "/fixture/hermes", cliMissing: true, fields: field("", [{ value: "hermes-own", label: "Own" }]) },
  ];
}

function server(lang, w, posts) {
  const settings = () => ({ lang, theme: "light", guideHidden: w.hidden || undefined });
  const state = () => ({ agents: agents(w), profiles: [], served: w.served, settings: settings() });
  const provider = () => ({ id: "relay", name: "Relay", icon: "generic", host: "fixture.invalid", chat: "https://fixture.invalid/v1/chat/completions", ready: true,
    models: [{ id: "m1", name: "m1", on: !!w.served }], chosen: w.served ? ["m1"] : [], noModels: !w.served,
    key: { set: true, masked: "fixture" }, keyList: [], agents: [], fallback: [] });
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data, status = 200) => route.fulfill({ status, json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (req.method() === "POST") posts.push(url.pathname + url.search);
    if (url.pathname === "/api/state") return json(state());
    if (url.pathname === "/api/settings") return json(settings());
    if (url.pathname === "/api/settings/guide") {
      w.hidden = !!req.postDataJSON().hidden;
      return json(settings());
    }
    if (url.pathname === "/api/agents/connect/codex") {
      w.wired = true;
      return json(state());
    }
    if (url.pathname === "/api/groups") return json({ groups: [], models: [], pools: [], deciders: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
    if (url.pathname === "/api/providers") return json({ providers: w.provider ? [provider()] : [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const LAYOUTS = [
  { name: "window", url: "/", viewport: { width: 1100, height: 760 } },
  { name: "narrow", url: "/", viewport: { width: 420, height: 760 } },
  { name: "panel", url: "/?mode=panel", viewport: { width: 400, height: 640 } },
];
const LANGS = ["en", "zh", "zh-TW", "ja", "de"];
const STRINGS = ["Get started", "Connect a provider", "Model selection", "Choose models", "Connect an agent", "Hide Get started",
  "A subscription you already pay for, a provider, or an API key", "magpie found these on this computer. Click one to connect it:",
  "Open a provider, choose the models to share, then Save.", "+{n} more below", "{n} of {m} done", "{n} models ready to use", "Open Providers", "Open Agents"];

async function open(browser, layout, lang, w, posts) {
  const context = await browser.newContext({ viewport: layout.viewport, reducedMotion: "reduce" });
  await context.addInitScript(() => { try { localStorage.setItem("magpie.panelTab", "agents"); } catch {} });
  const page = await context.newPage();
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  page.errors = [];
  page.on("pageerror", (e) => page.errors.push(e.message));
  await page.route("**/*", server(lang, w, posts));
  await page.goto("http://magpie.test" + layout.url);
  await page.waitForFunction(() => load.done);
  return { context, page };
}

// the agents' rows in view, not folded under the list's Show more
const inView = (page) => page.locator(".row.agent").evaluateAll((rs) => rs.filter((r) => !r.closest(".agent-fold")).map((r) => r.dataset.id || r.id));
const folded = (page) => page.locator(".agent-fold .row.agent").count();
const cardFor = (page, layout) => page.locator(layout.name === "panel" ? "#agentsLead .guide" : "#quickstart .guide");
const stepDone = (page, step) => page.locator(`.guide-step[data-step="${step}"]`).evaluate((e) => e.classList.contains("done"));
const tops = (page) => page.evaluate(() => [document.scrollingElement.scrollTop, document.querySelector("main.view:not([hidden])")?.scrollTop]);
const sideways = (page) => page.evaluate(() => {
  const v = document.querySelector("main.view:not([hidden])"), g = v.querySelector(".guide");
  return v.scrollWidth > v.clientWidth + 1 || document.scrollingElement.scrollWidth > innerWidth
    || (g && [...g.querySelectorAll("*")].some((e) => e.getBoundingClientRect().right > g.getBoundingClientRect().right + 1));
});

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of LANGS) {
    for (const layout of LAYOUTS) {
      test(`${engine} ${lang} ${layout.name}: Get started updates its steps and preserves completed window guidance`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const w = newWorld(), posts = [];
        const { page } = await open(browser, layout, lang, w, posts);
        const tr = (s, v) => page.evaluate(([s, v]) => t(s, v), [s, v]);
        if (lang !== "en") {
          for (const s of STRINGS) assert.notEqual(await tr(s), s, `${lang} has "${s}"`);
        }

        // a new user: nothing served, nothing wired
        const card = cardFor(page, layout);
        await card.waitFor();
        if (layout.name !== "panel") {
          assert.equal(await page.locator("#agentsLead .guide").count(), 0, "guidance lives on its own page");
          assert.equal(await page.locator('.guide-x').count(), 0, "the permanent page has no dismiss control");
          assert.equal(await page.locator('#nav [data-view="quickstart"]').textContent(), await tr("Get started"));
        }
        assert.equal((await page.locator(".guide-h").textContent()), await tr("Get started"));
        assert.equal(await stepDone(page, "source"), false);
        assert.equal(await stepDone(page, "models"), false);
        assert.equal(await stepDone(page, "agent"), false);
        assert.deepEqual(await card.locator(".guide-step").evaluateAll((es) => es.map((e) => e.dataset.step)), ["source", "models", "agent"], "only provider, model selection and agent steps, in order");
        assert.deepEqual(await card.locator(".guide-title").allTextContents(), await Promise.all(["Connect a provider", "Model selection", "Connect an agent"].map((s) => tr(s))));
        assert.equal(await page.locator(".guide-n").textContent(), await tr("{n} of {m} done", { n: 0, m: 3 }));
        // the agents found here are named, Cursor (never connectable) not;
        // with no models yet they can't be connected
        assert.deepEqual(await page.locator(".guide-agent").evaluateAll((bs) => bs.map((b) => b.dataset.id)), ["codex", "claude", "opencode"]);
        assert.ok(await page.locator(".guide-agent").evaluateAll((bs) => bs.every((b) => b.disabled)));
        assert.equal(await card.locator('[data-step="routing"], .guide-tpl').count(), 0, "routing is absent from guidance");
        if (layout.name === "panel") {
          // the card alone, the list folded under it
          assert.deepEqual(await inView(page), [], "no agent row in view under Get started");
          assert.equal(await folded(page), 5);
        }
        assert.equal(await sideways(page), false, "nothing runs off sideways");
        if (shots && (lang === "en" || lang === "zh")) await page.screenshot({ path: path.join(shots, `getting-started-${engine}-${lang}-${layout.name}-new.png`) });

        // the first step's button: the window opens the add sheet, the
        // panel the window on Providers
        const top = await tops(page);
        await page.locator('.guide-step[data-step="source"] .guide-go').click();
        if (layout.name === "panel") {
          for (let i = 0; i < 50 && !posts.includes("/api/window/main?view=providers"); i++) await page.waitForTimeout(20);
          assert.ok(posts.includes("/api/window/main?view=providers"), posts.join(" "));
        } else {
          await page.locator("#view-providers").waitFor({ state: "visible" });
          await page.locator("#addSheet").waitFor({ state: "visible" });
        }

        // Credentials alone complete only step 1, not model selection.
        w.provider = true;
        await page.goto("http://magpie.test" + layout.url);
        await card.locator('[data-step="source"].done').waitFor();
        assert.equal(await stepDone(page, "models"), false);
        assert.equal(await page.locator(".guide-n").textContent(), await tr("{n} of {m} done", { n: 1, m: 3 }));
        assert.equal(await page.locator('.guide-agent[data-id="codex"]').isDisabled(), true);
        await card.locator('[data-step="models"] .guide-go').click();
        if (layout.name === "panel") assert.ok(posts.includes("/api/window/main?view=providers"));
        else {
          await page.locator("#view-providers").waitFor({ state: "visible" });
          await page.locator("#modal .editor").waitFor({ state: "visible" });
        }

        // Saving a model selection completes step 2 and enables connection.
        w.served = 12;
        await page.goto("http://magpie.test" + layout.url);
        await card.waitFor();
        assert.equal(await stepDone(page, "source"), true);
        assert.equal(await stepDone(page, "models"), true);
        assert.match(await page.locator('.guide-step[data-step="models"] .guide-say').textContent(), /12/);
        assert.equal(await page.locator(".guide-n").textContent(), await tr("{n} of {m} done", { n: 2, m: 3 }));
        const codex = page.locator('.guide-agent[data-id="codex"]');
        assert.equal(await codex.isDisabled(), false);
        if (shots && (lang === "en" || lang === "zh")) await page.screenshot({ path: path.join(shots, `getting-started-${engine}-${lang}-${layout.name}-source.png`) });

        const before = await tops(page);
        // Connecting completes all three required steps. The window keeps every
        // completed step and its actions; only the compact panel's optional card goes away.
        await codex.click();
        if (layout.name === "panel") await card.waitFor({ state: "detached" });
        else {
          await page.locator('#quickstart .guide-step[data-step="agent"].done').waitFor();
          assert.equal(await card.locator(".guide-step").count(), 3);
          assert.equal(await card.locator(".guide-step.done .guide-tick svg").count(), 3);
          assert.equal(await card.locator('.guide-step[data-step="agent"] .guide-say').textContent(), "Codex");
          assert.equal(await page.evaluate(() => view), "quickstart", "completion leaves the reader on the guide");
          assert.equal(await card.locator(".guide-n").textContent(), await tr("{n} of {m} done", { n: 3, m: 3 }));
          assert.equal(await sideways(page), false);
          if (shots && (lang === "en" || lang === "zh")) await page.screenshot({ path: path.join(shots, `getting-started-${engine}-${lang}-${layout.name}-complete.png`) });
        }
        assert.ok(posts.includes("/api/agents/connect/codex"));
        assert.deepEqual(await tops(page), before, "no click moved the page");
        assert.equal(top[0], before[0]);

        // A configured launch restores the last page. The guide can still be
        // opened and used to configure each step again.
        if (layout.name !== "panel") await page.locator('#nav [data-view="agents"]').click();
        await page.goto("http://magpie.test" + layout.url);
        await page.waitForFunction(() => load.done);
        await page.locator(".row.agent").first().waitFor();
          if (layout.name === "panel") {
            assert.equal(await card.count(), 0);
            // the agent connected is in view, the rest folded as for anyone
            assert.equal(await folded(page), 4);
          } else {
          await page.locator('#nav [data-view="quickstart"]').click();
          await card.waitFor();
          assert.equal(await card.locator(".guide-step.done .guide-tick svg").count(), 3);
          await page.reload();
          await card.waitFor();
          assert.equal(await card.locator(".guide-step").count(), 3, "a completed guide survives reload");
          // Completed steps still lead to the full configuration pages. Opening
          // them must not reconnect an agent or change the saved model selection.
          for (const [step, target, label] of [["source", "providers", "Open Providers"], ["models", "providers", "Choose models"], ["agent", "agents", "Open Agents"]]) {
            const action = card.locator(`[data-step="${step}"]`).getByRole("button", { name: await tr(label), exact: true });
            assert.equal(await action.count(), 1, `${step} keeps a configuration action after completion`);
            assert.equal(await action.textContent(), await tr(label));
            assert.equal(await action.isEnabled(), true);
            await action.click();
            await page.locator(`#view-${target}`).waitFor({ state: "visible" });
            assert.equal(await page.evaluate(() => view), target);
            if (step === "models") {
              await page.locator("#modal .editor").waitFor({ state: "visible" });
              await page.keyboard.press("Escape");
              await page.locator("#modal").waitFor({ state: "hidden" });
            }
            await page.locator('#nav [data-view="quickstart"]').click();
            await card.waitFor();
            assert.equal(await card.locator(".guide-step.done .guide-tick svg").count(), 3);
          }
          assert.equal(posts.filter((p) => p === "/api/agents/connect/codex").length, 1, "navigation does not reconnect the agent");
          assert.equal(posts.filter((p) => p === "/api/provider/save" || p === "/api/groups/template").length, 0, "navigation changes no model selection or routing group");
        }
        assert.deepEqual(page.errors, []);
      });

      test(`${engine} ${lang} ${layout.name}: panel dismissal keeps its preference; the window keeps all steps`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const w = newWorld(), posts = [];
        if (layout.name !== "panel") w.hidden = true;
        const first = await open(browser, layout, lang, w, posts);
        const page = first.page;
        const card = cardFor(page, layout);
        await card.waitFor();
        if (layout.name !== "panel") {
          assert.equal(await page.evaluate(() => view), "quickstart", "unconfigured launch opens guidance even after panel dismissal");
          assert.equal(await card.locator(".guide-step").count(), 3);
          assert.equal(await card.locator(".guide-x").count(), 0);
          await page.locator('#nav [data-view="agents"]').click();
          assert.equal(await page.locator("#agentsLead .guide").count(), 0);
          // Focus refresh must not send the user back while configuring.
          await page.evaluate(() => load(true));
          assert.equal(await page.evaluate(() => view), "agents");
          await page.reload();
          await card.waitFor();
          assert.equal(await page.evaluate(() => view), "quickstart", "each unconfigured launch returns to guidance");
          assert.equal(posts.includes("/api/settings/guide"), false, "reading the guide writes no setting");
          // Explicit navigation (an import or a tray link) keeps its target.
          await page.goto("http://magpie.test/?view=agents");
          await page.waitForFunction(() => load.done);
          await page.locator(".row.agent").first().waitFor();
          assert.equal(await page.evaluate(() => view), "agents");
          // A connected agent does not finish setup after all models are
          // deselected: step 2 is required on the next normal launch too.
          w.provider = true;
          w.wired = true;
          await page.goto("http://magpie.test/");
          await card.locator('[data-step="source"].done').waitFor();
          assert.equal(await page.evaluate(() => view), "quickstart");
          assert.equal(await stepDone(page, "models"), false);
          assert.equal(await stepDone(page, "agent"), true);
          assert.equal(await card.locator(".guide-n").textContent(), await page.evaluate(() => t("{n} of {m} done", { n: 2, m: 3 })));
          assert.deepEqual(page.errors, []);
          return;
        }
        const top = await tops(page);
        await page.locator(".guide-x").click();
        await page.locator(".guide").waitFor({ state: "detached" });
        assert.ok(posts.includes("/api/settings/guide"));
        assert.equal(w.hidden, true);
        assert.equal(await page.locator("#status").textContent(), await page.evaluate(() => t("Get started hidden; Settings › General brings it back")));
        // put away, the list is in view as it was before there was a card
        assert.equal(await folded(page), 0);
        assert.deepEqual(await tops(page), top, "no click moved the page");
        assert.deepEqual(page.errors, []);
        await first.context.close();

        // a fresh browser storage (the other window, a reinstall's webview):
        // still away, though no step is done
        const again = await open(browser, layout, lang, w, posts);
        await again.page.waitForTimeout(150);
        assert.equal(await again.page.locator(".guide").count(), 0);
        assert.equal(await again.page.evaluate(() => { try { return Object.keys(localStorage).filter((k) => /guide/i.test(k)).length; } catch { return 0; } }), 0, "nothing kept in localStorage");
        assert.deepEqual(again.page.errors, []);
        if (layout.name === "panel") return;

        // and brought back from Settings › General, where its hide said:
        // a row there only while it is away and would show
        const pg = again.page, tr = (s) => pg.evaluate((s) => t(s), s);
        await pg.goto("http://magpie.test/?view=settings&tab=general");
        const back = pg.locator("#guideBackRow");
        await back.locator("button").waitFor();
        assert.equal(await back.locator(".name").textContent(), await tr("Get started"));
        // Settings' search finds it by its name
        await pg.locator("#setQ").fill((await tr("Get started")).toLowerCase());
        await pg.waitForTimeout(150);
        assert.ok(await back.isVisible(), "the search finds the row");
        await pg.locator("#setQ").fill("");
        const before = await pg.locator("#view-settings").evaluate((v) => v.scrollTop);
        await back.locator("button").click();
        await back.waitFor({ state: "hidden" });
        assert.equal(w.hidden, false);
        assert.equal(await pg.locator("#status").textContent(), await tr("Get started is back on the Agents page"));
        assert.equal(await pg.locator("#view-settings").evaluate((v) => v.scrollTop), before, "no click moved the page");
        await pg.goto("http://magpie.test" + layout.url);
        await pg.locator("#agentsLead .guide").waitFor();
        assert.deepEqual(pg.errors, []);
      });
    }
  }
}
