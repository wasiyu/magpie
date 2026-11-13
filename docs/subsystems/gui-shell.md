# GUI app shell

The desktop app has a tray (menu bar) icon whose click drops a compact
panel, and a regular window. Both show one small web UI served from
embedded assets. The page talks to Go over a JSON API on the same handler,
with no binding generator and no bundler. `magpie web` serves the same page
to a browser for a machine with no desktop. Package `internal/gui` hosts all
three.

## Responsibilities and sources of truth

| Part | Responsibility | Source |
| --- | --- | --- |
| Native host | `Run` starts the Wails v3 app: the tray icon, the panel, the main window, single-instance handover, `magpie://` import links and open at login. `host` implements `Windows` for the API: show and hide, open URLs and folders, copy to the clipboard, fit and tint the panel, and set the text size. A copy on Linux sets PRIMARY before CLIPBOARD, since GTK 3 serves CLIPBOARD empty when PRIMARY is set after it on a compositor with no primary selection (KDE Plasma with middle-click paste off, #1881); on Wayland the copy is read back through the compositor, and one that doesn't read back whole answers `/api/copy` with 501, so the page tries its clipboard API, then the copy command, then says the copy failed with the text to copy by hand (`copy-fallback.test.cjs`). | [`app.go`](../../internal/gui/app.go), [`clipboard_gtk3.go`](../../internal/gui/clipboard_gtk3.go), [`clipboard_gtk3.h`](../../internal/gui/clipboard_gtk3.h), [`mainwindow.go`](../../internal/gui/mainwindow.go), [`traymenu.go`](../../internal/gui/traymenu.go) |
| Backend | `startBackend` starts the gateway this process serves, unless another magpie already serves it. In that case this process only shows the other one's status (`gw` is nil). `stopServing` drains requests in flight. A restart to update can hand the listening socket to the new version instead (see [A restart to update hands the gateway over](#a-restart-to-update-hands-the-gateway-over)). | [`backend.go`](../../internal/gui/backend.go) |
| API and assets | `Handler` serves `assets/` (embedded with `//go:embed assets`) and the `/api/*` routes, such as `/api/state`, `/api/set`, `/api/agents/{action}/{id}` and `/api/agents/restart/{id}` (a desktop app's Restart, after the row's confirm). `held` lets one API request's reads share one catalog build (`provider.Hold`), and drops it after any write. `revalidated` and `versionedPage` name each script by its content hash, so a cache never serves an old `app.js` under a new page; a file asked for under its current hash (`?v=`) is cached for good (`immutable`), anything else is revalidated by its ETag. | [`api.go`](../../internal/gui/api.go) |
| Browser mode | `StartWeb` serves the page and the gateway for `magpie web`. The browser signs in with a run key in a link. Added to a phone's home screen the page opens as an app of its own, with no browser bars (ninja_s6114 on Discord): `index.html` names `manifest.json` (display standalone), an apple-touch-icon and the iOS meta tags, the status bar keeps its own ground (`default`), and its theme colours match only `(display-mode: standalone)`, so a browser's tab and the app's windows take none. `webGuard` serves those three files (`webAppFiles`: the manifest, `icons/apple-touch-icon.png`, `icons/icon-512.png`) without the key, to GET and HEAD at those exact paths alone, as a home screen may ask without the cookie; see `TestWebAppAddsToHomeScreen` and `web-app-standalone.test.cjs`. Gateway mode shows Providers, Gateway, Routing, Usage, Sessions, Plugins and Settings on a machine that is only a gateway. `webHandler` gzips the page's text files and the API's JSON for a browser that takes gzip (`webCompressed`, with a `-gzip` ETag that still answers 304); the app's own windows read the page in-process and get it uncompressed. The page's port answers the gateway's client routes too (`onePort`, `gateway.ClientPath`: `/v1/…`, `/v1beta/…`, `/mcp/…`, the Codex backend path and the routes without `/v1`; never `/api/*` or `/_magpie/*`, never a path that isn't clean), so one reverse-proxy location serves the page and the agents (zgcwkj on Discord). They skip the page's key and gzip and go to the gateway's own guards, always as a request from another machine (`gateway.FromPagePort`, the `X-Magpie-Page-Port` mark: `local` is false for it, from loopback too and under `MAGPIE_TRUST_PROXY`, so magpie must be shared and the request carry an enabled gateway key), since a raw TCP forward or a proxy that adds no forwarding header reaches this port from loopback looking like an agent here (reported privately by Coffemon); agents here use the gateway's own port. `Server.Front` (lanGuard over Handler) serves them in process when this process serves the gateway, or `toGateway`, a reverse proxy to `gateway.URL()`, when another magpie does; it keeps the Host, the forwarding headers and the mark, adds X-Forwarded-For for a peer that isn't loopback, and the peer's loopback address as X-Forwarded-For when none came, so a gateway of an older magpie that doesn't know the mark still asks for a key. `lanGuard` moves the mark into the request's context and takes the header off, so it never goes upstream. The page itself calls no client route. See `TestWebOnePortServedHere`, `TestWebOnePortServedElsewhere`, `TestWebOnePortAgentsHereUseTheGatewayPort`, `TestPagePortMarkIsNeverLocal` and `TestClientPathCoversHandler`. | [`web.go`](../../internal/gui/web.go), [`gatewaymode.go`](../../internal/gui/gatewaymode.go), [`oneport.go`](../../internal/gateway/oneport.go) |
| The app's page in a browser | Settings › Network and sharing › Browser access (`renderWebPage` in `plugins/gateway/settings.js`, drawn with the Web pages rows) has the app serve its own page to a browser while it runs, so a tunnel (Cloudflare Tunnel, SSH) or a computer on the network can reach it without `magpie web` kept running in a terminal (jiakun_zhao on X). Off, This computer (`127.0.0.1`, which a tunnel's service points at) or Local network (every address; a "Not encrypted" row without `--tls-cert`). It is `magpie web`'s page and guard, not a second kind: `startAppPage` (from `Run`, after `startBackend`) serves `webServed` over a `Handler` of its own, whose `Windows` is `appWebHost` (a browser tab for `isWeb`, but not `ownWeb`, so the app's windows aren't taken for `magpie web`'s: `webPage` and gateway mode stay the app's; quitting quits the app, and a restart to update from it brings back the windows the app has up, `restartFrom`). The key is always asked for: no mode serves without it. It lives with the mode and port in `web-page.json` beside settings.json (0600, never in settings.json, so sync and backup don't carry it), is made the first time the page is turned on, kept across restarts (a browser's cookie stays good), and replaced by New key, which turns away every browser signed in with the old one (the guard is swapped atomically, `appPage.cur`). A file that can't be read, or names no known mode, is off and says why; Settings rewrites it only given a whole choice. A port another program holds (`magpie web`; the magpie before a restart to update) is said and tried again every 4 s (`appPageRetry`); the port must be 1024 up and not the gateway's. `GET/POST /api/web-page` are on the app's own windows' API only, never a browser's, so a browser on the page can neither read the key it came in without nor turn the page off under itself. `magpie web` on the port the app serves prints the app's link and opens it rather than starting a second (`gui.AppPage`, `samePort`). Sessions are warmed once a process however many `Handler`s there are (`warmOnce`). See `TestAppPageServesOnlyWithItsKey`, `TestAppPageWaitsForABusyPort`, `TestAppPageUnreadFileServesNothing`, `TestAppPageTurnsDownBadChoices`, `TestAppPageBrowserIsATab` and `browser-access.test.cjs`. | [`apppage.go`](../../internal/gui/apppage.go), [`web_cli.go`](../../internal/cli/web_cli.go) |
| The page | `app.js` keeps one state object per view and renders it into a list, with no framework. The views have their own files: `plugins.js` and `context.js`, and those a plugin adds under `assets/plugins/<id>/` (the Gateway, Sessions, Library, Routing and Usage pages, see [plugin runtime](plugin-runtime.md)) (Usage's Context tab, and the context window card the Routing page draws under a request's story, fed by `/api/context` in `context.go`, which scores and tags each agent; clicking a part, a cell or an item opens its text from `/api/context/text`, the last 10 requests' masked bodies in memory (`openCtxText`), rendered 100 items and 20000 characters at a time; its "Tuned for you" cards are fed by `/api/tune` in `tune.go`, see [agent wiring](agent-wiring.md)). Strings go through `t()` with translations in `i18n.js`, and a plugin's own in its `i18n.js` (zh, zh-TW, ja and de; the English text is the key and the fallback). zh-TW is Traditional Chinese with Taiwan's words; a system language of zh-TW, zh-HK, zh-MO or any zh-Hant picks it, and the tray menu and notifications follow (`trayLang`). Dropdowns use `openProtoMenu`. Every app menu (`openProtoMenu`, `openRowMenu`, `openSessCombo`) is placed by `placeMenu`: under its anchor, or over it when the window runs out below; one that fits neither way takes the larger room, its height held to it, and scrolls inside itself, and a scroll inside a menu doesn't close it (#1437, `removed-groups-menu-scroll.test.cjs`). The highest any popover goes is `popTop`: 8px under `header.top` on a Mac window, where the traffic lights are drawn over the page, and 8px from the top elsewhere; `placeMenu`, the field picker's `placePop` (which also holds `#pop`'s height to that room) and an agent's model list (`openAgentModels`) all use it (#1573, `popover-mac-chrome.test.cjs`). Scrolling is done only by `scrollOnPurpose(e)` with the reader's event. | [`assets/app.js`](../../internal/gui/assets/app.js), [`assets/i18n.js`](../../internal/gui/assets/i18n.js) |
| Window navigation | In the window from 1024 points wide (`SIDE_AT`, the same width as app.css's `side-nav` rules), `layoutNav` moves the brand, the pages (`#nav`) and Settings into the sidebar (`#side`): the pages in four groups (Work, Models, Insights, Extend; a group none of whose pages shows, as in gateway mode, leaves out its name), the gateway's address at its foot (`sideGwRead`, read from `/api/providers` only while there is a sidebar, or drawn at once from boot.js's `sideGateway`; on a page other machines open, `magpie web`, it is `MAGPIE_PUBLIC_URL` while they are let in, as the Gateway page's address and About's Gateway URL are, noahhrreion on Discord; a port applied in Settings redraws it), and the header a breadcrumb (`renderCrumb`). A page whose head holds only its tools (Library, Plugins, Settings) has them in the title bar after the breadcrumb, `tools` setting the room between it and the window's own buttons. When they don't fit there the tools show their icons alone (`tools-tight`; each keeps its name as `aria-label` and in its tooltip), then the breadcrumb drops its leading parts, and last the page's name is cut at its end, so no tool lies over the title (#2625, `title-bar-tools.test.cjs`). The open page's own tabs (`NAV_SUBS`: Usage, Library, Plugins, Settings) are listed under it as `.nav-s` rows (`role="tab"`, `data-of` the page tab's id) and the page's strip is hidden; a row's click clicks that tab at once, the selection (`.nav-hl`) having set off as the row was pressed (`navSubGlide`; back if the press leaves the row), and is the list's only shape: a row hovered or pressed has no ground of its own, and a `MutationObserver` draws the list again when the strip changes. A page's tabs share its one scroll, so a row that picks another tab opens it at its top (`scrollOnPurpose` with the row's click); the tab already open, picked again, leaves the page where it is (`sidebar-tab-top.test.cjs`). The list opens and folds with an iOS-like spring (`navSubEnter`, `navSubLeave`; none under reduced motion), and a folding list is out of reach at once. The open page's own row is marked the same way, by one shape (`.nav-top-hl`, the rows' own ground given up to it, hovered or pressed too) that `navTopGo` moves to the row picked on a damped spring, as iOS moves a selection (`NAV_SPRING`, SwiftUI's response 0.42s and damping 0.84; `navTopStep` steps it each frame): it sets off as the row is pressed and comes back if the press leaves the row or the page isn't left, turns with the speed it has when the press changes its mind, and follows its row while the list above it folds, landing where the row ends up. It is put in place, not sent, when the window opens on a page or the rows move under it at rest (a `ResizeObserver`), is away while Settings is open (it has its own selection at the foot), comes back fading in on its row, jumps under reduced motion, and isn't drawn in the header's tabs, which have their `.thumb`. `#nav` is `position: relative` in the sidebar for it, so `.seg`'s centring `left` and `top` are reset. See `sidebar-glide.test.cjs`. Narrower, the page beside a sidebar would be narrower than Usage's requests table needs (768 points), so they go back into the header's row, as a Mac sidebar gives way in a narrow window. At every width the reader can pick the sidebar or the header's tabs from the title bar's button (`#sideToggle`, ⌃⌘S or Ctrl+Shift+S; `toggleSideNav`), kept in localStorage `magpie.navLayout` (`side` or `top`) and so laid out from the first frame; none picked, the sidebar comes from `SIDE_AT`. Picked in a narrow window the sidebar is narrower (184 points under 860), and magpie web on a phone keeps its own tab bar and has no button. The two layouts trade places in a view transition (`navMorph`, app.css `.nav-morph`): the sidebar slides, each page's tab flies to its place in the other, and the page slides as one (its old image dropped, so no line prints twice); without view transitions, under reduced motion, or in the Linux app the change is at once: WebKitGTK with compositing off (`WEBKIT_DISABLE_COMPOSITING_MODE`, which `webkit_linux.go` sets under Wayland) crashes in a view transition. Elsewhere a transition still going after `MORPH_MAX` (1.5s) is skipped, so a webview that never ends one can't leave the window on the old layout's picture. See `side-toggle.test.cjs`. A window made wider or narrower drops what a click held (`heldWidth`): the page laid out again isn't the click's doing. The reader's own place is kept instead for a second while the page is laid out again (`relaid`, a `ResizeObserver` on the open page's parts calling `backToReader`): crossing `SIDE_AT` the browser clamps the scroll to each layout it passes through, and the page ended short of its foot (#1672, `text-size.test.cjs`). See `sidebar.test.cjs`. A sidebar longer than a short window keeps no scroll bar beside its pages: the native bars of `.side-scroll` and `.side-foot` are hidden, and `sideBars` lays a thin bar over each box's edge that shows while it scrolls, fades about a second after it stops (kept while hovered or dragged), can be dragged or clicked to page, and is gone when the box fits, the same in WebKit, WebView2 and WebKitGTK. See `sidebar-overlay-scrollbar.test.cjs`. The window opens on the page the reader last left, as a Mac app comes back as it was (molvqingtai and lc on Discord): `show` keeps the page in localStorage `magpie.lastView` (which the app's webview keeps across restarts, as it keeps `magpie.navLayout`), and `keptView` opens it at start. A page asked for in the address (`?view=`, the tray's links, a reload) wins, Agents too; a kept name that is no page opens the first page, and one gateway mode leaves out opens Providers (`show`). Opening on it scrolls nothing. See `last-page.test.cjs`. | [`assets/app.js`](../../internal/gui/assets/app.js), [`assets/app.css`](../../internal/gui/assets/app.css) |
| Settings › Privacy | The page opens on what masking kept from vendors (`drawMaskObs` in app.js, fed by `/api/masking/summary`): the values masked over 7 or 30 days with a chart of the days, the kinds, what only the local model found, agents to providers, and the values masked most, each shown only while pointed at (`maskTip`, `/api/masking/value`, which says it is gone after a restart). Its records (`drawMaskRecs`, `/api/masking/records` and `/api/masking/record`) take the page's place, filtered by kind, finder, agent or provider, a day at a time, each opening on the text the vendor saw with its placeholders marked. `maskedBadge`, on the Gateway page's recent calls and the Routing page's log, opens a request's own record (`openMaskRecord`), in amber with the count sent as written when every value found was on Warn. Below the card, *What to mask*, *How it's found* (the rules, always on while anything is masked, and the local model on top of them), *When a value is found* (Mask, Warn or Block per kind, the secret kinds first and the personal ones behind *Show all kinds*, and the *Never mask* list; `renderMaskActions`), *Try it* (`renderMaskTry`, the text as the vendor would get it from `/api/masking/preview`, kept in no storage) and *Sharing*. See [gateway routing](gateway-routing.md) for what is recorded. | [`assets/app.js`](../../internal/gui/assets/app.js), [`masking.go`](../../internal/gui/masking.go) |
| Built-in plugins | Plugins › Built-in, the page's first tab (`drawBuiltin` in `plugins.js`, rows by `renderBuiltins` in `app.js`), lists magpie's built-in plugins, each with a name, a line on what it is and a switch: Routing groups, Library, Observability, Sessions, Warm-up and check-in, and the Gateway and Routing pages (Essential). A plugin on with settings has their rows under its row, drawn from its schema by the kernel's `ui.form`, and a change posts `{id, config}`. The Gateway page is drawn by its plugin's own scripts through the window's kernel (`kernel.js`, `assets/plugins/gateway/`); see the [plugin runtime](plugin-runtime.md), "The window's kernel". They are `plugins/*` registered with the [plugin runtime](plugin-runtime.md); `GET /api/plugins/builtin` says each one (`builtinJSON`: on is active, the user's own choice, what of it is in use) and `POST {id, on}` (or `{id, config}`) writes plugins.yml. A page marks what goes with a plugin `data-feature="<id>"`. Off, `featureStyle` hides everything so marked (rows drawn later too), `navHides` leaves its page's tab out of the sidebar and the header, `show` opens Agents for an address kept for it, and `setSetTab`, `partShows` and `findSettings` leave its Settings tab and rows out, of the search too. One in use stays on: its switch is disabled and the row says what uses it, and the POST refuses it off. A plugins.yml that doesn't parse is said above the list and no switch moves. `boot.js` says which are off (`featuresOff`) and their settings (`pluginConfig`) before the first paint, and `settings.builtins` carries the list on every state read. The TUI doesn't follow them. See `builtins.test.cjs`, `builtins_test.go`. | [`builtins.go`](../../internal/gui/builtins.go), [`assets/plugins.js`](../../internal/gui/assets/plugins.js), [`assets/app.js`](../../internal/gui/assets/app.js) |
| No GUI build | `-tags nogui` builds magpie without the GUI: `hasGUI` is false and `runGUI` says to use `magpie tui`. The files of `internal/gui` that need Wails build under `!nogui`. | [`gui_off.go`](../../internal/cli/gui_off.go) |
| The terminal quitting | `magpie tui` that served the gateway with an agent connected hands the gateway on as it quits, on Linux where nothing opens magpie at login (a WSL distro, a server over SSH; zaidu259434 on X): `keepGateway` starts magpie's service when it is installed, installs it (`daemon.Install`) when the user never removed it (`daemon.Removed`, a mark `magpie daemon uninstall` leaves), and otherwise starts `magpie serve` in its own session (`serveInBackground`, log in magpie's cache). Elsewhere, with nothing connected, or where magpie opens at login, it only says the gateway stopped. | [`keep.go`](../../internal/tui/keep.go) |

## Runtime path

1. `magpie` with no arguments, `magpie app` or `magpie tray` calls `runGUI`, which calls `gui.Run`. A `nogui` build runs the TUI instead. A second launch hands over to the instance already running. On Windows, before any window is made, `webView2Check` asks Wails whether the WebView2 runtime is there (`Env.Info().PlatformInfo["WebView2"]`, "" when its lookup fails; a key Wails doesn't set counts as there). Without it Wails would exit on the first window with nothing on screen, so magpie logs it, shows a Windows message box in the reader's language naming the runtime, Microsoft's download page (`webView2Page`) and `magpie web` / `magpie tui`, opens the page on Yes, and `Run` ends (#1806, `webview2.go`, `TestWebView2MissingFromWails` on Windows). The box can't come from `ErrorHandler`, which runs on the thread Wails makes its windows on.
2. `startBackend` brings up the gateway, or finds another magpie serving it.
3. The panel and the window load `index.html` from `Handler`. `boot.js` passes the saved language, theme, colorblind mode, text size and desktop font choices before the first paint.
4. The page reads `GET /api/state` and writes through `POST` routes. Each write answers with the new state, built under `held`.
5. Closing the window hides it. On the Mac a full-screen window first leaves full screen (`closeStep`). The system's close on the panel (a title bar's X, Alt+F4, a window manager's close key) hides it as Escape does, so the tray icon opens it again; a panel lightweight mode let go, no longer `h.panel`, closes (`makePanel`, `TestPanelCloseHidesIt`). Lightweight mode lets go only of a window closed this way: one covered by another app's window, minimised, or open as magpie was hidden is still open (`windowUp`, not Wails' `IsVisible`, which is the occlusion state), and so is it for the Dock while the window is open and for a restart (#1381, `TestCoveredWindowStaysUp`). On the Mac the tray icon and a usage cell close an open panel by the same test (`windowOpen` in `togglePanelNow` and `trayCellClick`, not Wails' `ToggleWindow`), so under a window over the whole screen, such as a remote-desktop server's privacy curtain, the icon still closes the panel instead of showing it again (#1661, `TestTrayCellClickReleasedPanel`).
6. The window opens as it was last left. Its settled size and whether it was maximised are kept in `settings.Window` and `settings.WindowMaximised` (`settle`; per machine, see `KeepOwn`). A maximised window keeps the size it restores to. `makeMain` opens it at that size (`openSize`). On its first show, `placeMain` maximises it again on the Mac and Windows (on Windows once the page has come). On Linux, `makeMain` makes it maximised with `StartState`. On Windows a size larger than the screen's work area is fitted and centred (`fitRoom`), and the larger size stays kept. The window's position is not kept.
7. The page's header is the window's title bar. `plainTitlebar` sets it up once the app has started, and again on a window lightweight mode made again (`madeAgain`). On Linux it hides GTK's title bar and keeps the shadow and the resize edges; GTK 3 leaves the corners square. On the Mac the traffic lights are inset into the header (`MacTitleBarHiddenInset`). In the design of macOS 26 a window with a toolbar has larger corners than one with a plain title bar, so `insetLights` ([`titlebar_darwin.go`](../../internal/gui/titlebar_darwin.go)) takes the toolbar off, and `MagpieLights` keeps the lights and the title bar's height where the toolbar had them: after a resize, a new title, light or dark, more contrast or less transparency, and leaving full screen. A sheet, such as the folder chooser, still begins below the title bar. A first press while another app is in front still moves the window from anywhere in that title bar (`firstPress`), as AppKit moved it with the toolbar; AppKit itself now does so only in a plain title bar's top 32pt. AppKit gives a program that design from macOS 26 when the program is linked against the macOS 26 SDK or a later one and its Info.plist doesn't ask for the earlier design (`UIDesignRequiresCompatibility`); `designOf26` reads each at run time, as the SDK depends on the toolchain that built magpie. The toolbar stays in the earlier design, where it doesn't change the corners, with the lights on the right (a right-to-left language), or when the title bar isn't laid out as expected; this is decided once, when the window is set up. In full screen the title bar is AppKit's own. Windows keeps its own title bar.
8. On the Mac, Settings › Show in Dock puts magpie in the Dock never (the default: out of the Dock, and so out of Command-Tab), while the window is open (`DockWindow`) or always (`Dock`); a full-screen window joins it whatever the setting (`inDock`, `dockOnFullscreen`). `openMain` joins before it shows the window (`setDock` runs on the main thread at once). An app activated out of the Dock (the quick panel's focus) that then joins it stays last in Command-Tab's order though it is in front, so joining to the front activates magpie by way of the Dock (`dockStep`, `activateAgain`; #1381, `TestDockStep`, `TestDockJoinLeadsCommandTab`). It does so without asking NSApp whether magpie is active: the Mac makes it the front app at once, and NSApp says so only once the main thread has taken that in, after the join on a busy Mac (#1651).

## Constraints and failure behavior

### Allowance card refreshes

A card refresh applies only the requested account from the API's full
card list. A whole-page read begun before that refresh completed keeps
the newer card when its older response arrives; other cards still update,
and a later whole-page read can replace or remove the card. This also
keeps two different card refreshes from undoing each other's results.
If a refresh first supplies the account's name, its provider's cards are
replaced while newer card refreshes and other providers stay. The replaced
keys are protected too: an older whole-page response cannot restore an
unnamed card or erase its newly named replacement. Only a completed
whole-page read retires the ordering markers it no longer needs.
A remote magpie's summary can likewise become its namespaced child cards;
refreshing it preserves local providers and other remote magpies.
`keepRefreshedQuotas` and `quotaRefresh` in
[`app.js`](../../internal/gui/assets/app.js) own this publication order;
`quota-refresh-order.test.cjs` covers held responses and failures, and
`usage-card-refresh.test.cjs` checks the real button in both browser
engines and all five languages. A cached `readDeferred` card says no
refresh was requested this time; a failed read retains its failure notice.
The tray panel's Allowances cards use the same `quotaRefresh`, at the
start of the line under the account (the reset deadline, the error, or a
balance's name), opened as the card is hovered or the button is focused.
That line names the deadline ("Resets 14:26"), so no bare `↻` stands for
it; `renderPanelQuota` puts the focus back on the button after the redraw.
`panel-quota-refresh.test.cjs` covers it in both engines, all five
languages, at 440 and 360px.
Redrawing the cards preserves their refresh controls and any running turn.
The reply updates the reading immediately; the icon then finishes its
current full turn before stopping (no rotation under reduced motion).

### Streamed output speed

Usage rows and aggregates use `Record.Decode` and `DecodeOf` in
[`internal/usage/usage.go`](../../internal/usage/usage.go). Routing uses
`decodeOf` in [`plugins/routing/client.js`](../../internal/gui/assets/plugins/routing/client.js); Usage
rows use `ledDecode` in [`plugins/usage/page.js`](../../internal/gui/assets/plugins/usage/page.js).

When an answer follows reasoning, only answer tokens are timed from the
first answer text. Without answer text, all output tokens are timed only
when the gateway records paced visible reasoning and a usable content flow.
`firstToken.reasoningMs` in
[`ttft.go`](../../internal/gateway/ttft.go) requires reasoning at the first
content and subsequent reasoning deltas at least 100 ms later. It excludes
native Responses and Gemini, whose summaries do not measure their reported
reasoning tokens, and redacted thinking, including translated sealed blocks.
The upstream protocol is retained when the client receives a translation.
These are receive-side timing checks, not access to the vendor's generation
clock: a relay that releases a buffered reply evenly can still look paced.
Without an upstream guarantee, this is an observed output-rate estimate.
Summaries stay excluded rather than extending their visible portion's time
to an unobserved reasoning-token count.

`Record.FirstReasoning` (`first_reasoning_ms`) and the corresponding
`Call`, `Route` and `Try` field (`firstReasoning`) record this evidence with
the same origin as their duration. Historical records have no evidence and
keep their previous speed. Request-page caches and compressed snapshots
preserve the optional field; the packed row keeps `ReasoningHidden` in
its own `Flags` bit (`rowReasoningHidden`), apart from pricing's
`swapKnown`, which every priced row carries. `ReasoningHidden` (`reasoning_hidden` in the
ledger, `reasoningHidden` in traces) suppresses a no-text speed even when
redacted/encrypted thinking has no reported reasoning-token count. A
Responses summary is marked this way on its own-login path too, and after
an endpoint fallback, where the try's protocol still names the first
endpoint, by the upstream that answered (`summaryUpstream` in
[`continuation.go`](../../internal/gateway/continuation.go)). Optional
OTel attributes carry the evidence without response text. The existing burst and held-content limits in
`DecodeOf` still apply, and hidden reasoning does not enter average speed.

A streamed request's duration (`Millis`, which ends every speed window)
ends when its last try's reply ended, not when `serve` returns: what the
gateway writes down after an answer (`answered`'s sticks, the provider's
last answer in `NoteServed`) goes to disk, and on a busy disk took
seconds, which made a paced reply read as held back and tell no speed
(#1939, `TestSpeedEndsWithTheReply`). A decision group's row is timed the
same way.

`TestVisibleReasoningSpeed`, `TestReasoningSpeedEvidence`,
`TestReasoningSpeedThroughGateway`, `TestMixedRedactedReasoning`,
`TestHiddenReasoningThroughTranslation`, `TestSlowReleasedReasoningIsUntimed`,
`TestPricedRowKeepsReasoningEvidence`, `TestSummaryAfterEndpointFallbackStaysHidden` and
[`reasoning-speed.test.cjs`](../../internal/gui/tests/reasoning-speed.test.cjs)
cover the calculation, cache round trip, translated streams and both pages.

### Routing purpose filter

The request heading's purpose menu in [`plugins/routing/client.js`](../../internal/gui/assets/plugins/routing/client.js)
uses `openProtoMenu`'s live checkbox selection: each tick immediately shows
requests matching any selected purpose, with the menu kept open. An empty
selection, All purposes or Clear filter restores every purpose without
changing the day or request/session view. A live menu stays open when its
redraw clamps the list's scroll; the reader scrolling outside still dismisses it.
Choosing All purposes closes the menu and returns keyboard focus to the
purpose button without scrolling; unticking the last checkbox keeps the
menu open and focused on that checkbox.
A Claude Code subagent's turn has the Subagent tag (`tagKind`,
`Route.SubagentID`) and no kind, so it stays under Unmarked, as on the
Usage page. Counts, the current story and replay
follow the same filtered list. Selected purposes remain available when a day
has no matching requests. Opening a request from another page clears the
filter only when that request is excluded. Usage's purpose picker remains a
single choice sent to the ledger API. See `purpose-filter.test.cjs` and
`routing-purpose-state.test.cjs`.

### Routing mode chip

The stage's chip and the Accounts and keys heading in
[`plugins/routing/client.js`](../../internal/gui/assets/plugins/routing/client.js) name how a route
routes only where that can be changed: a group, or a provider in
`/api/groups`' `pools` (two or more accounts or keys on). There they are
`.rt-go` controls (`goTo`, `goRouting`) that bring the group's card or the
provider's row under Several accounts or keys into view through
`scrollOnPurpose`, mark it (`rt-found`) and focus its routing. A provider
with one on names no routing, and the mode paragraph says how to get one
(`LONE`). The header is drawn again when the groups come in (`headOf`).
See `routing-mode-goes.test.cjs`.

### Routing key and account folds

Three or more API keys, or three or more accounts, of one provider standing
next to each other for one model (same fixed effort, fallback and
group-in-group heading) fold into one stage row, `li.rt-fold` in
[`plugins/routing/client.js`](../../internal/gui/assets/plugins/routing/client.js) (`FOLD_AT`,
`rebuild`). It shows the count and their state together: the one that is
lit and what it is doing, how many are available and how many rest
(`together`). Requests that land on a folded seat fly to the fold
(`shownRow`). Clicking it (or Enter / Space) shows each in place, wired
from the fold. Accounts and keys folds a provider's keys, and its accounts,
into one `.rt-keys` row each with their summed tally.

A group in the group's heading (`li.rt-sub`, `subNode`) folds the same way
(`toggleSub`): folded, it says how many it routes to and what they do
together, a request to any seat under it flies to the heading
(`row.shut`), and its wire is the last one drawn (`wiresTo`).

Keys start folded. Accounts and group-in-group headings start open when
they hold fewer than `LONG` (6), folded otherwise (Aiirobyte on Discord: a
group of many accounts took the page). What the reader opens is kept in
localStorage `magpie.routingKeysOpen` and what they fold in
`magpie.routingFolded` (`keepOpen`, `isOpen`), keyed `provider/model` for
keys and `provider/model@` for accounts on the stage, `provider/*` and
`provider/@` in the list, and `group/<id>` for a heading. Display only: the
gateway's order and choice of seat don't change. Two stay two rows. See
`routing-keys-fold.test.cjs` and `routing-group-fold.test.cjs`.

### Routing account windows

A subscription account's row in Accounts and keys shows each of its
windows under the one figure routing went by, with when each resets
("5 hours 40% used · resets in 2h"), as the Usage page reads them (#1833).
`cardOf` in [`plugins/routing/client.js`](../../internal/gui/assets/plugins/routing/client.js) matches
the route's account to app.js's `quotas`: a `subscription` card of this
computer (no `from`) whose provider is the account's `agent` (a plugin's
provider id) or its provider, and whose user is the account's, ignoring
case. `winsOf` names them with `familyWindows`, `quotaText` and
`resetInText`. Nothing new is read: opening Routing asks
`/api/usage/quotas` in the ordinary way (`show`, when the readings are
missing or over a minute old), never `?asked=1`. A kept day's list shows
none, since those requests were weighed by readings long gone. See
`routing-account-windows.test.cjs`.

The refresh beside the Accounts and keys heading (`#rtReadAllowances`,
#1834) reads them again by hand: `loadQuotas(true)`, the asked read the
Usage page's Refresh now makes. It turns while the read is on its way,
and a click then asks nothing more. Nothing on the page reads again by
itself. It shows only live, with a subscription account listed. See
`routing-read-allowances.test.cjs`.

On the stage, an account's seat tells the same windows in its tooltip,
under its id (`render`, #1835). The row itself keeps one line: the
fullest window while idle, what it is doing while it answers. See
`routing-seat-windows.test.cjs`.

### The Sessions page's folders

The Sessions page lists an agent's project folders as one list, newest
session first ([`client.js`](../../internal/gui/assets/plugins/sessions/client.js)).
A row says the folder's name, the folder it is in (home as `~`, from the
manage reply's `home`), its newest session's title, when, and how many.
Temporary folders, the disk's root and apps' hidden folders under home
(`scratch`) wait folded under Other folders at the end; a search, or a
session opened from elsewhere, unfolds them. Checkboxes and the Delete bar
come only with Select, and Done lets the pick go. Relocate project and
Delete all sessions are in a folder's ⋯ menu, shown on hover (always on a
touch screen) and on a right-click. See `sessions-folder-list.test.cjs`.

### Stored agent sessions in WSL

The Sessions page uses `sessions.ListAgent`; Usage → Sessions uses
`sessions.List` and `StatsFor` through the handlers in
[`sessions.go`](../../internal/gui/sessions.go) and
[`sessions_manage.go`](../../internal/gui/sessions_manage.go).
`ListAgent` also lists a session nothing was said in, so it can be cleared
away; `List` leaves such a session out. The routes a row opens
(conversation, Markdown, Terminal) look the session up with
`sessions.Find`. `Find` finds every session `ListAgent` lists, empty ones
included, so such a row doesn't answer "no such session" (#2273,
`TestFindEmptySession`, `TestEmptySessionOpens`).
On Windows, [`internal/sessions/wsl.go`](../../internal/sessions/wsl.go)
reads running distros through their WSL shared paths. A stopped distro keeps
its cached listing and is not opened; running distros are relisted at most
every 30 seconds.

For omp, [`ompRootsIn`](../../internal/sessions/omp.go) includes both
`~/.omp/agent/sessions` and
`~/.omp/profiles/<name>/agent/sessions`. Subagents and advisor files count
in their parent session. A profile session's Resume command selects its
own profile with `omp --profile <name> --resume <id>`, including when
resumed through `wsl.exe`. Delete and Restore preserve its original
profile path and artifacts. Windows does not discover arbitrary stores
selected by a distro's `PI_CODING_AGENT_DIR`, XDG variables or
`--session-dir`.

### Desktop fonts

Settings → General offers independent interface and code fonts, each with
the installed family's actual styles. `GET /api/fonts` calls
[`fonts.List`](../../internal/fonts/fonts.go): DirectWrite on Windows,
CoreText on macOS and Fontconfig on Linux. Discovery is lazy, cached in the
handler, and refreshed by the menu. It returns names and CSS traits, never
font files or paths. A failed refresh answers 503 and preserves the previous
collection; an empty successful collection is distinct from failure.
A `nogui` build can't read the catalogue and answers the app 404.

A browser tab (`magpie web`, or the app's page opened in a browser) always
has the same rows (Norcleeh on Discord). Its choice is the
browser's own, kept in `localStorage` under `magpie.fonts` and applied by
`fonts.js` before the first paint: the tab can be on another computer, so
it neither changes nor takes the app's `settings.UIFont`/`CodeFont`. A
tab's `/api/fonts` is the server's families (none in a nogui build such as
Docker or `magpie-cli`, and none when reading them fails, never an error)
plus `commonFamilies` in `internal/gui/fonts.go`, in regular and bold.
The tab keeps those it draws (`webFonts.drawn` in `webfonts.js`: text in
the family measures apart from a generic family's on a canvas), so another
computer's fonts, and Safari's user-installed ones, aren't offered. When
none is kept, the rows still offer System default.

Those are the server's names, so fonts installed only on the browser's
computer were missing (Norcleeh again, magpie in Docker). `webfonts.js`,
which only a tab loads (app.js `loadFonts` imports it; the app's windows never
need it, and it is outside the core script budget), adds them: where the
Local Font Access API is (Chromium on https or localhost),
`queryLocalFonts()` is called in the press that opens a family menu, once
a page, and at load when the `local-fonts` permission is already given;
its style names ("SemiBold Condensed Italic") become weight, slope and
width. Elsewhere (Safari, Firefox, a plain-http LAN address, a refused
permission) the menu's search also takes a typed family: "Use “name”" is
offered when the canvas probe says this browser draws it, and greyed out
when not. A chosen family the server doesn't list stays in the tab's list
on later loads while the browser draws it, so `desktopFonts.available`
keeps applying it. Until 10-28 the rows were
gated on a boot `fonts` flag, so a nogui server had none, and what
"flashed and vanished" on a refresh was Text size: the rows a tab never
shows (Text size, the tray click, Open at login, the menu bar's
allowances) are `.app-only`, which `body.web` hides before the first paint.

`settings.UIFont` and `settings.CodeFont` keep the family, style name,
OpenType weight, CSS slope and width percentage. A missing choice keeps the
platform default. A settings POST can restore that default with an explicit
null; omitting the field preserves it, as does every browser-mode save.
`KeepOwn` preserves this computer's choices during sync and backup restore.
Invalid traits reject the save without replacing the previous settings.

[`fonts.js`](../../internal/gui/assets/fonts.js) applies choices at boot and
through `applyPrefs`. After a successful font change `host.fontsChanged`
notifies both existing webviews through `receiveFonts`, without waiting for
the other window to gain focus; a released lightweight webview uses boot
settings when recreated. An in-flight local save ignores the notification
and finishes its own queue.
The selected family precedes the existing platform fallback stack. The
interface and code traits apply independently; explicit component weights
still mark emphasis. A choice takes precedence over Omarchy's font, while
restoring the default follows the theme again. If discovery finds a saved
face missing, the page retains its name, explains the fallback and uses the
default until the face is available or the user chooses another. A discovery
error never clears a saved choice. The browser's own typography is unchanged.

The menus support search, keyboard selection and refresh. Styles are ordered
by weight, then normal, italic and oblique; width and name break remaining
ties. Family changes keep a matching style where possible, otherwise the
nearest regular face; named variable instances are offered without arbitrary
axis controls.

### Recent-call bodies

`providersState` in [`providers.go`](../../internal/gui/providers.go) sends
recent-call metadata without request or response bodies, including after
provider writes (#1521: forty calls' bodies made `/api/providers` about
20 MB). Each call in the gateway's 40-call ring has an `id`, numbered in
`Server.record`. Opening a recent call on Gateway fetches its captured
previews from `GET /api/gateway/call?id=…` (`Server.RecentCall`), sent
`Cache-Control: no-store`. The route sits on the same mux as the rest of
the API, so `magpie web`'s key guard covers it. The bodies are the ones the
ring kept, masked as the request was (`redacted`); the route doesn't fetch
S3 or change capture, export or archival. A call evicted from the ring, or
a gateway served by another magpie, returns 404. The page keys a row by
that id (`callRow`), keeps fetched bodies in memory while their calls
remain in the list, preserves them across summary refreshes, and offers a
retry after a failed read. See `TestRecentCallBodiesOnDemand` and
`recent-call-bodies.test.cjs`.

A request on the Routing page opens to the same bodies (Fate on Discord):
its story's head has "Request and response", which fetches
`GET /api/gateway/call?route=…` (`Server.RouteCall`) only when pressed.
A call carries the Routing request it was (`Call.Route`, the trace's
`Route.ID`, set where `serve`, the Codex backend and a refused title
record it; the Codex backend's refused key and empty failure too, but not
a compaction's, which opens no route: `TestCodexKeyRefusedCarriesItsRoute`);
a route id is not a call id. When the ring no longer has it,
the story shows the request archive's copy through `archivePanel` if the
route names one (`Route.Archive`, `call.archiveName()`, set only when the
archive was on for that request), and otherwise says how to turn the
archive on. Nothing more is captured than before. The archive's switch
(`POST /api/settings/archive`) sets `RequestArchive` while sync has an S3
bucket and `RequestArchiveLocal` while it has none; `GET
/api/archive?name=…` reads this computer's copy first and then the bucket's
(`TestArchiveSwitchKeepsOnThisComputer`, `request-archive.test.cjs`). See
`TestRoutingRequestOpensItsBodies`, `TestArchiveWholeBodies` and
`routing-bodies.test.cjs`.

### Plugin check-in switches

A plugin's Usage-card Auto check-in button reads the persisted
`settings.pluginCheckins[provider]` first (`pluginCheckinOn` in
[`app.js`](../../internal/gui/assets/app.js)). `/api/state` returns that map
without the derived `checkinPlugins` list included by `/api/settings`.
An explicit saved false wins over the list; when no saved choice exists,
the list's resolved vendor default remains the fallback, and without the
list (a state refresh) the switch of the built-in check-in the plugin took
over is (`PLUGIN_CHECKIN_INHERITS`, the ids `provider.PluginCheckinOn` names;
a test reads both). This keeps a saved
switch on through a focus refresh and lets the next click turn it off.
[`plugin-checkin.test.cjs`](../../internal/gui/tests/plugin-checkin.test.cjs)
covers both response shapes, refreshes and the subsequent toggle.

### Plugin options

A plugin provider's editor shows the options its plugin declares
(`pluginOptionsField` in [`app.js`](../../internal/gui/assets/app.js); see
[provider-plugins.md](provider-plugins.md#a-plugins-own-options)). The
plugin's label, description and `confirm` text go in as text, never as HTML.
A change is kept in `draft.pluginOptions` until Save, which posts each one
to `POST /api/settings/plugin-option` before the provider's own save. An
option switched back to where it was is dropped from the draft, so it isn't
posted and doesn't make the editor dirty.
[`plugin-options.test.cjs`](../../internal/gui/tests/plugin-options.test.cjs)
covers it.

### A plugin's own page

A community plugin that declares `magpie.page` gets a tab in a Plugins
group at the sidebar's foot (`pluginpages.js`, loaded after `plugins.js`),
listed from `GET /api/plugin-pages` and again whenever Plugins reloads its
list (`window.loadPluginPages`). The tab's title is the plugin's own and is
not translated; `renderCrumb` and the sub-nav's label read its plain
`<span>` where a built-in tab has `span[data-t]`. The frame is made the
first time the page opens and fills the view (`.view.plugin-page`,
`.plugin-frame`). The theme and language reach it on every change of
`data-theme`, `lang`, `class` or `style` on `:root`. A page gone off or
away loses its tab, and the window goes to Plugins if it was on it. The
security model, the bridge and the reads are in
[plugin-runtime.md](plugin-runtime.md#p4-community-plugins-at-every-point).
[`plugin-page.test.cjs`](../../internal/gui/tests/plugin-page.test.cjs)
covers it in both engines, every language and a narrow window.

### Shared UI rules

- Windows desktop windows and tray panels keep the modal's dimming veil without backdrop blur or saturation: full-window filtering can stall WebView2 while a provider test or sign-in dialog is open. The existing `win` marker from [`layout.js`](../../internal/gui/assets/layout.js) scopes the rule in [`app.css`](../../internal/gui/assets/app.css) to the desktop app; browser pages and other platforms keep their existing effect. `modal-backdrop.test.cjs` checks the computed filter and veil in both engines and modes.

- Settings → General is laid out as macOS System Settings is (1ooogle on X): a row says its name alone when that says enough (Appearance, Colorblind mode, Language, Text size, the fonts, the tray click, Open at login, Show in Dock), and keeps one short line where the setting isn't clear from its name or has a consequence (Lightweight mode, Prevent sleep, Session terminal). Codex's, Claude Code's and Grok Build's own settings (Codex subagents, Long conversations, which is all of Grok Build's, background compaction, thread titles and descriptions, composer predictions, the auto-review and memories models) aren't here: they are in the agent's opened row on the Agents page (`AGENT_PREFS`, `connectPanel`), the panel's row has a Settings line that opens them in a sheet (`openAgentPrefs`), as does the agent's menu, and Settings' search finds them with a row that opens that sheet (`#agentPrefLinks`). Each row is one element moved between its places, so its handlers and state go with it (the owner, 10-20; `agent-prefs.test.cjs`). Long conversations is the exception with no Codex, Claude Code or Grok Build here, in gateway mode or a magpie in Docker: it sets the windows the gateway serves, which agents on other computers read, so it stays in General (`renderAgentPrefLinks`, Linka on Discord). A font row's line shows only while its fonts load or fail; the text size keys are the choices' tooltip, and Colorblind mode's note is its Off/On control's (`#colorblindSegs`). Colorblind mode (`settings.Colorblind`, `data-colorblind="1"` on `:root`, #2073) switches the chart series (`--c1`…`--c7`) in both light and dark themes to an Okabe–Ito palette where adjacent series differ in hue and lightness. On a quota curve each window (`data-series`, its index mod 7, set by `drawQuotaPlot` and `quotaCurve`'s legend only) gets a dash of its own on `path.qc-line`, its legend key shows a sample of that line (`.qc-sample`) for the dot, and the even paces (`line.qc-even`) are dotted; the curve's tooltip says so. The balance curve sets no `data-series` and keeps its solid line, dashed fit and dot. The Context tab's "good" grade, which takes `--c2` as a colour rather than a series, keeps a blue (`--c1`). Language is one menu button showing the language picked, opened in the app's menu (`renderLangPick`). See `settings-general.test.cjs` and `quota-curve.test.cjs`. Open at login reads `autostart.Enabled`, which on macOS 13 and later also asks Background Task Management about magpie's launch agent (`SMAppService.statusForLegacyURL:`, matched by the plist's Label, not its path); a record the user switched off in System Settings › General › Login Items reads as needing allowing, and the row keeps On with one line saying so and a button that opens Login Items (`/api/open` allows that one `x-apple.systempreferences` URL). The status read again when the window comes back to the front clears the line. On macOS 12, in a build without cgo, or when the system can't be asked, the plist alone decides, as before (#2120, `login-allow.test.cjs`, `login_allow_test.go`).
- A setting row's line under its name (`.row.pref .sub`, in Settings, Plugins › Built-in and an agent's own settings) wraps whole beside its control, never cut to one line with an ellipsis (arculongus on Discord, `settings-lines-wrap.test.cjs`).
- Settings' search-provider picker offers **Off**, saved as `searcher: "off"`. Its help explains API-only search and suppresses provider fallback/own-search hints while off; the search priority row is hidden because no provider competes with the APIs. See [gateway search behavior](gateway-routing.md#turning-provider-search-off), [`renderSearcher`](../../internal/gui/assets/app.js), `TestSettingsSearchProviderOff` and `searcher-pick.test.cjs`.
- Settings' search APIs are asked in their listed order: each API's row (with its keys' row) is dragged as one, or moved with Alt+↑/↓, and has a switch that turns it off without removing its key (#2286). See [the search APIs' order](gateway-routing.md#the-search-apis-order-and-one-switched-off), `searchArrange` in [`app.js`](../../internal/gui/assets/app.js) and `search-order.test.cjs`.
- Settings' Search APIs add row has a base URL field under the key for every API (#1580): the vendor's own address is its placeholder, picking an API already added fills in its saved URL so a new key alone keeps it, and Add with no key changes only the URL. See [a search API's base URL](gateway-routing.md#a-search-apis-base-url), [`renderSearch`](../../internal/gui/assets/app.js) and `search-api.test.cjs`.
- Every user-visible string has zh, zh-TW, ja and de translations with the same placeholders. `gui-zh-tw.test.cjs`, `gui-ja.test.cjs` and `gui-de.test.cjs` fail on a missing one; `gui-zh-tw.test.cjs` also fails on a zh-TW string with a Simplified character left in it.
- A click never moves the page. Code scrolls a view only with the reader's event in hand (`scrollOnPurpose`); `click-scroll.test.cjs` guards this. A click is taken where the reader left the view: a scroll not theirs made just before it, whose scroll event the browser sends only at the next frame, after the click, is put back first (`backToReader` in the click guard, #1554). What was clicked is held where it was (`hold`); a view at its top when clicked stays at its top, and what comes in above the control moves it down instead (#2134, `update-auto.test.cjs`), as long as it moves less than half its own height and so stays under the pointer. When what the click does leaves the page shorter under it (a list emptied), the view keeps room at its foot for it to stay (`.view-room`), room that goes as the reader scrolls back. A control under what it unrolls (`data-unrolls`, "Show 7 more") goes down with what it opens: what's held is the part it is in, so the rows open downwards. Anything larger above it, as the stage a day picked on Routing brings in above the bar, is scrolled past, and the control stays where it was clicked (`routing-days-fit.test.cjs`, `routing-failed-open.test.cjs`).
- A scroll not the reader's is put back, so every way the reader scrolls has to count as theirs: the wheel and a trackpad's coast after the lift (WebKit on the Mac plays the coast itself and the page hears its scrolls with no wheel for each, so the wheel starts the same chain a touch fling does, `flings`/`flingUntil`: each scroll keeps the next one the reader's till it rests, and a click ends it; issenn on Discord, `view-trackpad-coast.test.cjs`), a wheel or coast the page is never told of (macOS 13's WebKit, Safari 16.6's, scrolled a view under wheels it never dispatched, so every scroll was put back and only the bar's drag moved the page: three scrolls put back the same way within a second are taken as the reader's run and given back, and so is each one after it that goes on the same way within a second of the last, so a wheel turned slowly or on a loaded Mac loses no step; a scroll by code is one, not a run, and two at rest are still put back; issenn on Discord, `view-wheel-unheard.test.cjs`. Under a click's 4s hold the same holds: only a put-back that comes with the page redrawn since the hold last held (`redrawn`: the view's height or where the first held parts sit moved from `h.lay`) is the hold's and never counts toward a run, so a profile opened on the Agents page and wheeled through at once scrolls, issennknight on Discord, `view-wheel-after-click.test.cjs`. The parts `h.lay` measures are those still in the page that scroll with it: a part drawn again since (the Usage page's tabs, the agents' list under an agent opened or closed) is a redraw once, not at every scroll, and a sticky one (Settings' tabs at a narrow width) is left out, as it sits still while the page scrolls; `view-wheel-after-redraw.test.cjs`), a touch and its fling, the keys, a drag, the scrollbar's own drag (`barDrag`, #1089) and the middle button's autoscroll (`panning`, 多喝热水 on Discord). Chromium and WebView2 give the autoscroll a press of button 1 and then no event of the reader's while it scrolls; from that press the view follows it till Chromium ends it, on the next press (which reaches the page only as a pointerup), a key (a keyup), or the lift after the mouse moved while held. A field the guard brings back into view after putting a scroll back is one focused since the reader last scrolled that view, not one they scrolled away from (`scrolledFrom`): the middle press focuses the button under it. See `view-middle-autoscroll.test.cjs`.
- Typing never moves the page either. A textarea that grows with its text (the Library's `autosize` in plugins/library/client.js: a set's text, an agent's own text, a skill's description, a subagent's fields) is measured at height auto with what holds it kept at its height, so the page never goes short for a moment: a page gone short has its view's scroll clamped by the browser at every input, an input method's every composing step included, and the guard's putting it back pulled a textarea taller than the view to one of its edges (#2636, `library-ime-scroll.test.cjs`).
- A box that scrolls inside a page keeps the wheel at its ends: wheeled past its top or foot, the page stays where it was (碳碳双键 on Discord, Requests' list on Usage). `holds` in app.js marks every box between the pointer and its `.view` that scrolls vertically and overflows (`.holds`), on `pointerover` and on each wheel, and app.css gives `.holds` `overscroll-behavior-y: contain` (the Mac's bounce stays, on the box). It is not one CSS rule for every box because Chromium (WebView2, magpie web) holds the wheel in a box told to contain even when it doesn't overflow, and in one that scrolls only sideways, so a short list or a wide table would stop the page. `.holds` is under `:where()`, so a rule that passes the wheel on still does (`.mchips` in a short window, #833). The page itself (`.view`) and the document chain as before. An engine with no `overscroll-behavior` (Safari 15, macOS 12's WebKit until Safari is updated; Safari 16+, WebKitGTK and Chromium have it) gets the same from `compat.js`: a wheel over a `.holds` box at its end in the wheel's direction is cancelled, so there the Mac's bounce at the box's end is lost. A guard in `inner-scroll-contain.test.cjs` builds every rule in every stylesheet under assets that scrolls a box vertically from its own selector, fills it and points at it, and fails naming the file and selector of one that doesn't hold the wheel (a rule that sets `overscroll-behavior` back to auto, or a box `holds` doesn't reach). See `inner-scroll-contain.test.cjs`.
- There are no native `<select>` elements and no colored left-border stripes. State is shown with a dot or a swatch.
- What the window shows can be selected and copied, its controls can't, as in a Mac app (Outvi V on Discord: nothing could be copied into a bug report). The page is `user-select: none`, and two `:where()` rules after `body` in `app.css` make text content selectable (paragraphs, `pre` and `code`, a detail's values, hints and notes, every error class, a conversation, a request's story on Routing) and keep every control unselectable with what is in it (buttons, labels, tabs, menus, the navigation, grips). Both have no specificity, so a view's own rule still decides, as a list being sorted does; a field stays selectable. See `user-select.test.cjs`.
- The Usage page's cards and the tray panel's Allowances tab share one order, `settings.UsageOrder` (`byUsageOrder`). In the panel a balance-only card stands where that order puts it, and balances next to each other share one *Balances* grid (`panelBalances`, #1510); none is gathered after the plans. The panel's *Arrange* (`panelArrange`) moves rows in it and hides subscriptions from that tab alone (`settings.PanelUsageHidden`); both save through `POST /api/usage/arrange`, which changes only the field it is sent, and the Settings save keeps both. Hiding is display only: a menu bar cell's card shows though hidden (`panelPeek`). See `TestUsageArrangePanelHidden`, `panel-arrange.test.cjs` and `panel-balance-order.test.cjs`.
- A client in the Usage page's Agents list can be hidden from that list (ikunbots on X): its row's "…" button or right-click opens the row menu with *Hide from this list*, saved by client id in `settings.UsageAgentsHidden` through the same `POST /api/usage/arrange` (`agentsHidden`), and kept by the Settings save. The ledger, the totals and the chart still count a hidden client. A quiet "{n} hidden" line under the list opens them, each with *Unhide* (`hideUsageAgent`). See `TestUsageArrangeAgentsHidden` and `usage-agents-hide.test.cjs`.
- Usage › Overview's Agents, Models, Accounts and Gateway keys lists each fold under their heading (#2378): the heading is a `.fold` button with its chevron, as on the Gateway page, and the click doesn't move the page (`backToReader`). What is folded is kept per viewer in localStorage `magpie.usageFolded` (`mountUsageFolds`, `showUsageFolds`). The totals leave the cache out, as before, unless the tokens tile's label, a pop-up button (`usageTokensPick`, the app menu), is set to *With cache*: then the tile, the chart and every row's count and share add cache read and written (`usageTokens`), and the tile says the cache's count; kept in localStorage `magpie.usageWithCache`. The Requests tab's total already counts every token (`allTokens`). A token line in parts (the Requests strip's Tokens card, the Overview's tokens tile) wraps between its counts and keeps each whole (`usageParts`, `.parts`); only a count wider than its card on its own is cut. See `usage-overview-fold.test.cjs`.
- A request's details in the Usage ledger say its tokens at every width (#1839, `ledDetail`): "{i} in · {o} out · {r} cache read · {w} cache creation", 0 said as 0, with how much was written for 5 minutes and for an hour when it says so. A failure that spent nothing has no Tokens row. See `ledger-cache-tokens.test.cjs` and `usage-ledger-detail.test.cjs`.
- The requests magpie makes by itself (a routing classifier's, a web search's, an image description's) are told apart from the agents' on the Usage page (#1839): magpie's row among the agents and its rows in the ledger carry a "magpie's own" mark (`ownBadge`), an account's, key's or model's row adds "{n} by magpie itself" from the summary's `own`, and an account's card says how many of the period's requests were magpie's own (`ownLine`, drawn again once the summary comes after the allowances). See `own-requests.test.cjs` and `TestSummaryCountsMagpiesOwnCallsApart`.
- Opening the allowances is asking to see them (#1588, `quotasOpened` in `app.js`): the Usage page from the nav or its Overview tab picked, the window brought back to the front on the Overview, the tray panel's Allowances tab picked or the panel brought up on it, and a menu bar cell clicked each load `usage/quotas?asked=1`, as Refresh now does. An asked read makes the backend read every account again (`AskClaudeUsage`), so an opening within 30s (`QUOTAS_OPEN_GAP`) of the last asked read sends an ordinary read instead, which joins one on its way; Refresh now always asks. A panel page loaded while it is put away (hidden, or without focus) doesn't ask. The timed refresh is unchanged and never asks. See `allowance-open-read.test.cjs`.
- The menu bar can show magpie's own totals beside the allowances (#1984): `usage:today` and `usage:7d` in `settings.TrayUsages`, offered after the cards in the same picker. `trayUsageCards` sums them with `usage.QueryPage(p, Filter{}, 0, 1).Sum` (the Requests tab's and the panel's cached ledger sum, gateway and session-file calls together) and asks no vendor when only totals are picked; `trayTotalCard` makes every token (in, out and cache, as `allTokens`), marked `Σ` so it doesn't read as the input, over the cost in Settings' currency, `+` when some calls had no price, the cost row left out when none had one. The Mac draws them as plain cells, the bird beside them saying whose. A click opens the Requests tab at the period (`quotaView`, `?tab=requests&period=`) or the panel's Usage tab (`panelUseFocus`). No 3-day total: the Usage page has no such window. See `TestTrayTotals` and `tray-totals.test.cjs`. Settings › *Menu bar cost* (`settings.TrayNoCost`, on by default, on every OS, per computer through `KeepOwn`) turned off leaves the money out (BussanQ on Discord): `trayMoney` drops the total's cost and its count of unpriced calls before `trayTotalCard` and the no-card tooltip `trayIdleTip` see it, so the cards, their tooltip and the idle tooltip say the tokens and requests alone. A relay's or subscription's balance card is money itself and stays. See `TestTrayNoCost` and `tray-cost.test.cjs`.
- The tray's icon is the bird alone where the bar's other icons are glyphs: the Mac's menu bar takes `tray.png` as a template image, and a Linux bar (StatusNotifierItem `IconPixmap`) gets the same silhouette in one colour on transparency (`trayIconFor`, `linuxTrayIcon` in [`trayglyph.go`](../../internal/gui/trayglyph.go)), so a bar that tints its icons by their alpha tints the bird, not a tile (ylorn on Discord). A bar that doesn't tint shows it light, or dark when the portal's `org.freedesktop.appearance` `color-scheme` prefers light (`traySchemeLight`), and it is drawn again when that changes (`watchTrayScheme`). No `IconName` is set: a host that found a `-symbolic` name and drew it untinted would show a black bird on a dark bar, with no pixmap to fall back on. Windows' tray keeps the app's coloured icon. With no cards in the menu bar the tooltip is magpie over today's total (`trayIdleTip`: every token, split into input, output and cache as the today card's tooltip is, then the cost and the requests), brought up to date with the cards' refresh; the tray's menu stays Open, the version and Quit. Wails sets the tooltip only on Windows: on Linux `setTrayTip` sets the StatusNotifierItem's `ToolTip` and sends `NewToolTip` ([`traytip_linux.go`](../../internal/gui/traytip_linux.go)), and on the Mac it is the status item's button's `toolTip` (`trayTipNative` in [`trayimage_darwin.go`](../../internal/gui/trayimage_darwin.go); NextCommit on Discord: no hover there showed anything). See `TestTrayIconPerOS`, `TestTrayIdleTip` and `TestMacTrayTipIsSet`.
- A window's Pace is a number on its card's reset line (fflake33 on Discord): the share used less the share of the window gone by, in points, "Pace +12%" over an even pace and "−8%" under it, turned with the meter when it shows what is left, amber while more is used than an even pace would have (`quotaPace`, `paceLine`). The window's start is the gateway's, `startsAt` from `provider.WithWindowStarts` (its reset less its span, `QuotaWindow.Bounds`); a window whose span isn't known, with no limit, or a past cycle has none. Settings › *Menu bar pace* (`settings.TrayPace`, off by default, hidden on Windows, whose tray shows no text) puts the same number beside each figure in the menu bar and in its tooltip (`trayPace`, `QuotaWindow.Pace`). See `TestWindowPace`, `TestTrayUsagePace` and `quota-pace.test.cjs`.
- In Usage → Allowances, Trends → Cycle offers each account's own recorded cycle menu beside its curve (`quotaCurve`, `quotaRecordedCycles` in [`app.js`](../../internal/gui/assets/app.js)). The longest window (usually weekly) owns the menu; shorter windows are drawn over the selected dates with their own reset breaks. `/api/usage/quotas/history?days=45` supplies all retained readings, including remote cards. Completed cycles come from observed boundaries, not seven-day arithmetic; an early reset ends the previous cycle at the vendor's new start if it fits between readings, otherwise at the first new reading. Each menu option gives its observed duration, so an early reset is not labelled as a full seven days. The selected meters, plot, legend, tooltip and dashed pace use that cycle's last readings, never today's percentages. A window without a reading in the selected interval says so instead of showing a current value. A local account's past cycle asks `/api/usage/quotas/history/holds?provider=&user=&from=&to=` once per selection (`quotaCycleHoldsOf`), which asks no vendor (#2009, NorethSea): `HistoricalWindowHolds` in [`window_holds_history.go`](../../internal/usage/window_holds_history.go) reckons each window's whole at its own last reading in that cycle, from the request log up to that reading, so the 5-hour figure is the cycle's last 5-hour window, never its short windows added up. Its tooltip names that window's start and the reading's time. It shows "Loading…" until the answer comes. A window it can't reckon says "not enough data" with the reason, as a current card does; a reply that comes after another cycle was picked is dropped, and a card's Refresh or an asked read reckons again. Historical meters show the cycle end and sampling time; current account actions, balances and policies are left out until returning to the current view. Accounts are independent, and a selection survives card refreshes in the same page. Trends → 2 days temporarily shows recent readings; Off removes the curves; the tray always shows its current cycle. See `quota-curve.test.cjs` and the [history limits and reset inference](providers-accounts.md#constraints-and-failure-behavior).
- The Usage page's cards are packed into columns, never all stretched to a row's height (`packUsage`, #860): each card in that order goes to the leftmost column that ends highest, on 1px grid rows, so short cards stack beside a tall one. The last card in each column then reaches down to where the tallest column ends (a `min-height`, cleared before the cards are measured again), so cards side by side in one row are as tall as each other and a stack ends with the tall card beside it (ikunbots on X); content stays at each card's top. The page's order (DOM, dragging, Alt+arrows) stays `UsageOrder`. The column count is the most 260px columns that fit, or one fewer when that leaves much less empty room; it is chosen again only when the cards change or the width fits another number of columns, so a card opened or folded stays where it is, even as the scrollbar comes in. See `usage-pack.test.cjs`.
- The Providers page has a search box over its list (#1614, `findProviders` in [`app.js`](../../internal/gui/assets/app.js)): the words typed keep the rows, on and off, whose text or data say them all: name, id, preset, addresses, every model's id and name, the account's user and plan and each login's, account names and key names. The Turned off fold stays open while searching; Escape or clearing the box shows every row and the fold as it was. A row that is on reorders by dragging its logo, Alt+↑/↓, Alt+Shift+↑/↓ (top, bottom) or its right-click menu (`openProviderMenu`: Move to top, Move up, Move down, Move to bottom). While searching, a step or a drop moves it among the rows shown (it takes the place of the row it passes, `stepProvider`, `shownToOn`) and the hidden ones keep their order; Move to top and Move to bottom go to the ends of the whole list. Every move saves the whole order through `POST /api/providers/arrange`. See `provider-find.test.cjs` and `provider-order.test.cjs`.
- Hide accounts (`window.hideAccounts` at the end of [`app.js`](../../internal/gui/assets/app.js), stored in `magpie.maskEmails`, which the panel follows) masks every account name and address in the page's text and tooltips. While it is on, an account's name on the provider editor's row (`accountNameButton`, and the one-account line) and on its Usage card (`quotaWho`), and on the tray panel's Usage card (`panelQuotaCard`'s `.pq-who`, in sight while the card is hovered), has an eye (`hideAccounts.eye`), in sight on hover or keyboard focus. A click shows that one account, its address and the name the user gave it together (`kin`), everywhere that page draws it, tooltips included, while the others stay hidden (yogurt36 on Discord, for a screen share). The accounts shown are a set held in memory that `hits()` skips; nothing stores them. The window and the panel are pages of their own, each with its own set: one shown in the window stays hidden in the panel and the other way round, so a screenshot of either shows only what was shown there. Closing the window hides them again: the page stays loaded while the window is hidden, so `hideMain` runs `mainClosedJS` (`hideAccounts.closed`) first. Putting the panel away hides the panel's again, from its `visibilitychange` (the one that drops `panelPeek`) (`account-eye.test.cjs`, `TestClosingHidesShownAccounts`).
- The window has a separate *Get started* navigation page (`quickstart`, `renderQuickstart` in [`app.js`](../../internal/gui/assets/app.js)). It always shows three required steps, in order: connect a provider, select models, and connect an agent. Opening the page or returning focus rereads `/api/state` and `/api/providers`, without opening provider editors or changing configuration. The same provider read updates the sidebar gateway status, so opening the sidebar reuses it. Provider readiness requires `ready`, excluding switched-off or signed-out providers; a cached model count cannot establish a source. Model selection requires an active source and positive `served` (routing groups left out). Agent completion independently requires an agent wired, even if its source has signed out. While checking, a status notice is shown; a failed read marks completion unknown, removes the check icons and offers retry while retaining configuration actions. A response from an earlier visit or before a saved edit cannot overwrite newer state. Each completed step keeps its title, current-state details, check icon and configuration action. Model selection opens the sole ready provider directly, or the Providers page when several are available; opening an action alone saves no configuration. Routing is configured on its own page and has no step, template or completion requirement here. On a normal window launch with any step incomplete, Get started takes priority over the last remembered page; an explicit `?view=` link retains its target. Later refreshes keep the reader on the page they chose. The compact panel keeps its dismissible card on the Agents tab (`guideShown`): completion or `settings.GuideHidden`, saved through `POST /api/settings/guide`, removes only that card. The permanent navigation page ignores that dismissal. See `getting-started.test.cjs`.
- Every page of the window keeps to one width, at most 1200px of content centred, with a 16px gutter in a narrow window (`padding: max(16px, calc(50% - 600px))` on each `#view-…`, #860). Routing, Usage and Sessions are in that rule too (面条 on Discord), so a large screen doesn't stretch them edge to edge while the others sit in the middle; their columns follow the room they get (`byWidth`). See `page-width.test.cjs`.
- The page must work in Chromium (Windows' WebView2) and WebKit (macOS, and WebKitGTK on Linux). GUI tests run in both engines.
- In the design of macOS 26 the main window has the corners of a window with a plain title bar, and its lights stay where the toolbar had them. `TestMainWindowCorners` ([`titlebar_darwin_test.go`](../../internal/gui/titlebar_darwin_test.go)) measures this in a process of its own, with package `windowshape`: after a new title and size and a new system appearance, as AppKit draws that appearance, where a first press with another app in front begins a window drag, and where a sheet begins. It runs that process as linked against the macOS 26 SDK and against macOS 15's, whatever SDK the toolchain has (`runAppKitSDK` and `stampSDK` give the copy it runs that SDK, and sign it again). In the earlier design, on an older macOS or with macOS 15's SDK, it checks that the window keeps its toolbar; `TestMainWindowRightToLeft` checks that it keeps it with the lights on the right. Both run in CI's macOS job. No test covers `UIDesignRequiresCompatibility`: AppKit reads it from an app bundle's Info.plist, LaunchServices registers each bundle that runs, and a press sent within a process run from one doesn't always begin a window drag.
- On Linux the panel is frameless and the main window has a hidden title bar (`plainTitlebar`). GTK 3 asks KDE's Wayland session (KWin's decoration protocol) for a frame drawn by KWin on any window without client-side decorations, an undecorated one too, so `ownFrame` tells KWin the panel draws its own on each realize (#1283). GTK 4 builds ask for none themselves. With no KWin title bar, and with a Wayland window placed by the compositor rather than by the icon, the panel's header is its drag handle on Linux, as the window's is (`--wails-draggable` in `app.js`; `panel-drag-linux.test.cjs`); on the Mac and Windows the panel isn't dragged (#1430).
- On Linux the panel is a solid window, as the main window is, and its page paints `--bg` whole (`panelOptions`, `body.linux.panel` in `app.css`). The Mac's and Windows' panels stay translucent. A see-through panel is an RGBA, app-paintable GTK window under a transparent webview, and on KDE Plasma 6 its page, painted at 93%, left a fainter copy of the old scroll position as it scrolled (Wayland and XWayland, compositing mode on or off) while the main window never did (#1803, `TestPanelSolidOnLinux`, `panel-linux-opaque.test.cjs`). Xvfb with picom on WebKitGTK 2.54 doesn't show the ghost either way, so the fix rests on the reporter's machine.
- A Wayland window can't place itself, so under Hyprland and niri the compositor is asked to put the panel under the bar. Under Hyprland `placePanel` gives it a window rule at the cursor (`omarchy.PlacePanel`) and closes it on a click outside (`omarchy.ReportClicks`). The rule names the monitor under the cursor and moves the panel to x, y from that monitor's corner (`omarchy.PanelAt`): Hyprland adds the window's monitor's place to a `move`, so a position across the whole layout put the panel off a monitor not at 0,0 (#1785, `TestPanelAtMonitorOffScreenOrigin`). The cursor and the clients' places that `omarchy.ClickedOutside` compares are both across the layout. niri (`omarchy.Niri`, `NIRI_SOCKET` set) takes no rules at runtime and doesn't report the cursor. There `omarchy.NiriPanel` watches niri's event stream and moves the panel each time it opens: the window of magpie's pid titled `panelTitle`, moved with `MoveFloatingWindow` (after `MoveWindowToFloating` if it was tiled). It goes 6 px under the working area's top, centred on the x the tray host passed to `Activate(x, y)`. Wails keeps that x in its Linux tray (`trayClick`, `TestTrayClickIsWails`), and it is taken across the bar, since that's all a Wayland bar knows. With no click (`magpie panel`), or with one off the output, it goes top right. Wails' `HideOnFocusLost` closes it, since a click on another window takes niri's focus. It still opens in the middle and slides up, and a bar at the bottom or the side isn't accounted for (#1670).
- Wails calls the tray's click handler both for the icon's click (`Activate`) and when the tray host opens the icon's menu (dbusmenu's "opened" event). KDE opens the menu on a right-click, and the panel that click opened took the focus and shut the menu on Wayland. On Linux the handler ignores the call when it comes from the menu opening (`menuOpening` in [`trayclick.go`](../../internal/gui/trayclick.go), which looks for Wails' `(*linuxSystemTray).Event` among its callers; `TestTrayMenuOpenFrameIsWails` fails if Wails renames it or stops calling the handler there) (#1430).
- On GTK 3 builds, the app gives GDK a font DPI before the first webview when it has none (`fontDPI`, [`fontdpi_gtk3.h`](../../internal/gui/fontdpi_gtk3.h)). GDK's unset value is -1, and WebKitGTK 2.54's GTK 3 settings reader multiplies it by 1024 unchecked, so every page lays out at a negative width with a 9000000px font (#1371). An unset DPI takes `gtk-xft-dpi`, or 96, times a valid `GDK_DPI_SCALE`; a positive one is left alone. Wails initialises GTK (its GtkApplication) only inside `Run` and makes each window from a default-idle-priority callback on the main loop, so `fontDPI`, called just before `Run`, queues the check as a `G_PRIORITY_HIGH` idle: it runs on the main thread after GTK's own start, before the first window. `TestFontDPIUnset` opens a real WebKitGTK page through `magpie_font_dpi` and needs a display (Xvfb).
- Tests never touch a live agent config. The package's `TestMain` runs under `testenv`'s home of its own. A macOS test that shows windows runs them in a process of its own through `runAppKit` ([`appkit_darwin_test.go`](../../internal/gui/appkit_darwin_test.go)). AppKit and WebKit take their home from the account, not from HOME, so `runAppKit` gives them a temporary one with `CFFIXED_USER_HOME`; appearance, contrast and languages still come from the account. The test fails when that process leaves a folder in the real `~/Library/WebKit` or `~/Library/Caches`. Playwright tests serve `assets/` with isolated `/api` fixtures.
- `POST /api/settings/codex-auto-review` accepts an empty value (Codex's own choice, or the session's model when Codex's sign-in is on a Free plan), `session` (the session's own model when it is one of magpie's; "Same as the session" in the picker), or an exact model/group ID in `provider.Served`, including unlisted providers. A known provider with an unknown or empty model name is rejected without changing the saved reviewer or catalog tag. Generic gateway request resolution remains permissive; it is not the validator for this setting. See `Handler` in [`api.go`](../../internal/gui/api.go) and `TestCodexAutoReviewSetting` in [`codex_auto_review_test.go`](../../internal/gui/codex_auto_review_test.go).
- `Settings -> Sync and backup` and `/api/davsync/{action}` configure and run encrypted WebDAV, S3 or GitHub setup sync. They share the CLI's operations and expose credential presence, never the secrets. State, merge, failure and compatibility behavior are described in [Sync and backup](sync-backup.md).

`openModal` makes the background inert when an editor opens fresh, leaving
confirmations and menus interactive through redraws. `closeModal` releases
that background as soon as the close animation starts, so clicks reach the
page while the editor fades away. Opening an editor during that animation
protects the background again. `ux-safety.test.cjs` checks these transitions.
When closing finishes, focus returns to the opener only if it remains in the
closing modal or on the document body. A control the reader already focused
outside the modal keeps focus; completing an animation must not interrupt new
typing or keyboard navigation. The same rule applies to the Add sheet's focus.
Routing's New group buttons share `newGroup`, which asks before replacing a
dirty group draft; Cancel retains the draft and Discard opens the new editor.
`newGroupWith` applies the same guard when creating a group from a model and
waits for `show` to accept navigation before creating its draft.
The Routing page's head also has Routing groups (`#rtGoGroups`), since the
groups sit below every request. Its click goes through `scrollOnPurpose`,
brings `.rt-gsec` to the top of the view and focuses it; the page's layout is
unchanged. `fitGo` gives it only the head's spare room: named, its arrow
alone, or left out, so nothing else in the head moves or wraps for it
(`routing-groups-jump.test.cjs`).

The Plugins page updates the npm search-results section when a search answers,
leaving suggested plugin cards in place so a click begun before the answer still
opens the intended plugin. `plugin-market.test.cjs` holds the search response
between pointerdown and pointerup to check this in Chromium and WebKit.

### Routing shown by the magpie that serves the gateway

A magpie on this computer that does not serve the gateway shows the serving
one's routing in its Routing page, instead of the notice that its routing
cannot be watched here (#2606). The trigger is `served.Load()` being nil while
a magpie answers at the gateway's address. Then `/api/gateway/trace` reads
the serving magpie's `GET /v1/magpie/trace` over loopback
([`OwnerTrace`](../../internal/gateway/trace.go)) and answers with it as its
own (`mine` true, so the page plays it), and `/api/lanes` reads
`GET /v1/magpie/concurrency` ([`OwnerLanes`](../../internal/gateway/trace.go))
so a provider's rows show the same pills. Both are read on loopback alone
(`ownerURL`): a gateway address on another interface or host is never asked,
whatever `MAGPIE_ADDR` says, and one listening everywhere is read at
127.0.0.1. `OwnerTrace` lets a hung magpie go 5s past the 25s long poll, or
when the page stops asking; `OwnerLanes` waits 700ms, as `ServedBy` does, and
is asked only when a magpie answers. The state read carries the serving
magpie's `serves` and `pid` ([`gatewayJSON`](../../internal/gui/providers.go)),
and the trace carries them as `by`. The Routing page's heading names the
magpie from `by` ("serve (pid 49217) is routing · 127.0.0.1:3525"), so the
name goes with the trace: when the page's trace comes from another magpie
than before (a takeover, either way), it drops the routes it had and reads
the new one's whole trace. Nothing is written; serving and takeover are
unchanged. A
magpie that serves the gateway keeps its own trace, and one older than the
endpoint keeps the notice as before (a refused or unreadable read is unknown,
never empty: `OwnerTrace` says so with its second result). See
`TestConsoleShowsTheServingMagpiesRouting` and
`TestConsoleKeepsTheNoticeFromAnOlderGateway` in
[`trace_peer_test.go`](../../internal/gui/trace_peer_test.go), and
`TestGatewayTraceIsLoopbackOnly` in
[`trace_peer_test.go`](../../internal/gateway/trace_peer_test.go).

### A restart to update hands the gateway over

On a Mac and on Linux, a restart into a downloaded update (the Update pill,
Settings › Version, the tray's Restart to update, Restart automatically)
starts the new version beside the running one and hands it the gateway's
listening socket, rather than ending the gateway and waiting for the new
one to listen again (xuanwo on X). Nothing an agent sends meanwhile is
refused: the port never closes, and requests already in flight finish in
the old process.

The trigger is `handsOver` in
[`handover.go`](../../internal/gui/handover.go): the app (`Run` sets
`handoverHost` and `startExe`), on darwin or linux, serving the gateway
itself, not a dev backend (whose own handover is `gateway.Handover`,
below) and no `MAGPIE_NO_HANDOVER` in the environment. `restartInto` in [`update.go`](../../internal/gui/update.go)
then calls `beginHandover` instead of the relaunch-after-exit. The steps:

1. The old magpie opens an Offer ([`internal/handoff`](../../internal/handoff/handoff.go)):
   a unix socket in a 0700 temporary folder (in `/tmp` when `TMPDIR`'s
   path leaves a socket's name no room, `sockDir`), named by `handover.json` in
   the config folder with its pid, its program path and the time. Then it
   starts the new version beside itself: `open -n` on the bundle on a Mac
   (`update.OpenBeside`), the binary at the path it started from on Linux
   (`update.StartBeside`; `startExe`, since `/proc/self/exe` reads
   "(deleted)" once the update replaced it).
2. The new magpie's `Run` calls `takeOver` before anything else: a pointer
   fresh (2 minutes), of a process still running, for the same program and
   not its own is dialled, the new one says hello, and receives the
   listening socket (SCM_RIGHTS) with the address, the bridge's callback
   socket and the old pid (`handoff.Take`). The peer's uid is checked on
   accept. `gateway.Inherit` keeps the socket for the next `ListenAndServe`
   on the same address (`sameAddr`; another address lets it go), which
   serves on it and then acks.
3. The old one, on the ack (`AwaitAck`), withdraws the pointer, hides
   itself where it can (below), calls `gateway.Server.HandOff`, ends its
   backend and waits for its requests, then quits. `HandOff` closes the
   old one's own copy of the socket at once, so every connection from
   then on waits there for the new one: one the old one took as its end
   began was closed unanswered (#3568). A handed-off server's shutdown
   waits up to `handedOffDrain` (15 minutes) for requests in flight, not
   the 2s an ordinary quit gives them (`shortDrain`), and first lets each
   connection it took send its first request (`unreadWait`, 5s at most),
   since `http.Server.Shutdown` closes one whose request it reads after
   it began. Its `OnShutdown` then skips what goes with the gateway,
   which the new one now does: ending Codex's failover, porting Codex
   rollouts and installing the update.
4. Should the Offer not open, the new version not start, not say hello in
   60s (`helloWait`), not ack in 90s (`ackWait`), or drop the connection,
   the old one keeps the socket, takes its instance lock back and restarts
   as before: the new one opened once it has exited (`fail`,
   `update.Relaunch` or `relaunchBinary`). It first waits, still serving,
   for its requests in flight to finish (at most `fallbackWait`, 15
   minutes), since the restart was let go without waiting for them. A
   second restart click while a handover is under way does nothing
   (`pending`).

Two magpies never show at once. On a Mac the old one lets go of its
instance lock before it gives the socket (`releaseInstance`), so the new
one's `runningAlready` takes it, and once the ack comes the old one hides
its menu bar icon, panel and window and leaves the Dock (`onHandedOver`,
`leaveMenuBar`). AppKit keeps a status item's `setVisible:NO` in the
app's defaults by its autosave name (`NSStatusItem VisibleCC Item-0` on
macOS 26), which the new one's item and every later one start from, so
the hide is forgotten once it is made (`forgetTrayHiddenLater`,
[`trayshown_darwin.go`](../../internal/gui/trayshown_darwin.go)). From
v0.1.1354 to v0.1.1367 it wasn't, and the icon stayed gone after the
update, through a quit and reopen too (Mitsuha, audie). `bringTrayBack`
forgets a kept hide once per computer before the tray is made, and a
magpie handed over to by an older one forgets it again once that one has
quit (`awaitExit`), unless a hide was kept when it started: one kept after
the bring-back is the user's (`TestHandoverLeavesTheMenuBarIconForTheNextMagpie`).
On Linux the D-Bus name Wails holds can't be let go, so the new one serves
the gateway at once but waits for the old pid to end (`awaitExit`, at most
`handedOffWait`, 16 minutes) before it makes its app and tray; the old one
keeps its tray icon until it has drained.

What the old process alone has:

- Sticky routing (which account each conversation stuck to) is flushed to
  disk before the socket is given (`gateway.FlushSticks`).
- Claude Code runs the subscription bridge started send their tool calls
  to the gateway's port, which the new one now serves. The old one serves
  them on a unix socket in the Offer's folder (`ServeCallbacks`), and the
  new one forwards a claude-mcp token it doesn't know there
  (`ForwardCallbacks`, `forwardCallback`; `ownCallback` lets them in while
  forwarding is on). See [Claude subscription bridge](claude-subscription-bridge.md#a-restart-to-update).
- Bridge turns can't move to another process, so a restart that hands
  over waits only for them (`restartBusy`, `blocking`: `Busy.Bridge` as
  Requests, `Tools`, `BridgeLast` as Last). The page is told
  (`updateJSON.Handover`) and says it waits for the Claude subscription's
  replies, with other requests carrying on across the restart.
- Refreshes of rotating OAuth tokens hold a file lock across both
  processes (see [Providers and accounts](providers-accounts.md)).

Not carried over, accepted: each process counts its own requests per
minute and concurrency, so for the drain a provider's limits can be
exceeded by what the old one still has in flight; a bridge run idle
between turns ends with the old process, and the next turn starts a new
run as after any restart; the new one's start-up migrations run while the
old one still serves its last requests.

Windows has no SCM_RIGHTS for a listening socket in this form, and
`magpie web` re-executes itself in place; both restart as before, waiting
for the gateway to be idle (#577). `make dev` keeps its own handover, a
new backend listening beside the old one (`gateway.Handover`, `devBackend`).

See `TestRestartHandsTheGatewayOver` (the new version a second process,
a stream across the restart finishing whole, requests during it answered,
and the old one quitting only after its stream) and
`TestHandoverNotMadeRestartsAsBeforeOnceIdle` in
[`handover_unix_test.go`](../../internal/gui/handover_unix_test.go),
`TestRestartWaitsForWhatAHandoverCantCarry` in
[`handover_test.go`](../../internal/gui/handover_test.go), the gateway's
[`handover_test.go`](../../internal/gateway/handover_test.go), the
socket passing in [`handoff_test.go`](../../internal/handoff/handoff_test.go),
and `update-wait.test.cjs`.

## Verification

The session readers include Cursor CLI chat stores, Cursor editor project
transcripts and older editor `cursorDiskKV` conversations. CLI stores win
when the same session ID has a project transcript. Editor conversations are
read only, including at the delete API, and cannot be resumed by cursor-agent.
Project transcripts contain no message timestamps or billing data; their
existing file modification time supplies the activity date, and tokens and
cost are not inferred. Database bubbles retain their own timestamps.
The editor database is opened as one read-only snapshot per listing, shared
by its chats and removed when the listing ends. Metadata changes invalidate
the cached summary even if the project transcript has not changed. See
[`cursor_editor.go`](../../internal/sessions/cursor_editor.go),
`TestCursorEditorTranscript` and `TestCursorEditorLiveDatabaseAndMetadataRefresh`
in [`cursor_editor_test.go`](../../internal/sessions/cursor_editor_test.go).

Messages without tokens or an active-time estimate still count as sessions
in the overview. Cursor and OpenCode conversations can be opened through
the existing transcript API, including CLI reasoning and tool calls. So can
Kimi Code's main agent, from its `wire.jsonl`: a turn's `turn.prompt` and
loop events from protocol 1.4 on (so a turn still running shows), and the
untimed `context.append_message` records of a session imported from
kimi-cli; Kimi Code's own timed messages repeat those and are skipped
([`transcript_kimi.go`](../../internal/sessions/transcript_kimi.go),
`TestKimiCodeTranscript`). Each reply in the conversation names its model
(#2253), muted after its first part and again where it changes inside a
reply: a part's `model` is what its own line in the agent's file names,
Claude Code's `message.model` (the model that answered; none for its own
`<synthetic>` lines), Codex's `turn_context.model` (the model the turn was
sent to), Pi's and omp's `message.model`, OpenCode's `modelID` or OpenCode
2's `model.id`. Kimi Code (whose model is only on a `usage.record` after
the reply), Cursor and a gateway transcript (which records the model asked
for, not the one that answered) name none, and nothing is guessed in their
place. The Usage page's per-request content doesn't repeat it. See
`TestTranscriptModel*` and `sessions-talk-model.test.cjs`. When the
selected date range is empty but
matching history exists, Usage offers **All sessions**, preserving the agent,
model, folder and search filters. See `sessions-empty-range.test.cjs`.

```sh
go test -tags nogui ./internal/gui
go test -run TestMainWindow ./internal/gui
GOARCH=amd64 CGO_ENABLED=1 go test -run TestMainWindow ./internal/gui  # as for an Intel Mac
go test -run TestWebView2 ./internal/gui   # on Windows: Wails' runtime lookup, missing and present
go test -v ./internal/fonts         # native discovery on each desktop OS
make test-ui                      # every internal/gui/tests/*.test.cjs, Chromium and WebKit
BROWSER=webkit node --test internal/gui/tests/click-scroll.test.cjs
```

`make test-ui` needs Node.js and Playwright (`playwright install chromium
webkit`). Set `NODE_PATH` when Playwright is installed outside the repo. The
Test workflow in CI does not run this suite, so a GUI change must run it
locally and report the result. See [`tests/README.md`](../../internal/gui/tests/README.md).

`TestMainWindowCorners` and `TestMainWindowRightToLeft` run on a Mac with a
desktop session, as CI's macOS job has, and show a few windows for some
seconds. On macOS 26 they check both designs, whatever SDK the toolchain
links against. CI builds them for arm64. Built for amd64 (under Rosetta on
Apple silicon), they check a program that names its SDK in
`LC_VERSION_MIN_MACOSX`, as go test links one for an Intel Mac. Their
presses are sent within the test's own process, and no system setting
changes.

### Agent visibility management

The Agents page's Manage button sits inside the list's footer beside the fold
control, and stays available when nothing is folded. The tray panel keeps its
existing fold control. The Manage agents dialog includes folded and hidden agents.
Search matches names and IDs; select all and invert apply to the search results,
while Select unconnected agents replaces the selection with matching agents not
on magpie (neither wired, added nor drifting). Selection survives search changes.
Hide selected and Unhide selected each save one arrangement through the existing
`POST /api/agents/arrange`, preserving order and agents outside the operation.
Only a successful save changes the list; a failure keeps the selection for retry.
While saving, Done, Escape and the backdrop cannot close the manager and expose
conflicting row actions. Closing is restored on either success or failure.
A completed save invalidates older in-flight state reads for the arrangement
fields only. Fresh reads still accept other windows' visibility changes; bulk
unhide does not add a lasting client-side override. These races are covered by
`agent-visibility-saving.test.cjs`.
While the manager is open, state refreshes update its visibility labels and
action availability in place, preserving the search, selection and keyboard focus.
These operations never connect, disconnect or edit an agent's files. Unhiding
still follows the existing Not set up folding rules. The dialog explains that
restoring visibility does not pin an unconfigured agent into the main list.
See `agent-bulk-visibility.test.cjs` for persistence, partial selection, failed
saves, narrow layouts and all five languages in Chromium and WebKit.

## Jellytool build

### Jellytool download notices

Jellytool reuses [`updater`](../../internal/gui/update.go), its `/api/update`,
`/api/update/check`, `/api/update/notes` and `/api/update/install` routes, SemVer
comparison, translated release notes, header button, version row, and existing
`UpdateSkip` / `NoUpdatePill` preferences. Its separate native entry starts the
same check loop with the app's context and joins it on shutdown. It checks after
startup and, by default, every 24 hours while running; `NoAutoUpdate` disables
scheduled checks, while Settings' Check still works.
An open page reads the local update state every 30 seconds, so a background
result appears without refocusing the window. Routine polls use the cached
result; changing the UI language can refresh the release notes through the
shared language-refresh logic.

In a Jellytool build, `place` never chooses an executable or bundle to replace,
the check stops at `available`, and `install` refuses self-installation. The
download action shows the existing release-notes dialog with Download and Later;
Download opens the feed's HTTPS release page in the system browser. It never
fetches an asset, installs on quit, or restarts the app. Automatic downloading,
restart, channel and mirror controls, and magpie's release history are omitted.
`GET /api/whatsnew` returns no history; `/api/update/notes` uses only the release
already read from the Jellytool feed. A failed check remains an error with the
last good release retained, never an 'up to date' result.

[`update.Feed`](../../internal/update/update.go) uses the build's `JellytoolFeed`,
or `JELLYTOOL_UPDATE_FEED` for a local override. It ignores `MAGPIE_UPDATE_FEED`
and never falls back to usemagpie.ai. An empty feed is `unconfigured`, shown as
such in About without contacting a service. A response uses the existing
Release JSON with `product: "jellytool"`, a SemVer `version`, `notes` (Markdown,
optionally split by `<!-- lang:zh -->`), and an HTTPS `url` without credentials.
`assets` is unnecessary. A response identifying another product or naming an
invalid page is rejected.

[`package.py`](../../build/jellytool/package.py) accepts `--version` and
`--update-feed`; release versions require an HTTPS feed. It stamps the same
version into `main.version` and both plist version fields. Local builds default
to `dev` (plist `0.0.0`) with no feed. [`run.py`](../../build/jellytool/run.py)
passes these options through. Publishing/hosting the JSON and download page is
a separate deployment; no production endpoint is assumed by the packager.

For example, host a JSON file like this at the configured feed URL (the
`downloads.example` addresses below are placeholders):

```json
{
  "product": "jellytool",
  "version": "0.2.0",
  "notes": "- Improved model selection\n<!-- lang:zh -->\n- 改善模型选择",
  "url": "https://downloads.example/jellytool/0.2.0"
}
```

Build the current version against that file, then publish a newer `version`
only after its download page and complete application package are available:

```sh
python3 build/jellytool/package.py --bun /absolute/bun \
  --output /new/Jellytool.app --version 0.1.0 \
  --update-feed https://downloads.example/jellytool/latest.json
```

The offline regression checks are `TestJellytoolUpdate*`,
`TestJellytoolFeedIdentity`, [`jellytool-update.test.cjs`](../../internal/gui/tests/jellytool-update.test.cjs)
(both engines, five languages, wide/narrow), and
[`test_package.py`](../../build/jellytool/test_package.py).

### Jellytool shell and models

The `jellytool` build uses [`RunJellytool`](../../internal/gui/jellytool_app.go),
a separate native window and lifetime, instead of Run. It starts the normal
backend and shares magpie's page scripts and Settings API through
[`jellytoolHandler`](../../internal/gui/jellytool.go), with additional Jellytoken
connection routes and product-specific [page controls](../../internal/gui/assets/jellytool.js).
The current wrapper retains the normal feature set; the earlier restricted
P1 implementation is recorded in [its historical report](../plans/jellytool-plugin-p1.md).
The download-only update behavior above is enforced in the shared updater and
routes, independently of what settings were copied into the product's data root.


Jellytool's recommendation picker lives only in Get started's model step.
Its Recommendations and Selected tabs share one local selection. Each model is
a card with its name, a short suggested use and an upper-right selection mark.
Selected includes picks from the full directory and permits removal. The count,
tab keyboard controls and card focus survive redraws. Each category has one bulk-action
button, labeled Select available models until its permitted models are selected,
then Clear. Clear removes only that category's IDs, retaining other categories
and directory-only choices. Labels are the same for full and partial access,
and keyboard focus stays on the control. Metadata can update the control's label
without replacing a card during an active click. Details/evidence disclosures are
omitted. Locally bundled Phosphor brain/code/lightning line icons and shallow themed
backgrounds distinguish the categories. Card selectors use the app's vector
check mark inside a refined rounded frame, with selected/hover/focus states. Compact cards reserve three description
lines with full text available in the tooltip. Reviewed card summaries come from
the marketplace Basic information, keyed to exact model IDs; records not in that
public source retain neutral task suggestions. Sources and review date are in
[model-card sources](../plans/jellytool-model-card-sources.md). The existing theme
and font apply in both modes.

Browse more models opens Jellytoken's original complete model picker on Providers
in a focused model-only editor. `renderModels` uses its selection-only mode there,
retaining search, bulk selection and the list while leaving connection, serving
modes, model metadata and diagnostic controls in the ordinary supplier editor. `jellytoolProviderEditor` supplies that editor
only for a selection session originating in Get started; a direct supplier visit
keeps its usual editor. Back to recommendations carries all unsaved picks back,
including the current tab. Reopening the directory uses that same selection.
Save selection commits the complete set once and waits for authoritative provider
and state reads before closing, so a quick reopening uses the saved picks. Late
price metadata updates do not replace an active card; the next user action uses
the updated ordering. Save returns to Get started, locates the collapsed model
step near the top of the view and focuses Choose models. Both recommendation and
directory saves do this after the saved state has been read. The model step
shows saved model names in wrapping labels, including directory-only choices;
background reads update this list even when the selection count stays the same.
Failed saves retain the draft for retry and leave the saved labels unchanged.
Cancel discards the session and returns to the saved
choices, using the existing discard confirmation when the directory is dirty.
`jellytoolCancelModels` handles that editor's Cancel/Escape/backdrop paths. Direct
supplier saves continue to stay on Providers. Navigation alone never saves or
edits an Agent.

`GET /api/jellytool/models` supplies the current authorized catalog and price
metadata. GPT-6 Astra/Luna and GPT-6.1 Sol/Terra are the default fixed recommendations: Astra
uses complex problems, Sol everyday work and coding, and both Terra and Luna
simple tasks. Each carries a Recommended badge even when the current key has
no access. Key access is determined only by exact IDs in the private plugin's
`GET /api/jellytool/models` catalog response. The supplier editor's model array
is not an access source: it also includes saved IDs absent from the vendor list.
Selection waits for the catalog read. A failed read or `catalogError` leaves
access unknown and asks the user to refresh, rather than saying it is denied.
Clicking an unavailable card shows a toast asking the user to enable model
access in Jellytoken. Bulk selection skips unavailable IDs with the same toast;
its labels are always Select available models and Clear, so partial access
never claims every displayed card is selected.
Displaying a recommendation grants no access, and saving still validates the
complete selection against the authorized catalog. Recommendation badges use
an opaque accent background, contrasting text and a star.

The [configuration reader](../../internal/gui/jellytool_model_config.go) uses default results stored in [`jellytool-models.json`](../../internal/gui/assets/jellytool-models.json).
On first startup, Jellytool copies them atomically to `model-recommendations.json`
in `appdir.Config()`; existing files, even malformed ones, are never replaced.
`GET /jellytool-model-config.js` validates and serves that file without caching,
before `jellytool.js`. Reloading the page reads edits. Invalid JSON, unknown
fields/tiers and duplicate model IDs produce a visible configuration error,
without falling back over the user's changes. This file controls display alone.

Each configured `tiers.complex` / `daily` / `simple` array replaces that tier's
calculated result in the given order. Entries accept exact `id`, optional `name`,
`recommended` and `alwaysShow`. `alwaysShow` displays a card without catalog
access; selecting it still needs access. An empty array hides that tier's cards;
omitting the tier restores automatic calculation. Configured IDs are removed
from automatically calculated tiers, allowing a model to be moved without
appearing twice. Automatic matching covers GPT, GLM, Qwen, Doubao Seed, Kimi and
DeepSeek, retains both GPT simple roles, and excludes MiniMax. The shipped file
contains the current calculated baseline with GPT's corrected exact IDs and
DeepSeek V4 Pro `-0813` / V4.1 Flash `-1017`. Automatic DeepSeek matching compares
numeric model versions first, then prefers a dated `-MMDD` variant over an
undated one and the latest date within that version. Explicit configured IDs
remain pinned, including older dates.
Updates neither change saved selections nor edit Agents. User editing details:
[recommendation configuration](../plans/jellytool-model-recommendations.md).
See [P2 behavior and verification](../plans/jellytool-plugin-p2.md).


The Jellytool native window and bundle ICNS/Dock use the user-selected A2
soft-matte artwork in `assets/icons/jellytool-app.png`, supplied by
`jellytoolAppIcon` and converted offline by the packager with macOS tools.
The page brand, favicon, supplier entry and tray glyph source retain the
Jellytoken website icon in `assets/icons/jellytoken.png`.
Sources and resolutions are recorded in
[ICON-SOURCE.md](../../build/jellytool/ICON-SOURCE.md). Jellytool's macOS window
keeps its native title bar. Its sidebar brand is a compact, left-aligned header
sharing the navigation icons' left edge, rather than reserving space for traffic
lights a second time. The existing 22px icon keeps its proportions, and the
wordmark fits even at the narrowest saved sidebar width. The existing sidebar
toggle sits to the right of the wordmark while expanded, returning to the main
toolbar when collapsed. `jellytoolSideToggle` moves the same button, retaining
its translated label, shortcut, saved layout and keyboard focus. The placement
and presentation are supplied by Jellytool; ordinary magpie branding and toggle
placement are unchanged. WorkBuddy model-list POSTs on the reused Agent page explicitly sync
only that connected adapter, since Jellytool does not run background Agent sync.

In Jellytool, the Providers list pins Jellytoken first within its on/off group.
The Add sheet places From plugins first when Jellytoken is present, with
Jellytoken ahead of the other plugins, including while searching. These are
display priorities; the saved routing order is unchanged. Its supplier entries
and editor use the same local PNG as the application brand.


### Local Jellytool launches

[`build/jellytool/run.py`](../../build/jellytool/run.py) packages and launches the
current checkout in one command: `python3 build/jellytool/run.py`. It selects a
working Go from PATH or local mise installations, and Bun 1.4.2 from previous
Jellytool bundles or PATH (`--go` and `--bun` override discovery). It downloads
no runtime and changes no global tool configuration. Each invocation builds a
fresh staging bundle under `~/.cache/jellytool-build/artifacts/`, copies the
completed bundle to `~/Applications/Jellytool.app`, and launches that fixed
copy. Only after `package.py` succeeds does it install and call `launch.py`, so
a failed build leaves the running application alone. Packaging, installation
and launch failures stop the command; launch failures retain the launcher's
diagnostics and retry limits below.

[`build/jellytool/launch.py`](../../build/jellytool/launch.py) replaces a local
macOS build without asking `open -n` for a second instance. A per-data-root
launcher lock rejects competing launchers. The launcher validates the bundle
executable and instance lock before stopping previous instances, then ends
instances with the same bundle ID and Jellytool processes with that exact lock
file open (including another bundle ID). An unrelated process holding the file
is reported, never terminated. Its default lock location follows
[`appdir.Config`/`Resolve`](../../internal/appdir/appdir.go), including portable
data and ignored relative XDG paths; `--lock` overrides the inspected path.
The app receives the launcher's HOME and configured XDG paths explicitly.

LaunchServices registering a new PID is not readiness. Success requires the
requested bundle's new PID to be alive, AppKit's `finishedLaunching` flag to be
true and the instance lock to be held, continuously for one second. This checks
native startup, not a model request or frontend rendering. Output goes to
`jellytool-instance.launch.log` beside the instance lock. A startup exit fails
with that output, a startup timeout stops the new process, and neither causes
an automatic application relaunch. Only `open` returning LaunchServices error
`-600` is retried, at most three attempts. An unreadable lock or process inventory
is an error, never evidence that nothing is running. Dry runs write nothing
and stop no processes. The application and updater restart path are unchanged.

Offline regression checks use temporary homes, unique bundle IDs and native
AppKit fixtures, including startup failure, timeout, replacement, SIGKILL,
another bundle ID sharing the lock, competing launchers and preview behavior:

```sh
python3 -B -m unittest discover -s build/jellytool -v
```

### Jellytool menu bar and supplier guidance

From the 2026-11-09 development build, Jellytool creates its status-bar icon
from the same website PNG in `setupJellytoolTray`: a padded monochrome alpha
glyph is used as a macOS template image and as a theme-coloured Linux glyph,
while Windows keeps the full-colour application icon. Its menu reuses the
existing language selection and provides Open, Version and Quit, without the
ordinary magpie quick panel, updater or handover. Closing or hiding the main
window leaves the gateway and plugin running. A primary click opens and focuses
the main interface, while the secondary click opens the private menu. The
menu's Open and the Dock also reopen that window; Quit shuts down the owned
runtime and listener. Display language saves also relabel this private menu. The native
`TestJellytoolTrayKeepsWindowAvailable` runs under an isolated AppKit home and
checks a real status-bar button, close-to-hide and reopening.

The supplier page has a three-step guide keyed to the saved supplier state:
add a key, select text models and Save, then connect WorkBuddy on the Agents
page. Its help stays expanded across provider refreshes. The editor explains
key replacement and Save/Cancel. The recommendation picker on Get started
links to the supplier directory when more models are needed. These controls reuse the original sign-in, draft, discard and Agent
navigation flows, and never connect an Agent or submit a Key by themselves.
The empty supplier summary says that no models are selected instead of
claiming usage-only support. Five-language browser checks cover the guidance,
actions, empty states and draft behavior in Chromium and WebKit.

[`jellytool-guide.md`](../../internal/gui/assets/jellytool-guide.md) is the
bundled Chinese usage document. The packager copies it into Resources and
beside the app as `使用说明.md`, so an offline build carries its instructions.

### Jellytoken connection guidance and settings

The permanent Get started page's provider step is extended by
[`jellytool.js`](../../internal/gui/assets/jellytool.js) only in a Jellytool
build: a link to `https://www.jellytoken.com/api-keys`, a masked key field and
Verify and connect. This uses `POST /api/jellytool/key`; a successful key read
completes provider access and offers model selection without selecting models
or connecting an Agent for a first key with no previous provider choice. Legacy
keys retain their implicit vendor-list mode, and saved selections survive key
replacement or reconnection (`TestJellytoolKeyPreservesExistingModelChoices`).
Once connected, key replacement and endpoint editing share one Connection
settings action; the website key link remains available. The connection
editor defaults to Responses for new connections and Restore default, while
preserving an explicitly saved protocol. See the [plugin connection contract](provider-plugins.md#jellytools-offline-plugin-contract-p0).
The model-selection step expands its three recommendation categories in place: complex problems,
everyday work and coding, and simple tasks. The expanded picker reuses
`jellyModelPanel` with its own local draft, preserving saved choices. All three
categories remain expanded in the page; individual and category selection only
change that draft. Choose from All models transfers it to the supplier directory. Cancel drops it; Save calls
the existing supplier Save endpoint with only model choices, then rereads setup
progress. A failed Save retains the draft for retry, and background setup reads
retain the expanded picker. Connection settings remain a separate action.

Jellytool's permanent checklist reuses the app's system font, accent, theme
tokens and existing check icons. `jellytoolQuickstart` arranges explanation and
actions in the same two columns for each configuration step. They stack below
700px or when a saved sidebar leaves insufficient content width.
The model action toggles Choose models / Collapse models and `aria-expanded`.
Collapsing retains the draft and active tab; reopening resumes the same model
selection. Cancel and Save end the selection session. The three configuration steps
stay visible; only the first unfinished configuration step has a primary action,
and a completed page keeps
all configuration actions. Configured steps retain their original numbers and
show a separate green status dot; configuration actions remain available for
editing. The model picker's Browse more models action keeps its full label and
arrow, with its own row on narrow windows. The key has a visible label, Enter submission,
inline error and busy states. Unknown provider state shows a matching skeleton;
a failed read exposes an explicit retry. Identical background reads retain the
existing guide DOM so typing, IME composition, selection and visibility survive.
Meaningful progress/language changes redraw it, restoring a focused key field.
A fourth, permanent **Use models in your agent** step shows usage tabs only for
currently wired Agents, defaulting to the first connected Agent. Installed but
unconnected Agents have no tab. Without a connected Agent, the step asks the
reader to complete step 3; a failed setup read is shown as unknown. Tabs wrap
on narrow windows and support arrow keys, Home and End. It reuses `PICKS_IN`
and `NO_PICKER` from the Agent page for the
selection method, with Claude tier mapping and Codex CLI/app guidance. Each guide
explains reopening, selecting a model and starting a conversation, plus a
collapsible missing-model checklist. Switching guides only changes local display
state: it neither edits Agent configuration nor submits a request. Reading the
guide does not assert successful model usage; the progress label explicitly
counts the three configuration checks. Selection, expanded help and tab
focus survive meaningful configuration redraws. Disconnecting the selected Agent
falls back to the first remaining connected Agent; disconnecting all removes
the tabs and instructions. These presentation hooks apply only to Jellytool's permanent page, not the tray
card or ordinary magpie. Five-language checks cover both themes and engines,
wide/narrow layouts, contrast, focus, validation and completion states.

A fifth **Image generation setup (optional)** step explains replacing an Agent's
image model through the image MCP tool. It links to Settings → Models → Image
generation, where choosing a model instead of Off enables generation. Its explicit
Check action reads `/api/library?glance=1` without changing configuration. The
result distinguishes installed, unassigned, found-in-Agent and missing servers;
it recognizes the current or legacy product's `mcp image` command regardless of
the saved server name. Failed reads remain unknown with retry, and failed Agent
writes are reported rather than counted as configured. If the Library is off,
the action offers its existing enable operation. Installation and Agent selection
reuse the Library's MCP tab and market card; no duplicate server is installed by
the check. The guide explains reopening the Agent, enabling MCP (including
WorkBuddy's manual approval), an explicit sample prompt with Copy, project output,
image editing and `generation_result` polling. Size guidance explains passing
`size` to `generate_image` as an aspect ratio or `WIDTHxHEIGHT` pixels, the
model-dependent supported sizes and the default when omitted. Separate copyable
prompts show a ratio, an explicit pixel size and a natural-language model
selection. A ratio alone promises no exact pixel resolution. Naming a model in a
prompt passes it to `generate_image.model` for that call only; omitting it keeps
the saved Settings default. Neither this optional step nor
reading its instructions contributes to the three required setup checks. See
`jellytool-connection.test.cjs` for both engines and all five languages.

The supplier editor keeps its existing models, draft, discard and Save behavior.
Its Jellytoken connection section (also used before first sign-in) has a masked
replacement key field, editable Base URL, five protocol choices, Restore default,
a trial-model field, Detect APIs and Each picked model. `GET /api/jellytool/config`
reads settings; `POST /api/jellytool/detect` probes the current draft; Use these
changes only the draft. Save calls `POST /api/jellytool/config` to validate and
commit connection changes before the existing supplier Save. Blank keys retain
the stored key, and a validation failure leaves that connection unchanged and
keeps the editor open. Reading initial values cannot overwrite typing or make an
untouched draft dirty. The original magpie guide/editor uses none of these hooks.
Provider/plugin persistence, protocol selection and probe limits are described
in [the private plugin contract](provider-plugins.md#jellytools-offline-plugin-contract-p0).
See `jellytool-connection.test.cjs` and `jellytool-providers.test.cjs` for both
browser engines, all five languages and narrow/wide windows.

### Jellytool display branding

Build-time [`product.Name`, `ID`, `Website` and `Text`](../../internal/product/text.go)
give Jellytool its display name, command spelling and website. `Text` applies
only to application-owned prose templates before their user values are formatted.
The ordinary build retains the original text. The page's `t` and `tNotice` use
[`productText`](../../internal/gui/assets/i18n.js) on registered translation
templates before substituting names, commands or paths; third-party text that
has no registered key is shown as received. This covers built-in plugins too,
without changing their translation keys or `window.magpie` API.

Package names, URLs, headers, protocol paths, config identifiers, references to
remote magpie, the upstream community and existing magpie entries keep their
spelling. The Library's MCP hub remains named `magpie`. About links to Jellytoken
as the product website. Its existing Open-source licenses row says "Based on
magpie", without a separate upstream link or community row. View opens the
bundled notices, including magpie's original copyright and complete MIT license;
the app bundle also retains the unchanged root `LICENSE`. `test_package.py`
checks both copies and unchanged third-party statements. The home-screen metadata
and manifest use the Jellytool name and icon. Its bundled PNG joins the existing
home-screen asset whitelist for cookie-free GET/HEAD; API requests still require
the page's key.
`/index.html` serves the same branded page as `/`.
The bundled notices change only their product introduction; original copyright
and license sections are preserved.

[`jellytool-branding.test.cjs`](../../internal/gui/tests/jellytool-branding.test.cjs)
checks translated templates, literal user values, retained protocol and upstream
names, and the Agents/Settings/About pages in both engines, five languages and
wide/narrow windows. `TestJellytoolPage` covers the page aliases and manifest.
