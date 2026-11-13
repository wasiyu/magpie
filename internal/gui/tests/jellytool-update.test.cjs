// Jellytool reuses magpie's update UI, with a browser download instead of an
// install/restart. Every request is a fixture; no release service is contacted.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { reach, settled, watchSettingsReads, settingsDrawn } = require("./reader.cjs");
const assets = path.resolve(process.env.JELLYTOOL_UPDATE_ASSETS || path.join(__dirname, "../assets"));
const downloadURL = "https://downloads.example/jellytool/0.2.0";

function server(lang, ctl) {
  const settings = () => ({ lang, theme: "light", version: "0.1.0", guideHidden: true, updateEvery: 1440, updateSkip: ctl.skip, dir: "/fixture/jellytool", gateway: "http://127.0.0.1:3435" });
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    ctl.paths.push(url.pathname);
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = ${JSON.stringify({ jellytool: true, lang, theme: "light", web: false })};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], served: 0, settings: settings() });
    if (url.pathname === "/api/settings") return json(settings());
    if (url.pathname === "/api/settings/update-skip") { ctl.skip = req.postDataJSON().version; return json(settings()); }
    if (url.pathname === "/api/update") return json(ctl.update);
    if (url.pathname === "/api/update/check") { ctl.checks++; return json(ctl.update); }
    if (url.pathname === "/api/update/notes") return json({ latest: ctl.update.latest, releases: [{ version: ctl.update.latest, notes: ctl.update.notes }] });
    if (url.pathname === "/api/update/install") { ctl.downloads++; return json(ctl.update); }
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    let body;
    try { body = await fs.readFile(file); } catch { return route.fulfill({ status: 404, body: "" }); }
    if (url.pathname === "/") body = Buffer.from(body.toString().replace('<script src="start.js">', '<script src="jellytool.js"></script><script src="start.js">'));
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    return route.fulfill({ body, contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: Jellytool offers downloads without self-installing`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) for (const width of [420, 1100]) {
      await t.test(`${lang} ${width}px`, async () => {
        const ctl = { paths: [], checks: 0, downloads: 0, skip: "", update: { manual: true, state: "", current: "0.1.0", latest: "0.2.0", url: downloadURL, notes: "- Jellytool fixture release" } };
        const context = await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" });
        try {
          const page = await context.newPage(), errors = [];
          await page.clock.install();
          page.setDefaultTimeout(8000);
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", server(lang, ctl));
          await watchSettingsReads(page);
          await page.goto("http://jellytool.test/?view=settings&tab=about");
          await settingsDrawn(page);
          const tr = (key) => page.evaluate((k) => t(k), key);
          const badge = page.locator("#update");
          assert.equal(await badge.isHidden(), true);
          ctl.update.state = "available";
          await page.clock.fastForward(30_000);
          await badge.waitFor({ state: "visible" });
          assert.equal(await badge.locator("span").first().innerText(), await tr("Download update"));
          assert.equal(await page.locator(".update-channel-row, .update-download-row, .update-restart-row, .update-mirror-row, .update-every-row, .whatsnew-row").count(), 0);
          assert((await page.locator(".update-auto-row").innerText()).includes(await tr("Check for updates automatically")));
          assert(!ctl.paths.includes("/api/whatsnew"), "no magpie release-history request");

          await badge.click();
          const dialog = page.locator(".update-ask");
          await dialog.waitFor();
          assert((await dialog.innerText()).includes("Jellytool fixture release"));
          assert.equal(await dialog.getByRole("button", { name: await tr("Restart to update"), exact: true }).count(), 0);
          if (process.env.ARTIFACT_DIR && lang === "zh") {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await settled(page);
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${width}-update.png`) });
          }
          await dialog.getByRole("button", { name: await tr("Download"), exact: true }).click();
          assert.equal(ctl.downloads, 1, "Download asks the backend to open the browser");

          await badge.hover();
          await page.locator("#updateHide").click();
          await badge.waitFor({ state: "hidden" });
          assert.equal(ctl.skip, "0.2.0");
          await page.reload(); await settingsDrawn(page);
          assert.equal(await badge.isHidden(), true, "skip survives reload");
          ctl.update.latest = "0.3.0";
          await page.evaluate(() => renderUpdateBadge());
          await badge.waitFor({ state: "visible" });

          const version = page.locator("#about .row.pref").first();
          const check = version.getByRole("button", { name: await tr("Check"), exact: true });
          await reach(page, check); await check.click();
          assert.equal(ctl.checks, 1);
          ctl.update = { ...ctl.update, state: "error", error: "offline" };
          await check.click();
          await page.waitForFunction(() => document.querySelector("#about .row.pref .sub")?.textContent.includes("offline"));
          assert((await version.innerText()).includes(await tr("Couldn't check for updates")), "failed check is not reported as a failed download");

          ctl.update = { manual: true, state: "unconfigured", current: "dev" };
          await page.evaluate(() => renderUpdate(document.querySelector("#about .row.pref")));
          assert((await version.innerText()).includes(await tr("Update checks are not configured for this build.")));
          assert.equal(await version.locator("button").count(), 0);
          assert.equal(ctl.downloads, 1);
          assert(!ctl.paths.includes("/api/window/quit"));
          assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
          assert.deepEqual(errors, []);
        } finally { await context.close(); }
      });
    }
  });
}
