// Keep the full magpie UI and behavior in the standalone Jellytool build.
// The product wrapper supplies its branding and supplier display priority.
(() => {
  if (!window.bootPrefs?.jellytool) return;
  document.title = "Jellytool";
  const brand = document.querySelector(".brand > span:last-child");
  if (brand) brand.textContent = "Jellytool";
  pngIcons.add("jellytoken");
  // Reuse the same control, labels, shortcut and saved layout in both places.
  window.jellytoolSideToggle = (b) => {
    const host = sideNav() ? $(".side-top") : $(".top");
    if (b.parentElement === host) return;
    const focused = document.activeElement === b;
    if (sideNav()) host.append(b); else $(".brand").after(b);
    if (focused) b.focus({ preventScroll: true });
  };
  renderSideToggle();
  fitTop();
  // Display priority leaves the saved routing order intact. The shared page
  // redraws these lists, so observe their containers instead of adding a
  // Jellytool branch to the core renderer.
  const pin = () => {
    for (const list of document.querySelectorAll("#providers, #offProviders")) {
      const row = list.querySelector('.row.provider[data-id="jellytoken"]');
      if (row && list.firstElementChild !== row) list.prepend(row);
    }
    const tiles = document.querySelector("#addSheet .tiles");
    const supplier = tiles?.querySelector('.tile[data-pick="Jellytoken"]');
    if (!supplier) return;
    const grid = supplier.parentElement, heading = grid.previousElementSibling;
    const signing = grid.nextElementSibling?.classList.contains("signing") ? grid.nextElementSibling : null;
    if (grid.firstElementChild !== supplier) grid.prepend(supplier);
    if (tiles.firstElementChild !== heading) tiles.prepend(...[heading, grid, signing].filter(Boolean));
  };
  for (const node of [document.querySelector("#providers"), document.querySelector("#addSheet")]) {
    if (node) new MutationObserver(pin).observe(node, { childList: true, subtree: true });
  }
  pin();

  const keySite = () => api("open", { url: "https://www.jellytoken.com/api-keys" }).catch((e) => status(e.message, "err"));
  const defaultBase = "https://aiservice.jellytoken.com/v1";
  let connection = null, guideKey = "", guideMessage = "", pendingConnection = null;
  let guideKeyVisible = false, guideChecking = false, guideError = "", guideReplace = false;
  let guideRevision = 0, guideRendered = "";
  const connectionWarning = () => (connection?.warnings || []).map((message) => t(message)).join(" ");
  const config = () => api("jellytool/config").then((c) => (connection = c));
  // Only a user edit enters the supplier draft. Reading the saved connection
  // asynchronously must not make an untouched editor dirty or overwrite typing.
  function connectionFields(owner, picked = () => []) {
    const box = el("div", "jelly-connection");
    const protocols = el("div", "segs base-apis");
    const url = input("", defaultBase, "url");
    url.classList.add("jelly-base-url");
    const key = input(owner.jellytoken?.key || "", t("Paste your Jellytoken API key"), "password");
    key.setAttribute("aria-label", t("{name} API key", { name: "Jellytoken" }));
    key.autocomplete = "off";
    key.classList.add("jelly-key");
    const eye = el("button", "text", t("Show"));
    eye.type = "button";
    eye.onclick = () => { key.type = key.type === "password" ? "text" : "password"; eye.textContent = t(key.type === "password" ? "Show" : "Hide"); };
    const site = el("button", "text action", t("Create a Jellytoken API key ↗"));
    site.type = "button"; site.onclick = keySite;
    const pair = el("div", "pair"); pair.append(key, eye);
    let current = { baseURL: defaultBase, api: "responses", ...connection }, edited = !!owner.jellytoken;
    Object.assign(current, owner.jellytoken);
    const stage = () => { edited = true; owner.jellytoken = { baseURL: url.value.trim(), api: current.api, ...(key.value.trim() ? { key: key.value.trim() } : {}) }; };
    const paint = () => {
      url.value = current.baseURL;
      for (const b of protocols.children) { const on = b.dataset.api === current.api; b.classList.toggle("on", on); b.setAttribute("aria-pressed", String(on)); }
    };
    for (const id of Object.keys(API_NAMES)) {
      const b = el("button", "opt", t(API_NAMES[id]));
      b.type = "button"; b.dataset.api = id;
      b.onclick = () => { current.baseURL = respellURL(url.value || defaultBase, id); current.api = id; paint(); stage(); };
      protocols.append(b);
    }
    url.oninput = () => { current.baseURL = url.value; stage(); };
    key.oninput = stage;
    key.onkeydown = (e) => e.stopPropagation();
    const reset = el("button", "text action", t("Restore default"));
    reset.type = "button"; reset.onclick = () => { current = { baseURL: defaultBase, api: "responses" }; paint(); stage(); };
    const model = input("", t("model to try · empty picks one from the vendor's list"));
    model.classList.add("detect-model");
    const output = el("div", "eps jelly-detect-out");
    output.setAttribute("aria-live", "polite");
    const detect = el("button", "text action", t("Detect APIs"));
    const each = el("button", "text action detect-each", t("Each picked model"));
    detect.title = t("Send the smallest request to each API (OpenAI chat completions, Responses, Anthropic messages, Gemini generateContent) at this URL, to see which answer");
    each.title = t("Ask every model picked below on each API, to see which API serves which model (up to {n}, a few at a time)", { n: 24 });
    detect.type = each.type = "button";
    const ask = async (all) => {
      const ids = all ? picked() : model.value.trim() ? [model.value.trim()] : [];
      if (all && !ids.length) { output.replaceChildren(el("span", "hint", t("Pick a model first."))); return; }
      detect.disabled = each.disabled = true;
      output.replaceChildren(el("span", "hint", t("Checking Jellytoken APIs…")));
      try {
        const data = await api("jellytool/detect", { baseURL: url.value.trim(), api: current.api, ...(key.value.trim() ? { key: key.value.trim() } : {}), models: ids });
        output.replaceChildren();
        for (const r of data.results || []) {
          const row = el("div", "ep");
          row.append(el("span", "pl", t(API_NAMES[r.protocol])), el("code", "", r.model), el("span", "res " + (r.ok ? "ok" : "bad"), r.ok ? "✓ " + t("Available") : "× " + (r.error || r.status)));
          output.append(row);
        }
        const good = (data.results || []).filter((r) => r.ok);
        const tested = [...new Set((data.results || []).map((r) => r.model))];
        const common = good.filter((r) => tested.every((model) => good.some((other) => other.model === model && other.protocol === r.protocol)));
        if (common.length) {
          const apply = el("button", "text action jelly-apply", t("Use these"));
          apply.type = "button";
          apply.onclick = () => {
            const selected = common.find((r) => r.protocol === current.api) || common[0];
            current = { baseURL: selected.base, api: selected.protocol }; paint(); stage();
            apply.disabled = true; apply.textContent = t("Taken · save to keep");
          };
          output.append(apply);
        }
      } catch (e) { output.replaceChildren(el("span", "hint err", e.message)); }
      finally { detect.disabled = each.disabled = false; }
    };
    detect.onclick = () => ask(false); each.onclick = () => ask(true);
    const actions = el("div", "jelly-connection-actions"); actions.append(reset, detect, each);
    const note = el("div", "hint", t("The endpoint and protocol are used by Jellytool's bundled Jellytoken plugin. Save applies them without changing your key."));
    box.append(site, el("label", "", t("API key")), pair, el("span", "hint", t("Leave the key blank to keep the saved key. A replacement is verified before it is saved.")), el("label", "", "Base URL"), protocols, url, actions, model, output, note);
    paint();
    config().then((c) => { if (!edited && !owner.jellytoken) { current = { ...c }; paint(); } }).catch((e) => output.replaceChildren(el("span", "hint err", e.message)));
    return box;
  }

  async function openConnection() {
    if (!await show("providers")) return;
    if (!providers) await loadProviders();
    const p = providers.providers.find((p) => p.id === "jellytoken");
    if (p) return openProvider(p.id);
    const own = { chosen: [], headers: [] }, ed = el("div", "editor jelly-connect-editor");
    const head = el("div", "ehead"); head.append(icon("jellytoken"), el("b", "", t("Connection settings")));
    ed.append(head, ...field("Jellytoken", connectionFields(own)));
    const bar = el("div", "bar"), error = el("div", "hint err jelly-connect-error");
    error.setAttribute("role", "alert");
    const cancel = el("button", "text", t("Cancel")); cancel.onclick = cancelEdit;
    const save = el("button", "text primary", t("Verify and connect"));
    save.onclick = async () => {
      saving(save, t("Verifying the key and loading models…"));
      try {
        connection = await api("jellytool/config", { ...own.jellytoken });
        if (connectionWarning()) status(connectionWarning());
        pendingConnection = null; editing = null; draft = null; adding = false;
        closeModal(); await loadProviders(); state = await api("state"); renderAgents(); await show("quickstart");
      } catch (e) { error.textContent = e.message; doneSaving(save); }
    };
    bar.append(cancel, save); ed.append(error, bar);
    pendingConnection = { owner: own, editor: ed };
    draft = own; providerDraftRef = own; providerDraftBase = providerDraftValue();
    renderProviders();
  }

  window.jellytoolPendingConnection = () => {
    if (!pendingConnection) return null;
    editing = "jellytoken:connect"; adding = false; draft = pendingConnection.owner;
    $("#addSheet").hidden = true;
    return pendingConnection.editor;
  };
  window.jellytoolCancelConnection = () => { pendingConnection = null; };
  window.jellytoolConnection = (p) => field(t("Connection settings"), connectionFields(draft, () => chosenIds()), "");
  window.jellytoolOpenConnection = openConnection;
  window.jellytoolBeforeSave = async (owner) => {
    if (!owner?.jellytoken) return;
    connection = await api("jellytool/config", owner.jellytoken);
    if (connectionWarning()) status(connectionWarning());
  };

  // Recommendations follow the product tiers; GPT roles are fixed product picks.
  const newer = (a, b) => {
    const av = a.version, bv = b.version;
    for (let i = 0; i < Math.max(av.length, bv.length); i++) if ((av[i] || 0) !== (bv[i] || 0)) return (av[i] || 0) > (bv[i] || 0) ? a : b;
    if ((a.date || 0) !== (b.date || 0)) return (a.date || 0) > (b.date || 0) ? a : b;
    return a.id.localeCompare(b.id) <= 0 ? a : b;
  };
  const newest = (items, key) => {
    const out = new Map();
    for (const item of items) {
      const k = key(item);
      if (!k) continue;
      const old = out.get(k);
      out.set(k, old ? newer(old, item) : item);
    }
    return [...out.values()];
  };
  const recommendationTiers = (models, priceByID = new Map(), config = window.jellytoolModelConfig) => {
    if (window.jellytoolModelConfigError) return { complex: [], daily: [], simple: [] };
    const text = models.filter((m) => (m.outputs || ["text"]).includes("text") && !/^claude[-_]/i.test(m.id) && !/(?:mock|test|example)/i.test(m.id));
    const catalog = (pattern) => newest(text.flatMap((m) => {
      const match = m.id.match(pattern);
      return match ? [{ ...m, version: (match[1] || "0").split(/[.-]/).map(Number), role: (match[2] || "base").toLowerCase() + (match[3] ? "-highspeed" : "") }] : [];
    }), (m) => m.role);
    const sameVersion = (a, b) => a.version.every((v, i) => v === (b.version[i] || 0)) && b.version.every((v, i) => v === (a.version[i] || 0));
    const latest = (items) => items.length ? items.filter((m) => sameVersion(m, items.reduce(newer))) : [];
    const complex = [], daily = [], simple = [];
    const addFlash = (models, middle) => (middle.length ? simple : daily).push(...latest(models));

    const gpt = catalog(/^gpt-(\d+(?:\.\d+)*)-(astra|sol|terra|luna)$/i);
    complex.push(...gpt.filter((m) => m.role === "astra"));
    daily.push(...gpt.filter((m) => m.role === "sol"));
    simple.push(...gpt.filter((m) => ["terra", "luna"].includes(m.role)));

    const glm = catalog(/^glm-(\d+(?:\.\d+)*)(?:-(prime|air|flash|flashx))?$/i);
    const glmMiddle = glm.filter((m) => m.role === "air");
    complex.push(...latest(glm.filter((m) => ["base", "prime"].includes(m.role))));
    daily.push(...glmMiddle); addFlash(glm.filter((m) => ["flash", "flashx"].includes(m.role)), glmMiddle);

    const qwen = catalog(/^qwen-?(\d+(?:\.\d+)*)?-(max|plus|flash)(?:-(?:latest|\d{4,8}(?:-\d{2}){0,2}))?$/i);
    const qwenMiddle = qwen.filter((m) => m.role === "plus");
    complex.push(...qwen.filter((m) => m.role === "max"));
    daily.push(...qwenMiddle); addFlash(qwen.filter((m) => m.role === "flash"), qwenMiddle);

    const seed = catalog(/^doubao-seed-(\d+(?:[.-]\d+)*)-(pro|turbo|lite|mini)$/i);
    complex.push(...latest(seed.filter((m) => ["pro", "turbo"].includes(m.role))));
    daily.push(...seed.filter((m) => m.role === "lite"));
    simple.push(...seed.filter((m) => m.role === "mini"));

    const kimi = catalog(/^kimi-k(\d+(?:\.\d+)*)(?:-(pro|lite|mini|flash))?(-highspeed)?$/i);
    const kimiMiddle = kimi.filter((m) => m.role === "lite");
    complex.push(...latest(kimi.filter((m) => ["base", "pro", "pro-highspeed", "base-highspeed"].includes(m.role))));
    daily.push(...kimiMiddle); simple.push(...kimi.filter((m) => m.role === "mini"));
    addFlash(kimi.filter((m) => m.role === "flash"), kimiMiddle);

    // MiniMax remains selectable in the full directory, outside recommendations.
    const deepseek = newest(text.flatMap((m) => {
      const match = m.id.match(/^deepseek-v(\d+(?:\.\d+)*)-(pro|flash)(?:-(\d{4}))?$/i);
      return match ? [{ ...m, version: match[1].split(".").map(Number), role: match[2].toLowerCase(), date: Number(match[3] || 0) }] : [];
    }), (m) => m.role);
    complex.push(...deepseek.filter((m) => m.role === "pro"));
    daily.push(...deepseek.filter((m) => m.role === "flash"));
    const scores = [...priceByID.values()].map((m) => m?.cost ? (m.cost.input || 0) * .25 + (m.cost.output || 0) * .75 : null).filter((x) => x != null && x > 0).sort((a, b) => a - b);
    const median = scores.length ? scores[Math.floor(scores.length / 2)] : 0;
    const by = (id) => {
      const m = priceByID.get(id);
      if (!m) return "";
      if (m.free || m.rate === 0) return "low";
      const score = m.cost ? (m.cost.input || 0) * .25 + (m.cost.output || 0) * .75 : 0;
      return score && median ? score <= median * .5 ? "low" : score >= median * 2 ? "high" : "standard" : "";
    };
    // A role's variants share one product series. Keep one card per series in
    // each tier; prefer the base spelling over speed variants, then use the
    // stable ID order when two otherwise equivalent variants remain.
    const modelSeries = (m) => {
      const id = m.id.toLowerCase(), role = m.role && m.role !== "base" ? m.role : "";
      if (!role) return id;
      const escaped = role.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
      return id.replace(new RegExp(`-${escaped}(?:-\\d{4,8}(?:-\\d{2}){0,2})?$`), "");
    };
    const variantRank = (m) => {
      const id = m.id.toLowerCase();
      return (/-highspeed$/.test(id) ? 1 : 0) + (/-flashx$/.test(id) ? 1 : 0);
    };
    const representative = (a, b) => {
      const ar = variantRank(a), br = variantRank(b);
      return ar !== br ? (ar < br ? a : b) : (a.id <= b.id ? a : b);
    };
    const used = new Set();
    const take = (list, tier) => {
      const onePerSeries = new Map();
      for (const model of list) {
        const key = /^gpt-/i.test(model.id) ? model.role : modelSeries(model), old = onePerSeries.get(key);
        onePerSeries.set(key, old ? representative(old, model) : model);
      }
      const enriched = [...onePerSeries.values()].map((m) => ({ ...m, priceTier: by(m.id) }));
      enriched.sort((a, b) => {
        const rank = (m) => m.priceTier === tier ? 0 : m.priceTier ? 2 : 1;
        return rank(a) - rank(b);
      });
      return enriched.filter((m) => !used.has(m.id) && (used.add(m.id), true));
    };
    const result = { complex: take(complex, "high"), daily: take(daily, "standard"), simple: take(simple, "low") };
    // A configured tier replaces that tier's computed result, retaining order.
    // Configuration controls presentation; the API catalog still controls access.
    // A pick with `latest` (a GPT role) shows the newest gpt-*-<role> this key
    // lists; its `id` is only the card shown when the key lists none.
    const configured = new Set();
    for (const tier of ["complex", "daily", "simple"]) {
      if (!config?.tiers || !Object.hasOwn(config.tiers, tier)) continue;
      result[tier] = config.tiers[tier].flatMap((pick) => {
        const id = (pick.latest && gpt.find((m) => m.role === String(pick.latest).toLowerCase())?.id) || pick.id;
        const listed = text.find((m) => m.id === id);
        configured.add(pick.id).add(id);
        return listed || pick.alwaysShow ? [{ ...listed, ...pick, id, priceTier: by(id) }] : [];
      });
    }
    for (const tier of ["complex", "daily", "simple"]) {
      if (!Object.hasOwn(config?.tiers || {}, tier)) result[tier] = result[tier].filter((m) => !configured.has(m.id));
    }
    return result;
  };
  const modelLabel = (m) => m.name && m.name !== m.id ? m.name : m.id;
  const modelTierLabels = { complex: "Complex reasoning", daily: "Everyday coding", simple: "Simple tasks" };
  const modelTierOrder = { complex: 0, daily: 1, simple: 2, other: 3 };
  const modelTierMap = (models) => {
    const byID = new Map();
    for (const [tier, picks] of Object.entries(recommendationTiers(models))) for (const m of picks) byID.set(m.id, tier);
    return byID;
  };
  // Condensed from Jellytoken model-market Basic information (desc/remark),
  // reviewed 2026-11-10/11. Shipped IDs without public detail use neutral,
  // task-focused copy. Match exact IDs; never substitute another release.
  // The gpt-6.2 entries repeat their gpt-6 / gpt-6.1 predecessors' copy and
  // still need checking against Jellytoken's own page for 6.2.
  const modelBasicInfo = {
    "gpt-6.2-astra": "OpenAI's flagship model for advanced reasoning, complex coding and long-horizon agent work.",
    "gpt-6.2-sol": "A balanced workhorse for writing, coding, debugging and multi-step collaboration.",
    "gpt-6-astra": "OpenAI's flagship model for advanced reasoning, complex coding and long-horizon agent work.",
    "gpt-6.1-sol": "A balanced workhorse for writing, coding, debugging and multi-step collaboration.",
    "gpt-6.1-terra": "A practical low-cost model for quick questions, rewriting and routine document tasks.",
    "gpt-6-luna": "A fast, lightweight model for quick questions, summaries and simple text transformations.",

    "minimax-m2.7": "For agent, coding and office workflows, with long context for iterative development, complex documents and multi-step collaboration.",
    "minimax-m2.7-highspeed": "A low-latency M2.7 variant using the same model weights, for agent, coding and office workflows that benefit from faster generation.",

    "doubao-seed-2-1-pro": "Designed for production work, with coding, agent and multimodal capabilities, autonomous planning, long-running execution and dynamic repair.",
    "doubao-seed-2.0-lite": "A lightweight model for quick drafting, information extraction and everyday text tasks.",
    "doubao-seed-2.0-mini": "A compact model for short replies, summaries, classification and simple text transformations.",
    "glm-5.3-flash": "A native multimodal member of the GLM-5 family, combining image and text understanding with a low-cost architecture.",
    "glm-5.3": "A flagship model for complex software engineering and long-running agent tasks, balancing results with token efficiency.",
    "qwen3.7-plus": "A balanced model for document understanding, structured analysis and coding assistance.",
    "qwen3.8-flash": "A native multimodal model for long documents and code repositories, coding assistance, agent collaboration and image understanding.",
    "qwen3.8-max": "Combines autonomous coding and professional workflows with visual understanding, long-document analysis and iterative task planning.",
    "deepseek-v4-pro": "A deep-reasoning model for mathematics, science and complex code, suited to research and multi-step analysis. Image input is not supported.",
    "deepseek-v4-pro-0813": "A deep-reasoning model for mathematics, science and complex code, suited to research and multi-step analysis. Image input is not supported.",
    "deepseek-v4.1-flash": "A lightweight flagship with native visual understanding, long context and a compact cache architecture for agent and document workloads.",
    "deepseek-v4.1-flash-1017": "A lightweight flagship with native visual understanding, long context and a compact cache architecture for agent and document workloads.",
    "kimi-k3.1-pro": "Combines native vision and a million-token context with coding, long-document synthesis, financial research and modeling.",
    "minimax-m3.1-flash": "Improves agent, coding and cybersecurity tasks, with a focus on complex collaboration and software development at competitive cost.",
    "minimax-m2.5-highspeed": "A low-latency M2.5 variant using the same model weights, with routing, batching and cache optimizations to accelerate generation.",
    "doubao-seed-2-1-turbo": "Designed for production work, with coding, agent and multimodal capabilities, autonomous planning, long-running execution and dynamic repair."
  };
  const modelPurpose = (m, category) => {
    if (modelBasicInfo[m.id.toLowerCase()]) return modelBasicInfo[m.id.toLowerCase()];
    if (/^kimi-/i.test(m.id)) return "Document reading, information synthesis and analytical writing.";
    if (/^qwen.*max/i.test(m.id)) return "Content understanding, analysis and structured writing.";
    if (/luna/i.test(m.id)) return "For lightweight information lookup, extracting key points and preparing concise summaries, short replies or simple text transformations.";
    return category === "complex" ? "For multi-step problem solving: break down goals, compare approaches and produce reasoned plans or long-form analysis." : category === "daily" ? "A general starting point for writing, coding and debugging, organizing documents and completing day-to-day collaborative tasks." : category === "simple" ? "For well-scoped tasks such as short questions, extracting key points, rewriting drafts, converting formats and producing concise summaries." : "Selected from the full model directory.";
  };
  const recommendationPanel = (p, owner) => {
    const box = el("div", "jelly-models"), toolbar = el("div", "jelly-model-toolbar"), tabs = el("div", "segs jelly-model-tabs"), content = el("div", "jelly-model-content");
    if (window.jellytoolModelConfigError) {
      const error = el("p", "hint err", t("Model recommendation configuration could not be loaded: {error}", { error: window.jellytoolModelConfigError }));
      error.setAttribute("role", "alert"); box.append(error);
    }
    const count = el("span", "jelly-model-count"); count.setAttribute("role", "status");
    const models = p.models.filter((m) => (m.outputs || ["text"]).includes("text"));
    let rec = recommendationTiers(models);
    // The supplier editor also includes saved IDs absent from the vendor list.
    // Only the private catalog response is evidence of key access.
    let accessIDs = null, refreshBulk = () => {};
    const available = (m) => accessIDs?.has(m.id) === true;
    const catalogRead = api("jellytool/models").then((data) => {
      if (data.catalogError || !Array.isArray(data.models)) return false;
      const listed = data.models.filter((m) => (m.outputs || ["text"]).includes("text"));
      accessIDs = new Set(listed.map((m) => m.id));
      rec = recommendationTiers(listed, new Map(data.models.map((m) => [m.id, m])));
      refreshBulk();
      return true;
    }).catch(() => false);
    const checkAccess = async () => {
      if (await catalogRead) return true;
      status(t("Could not verify model access. Refresh the model list and try again."), "err");
      return false;
    };
    const needsAccess = () => status(t("Please enable model access in Jellytoken first."), "err");
    owner.tab ||= "recommend";
    tabs.setAttribute("role", "tablist"); tabs.setAttribute("aria-label", t("Choose models"));
    content.id = "jelly-model-cards"; content.setAttribute("role", "tabpanel");
    const draw = () => {
      const focused = content.contains(document.activeElement) ? document.activeElement.dataset : null;
      const bulkUpdates = [];
      refreshBulk = () => bulkUpdates.forEach((paint) => paint());
      content.replaceChildren(); count.textContent = t("{n} selected", { n: owner.chosen.length });
      for (const tab of tabs.children) {
        const on = tab.dataset.tab === owner.tab;
        tab.classList.toggle("on", on); tab.setAttribute("aria-selected", String(on)); tab.tabIndex = on ? 0 : -1;
      }
      content.setAttribute("aria-labelledby", "jelly-tab-" + owner.tab);
      const card = (m, category) => {
        const on = owner.chosen.includes(m.id), button = el("button", "mchip pick jelly-model-card" + (on ? " on" : ""));
        button.type = "button"; button.dataset.model = m.id; button.title = m.id + "\n" + t(modelPurpose(m, category)); button.setAttribute("aria-pressed", String(on));
        const check = el("span", "jelly-model-check"); check.setAttribute("aria-hidden", "true"); check.append(svg(CHECK, 12, 2.1));
        const name = el("span", "jelly-model-name", modelLabel(m));
        if (m.recommended) {
          const badge = el("span", "jelly-model-recommended");
          const star = el("span", "", "★"); star.setAttribute("aria-hidden", "true");
          badge.append(star, document.createTextNode(t("Recommended"))); name.append(badge);
        }
        button.append(check, name, el("span", "jelly-model-purpose", t(modelPurpose(m, category))));
        button.onclick = async () => {
          if (!on && !await checkAccess()) return;
          if (!on && !available(m)) { needsAccess(); return; }
          owner.chosen = on ? owner.chosen.filter((id) => id !== m.id) : [...new Set([...owner.chosen, m.id])]; draw();
        };
        return button;
      };
      const names = { complex: "Solving complex problems", daily: "Everyday work and coding", simple: "Simple tasks" };
      const desc = { complex: "For in-depth analysis, planning, long-form writing and multi-step tasks.", daily: "For documents, information organization, coding and debugging.", simple: "For quick questions, rewriting, summaries and text formatting." };
      if (owner.tab === "selected") {
        const choices = el("div", "jelly-rec-options");
        for (const id of owner.chosen) {
          const category = Object.keys(rec).find((key) => rec[key].some((m) => m.id === id));
          choices.append(card(rec[category]?.find((m) => m.id === id) || models.find((m) => m.id === id) || { id }, category));
        }
        content.append(choices);
        if (!owner.chosen.length) content.append(el("p", "hint jelly-rec-empty", t("No models selected yet.")));
      } else {
        for (const key of ["complex", "daily", "simple"]) {
          const group = el("section", "jelly-rec-group"), head = el("div", "jelly-rec-head"), choices = el("div", "jelly-rec-options");
          const title = el("h4", "", t(names[key])); title.id = "jelly-rec-" + key;
          group.dataset.kind = key; group.setAttribute("aria-labelledby", title.id);
          const heading = el("div", "jelly-rec-heading"), symbol = el("span", "jelly-category-symbol ic");
          symbol.append(maskSpan("icons/jellytool-ui/" + { complex: "brain", daily: "code", simple: "lightning" }[key] + ".svg"));
          symbol.setAttribute("aria-hidden", "true"); heading.append(symbol, title); head.append(heading);
          if (rec[key].length) {
            const shown = rec[key], ids = new Set(shown.map((m) => m.id));
            const clears = () => shown.some((m) => owner.chosen.includes(m.id)) && shown.every((m) => owner.chosen.includes(m.id) || accessIDs !== null && !available(m));
            const button = el("button", "text action jelly-rec-select"); button.type = "button";
            button.dataset.category = key;
            const paint = () => { button.textContent = t(clears() ? "Clear" : "Select available models"); };
            bulkUpdates.push(paint); paint();
            button.onclick = async () => {
              const clear = clears();
              if (!clear && !await checkAccess()) return;
              const permitted = shown.filter(available);
              owner.chosen = clear ? owner.chosen.filter((id) => !ids.has(id)) : [...new Set([...owner.chosen, ...permitted.map((m) => m.id)])];
              if (!clear && permitted.length < shown.length) needsAccess();
              draw();
            };
            head.append(button);
          }
          group.append(head, el("p", "hint", t(desc[key])));
          if (!rec[key].length) group.append(el("span", "hint jelly-rec-empty", t("No reviewed recommendations match this key's text models. Choose from All models.")));
          for (const m of rec[key]) choices.append(card(m, key));
          group.append(choices); content.append(group);
        }
      }
      if (focused) {
        const button = [...content.querySelectorAll("[data-model], button[data-category]")].find((b) => focused.model ? b.dataset.model === focused.model : focused.category && b.dataset.category === focused.category);
        (button || content.querySelector("[data-model]") || tabs.querySelector(".on"))?.focus({ preventScroll: true });
      }
    };
    for (const [id, label] of [["recommend", "Recommendations"], ["selected", "Selected"]]) {
      const tab = el("button", "opt", t(label)); tab.type = "button"; tab.id = "jelly-tab-" + id; tab.dataset.tab = id;
      tab.setAttribute("role", "tab"); tab.setAttribute("aria-controls", content.id);
      tab.onclick = () => { owner.tab = id; draw(); };
      tab.onkeydown = (e) => {
        if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(e.key)) return;
        e.preventDefault(); owner.tab = e.key === "Home" ? "recommend" : e.key === "End" ? "selected" : owner.tab === "recommend" ? "selected" : "recommend";
        draw(); tabs.querySelector(".on").focus({ preventScroll: true });
      };
      tabs.append(tab);
    }
    toolbar.append(tabs, count);
    box.append(el("p", "jelly-model-intro hint", t("Choose recommended models for your tasks, or browse the full directory for more options.")), toolbar, content); draw();
    // Metadata must not detach a card between pointerdown and click. Keep
    // visible cards stable; the next user action draws the updated ordering.
    return box;
  };
  let guideModels = null, guideModelsOpen = false;
  const leaveModelDirectory = async () => {
    editing = null; draft = null; acctModels = null; renderProviders();
    await show("quickstart");
  };
  const cancelModelSelection = async () => {
    guideModels = null; guideModelsOpen = false; guideRevision++;
    if (view === "providers") await leaveModelDirectory(); else renderQuickstart();
    $("#quickstart [data-step='models'] .guide-go")?.focus({ preventScroll: true });
    return true;
  };
  const selectionActions = (p, fields, selected, cancel, more) => {
    const bar = el("div", "bar qs-model-actions"), error = el("div", "hint err"); error.setAttribute("role", "alert");
    if (more) { const button = el("button", "text action jelly-choose-all", t("Browse more models →")); button.type = "button"; button.onclick = more; bar.append(button); }
    const discard = el("button", "text", t("Cancel")); discard.type = "button"; discard.onclick = cancel;
    const save = el("button", "text primary", t("Save selection")); save.type = "button";
    save.onclick = async (e) => {
      const go = scrollOnPurpose(e);
      const buttons = [...bar.querySelectorAll("button")]; buttons.forEach((b) => b.disabled = true); fields.disabled = true; error.textContent = "";
      saving(save, t("Saving…"));
      try {
        const chosen = selected();
        providers = await api("provider/save", { id: p.id, models: chosen, noModels: chosen.length === 0, fallback: p.fallback, unlisted: p.unlisted });
        state = await api("state");
        const full = guideModels?.inSupplier;
        guideModels = null; guideModelsOpen = false; guideRevision++;
        if (full) await leaveModelDirectory();
        await refreshQuickstart();
        if (go && view === "quickstart") {
          // The save can outlast the click's scroll window. Renew it only
          // after success, before locating the newly collapsed model step.
          readerScrolls(1000);
          const step = $("#quickstart [data-step='models']");
          step.scrollIntoView({ block: "start" });
          step.querySelector(".guide-go").focus({ preventScroll: true });
          arrived($("#view-quickstart"));
        }
      } catch (e) { error.textContent = e.message; }
      finally { fields.disabled = false; buttons.forEach((b) => b.disabled = false); doneSaving(save); }
    };
    bar.append(discard, save); return [error, bar];
  };
  const quickstartModels = (p, own = { chosen: p.chosen?.length ? [...p.chosen] : [] }) => {
    const box = el("div", "qs-model-picker"); box.id = "qs-model-picker";
    const fields = el("fieldset"); fields.append(recommendationPanel(p, own));
    const more = async () => {
      guideModels.inSupplier = true;
      await openProvider(p.id);
      if (view !== "providers" || editing !== p.id) { guideModels.inSupplier = false; return; }
    };
    box.append(fields, ...selectionActions(p, fields, () => [...own.chosen], cancelModelSelection, more));
    return { box, own, locale, p };
  };
  window.jellytoolProviderEditor = (p) => {
    if (!guideModels?.inSupplier || p?.id !== "jellytoken") return null;
    draft ||= { ...draftOf(p), chosen: [...guideModels.own.chosen], none: true };
    const editor = el("div", "editor jelly-directory-editor"), head = el("div", "ehead");
    const back = el("button", "text jelly-back-recommendations", t("← Back to recommendations")); back.type = "button";
    back.onclick = async () => {
      guideModels.own.chosen = chosenIds(); guideModels.inSupplier = false;
      // Rebuild the cards from the current catalog, retaining the active tab.
      guideModels = quickstartModels(p, guideModels.own); guideRevision++;
      await leaveModelDirectory();
    };
    head.append(icon("jellytoken"), el("b", "", t("All models")));
    const fields = el("fieldset"); fields.append(renderModels(p, true));
    editor.append(head, back, fields, ...selectionActions(p, fields, chosenIds, () => window.jellytoolCancelModels()));
    return editor;
  };
  window.jellytoolCancelModels = async () => {
    if (!guideModels?.inSupplier) return null;
    if (providerDirty() && !await confirmDiscard()) return false;
    return cancelModelSelection();
  };
  window.jellytoolModels = async () => {
    if (!providers) await loadProviders();
    const p = providers.providers.find((p) => p.id === "jellytoken" && p.ready && !p.off && !p.account?.signedOut);
    if (view !== "quickstart" || !p) return openConnection();
    guideModels ||= quickstartModels(p);
    guideModelsOpen = !guideModelsOpen;
    guideRevision++; renderQuickstart();
    $("#quickstart [data-step='models'] .guide-go")?.focus({ preventScroll: true });
  };
  window.jellytoolGuide = (g) => {
    const box = el("div", "jelly-guide-source");
    const site = el("button", "text qs-key-site", t("Create a Jellytoken API key ↗")); site.type = "button"; site.onclick = keySite;
    const settings = el("button", "text guide-open qs-settings", t("Connection settings")); settings.type = "button"; settings.onclick = openConnection;
    if (!providers && !providersEarly?.got) {
      box.classList.add("qs-loading"); box.setAttribute("aria-busy", String(!g.unknown));
      if (!g.unknown) box.append(el("span", "guide-say", t("Checking your setup…")), el("span", "skeleton qs-loading-line"), el("span", "skeleton qs-loading-field"));
      return box;
    }
    const connected = g.ready.some((p) => p.id === "jellytoken");
    const replacing = connected && guideReplace;
    const help = el("span", "guide-say qs-key-help", connected && !replacing
      ? (guideMessage || t("Jellytoken is connected. Continue with model selection."))
      : connected
        ? t("Leave the key blank to keep the saved key. A replacement is verified before it is saved.")
        : t("Create a key on the Jellytoken website, then paste it here to connect."));
    const form = el("form", "qs-key-form"); form.setAttribute("aria-busy", String(guideChecking));
    const editingKey = !connected || replacing;
    const label = editingKey ? el("label", "qs-label", connected ? t("New API key") : t("API key")) : null;
    let key = null, pair = null;
    if (editingKey) {
      key = input(guideKey, t("Paste your Jellytoken API key"), guideKeyVisible ? "text" : "password");
      key.id = "jelly-guide-key"; key.classList.add("jelly-guide-key"); key.autocomplete = "off"; key.spellcheck = false;
      label.htmlFor = key.id;
      key.disabled = guideChecking; key.setAttribute("aria-required", "true");
      key.setAttribute("aria-invalid", String(!!guideError)); key.setAttribute("aria-describedby", "jelly-guide-result");
      const eye = el("button", "text qs-key-eye", t(guideKeyVisible ? "Hide" : "Show")); eye.type = "button";
      eye.setAttribute("aria-controls", key.id); eye.setAttribute("aria-pressed", String(guideKeyVisible));
      eye.onclick = () => {
        guideKeyVisible = !guideKeyVisible; key.type = guideKeyVisible ? "text" : "password";
        eye.textContent = t(guideKeyVisible ? "Hide" : "Show"); eye.setAttribute("aria-pressed", String(guideKeyVisible));
      };
      pair = el("div", "pair qs-key-entry"); pair.append(key, eye);
    }
    const connect = el("button", "text primary guide-go", t(guideChecking ? "Verifying…" : connected && !replacing ? "Replace API key" : connected ? "Save new API key" : "Verify and connect"));
    connect.type = connected && !replacing ? "button" : "submit"; connect.disabled = guideChecking;
    if (connected && !replacing) connect.onclick = () => { guideReplace = true; guideKey = ""; guideError = ""; guideRevision++; renderQuickstart(); document.querySelector("#jelly-guide-key")?.focus({ preventScroll: true }); };
    const result = editingKey ? el("span", "guide-say guide-result", guideError || (guideChecking ? t("Verifying the key and loading models…") : "")) : null;
    if (result) {
      result.id = "jelly-guide-result"; result.dataset.state = guideError ? "error" : "status";
      result.setAttribute("role", guideError ? "alert" : "status");
      key.oninput = () => {
        guideKey = key.value; guideError = ""; key.setAttribute("aria-invalid", "false"); result.textContent = "";
        result.dataset.state = "status"; result.setAttribute("role", "status");
      };
    }
    form.onsubmit = async (e) => {
      e.preventDefault();
      if (!editingKey) return;
      if (guideChecking) return;
      if (!guideKey.trim()) { guideError = t("Paste your API key first"); guideRevision++; renderQuickstart(); document.querySelector("#jelly-guide-key")?.focus({ preventScroll: true }); return; }
      guideChecking = true; guideError = "";
      connect.disabled = key.disabled = true; connect.textContent = t("Verifying…");
      key.setAttribute("aria-invalid", "false"); result.dataset.state = "status"; result.setAttribute("role", "status");
      form.setAttribute("aria-busy", "true"); result.textContent = t("Verifying the key and loading models…");
      try {
        connection = await api("jellytool/key", { key: guideKey.trim() });
        guideKey = ""; guideReplace = false; guideMessage = [t("Jellytoken is connected. Continue with model selection."), connectionWarning()].filter(Boolean).join(" ");
        await loadProviders(); state = await api("state"); renderAgents();
      } catch (e) { guideError = e.message; }
      finally {
        guideChecking = false; guideRevision++; renderQuickstart();
        if (guideError && view === "quickstart") document.querySelector("#jelly-guide-key")?.focus({ preventScroll: true });
      }
    };
    const actions = el("div", "qs-key-actions"); actions.append(connect, settings);
    if (replacing) {
      const cancelReplace = el("button", "text", t("Cancel replacement")); cancelReplace.type = "button";
      cancelReplace.onclick = () => { guideReplace = false; guideKey = ""; guideError = ""; guideRevision++; renderQuickstart(); };
      actions.append(cancelReplace);
    }
    if (connected) {
      const savedLabel = el("label", "qs-label", t("Registered API key")); savedLabel.htmlFor = "jelly-guide-saved-key";
      const saved = input("••••••••••••", t("API key configured"), "text");
      saved.id = "jelly-guide-saved-key"; saved.classList.add("jelly-guide-key", "jelly-key-masked"); saved.disabled = true; saved.setAttribute("aria-readonly", "true");
      form.append(savedLabel, saved);
    }
    if (editingKey) form.append(label, pair, result);
    form.append(actions);
    box.append(help, site, form);
    return box;
  };

  let usageAgent = "", usageHelpOpen = false;
  function agentUsage(g) {
    const step = guideStep(4, false, t("Use models in your agent"), t("Choose an agent to see how to select a model and start a conversation."));
    step.li.dataset.step = "usage";
    step.li.classList.add("qs-usage");
    const agents = state.agents.filter((a) => a.wired);
    const box = el("div", "qs-usage-content"), tabs = el("div", "jelly-model-tabs qs-usage-tabs");
    if (g.unknown || !agents.length) {
      box.append(el("p", "guide-say", t(g.unknown ? "Could not read your setup. Try again." : "Connect an agent in step 3 to see how to use its models.")));
      step.body.append(box); return step.li;
    }
    usageAgent = agents.some((a) => a.id === usageAgent) ? usageAgent : agents[0].id;
    tabs.setAttribute("role", "tablist"); tabs.setAttribute("aria-label", t("Agent to use"));
    const instructions = el("div", "qs-usage-instructions");
    instructions.id = "qs-usage-instructions"; instructions.setAttribute("role", "tabpanel"); instructions.tabIndex = 0;
    const paint = () => {
      const a = agents.find((a) => a.id === usageAgent), steps = el("ol", "qs-usage-steps");
      for (const tab of tabs.children) {
        const on = tab.dataset.usageAgent === usageAgent;
        tab.classList.toggle("on", on); tab.setAttribute("aria-selected", String(on)); tab.tabIndex = on ? 0 : -1;
      }
      instructions.setAttribute("aria-labelledby", "qs-usage-tab-" + usageAgent);
      const pick = a.id === "claude" ? t("In Claude Code, enter /model and choose Opus, Sonnet or Haiku. Set the model behind each tier on Jellytool's Agents page.")
        : a.id === "codex" ? t("In Codex CLI, enter /model and select a model provided by Jellytool. In the Codex app or IDE extension, use the conversation's model picker.")
        : NO_PICKER.has(a.id) ? t("Set {agent}'s starting model on Jellytool's Agents page, then start a new {agent} session.", { agent: a.name })
        : PICKS_IN[a.id] ? t("Open {agent}'s model picker with {cmd} and select a model provided by Jellytool.", { agent: a.name, cmd: PICKS_IN[a.id] })
        : t("Open the model picker in {agent}'s conversation window and select a model provided by Jellytool.", { agent: a.name });
      steps.append(el("li", "", t("After connecting, reopen {agent} as advised on the Agents page so it reads the new configuration.", { agent: a.name })), el("li", "", pick), el("li", "", t('Start a new conversation and send a short message, such as "Hello". Keep Jellytool running while using its models.')));
      instructions.replaceChildren(steps);
    };
    for (const a of agents) {
      const tab = el("button", "opt", a.name); tab.type = "button"; tab.id = "qs-usage-tab-" + a.id; tab.dataset.usageAgent = a.id;
      tab.setAttribute("role", "tab"); tab.setAttribute("aria-controls", instructions.id);
      tab.onclick = () => { usageAgent = a.id; paint(); };
      tab.onkeydown = (e) => {
        if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(e.key)) return;
        e.preventDefault();
        const index = agents.findIndex((a) => a.id === usageAgent);
        usageAgent = agents[e.key === "Home" ? 0 : e.key === "End" ? agents.length - 1 : (index + (e.key === "ArrowRight" ? 1 : -1) + agents.length) % agents.length].id;
        paint(); tabs.querySelector(".on").focus({ preventScroll: true });
      };
      tabs.append(tab);
    }
    box.append(tabs, instructions); paint();
    const help = el("details", "qs-usage-help"); help.open = usageHelpOpen;
    help.append(el("summary", "", t("Can't find a model?")), el("p", "", t("Check that the provider is signed in and enabled, that models are selected and saved, and that this agent is connected with the model included in its list. Then reopen the agent.")));
    help.ontoggle = () => { if (help.isConnected) usageHelpOpen = help.open; };
    box.append(help); step.body.append(box);
    return step.li;
  }

  function imageSetup() {
    const step = guideStep(5, false, t("Image generation setup (optional)"), t("If you need to replace your agent's image generation model, configure it here. This step is optional."));
    step.li.dataset.step = "images";
    step.li.classList.add("qs-usage");
    const steps = el("ol", "qs-usage-steps qs-image-steps");
    const section = (title) => {
      const item = el("li", "qs-image-section");
      const body = el("div", "qs-image-section-body");
      body.append(el("h4", "qs-image-section-title", t(title)));
      const actions = el("div", "qs-image-section-actions");
      item.append(body, actions);
      return { item, body, actions };
    };
    const model = section("Image model"), tool = section("MCP tool"), use = section("Prompt examples");
    const choose = el("button", "text qs-image-model", t("Choose an image model")); choose.type = "button";
    choose.onclick = (e) => openSettingsAt(e, "models", $("#imagesList"));
    model.body.append(el("p", "qs-image-section-description", t("Pick the model in Settings → Models → Image generation. Picking any model there instead of Off turns image generation on.")));
    model.actions.append(choose);
    const check = el("button", "text qs-image-check", t("Check image tool")); check.type = "button";
    const result = el("div", "qs-image-result"); result.setAttribute("aria-live", "polite");
    tool.body.append(el("p", "qs-image-section-description", t("Jellytool installs jellytool-image by default. Check it here, then choose which agents receive the tool in Library → MCP servers.")));
    tool.actions.append(check, result);
    // The optional check is read-only and runs on demand. A failed read is
    // unknown, never evidence that the server needs installing again.
    check.onclick = async () => {
      check.disabled = true; result.replaceChildren(el("p", "", t("Checking your setup…")));
      try {
        if (featuresOff.has("library")) {
          const enable = el("button", "text qs-image-enable", t("Turn on the Library")); enable.type = "button";
          enable.onclick = async () => {
            enable.disabled = true;
            try {
              const next = await api("plugins/builtin", { id: "library", on: true });
              if (state.settings) state.settings.builtins = next;
              applyFeatures(next); check.click();
            } catch (e) { enable.disabled = false; status(e.message, "err"); }
          };
          result.replaceChildren(el("p", "", t("The Library is off. Turn it on to configure the image tool for your agents.")), enable);
          return;
        }
        const lib = await api("library?glance=1");
        if (!Array.isArray(lib.servers) || !Array.isArray(lib.agents)) throw new Error(t("Could not read your setup. Try again."));
        const own = (s) => s.args?.join(" ") === "mcp image" && /^(jellytool|magpie)(?:$|[-.])/.test((s.command || "").split(/[/\\]/).pop().toLowerCase());
        const installed = lib.servers.find(own), found = lib.foundServers?.map((f) => f.server).find(own), server = installed || found;
        const agents = (installed?.agents || []).filter((id) => !installed.problems?.[id]).map((id) => lib.agents.find((a) => a.id === id)?.name || id);
        const say = installed ? agents.length
          ? t("{server} is installed and configured for: {agents}. Confirm it is enabled in your agent.", { server: installed.name, agents: agents.join(", ") })
          : t("{server} is installed but not configured for an agent yet.", { server: installed.name })
          : found ? t("{server} was found in an agent. Import it in the Library to manage it here.", { server: found.name })
          : t("jellytool-image is not installed. Open its Library card, select your agents and click Add.");
        const configure = el("button", "text qs-image-agents", t(installed ? "Configure image tool for agents" : found ? "Open the Library" : "Install and configure image tool")); configure.type = "button";
        configure.onclick = async () => {
          if (await show("library") !== false) magpie.call("library", "server", server?.name || "jellytool-image");
        };
        result.replaceChildren(el("p", "", say), configure);
        if (installed && Object.keys(installed.problems || {}).length) result.prepend(el("p", "err", t("Some agents could not be configured. Open the Library to review their errors and retry.")));
      } catch (e) { result.replaceChildren(el("p", "err", e.message)); }
      finally { check.disabled = false; }
    };
    use.body.append(el("p", "qs-image-section-description", t("After adding the tool, reopen your agent or start a new session and confirm jellytool-image is enabled in its MCP settings. WorkBuddy requires you to switch it on there once. An existing magpie-image entry can be used too.")), el("p", "qs-image-model-help qs-image-section-description", t("To use a specific image model for one request, name it in the prompt, for example: use gpt-image-2.5-flare to generate the image. The agent passes that name to generate_image for this call only; it does not change the default selected in Settings. The model must be configured and available in Jellytool. If you omit the model, the saved default is used.")), el("p", "qs-image-size qs-image-section-description", t("To set dimensions, ask the agent to pass size to generate_image: use an aspect ratio such as 1:1, 16:9 or 9:16, or WIDTHxHEIGHT pixels such as 1024x1024, 1536x1024 or 1024x1536. A ratio alone does not set the pixel resolution. Available sizes depend on the model; leaving size out uses its default.")), el("p", "qs-image-section-description", t("Ask your agent to use jellytool-image explicitly, for example:")));
    const prompt = t("Use the jellytool-image MCP to generate a 16:9 illustration of a seaside city at sunset and save it to assets/seaside.png.");
    const example = el("div", "qs-image-example"); example.append(el("code", "", prompt), copyBtn(prompt, t("Copy")));
    const modelPrompt = t('Use the jellytool-image MCP with the gpt-image-2.5-flare model to generate an illustration of a seaside city at sunset, set size to "16:9", and save it to assets/seaside.png.');
    const modelExample = el("div", "qs-image-example qs-image-model-example"); modelExample.append(el("code", "", modelPrompt), copyBtn(modelPrompt, t("Copy")));
    const pixelPrompt = t('Use the jellytool-image MCP to generate an illustration of a seaside city at sunset, set size to "1536x1024" (width 1536 px, height 1024 px), and save it to assets/seaside.png.');
    const pixelExample = el("div", "qs-image-example"); pixelExample.append(el("code", "", pixelPrompt), copyBtn(pixelPrompt, t("Copy")));
    use.actions.append(example, modelExample, pixelExample, el("p", "qs-image-section-description", t("The agent calls generate_image and saves the image in your project. To edit an image, provide its path and describe the changes. If the tool returns a task ID, let the agent call generation_result with that ID. Keep Jellytool running.")));
    steps.append(model.item, tool.item, use.item); step.body.append(steps);
    return step.li;
  }

  const savedModels = (g) => g.ready.flatMap((p) => (p.chosen || []).map((id) => p.models?.find((m) => m.id === id) || { id }));
  // Preserve the three configuration checks; the fourth step is a reference,
  // not a claim that a real request has been made in the selected agent.
  const guideSnapshot = (g) => JSON.stringify([locale, guideRevision, guideMessage, guideReplace, guideBusy, !!(providers || providersEarly?.got), state.served, g.source, g.models, g.agent, g.unknown,
    g.ready.map((p) => [p.id, p.name]), savedModels(g).map((m) => [m.id, m.name]), state.agents.map((a) => [a.id, a.name, a.icon, a.wired, connectKind(a)])]);
  // An identical background read must not replace a field while the user is
  // composing text, holding a key, or waiting for validation to answer.
  window.jellytoolKeepQuickstart = (page, g) => !!page.querySelector(".qs-guide") && guideRendered === guideSnapshot(g);
  window.jellytoolQuickstart = (page, g, focused) => {
    const card = page.querySelector(".guide"), head = card.querySelector(".guide-head");
    card.classList.add("qs-guide");
    card.querySelector(".guide-steps").append(agentUsage(g), imageSetup());
    if (!g.unknown) head.querySelector(".guide-n").textContent = t("{n} of {m} setup steps done", { n: [g.source, g.models, g.agent].filter(Boolean).length, m: 3 });
    head.querySelector(".guide-h")?.remove();
    const intro = el("div", "qs-intro");
    intro.append(el("p", "qs-tagline", t("Every agent, any model, one connection; keep your existing subscriptions and switch models as needed.")));
    intro.append(el("p", "qs-description", t("Jellytool brings Jellytoken models together in one place, recommends models by task, and quickly connects them to the agents you use; your agents' existing subscriptions and sign-in methods stay unchanged while you switch models as needed.")));
    head.prepend(intro);
    const pending = [g.source, g.models, g.agent].indexOf(false);
    for (const [index, step] of [...card.querySelectorAll(".guide-step")].entries()) {
      step.classList.toggle("is-current", index === pending);
      if (index === pending) step.setAttribute("aria-current", "step");
      if (step.dataset.step === "agent") {
        step.classList.add("qs-original");
        const body = step.querySelector(".guide-body"), title = body.querySelector(":scope > .guide-title"), open = body.querySelector(":scope > .guide-open");
        if (title && open) {
          const titleRow = el("div", "qs-original-title");
          titleRow.append(title, open);
          body.prepend(titleRow);
        }
        continue;
      }
      const body = step.querySelector(".guide-body"), copy = el("div", "qs-copy"), actions = el("div", "qs-actions");
      copy.append(...body.querySelectorAll(":scope > .guide-title, :scope > .guide-say"));
      actions.append(...body.childNodes);
      const source = actions.querySelector(".jelly-guide-source");
      if (source) {
        const help = source.querySelector(".qs-key-help");
        if (help) copy.append(help, source.querySelector(".qs-key-site"));
        const success = source.querySelector(".qs-key-success");
        if (success) copy.append(success);
      }
      for (const button of actions.querySelectorAll(".primary")) button.classList.toggle("primary", index === pending);
      for (const button of actions.querySelectorAll(":scope > button")) {
        button.classList.add("qs-step-action"); button.classList.toggle("primary", index === pending);
      }
      body.append(copy, actions);
      const more = actions.querySelector(".guide-more");
      if (more) more.textContent = t("{n} more agents", { n: state.agents.filter((a) => ["ok", "empty"].includes(connectKind(a))).length - GUIDE_CHIPS });
    }
    const modelStep = card.querySelector('[data-step="models"]');
    const summary = el("ul", "qs-model-summary"); summary.setAttribute("aria-label", t("Selected"));
    const tiers = modelTierMap(g.ready.find((p) => p.id === "jellytoken")?.models || []);
    const selected = savedModels(g).map((m, index) => ({ m, index, tier: tiers.get(m.id) || "other" }));
    selected.sort((a, b) => (modelTierOrder[a.tier] ?? modelTierOrder.other) - (modelTierOrder[b.tier] ?? modelTierOrder.other) || a.index - b.index);
    for (const { m, tier } of selected) {
      const tierLabel = t(modelTierLabels[tier] || "Other");
      const item = el("li", "", ""); item.dataset.tier = tier; item.title = m.id;
      item.append(el("span", "qs-model-tier", tierLabel), el("span", "qs-model-summary-name", modelLabel(m)));
      summary.append(item);
    }
    if (summary.childElementCount) modelStep.querySelector(".qs-copy").append(summary);
    const choose = modelStep.querySelector(".guide-go");
    const expanded = !!guideModels && guideModelsOpen && g.source && !g.unknown;
    choose.textContent = t(expanded ? "Collapse models" : "Choose models");
    choose.setAttribute("aria-expanded", String(expanded));
    choose.setAttribute("aria-controls", "qs-model-picker");
    if (expanded) {
      if (guideModels.locale !== locale) guideModels = quickstartModels(g.ready.find((p) => p.id === "jellytoken"), guideModels.own);
      modelStep.querySelector(".guide-body").append(guideModels.box);
      if (focused?.dataset.model) [...guideModels.box.querySelectorAll("[data-model]")].find((b) => b.dataset.model === focused.dataset.model)?.focus({ preventScroll: true });
    }
    if (focused?.id === "jelly-guide-key") {
      const key = page.querySelector("#jelly-guide-key");
      if (key && !key.disabled) { key.focus({ preventScroll: true }); key.setSelectionRange(focused.selectionStart, focused.selectionEnd); }
    }
    if (focused?.dataset.usageAgent) page.querySelector(".qs-usage-tabs .on")?.focus({ preventScroll: true });
    guideRendered = guideSnapshot(g);
  };
})();
