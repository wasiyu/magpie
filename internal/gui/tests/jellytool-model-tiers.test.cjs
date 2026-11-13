const assert = require("node:assert/strict");
const { test } = require("node:test");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const source = fs.readFileSync(path.join(__dirname, "../assets/jellytool.js"), "utf8");
const defaults = require("../assets/jellytool-models.json");
const context = vm.createContext({ window: { jellytoolModelConfig: defaults } });
vm.runInContext(source.slice(source.indexOf("  // Recommendations follow the product tiers"), source.indexOf("  const modelLabel")) + "\nglobalThis.tiers = recommendationTiers;", context);
const infoStart = source.indexOf("  const modelBasicInfo =");
const infoEnd = source.indexOf("  const modelPurpose", infoStart);
const modelBasicInfo = vm.runInNewContext("(" + source.slice(infoStart + "  const modelBasicInfo = ".length, infoEnd).trim().replace(/;$/, "") + ")");
const tiers = (models, config = defaults) => JSON.parse(JSON.stringify(context.tiers(models.map((id) => typeof id === "string" ? { id, outputs: ["text"] } : id), new Map(), config)));
const ids = (data, tier) => data[tier].map((m) => m.id);
const auto = { version: 1, tiers: {} };

test("default configuration uses GPT-6 Astra/Luna and GPT-6.1 Sol/Terra, even without access", () => {
  const data = tiers([]);
  assert.deepEqual(ids(data, "complex"), ["gpt-6-astra"]);
  assert.deepEqual(ids(data, "daily"), ["gpt-6.1-sol"]);
  assert.deepEqual(ids(data, "simple"), ["gpt-6.1-terra", "gpt-6-luna"]);
  assert(Object.values(data).flat().every((m) => m.recommended && m.alwaysShow));
});

test("shipped recommendation models have distinct task summaries and Astra is identified as flagship", () => {
  for (const [tier, picks] of Object.entries(defaults.tiers)) {
    const descriptions = picks.map(({ id }) => modelBasicInfo[id]);
    assert(descriptions.every(Boolean), `${tier} has a model without a task summary`);
    assert.equal(new Set(descriptions).size, descriptions.length, `${tier} repeats a task summary`);
  }
  assert.match(modelBasicInfo["gpt-6-astra"], /flagship/i);
});

test("configured tiers can change IDs, ordering, tiers, names and recommendation markers", () => {
  const data = tiers(["own-model", "glm-5.3"], { version: 1, tiers: {
    complex: [{ id: "own-model", recommended: true, name: "My model" }],
    daily: [], simple: [{ id: "gpt-next", alwaysShow: true }]
  } });
  assert.deepEqual(ids(data, "complex"), ["own-model"]);
  assert.equal(data.complex[0].name, "My model");
  assert.equal(data.complex[0].recommended, true);
  assert.deepEqual(data.daily, []);
  assert.deepEqual(ids(data, "simple"), ["gpt-next"]);
});

test("a configured model needs catalog access to appear unless alwaysShow is set", () => {
  assert.deepEqual(ids(tiers([], { version: 1, tiers: { complex: [{ id: "own-model" }] } }), "complex"), []);
  const models = [{ id: "own-model", outputs: ["text"], on: false }];
  const before = JSON.stringify(models);
  assert.deepEqual(ids(tiers(models, { version: 1, tiers: { complex: [{ id: "own-model" }] } }), "complex"), ["own-model"]);
  assert.equal(JSON.stringify(models), before);
});

test("omitted tiers use computed models and do not duplicate a model moved by configuration", () => {
  const data = tiers(["glm-5.3", "deepseek-v4-pro", "deepseek-v4.1-flash"], { version: 1, tiers: { daily: [{ id: "deepseek-v4-pro" }] } });
  assert.deepEqual(ids(data, "complex"), ["glm-5.3"]);
  assert.deepEqual(ids(data, "daily"), ["deepseek-v4-pro"]);
});

test("automatic tiers select newest numeric versions and one representative per non-GPT series", () => {
  const data = tiers(["gpt-6.9-sol", "gpt-6.10-sol", "gpt-6-terra", "gpt-6-luna", "qwen3.9-max", "qwen3.10-max", "glm-5.3", "glm-5.3-prime", "doubao-seed-2.0-pro", "doubao-seed-2-1-pro", "MiniMax-M3.1-Flash"], auto);
  assert.deepEqual(ids(data, "complex"), ["glm-5.3", "qwen3.10-max", "doubao-seed-2-1-pro"]);
  assert.deepEqual(ids(data, "daily"), ["gpt-6.10-sol"]);
  assert.deepEqual(ids(data, "simple"), ["gpt-6-terra", "gpt-6-luna"]);
  assert(!Object.values(data).flat().some((m) => /^minimax-/i.test(m.id)));
});

test("automatic Flash placement follows the presence of a middle tier", () => {
  const models = ["glm-5.3", "glm-5.3-flash", "qwen3.8-max", "qwen3.8-flash", "kimi-k3.1-pro", "kimi-k3.1-flash"];
  assert.deepEqual(ids(tiers(models, auto), "daily"), ["glm-5.3-flash", "qwen3.8-flash", "kimi-k3.1-flash"]);
  assert.deepEqual(ids(tiers([...models, "glm-5.3-air", "qwen3.8-plus", "kimi-k3.1-lite"], auto), "simple"), ["glm-5.3-flash", "qwen3.8-flash", "kimi-k3.1-flash"]);
});

test("default DeepSeek recommendations use the dated catalog models", () => {
  const data = tiers(["deepseek-v4-pro", "deepseek-v4-pro-0813", "deepseek-v4.1-flash", "deepseek-v4.1-flash-1017"]);
  assert.deepEqual(ids(data, "complex").filter((id) => id.startsWith("deepseek")), ["deepseek-v4-pro-0813"]);
  assert.deepEqual(ids(data, "daily").filter((id) => id.startsWith("deepseek")), ["deepseek-v4.1-flash-1017"]);
});

test("automatic DeepSeek picks the latest date within the latest model version regardless of catalog order", () => {
  const models = ["deepseek-v4-pro", "deepseek-v4-pro-0423", "deepseek-v4-pro-0813", "deepseek-v4-flash-1231", "deepseek-v4.1-flash", "deepseek-v4.1-flash-0731", "deepseek-v4.1-flash-1017"];
  for (const ordered of [models, [...models].reverse()]) {
    const data = tiers(ordered, auto);
    assert.deepEqual(ids(data, "complex"), ["deepseek-v4-pro-0813"]);
    assert.deepEqual(ids(data, "daily"), ["deepseek-v4.1-flash-1017"]);
  }
  assert.deepEqual(ids(tiers(["deepseek-v4-pro"], auto), "complex"), ["deepseek-v4-pro"]);
  assert.deepEqual(ids(tiers(models, { version: 1, tiers: { complex: [{ id: "deepseek-v4-pro-0423" }] } }), "complex"), ["deepseek-v4-pro-0423"], "an explicitly configured ID stays pinned");
});
