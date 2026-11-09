/**
 * 工作流占位 session：先 quick session 再跳转，跳转后在新页发起 pipeline-runs
 */

export interface PendingPipelineGenerate {
  workflow: string;
  /** official/defaults 的 pipelineName；缺省时回退 workflow */
  pipeline?: string;
  args: Record<string, unknown>;
}

const keyOf = (sessionId: string) => `official-pending-pipeline:${sessionId}`;

export function savePendingPipelineGenerate(sessionId: string, pending: PendingPipelineGenerate) {
  sessionStorage.setItem(keyOf(sessionId), JSON.stringify(pending));
}

export function peekPendingPipelineGenerate(sessionId: string): boolean {
  return sessionStorage.getItem(keyOf(sessionId)) != null;
}

export function takePendingPipelineGenerate(sessionId: string): PendingPipelineGenerate | null {
  const key = keyOf(sessionId);
  const raw = sessionStorage.getItem(key);
  if (!raw) return null;
  sessionStorage.removeItem(key);
  try {
    return JSON.parse(raw) as PendingPipelineGenerate;
  } catch {
    return null;
  }
}
