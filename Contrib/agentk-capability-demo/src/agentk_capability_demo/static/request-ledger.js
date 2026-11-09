(function (root, factory) {
  const api = factory();
  if (typeof module === "object" && module.exports) module.exports = api;
  root.AgentKRequestLedger = api;
})(typeof globalThis !== "undefined" ? globalThis : this, function () {
  "use strict";

  function validTimestamp(value) {
    return typeof value === "string" && value.trim() && !Number.isNaN(new Date(value).valueOf());
  }

  function collectRequestEntries(entryRequest, events = [], entryRequestedAt = "") {
    const entries = [];
    const byCallId = new Map();
    if (entryRequest && typeof entryRequest === "object") {
      const entryTime = validTimestamp(entryRequestedAt) ? entryRequestedAt : "";
      entries.push({
        id: "entry",
        kind: "ENTRY",
        name: entryRequest.pipeline || "execution",
        request: entryRequest,
        ...(entryTime
          ? { requestedAt: entryTime, requestedAtSource: "created" }
          : {}),
      });
    }
    events.forEach((event, index) => {
      const data = event && event.data;
      if (event?.type !== "progress") return;
      if (data?.kind === "request" && data.request && typeof data.request === "object") {
        const createdAt = validTimestamp(event.createdAt) ? event.createdAt : "";
        const observedAt = validTimestamp(event.observedAt) ? event.observedAt : "";
        const requestedAt = createdAt || observedAt;
        const entry = {
          id: event.id || `request-${index + 1}`,
          ...(data.callId ? { callId: data.callId } : {}),
          kind: String(data.requestKind || "call").toUpperCase(),
          name: String(data.name || event.step || "request"),
          request: data.request,
          ...(requestedAt
            ? {
                requestedAt,
                requestedAtSource: createdAt ? "created" : "observed",
              }
            : {}),
        };
        entries.push(entry);
        if (entry.callId) byCallId.set(entry.callId, entry);
        return;
      }
      if (data?.kind !== "response" || !("response" in data)) return;
      let entry = data.callId ? byCallId.get(data.callId) : null;
      if (!entry) {
        const kind = String(data.responseKind || "call").toUpperCase();
        const name = String(data.name || event.step || "response");
        entry = entries.find(item => item.kind === kind && item.name === name && !("response" in item));
      }
      if (entry) entry.response = data.response;
    });
    return entries;
  }

  function withObservedRequestTimes(events = [], observedTimes = new Map(), observedAt = "") {
    if (!Array.isArray(events)) return [];
    const fallback = observedAt || new Date().toISOString();
    return events.map((event, index) => {
      if (
        !event
        || typeof event !== "object"
        || validTimestamp(event.createdAt)
        || validTimestamp(event.observedAt)
        || event.type !== "progress"
        || event.data?.kind !== "request"
      ) return event;
      const key = String(
        event.id
        || event.data.callId
        || `${event.data.requestKind || "call"}:${event.data.name || event.step || "request"}:${index}`,
      );
      const firstSeen = observedTimes.get(key) || fallback;
      observedTimes.set(key, firstSeen);
      return { ...event, observedAt: firstSeen };
    });
  }

  function eventTimings(events = []) {
    if (!Array.isArray(events)) return [];
    const requestTimes = new Map();
    const responseTimes = new Map();

    events.forEach(event => {
      if (!event || typeof event !== "object") return;
      const createdAt = validTimestamp(event.createdAt) ? event.createdAt : "";
      const callId = typeof event.data?.callId === "string" ? event.data.callId : "";
      if (!createdAt || !callId || event.type !== "progress") return;
      if (event.data?.kind === "request" && !requestTimes.has(callId)) {
        requestTimes.set(callId, createdAt);
      }
      if (event.data?.kind === "response" && !responseTimes.has(callId)) {
        responseTimes.set(callId, createdAt);
      }
    });

    return events.map(event => {
      if (!event || typeof event !== "object") return {};
      const createdAt = validTimestamp(event.createdAt) ? event.createdAt : "";
      const callId = typeof event.data?.callId === "string" ? event.data.callId : "";
      if (event.type === "progress" && event.data?.kind === "request") {
        return {
          ...(createdAt ? { startedAt: createdAt } : {}),
          ...(callId && responseTimes.has(callId) ? { endedAt: responseTimes.get(callId) } : {}),
        };
      }
      if (event.type === "progress" && event.data?.kind === "response") {
        return {
          ...(callId && requestTimes.has(callId) ? { startedAt: requestTimes.get(callId) } : {}),
          ...(createdAt ? { endedAt: createdAt } : {}),
        };
      }
      if (event.type === "finished") return createdAt ? { endedAt: createdAt } : {};
      return createdAt ? { startedAt: createdAt } : {};
    });
  }

  function selectTaskInput(entryRequest, events = []) {
    const entries = collectRequestEntries(
      entryRequest,
      Array.isArray(events) ? events : [],
    );
    const entry = entries.find(item => item.kind === "ENTRY");
    if (entry) return entry.request;
    const workflow = entries.find(item => item.kind === "WORKFLOW");
    const selected = workflow || entries[0];
    if (!selected) return null;
    const request = selected.request;
    if (
      request
      && typeof request === "object"
      && Object.prototype.hasOwnProperty.call(request, "pipeline")
      && Object.prototype.hasOwnProperty.call(request, "args")
    ) {
      return request;
    }
    return { pipeline: selected.name, args: request };
  }

  const videoUriKeys = ["videoUri", "videoUrl", "sourceVideoUri"];

  function safeHttpUri(value) {
    if (typeof value !== "string") return "";
    const candidate = value.trim();
    if (!/^https?:\/\//i.test(candidate)) return "";
    try {
      const parsed = new URL(candidate);
      if (!parsed.hostname || parsed.username || parsed.password) return "";
      if (parsed.protocol !== "http:" && parsed.protocol !== "https:") return "";
    } catch {
      return "";
    }
    return candidate;
  }

  function findInputVideoUri(input) {
    if (!input || typeof input !== "object") return "";
    const queue = [input];
    const visited = new WeakSet();
    while (queue.length) {
      const value = queue.shift();
      if (!value || typeof value !== "object" || visited.has(value)) continue;
      visited.add(value);
      for (const key of videoUriKeys) {
        if (!Object.prototype.hasOwnProperty.call(value, key)) continue;
        const uri = safeHttpUri(value[key]);
        if (uri) return uri;
      }
      Object.values(value).forEach(child => {
        if (child && typeof child === "object") queue.push(child);
      });
    }
    return "";
  }

  return {
    collectRequestEntries,
    eventTimings,
    findInputVideoUri,
    selectTaskInput,
    withObservedRequestTimes,
  };
});
