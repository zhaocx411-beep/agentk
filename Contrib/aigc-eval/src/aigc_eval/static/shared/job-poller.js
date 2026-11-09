import { fetchJson } from "./api.js";

/**
 * Convert every backend job event to the stage contract the UI renders.
 *
 * Backends in flight emit either step_id/output_text or id/output. Accepting
 * both shapes here keeps a rolling deployment safe.
 */
export function normalizeJobEvent(event) {
  if (!event || typeof event !== "object") return null;
  if (event.type === "notice") {
    return {
      ...event,
      detail: String(event.detail || event.output || event.output_text || ""),
    };
  }

  const id = event.id || event.step_id;
  if (event.type !== "stage" && !id) return null;
  return {
    ...event,
    type: "stage",
    id: String(id || event.title || ""),
    kind: String(event.kind || "tool"),
    title: String(event.title || id || ""),
    status: String(event.status || "running"),
    input: String(event.input || event.input_text || ""),
    output: String(event.output || event.output_text || ""),
    detail: String(event.detail || ""),
  };
}

/**
 * 这次失败是否值得重试。
 *
 * 没有 status 说明连响应都没拿到（断网、代理抖动），值得重试；5xx / 429 是
 * 服务端临时状态，也值得。其余 4xx 是确定性的——尤其 404「任务不存在或已过期」，
 * 服务重启或作业被淘汰后必然如此，重试到天荒地老也不会变，只会让界面一直
 * 停在「运行中」，把一个明确的失败伪装成卡死。
 */
function isTransient(error) {
  const status = error && error.status;
  if (!status) return true;
  return status >= 500 || status === 429;
}

function wait(ms, signal) {
  if (signal && signal.aborted) return Promise.resolve();
  return new Promise((resolve) => {
    let timer;
    const finish = () => {
      if (timer) window.clearTimeout(timer);
      if (signal) signal.removeEventListener("abort", finish);
      resolve();
    };
    timer = window.setTimeout(finish, ms);
    if (signal) signal.addEventListener("abort", finish, { once: true });
  });
}

/**
 * Poll one background job until it completes, fails, or is cancelled.
 *
 * Network failures are retried by default because analysis jobs outlive a
 * transient proxy disconnect. Callers still receive terminal backend errors.
 */
export async function pollJob({
  jobId,
  intervalMs = 1000,
  retryDelayMs = 1200,
  retryNetworkErrors = true,
  signal,
  onEvents,
}) {
  let cursor = 0;
  while (!signal || !signal.aborted) {
    let snapshot;
    try {
      snapshot = await fetchJson(
        `./api/job?job_id=${encodeURIComponent(jobId)}&cursor=${cursor}`,
        signal ? { signal } : {},
      );
    } catch (error) {
      if (signal && signal.aborted) return { status: "cancelled" };
      if (!retryNetworkErrors || !isTransient(error)) throw error;
      await wait(retryDelayMs, signal);
      continue;
    }

    if (typeof snapshot.next_cursor === "number") cursor = snapshot.next_cursor;
    const events = (snapshot.events || []).map(normalizeJobEvent).filter(Boolean);
    if (events.length && onEvents) onEvents(events);

    if (snapshot.status !== "running") return snapshot;
    await wait(intervalMs, signal);
  }
  return { status: "cancelled" };
}
