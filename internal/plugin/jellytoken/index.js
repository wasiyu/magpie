const ID = "jellytoken"
const ORIGIN = "https://aiservice.jellytoken.com"
const packages = { openai: "@ai-sdk/openai-compatible", responses: "@ai-sdk/openai", anthropic: "@ai-sdk/anthropic", gemini: "@ai-sdk/google", decide: "@ai-sdk/openai-compatible" }

function credential(auth) {
  if (auth?.type !== "api" || typeof auth.key !== "string" || !auth.key.trim() || /[\r\n]/.test(auth.key)) {
    throw new Error("Jellytoken requires an API key")
  }
  return auth.key.trim()
}

function businessFailure(body) {
  if (body.success === false) return true
  return [body.error, body.errorCode, body.code].some((v) => v != null && v !== 0 && v !== "0" && v !== "")
}

// Present the gateway's Images/Videos contract over Jellytoken's task API.
// Result URLs are fetched by the gateway's guarded downloader, without the key.
async function mediaFetch(c, key, rest, init) {
  const signal = AbortSignal.any([init.signal, AbortSignal.timeout(14 * 60 * 1000)].filter(Boolean))
  const root = c.catalog.replace(/\/v1$/, "")
  const task = async (id, body) => {
    const res = await fetch(root + "/api/ai/tasks" + (id ? "/" + encodeURIComponent(id) : ""), {
      method: id ? "GET" : "POST", headers: { authorization: "Bearer " + key, "content-type": "application/json" },
      body: body ? JSON.stringify(body) : undefined, signal, redirect: "manual",
    })
    if (!res.ok) {
      await res.body?.cancel()
      throw new Error("Jellytoken task API: HTTP " + res.status)
    }
    const data = await res.json()
    if (data.error || data.errorCode || data.notFound || ["failed", "cancelled", "not_found"].includes(data.status)) {
      const reason = typeof data.error === "string" ? data.error : data.error?.message || data.errorCode || data.status
      throw new Error("Jellytoken task failed: " + String(reason).replaceAll(key, "[redacted]"))
    }
    return data
  }
  const videoID = (v) => "jt_" + Buffer.from(JSON.stringify(v)).toString("base64url")
  if (rest.startsWith("/videos/")) {
    const id = rest.split("/")[2]
    let v
    try { v = JSON.parse(Buffer.from(id.slice(3), "base64url").toString()) } catch { throw new Error("Invalid Jellytoken video ID") }
    if (!id.startsWith("jt_") || typeof v.taskId !== "string" || typeof v.model !== "string") throw new Error("Invalid Jellytoken video ID")
    const result = await task(v.taskId)
    if (rest.endsWith("/content")) {
      if (result.status !== "completed") return Response.json({ error: { message: "The video is still being made" } }, { status: 409 })
      const url = result.resultUrlPublic || result.resultUrl
      if (!url) throw new Error("Jellytoken completed the video without a result URL")
      return new Response(null, { status: 307, headers: { location: url } })
    }
    return Response.json({ ...v, id, object: "video", status: result.status === "completed" ? "completed" : "in_progress", progress: result.progress || 0 })
  }
  let body, refs = []
  if (new Headers(init.headers).get("content-type")?.startsWith("multipart/form-data")) {
    const form = await new Response(init.body, { headers: init.headers }).formData()
    body = Object.fromEntries([...form].filter(([, v]) => typeof v === "string"))
    if (form.has("mask")) throw new Error("Jellytoken task API does not support image masks")
    for (const [name, value] of form) {
      if ((name === "image" || name === "image[]") && typeof value !== "string") refs.push("data:" + value.type + ";base64," + Buffer.from(await value.arrayBuffer()).toString("base64"))
    }
  } else {
    body = JSON.parse(init.body)
    refs = (body.images || body.reference_images || []).map((x) => typeof x === "string" ? x : x.image_url)
  }
  if (typeof body.model !== "string" || !body.model || typeof body.prompt !== "string" || !body.prompt.trim()) throw new Error("Jellytoken media needs a model and prompt")
  const video = rest === "/videos"
  const params = { prompt: body.prompt }
  if (refs.some((ref) => typeof ref !== "string" || ref.startsWith("data:"))) {
    throw new Error("Jellytoken image editing needs a public https URL or asset:// reference image; local files cannot be uploaded through the task API")
  }
  if (refs.length) params.refImages = refs
  if (body.size && body.size !== "auto") {
    if (/^\d+:\d+$/.test(body.size)) params.aspectRatio = body.size
    else if (video && /^\d+x\d+$/.test(body.size)) {
      const [w, h] = body.size.split("x").map(Number)
      params.resolution = Math.min(w, h) >= 1080 ? "1080p" : Math.min(w, h) >= 720 ? "720p" : "480p"
      params.aspectRatio = w === h ? "1:1" : w > h ? "16:9" : "9:16"
    } else params.resolution = body.size
  }
  if (video) {
    if (body.seconds) params.duration = Number(body.seconds)
    if (body.input_reference) params.firstFrame = body.input_reference.image_url
  } else {
    const extra = {}
    for (const field of ["quality", "background", "output_format"]) if (body[field] && body[field] !== "auto") extra[field] = body[field]
    if (Object.keys(extra).length) params.extraParamsJson = JSON.stringify(extra)
  }
  const submit = () => task("", { modelKey: body.model, callbackId: crypto.randomUUID(), [video ? "videoParams" : "imageParams"]: params })
  if (video) {
    const v = await submit()
    if (typeof v.taskId !== "string" || !v.taskId) throw new Error("Jellytoken returned no task ID")
    return Response.json({ id: videoID({ taskId: v.taskId, model: body.model, seconds: String(body.seconds || ""), size: body.size }), model: body.model, status: "queued" })
  }
  const images = []
  for (let i = 0; i < Math.max(1, Math.min(Number(body.n) || 1, 4)); i++) {
    let v = await submit()
    if (typeof v.taskId !== "string" || !v.taskId) throw new Error("Jellytoken returned no task ID")
    const id = v.taskId
    while (v.status !== "completed") {
      await new Promise((resolve, reject) => {
        const timer = setTimeout(done, 2000)
        const abort = () => { clearTimeout(timer); signal.removeEventListener("abort", abort); reject(signal.reason) }
        function done() { signal.removeEventListener("abort", abort); resolve() }
        signal.addEventListener("abort", abort, { once: true })
        if (signal.aborted) abort()
      })
      v = await task(id)
    }
    const url = v.resultUrlPublic || v.resultUrl
    if (!url) throw new Error("Jellytoken completed the image without a result URL")
    images.push({ url })
  }
  return Response.json({ model: body.model, data: images })
}

