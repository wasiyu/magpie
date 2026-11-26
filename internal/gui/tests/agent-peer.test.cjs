// Run with Node's test runner and Playwright on the module path; see README.md.
// Jellytool beside magpie (the owner, 11-26: Codex said "config changed"
// for good, and its subscription's models were gone from the Codex app).
// magpie holding Codex reads as such: not connected, the row names magpie
// and that connecting moves it back; connected as Codex's provider while
// magpie holds the ChatGPT sign-in, it is drift of its own (peer) with
// Reconnect, not "config changed". The peer's name stays magpie in
// Jellytool's window, where every other magpie reads Jellytool. In every
// language, a wide window and a narrow one, Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const options = [{ value: "fake/m1", label: "m1", ref: "fake/m1" }];
const fields = () => [{ key: "model", label: "model", value: "fake/m1", options }];
const agents = () => [
  { id: "codex", name: "Codex", icon: "generic", path: "/fixture/codex", peer: true, fields: fields() },
  {
    id: "codex@wsl:Ubuntu-24.04", name: "Codex", icon: "generic", path: "/fixture/wsl-codex", wired: true, fields: fields(),
    drift: { kind: "peer", field: "model", now: "fake/m1", want: "fake/m1" },
  },
];

function server(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    const st = { agents: agents(), profiles: [], settings: { lang, theme: "light", guideHidden: true } };
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true,jellytool:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(st);
    if (url.pathname === "/api/drift") return json({ "codex@wsl:Ubuntu-24.04": st.agents[1].drift });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const off = "Not connected · {agent} is on {peer} now · connecting moves it back";
const held = "{peer} holds {agent}'s ChatGPT sign-in now, so its subscription models are missing · Reconnect takes it back";
const row = (id) => `.row.agent[data-id="${id}"]`;

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [980, 420]) {
      test(`${engine} ${lang} ${width}px: Codex held by magpie says so, and Reconnect where it is drift`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await (await browser.newContext({ viewport: { width, height: 640 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang));
        await page.goto("http://magpie.test/?view=agents");
        const tr = (k) => page.evaluate(([k, lang]) => (lang === "en" ? k : I18N[lang]?.[k]), [k, lang]);
        const fill = (s) => s.replaceAll("{agent}", "Codex").replaceAll("{peer}", "magpie");
        for (const k of [off, held]) assert.ok(await tr(k), `${lang} has ${k.slice(0, 40)}`);
        const first = page.locator(`${row("codex")} .ag-st`);
        await first.waitFor();
        assert.equal((await first.textContent()).trim(), fill(await tr(off)));
        const second = page.locator(`${row("codex@wsl:Ubuntu-24.04")} .ag-st`);
        await page.waitForFunction((sel) => document.querySelector(sel)?.textContent.includes("magpie"), `${row("codex@wsl:Ubuntu-24.04")} .ag-st`);
        assert.ok((await second.textContent()).startsWith(fill(await tr(held))), await second.textContent());
        assert.equal(await second.locator("button").count(), 1, "one Reconnect");
        const wide = await page.evaluate(() => document.scrollingElement.scrollWidth - document.scrollingElement.clientWidth);
        assert.ok(wide <= 0, `the page scrolls sideways by ${wide}px`);
        assert.deepEqual(errors, []);
      });
    }
  }
}
