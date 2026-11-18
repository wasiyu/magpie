// The Get started card (the panel's, wangye1644294 on X) and the window's
// Get started page: three steps, each ticked from what is so. app.js imports
// this as either is first drawn (guideJS), and keeps guideProgress, guideShown
// and guideRedraw, which every start reads; what is here replaces the stubs
// it has under the same names, so the core carries only those.

window.refreshQuickstart = async function refreshQuickstart(freshState) {
  const read = guideRead = { pending: true }, before = state, list = providers;
  renderQuickstart();
  try {
    const [s, p] = await Promise.all([freshState || api("state"), api("providers")]);
    if (guideRead !== read || view !== "quickstart" || state !== before || providers !== list) return;
    state = s; providers = p;
    applyPrefs(state.settings, state.fx);
    renderSideGw(p.gateway);
    renderAgents();
  } catch {
    if (guideRead === read && state === before && providers === list) read.error = true;
  } finally {
    if (guideRead === read) { read.pending = false; renderQuickstart(); }
  }
};

window.guideStep = function guideStep(n, done, title, say) {
  const li = el("li", "guide-step" + (done ? " done" : ""));
  const tick = el("span", "guide-tick");
  tick.setAttribute("aria-hidden", "true");
  if (window.jellytoolGuide) {
    tick.append(el("span", "guide-number", String(n)));
    if (done) tick.append(el("span", "guide-done-mark"));
  } else if (done) tick.append(svg(CHECK, 12, 2));
  else tick.textContent = String(n);
  const body = el("div", "guide-body");
  const h = el("b", "guide-title", title);
  body.append(h);
  if (say) body.append(el("span", "guide-say", say));
  li.append(tick, body);
  li.setAttribute("aria-label", (done ? t("Done") + ": " : "") + title);
  return { li, body };
};

window.guideCard = function guideCard(g, persistent = false) {
  const card = el("section", "guide");
  card.setAttribute("aria-label", t("Get started"));
  const head = el("div", "guide-head");
  const steps = [g.source, g.models, g.agent];
  head.append(el("b", "guide-h", t("Get started")), el("span", "guide-n", g.unknown ? "" : t("{n} of {m} done", { n: steps.filter(Boolean).length, m: steps.length })));
  if (!persistent) {
    const x = el("button", "guide-x");
    x.type = "button";
    x.title = t("Hide Get started");
    x.setAttribute("aria-label", x.title);
    x.append(svg(GUIDE_X, 12, 1.6));
    x.disabled = guideBusy === "hide";
    x.onclick = (e) => { e.stopPropagation(); hideGuide(); };
    head.append(x);
  }
  card.append(head);
  const list = el("ol", "guide-steps");

  // 1: a provider with credentials, independently of its model selection
  const one = guideStep(1, g.source && !g.unknown, t("Connect a provider"),
    g.ready.length ? g.ready.map((p) => p.name).join(", ") : t("A subscription you already pay for, a provider, or an API key"));
  one.li.dataset.step = "source";
  if (window.jellytoolGuide) {
    one.body.append(window.jellytoolGuide(g));
  } else if (persistent || !g.source) {
    const add = el("button", "text primary guide-go", t(g.source ? "Open Providers" : "Add a provider"));
    add.type = "button";
    add.onclick = (e) => {
      e.stopPropagation();
      if (mode === "window") g.source ? show("providers") : addProviderFromAgents();
      else api("window/main?view=providers", {}).catch(statusErr);
    };
    one.body.append(add);
  }
  list.append(one.li);

  // 2: choose the models the provider shares with agents
  const two = guideStep(2, g.models && !g.unknown, t("Model selection"),
    g.models ? t("{n} models selected", { n: state.served }) : t("Open a provider, choose the models to share, then Save."));
  two.li.dataset.step = "models";
  const choose = el("button", "text primary guide-go", t("Choose models"));
  choose.type = "button";
  choose.onclick = () => {
    if (window.jellytoolModels) window.jellytoolModels();
    else if (mode !== "window") api("window/main?view=providers", {}).catch((err) => status(err.message, "err"));
    else if (!g.source) addProviderFromAgents();
    else if (g.ready.length === 1) openProvider(g.ready[0].id);
    else show("providers");
  };
  two.body.append(choose);
  list.append(two.li);

  // 3: an agent, those found here named, each connected in a click
  const found = state.agents.filter((a) => connectKind(a) === "ok" || connectKind(a) === "empty");
  const three = guideStep(3, g.agent && !g.unknown, t("Connect an agent"),
    g.agent ? state.agents.filter((a) => a.wired).map((a) => a.name).join(", ") : found.length ? t("magpie found these on this computer. Click one to connect it:") : t("Install Claude Code, Codex, Gemini CLI, OpenCode… and magpie will list them here."));
  three.li.dataset.step = "agent";
  if (found.length) {
    const chips = el("div", "guide-agents");
    for (const a of found) {
      const b = el("button", "guide-agent");
      b.type = "button";
      b.dataset.id = a.id;
      b.append(icon(a.icon), el("span", "", a.name));
      const ready = connectKind(a) === "ok";
      const connected = !!a.wired;
      b.disabled = !ready || !!guideBusy;
      b.classList.toggle("connected", connected);
      b.classList.toggle("busy", guideBusy === a.id);
      b.title = connected ? t("Connected · switch off to put back what {agent} had before magpie", { agent: a.name }) : ready ? t("Switch on and {agent}'s model list gets magpie's models", { agent: a.name }) : t("Add a key or a subscription first; then there are models to connect");
      b.onclick = (e) => { e.stopPropagation(); connected ? askDisconnect(a) : guideConnect(a, e); };
      chips.append(b);
    }
    three.body.append(chips);
  }
  if (persistent) {
    const open = el("button", "text guide-open", t("Open Agents"));
    open.type = "button";
    open.onclick = () => show("agents");
    three.body.append(open);
  }
  list.append(three.li);
  card.append(list);
  return card;
};

