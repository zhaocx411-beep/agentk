/** AgentK 建立 execution ↔ worker 关系的前端保活窗口。 */
export const EXECUTION_BIND_GRACE_MS = 1000;

export function remainingExecutionGrace(
  graceUntil: number,
  now = Date.now(),
): number {
  return Math.max(0, graceUntil - now);
}

export function abortAfterExecutionGrace(
  controller: AbortController,
  delay: number,
): ReturnType<typeof setTimeout> {
  return setTimeout(() => {
    if (!controller.signal.aborted) controller.abort();
  }, Math.max(0, delay));
}