async function catalog(base, key, api = "responses") {
  const request = (headers) => fetch(base + "/models", {
    headers, redirect: "manual", signal: AbortSignal.timeout(15000),
  })
  let response = await request({ authorization: "Bearer " + key })
  if (response.status === 401 || response.status === 403) {
    await response.body?.cancel()
    response = await request({ "x-api-key": key })
  }
  if (response.status !== 200) {
    await response.body?.cancel()
    // Never include upstream text: it can echo the credential.
    throw new Error("Jellytoken model list: HTTP " + response.status)
  }
  const reader = response.body.getReader()
  const chunks = []
  let size = 0
  try {
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      size += value.length
      if (size > 8 * 1024 * 1024) throw new Error("Jellytoken model list is too large")
      chunks.push(value)
    }
  } finally {
    await reader.cancel()
  }
  let body
  try {
    body = JSON.parse(Buffer.concat(chunks).toString("utf8"))
  } catch {
    throw new Error("Jellytoken returned an invalid model list")
  }
  if (!body || typeof body !== "object" || businessFailure(body)) throw new Error("Jellytoken model list: business error")
  if (!Array.isArray(body.data) || body.data.length === 0) throw new Error("Jellytoken returned no models")
  const models = {}
  for (const row of body.data) {
    const kind = typeof row?.type === "string" ? row.type.toUpperCase() : ""
    if (typeof row?.id !== "string" || !row.id.trim() || !["TEXT", "IMAGE", "VIDEO"].includes(kind)) {
      throw new Error("Jellytoken returned an unsupported model entry")
    }
    if (Object.hasOwn(models, row.id)) throw new Error("Jellytoken returned a duplicate model ID")
    // Type describes output, not protocol, vision input, tools or pricing.
    Object.defineProperty(models, row.id, { enumerable: true, value: {
      id: row.id, name: row.id, providerID: ID,
      api: { id: row.id, url: base, npm: packages[kind === "TEXT" ? api : "openai"] },
      decides: kind === "TEXT" && api === "decide",
      capabilities: { output: { [kind.toLowerCase()]: true } },
      limit: { context: 0, output: 0 },
    } })
  }
  return models
}