window.renderQuickstart = function renderQuickstart() {
  const page = $("#quickstart");
  if (!page || mode !== "window" || !state) return;
  const g = guideProgress();
  if (!window.jellytoolKeepQuickstart?.(page, g)) {
    const focused = page.contains(document.activeElement) ? document.activeElement : null;
    page.replaceChildren(guideCard(g, true));
    window.jellytoolQuickstart?.(page, g, focused);
  }
  page.setAttribute("aria-busy", String(!!guideRead?.pending));
  page.querySelector(".guide-check")?.remove();
  if (guideRead?.pending || g.unknown) {
    const note = el("div", "guide-say guide-check", t(g.unknown ? "Could not read your setup. Try again." : "Checking your setup…"));
    note.setAttribute("role", g.unknown ? "alert" : "status");
    if (g.unknown) {
      const retry = el("button", "text", t("Try again"));
      retry.onclick = () => refreshQuickstart(); note.append(retry);
    }
    page.prepend(note);
  }
};

async function guideConnect(a, ev) {
  if (guideBusy) return;
  guideBusy = a.id;
  guideRedraw();
  try {
    state = await api("agents/connect/" + a.id, {});
    guideBusy = "";
    renderAgents();
    const msg = connectedSaid(a, state.connected);
    saidOk(msg, 7000);
  } catch (err) {
    guideBusy = "";
    guideRedraw();
    // every model taken out of its list: its list opens at its chip, as
    // at the row's switch (connectSwitch)
    const chip = err.code === "all_hidden" && document.querySelector(`.guide-agent[data-id="${CSS.escape(a.id)}"]`);
    if (chip) {
      noneLeft(a);
      openAgentModels(a, chip, ev);
      return;
    }
    status(t(err.message), "err");
  }
}

async function hideGuide() {
  if (guideBusy) return;
  guideBusy = "hide";
  guideRedraw();
  try {
    prefs = await writingPrefs(api("settings/guide", { hidden: true }));
    state.settings = prefs;
    guideBusy = "";
    renderAgents();
    status(t("Get started hidden; Settings › General brings it back"), "ok");
  } catch (err) {
    guideBusy = "";
    guideRedraw();
    status(t(err.message), "err");
  }
}
