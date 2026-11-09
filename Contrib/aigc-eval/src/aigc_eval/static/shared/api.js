/**
 * Shared JSON transport for every frontend entry point.
 *
 * Keeping response parsing and error normalization here prevents each page
 * from silently interpreting the same backend failure in a different way.
 */
export async function fetchJson(url, options = {}) {
  const response = await fetch(url, options);
  let payload;
  try {
    payload = await response.json();
  } catch {
    payload = {};
  }
  if (!response.ok || payload.error) {
    const error = new Error(payload.error || `请求失败（HTTP ${response.status}）`);
    // 带上状态码，调用方才能区分「这次不行，重试可能成功」（5xx / 429 / 断网）
    // 和「重试多少次都不可能成功」（4xx，例如任务已过期）。轮询器依赖这个区分，
    // 否则会把永久性错误当瞬时故障无限重试，界面就一直停在运行中。
    error.status = response.status;
    error.payload = payload;
    throw error;
  }
  return payload;
}