export default {
  id: "jellytoken",
  server: async (_input, options = {}) => {
    options ??= {}
    let origin = ORIGIN
    // Only the private Go test launcher permits loopback HTTP. Production
    // connection drafts and saved metadata require an explicit HTTPS base.
    if (options.testOrigin !== undefined) {
      const url = new URL(options.testOrigin)
      if (!process.env.MAGPIE_TEST_SANDBOX || url.protocol !== "http:" || url.hostname !== "127.0.0.1" || url.pathname !== "/" || url.search || url.hash || url.username || url.password) {
        throw new Error("Jellytoken test origin must be sandboxed loopback")
      }
      origin = url.origin
    }
    const connection = (auth, draft = {}) => {
      const saved = auth?.metadata?.connection || {}
      const api = draft.api ?? saved.api ?? "responses"
      if (!Object.hasOwn(packages, api)) throw new Error("Unsupported Jellytoken protocol")
      const raw = draft.baseURL ?? saved.baseURL ?? origin + "/v1"
      let url
      try { url = new URL(raw) } catch { throw new Error("Invalid Jellytoken Base URL") }
      const test = options.testOrigin && process.env.MAGPIE_TEST_SANDBOX && url.protocol === "http:" && url.hostname === "127.0.0.1"
      if ((!test && url.protocol !== "https:") || url.username || url.password || url.search || url.hash) throw new Error("Jellytoken Base URL must use HTTPS without credentials, query or fragment")
      const root = url.href.replace(/\/+$/, "").replace(/\/v1(?:beta)?$/, "")
      return { api, baseURL: root + (api === "anthropic" ? "" : api === "gemini" ? "/v1beta" : "/v1"), catalog: root + "/v1", wire: root + (api === "gemini" ? "/v1beta" : "/v1") }
    }
    const headersFor = (api, key) => api === "anthropic" ? { "x-api-key": key, "anthropic-version": "2023-06-01" }
      : api === "gemini" ? { "x-goog-api-key": key } : { authorization: "Bearer " + key }
    const probe = async (c, key, api, model) => {
      const base = connection(null, { baseURL: c.baseURL, api })
      const path = { openai: "/chat/completions", responses: "/responses", anthropic: "/messages", gemini: "/models/" + encodeURIComponent(model) + ":generateContent", decide: "/systemone" }[api]
      const body = api === "responses" ? { model, input: "ping", max_output_tokens: 16 }
        : api === "gemini" ? { contents: [{ role: "user", parts: [{ text: "ping" }] }], generationConfig: { maxOutputTokens: 1 } }
        : api === "decide" ? { model, state: { text: "ping" }, questions: { ok: { type: "noul", instructions: "Is this a test?" } } }
        : { model, messages: [{ role: "user", content: "ping" }], max_tokens: 1 }
      const start = Date.now()
      try {
        const res = await fetch(base.wire + path, { method: "POST", headers: { "content-type": "application/json", ...headersFor(api, key) }, body: JSON.stringify(body), redirect: "manual", signal: AbortSignal.timeout(12000) })
        let ok = false, error = "HTTP " + res.status
        if (res.ok && res.body) {
          const reader = res.body.getReader(), chunks = []
          let size = 0
          try {
            for (;;) {
              const { done, value } = await reader.read()
              if (done) break
              size += value.length
              if (size > 256 * 1024) throw new Error("oversized probe reply")
              chunks.push(value)
            }
            const reply = JSON.parse(Buffer.concat(chunks).toString("utf8"))
            ok = !!reply && !businessFailure(reply) && (api === "openai" ? Array.isArray(reply.choices)
              : api === "responses" ? Array.isArray(reply.output) : api === "anthropic" ? Array.isArray(reply.content)
              : api === "gemini" ? Array.isArray(reply.candidates) : !!reply.answers && Object.keys(reply.answers).length > 0)
          } catch { ok = false } finally { await reader.cancel() }
          error = ok ? "" : "Jellytoken returned an unexpected API response"
        } else await res.body?.cancel()
        // Only status/shape reaches the GUI; upstream text may echo the key.
        return { protocol: api, base: base.baseURL, model, ok, status: res.status, ms: Date.now() - start, error }
      } catch { return { protocol: api, base: base.baseURL, model, ok: false, ms: Date.now() - start, error: "Jellytoken connection failed or timed out" } }
    }
    const manage = async (current, request) => {
      const c = connection(current, request)
      const { baseURL, api } = c
      if (request.action === "config") return { baseURL, api, signedIn: !!current?.key }
      const key = credential(request.key?.trim() ? { type: "api", key: request.key } : current)
      if (request.action === "save") {
        const models = await catalog(c.catalog, key, api)
        // Returned only to host.js, which commits this single record after
        // validation and strips it before replying to the GUI.
        return { baseURL, api, first: !current?.key, count: Object.keys(models).length, auth: { type: "api", key, metadata: { ...current?.metadata, connection: { baseURL, api } } } }
      }
      if (request.action !== "detect") throw new Error("Unsupported Jellytoken operation")
      const deadline = Date.now() + 40000
      let ids = request.models?.filter((id) => typeof id === "string" && id.trim()) || []
      if (ids.length > 24) throw new Error("Test at most 24 models at a time")
      if (!ids.length) ids = [Object.keys(await catalog(c.catalog, key, api))[0]]
      const results = []
      // Five probes per model, with bounded parallelism and no saved changes.
      for (const model of ids) {
        if (Date.now() > deadline) throw new Error("Jellytoken detection timed out; test fewer models")
        results.push(...await Promise.all(Object.keys(packages).map((pr) => probe(c, key, pr, model))))
      }
      return { results }
    }
    return {
      config: async (cfg) => {
        cfg.provider ??= {}
        cfg.provider[ID] = { name: "Jellytoken", npm: "@ai-sdk/openai", api: origin + "/v1", decide: true, models: {} }
      },
      provider: {
        id: ID,
        models: async (_provider, { auth }) => {
          if (auth?.type !== "api") return {}
          const c = connection(auth)
          return catalog(c.catalog, credential(auth), c.api)
        },
      },
      auth: {
        provider: ID,
        single: true,
        methods: [{ type: "api", label: "API key" }],
        jellytool: manage,
        loader: async (getAuth) => ({
          apiKey: "jellytool-plugin", baseURL: connection(await getAuth()).wire,
          fetch: async (input, init = {}) => {
            const url = new URL(typeof input === "string" ? input : input.url)
            const auth = await getAuth(), c = connection(auth)
            const wire = new URL(c.wire)
            const prefix = wire.pathname.replace(/\/$/, "")
            const rest = url.pathname.slice(prefix.length)
            const method = (init.method ?? "GET").toUpperCase()
            const mediaPost = ["/images/generations", "/images/edits", "/videos"].includes(rest)
            const mediaGet = /^\/videos\/[^/]+(?:\/content)?$/.test(rest)
            const allowed = method === "POST" && (mediaPost || ["/chat/completions", "/responses", "/messages", "/systemone"].includes(rest) || /^\/models\/.+:(?:streamGenerateContent|generateContent)$/.test(rest)) || method === "GET" && mediaGet
            if (url.origin !== wire.origin || !url.pathname.startsWith(prefix + "/") || !allowed || url.username || url.password) {
              throw new Error("Jellytoken request route is not supported")
            }
            const key = credential(auth)
            if (mediaPost || mediaGet) return mediaFetch(c, key, rest, init)
            const headers = new Headers(init.headers)
            headers.delete("authorization")
            headers.delete("x-api-key")
            headers.delete("cookie")
            headers.delete("host")
            headers.delete("proxy-authorization")
            headers.delete("x-goog-api-key")
            for (const [k, v] of Object.entries(headersFor(rest === "/messages" ? "anthropic" : rest.startsWith("/models/") ? "gemini" : "openai", key))) {
              if (k !== "anthropic-version" || !headers.has(k)) headers.set(k, v)
            }
            return fetch(url, { ...init, headers, redirect: "manual" })
          },
        }),
      },
    }
  },
}
