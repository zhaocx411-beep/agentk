/**
 * 自主 Agent：每次生成可能先新建 ses_ 再跳转。
 * 跳转后页面会 loadSession，必须等账本就绪再 send，否则会被 startSession 重置冲掉。
 * 用 sessionStorage 把待发送文本挂在目标 sessionId 上。
 */

export interface PendingGenerate {
  text: string;
  prompt: string;
  intentPreview: string;
  payloadPreview: string;
  templateName: string;
  scene: string;
}

const keyOf = (sessionId: string) => `official-pending-generate:${sessionId}`;

export function savePendingGenerate(sessionId: string, pending: PendingGenerate) {
  sessionStorage.setItem(keyOf(sessionId), JSON.stringify(pending));
}

export function takePendingGenerate(sessionId: string): PendingGenerate | null {
  const key = keyOf(sessionId);
  const raw = sessionStorage.getItem(key);
  if (!raw) return null;
  sessionStorage.removeItem(key);
  try {
    return JSON.parse(raw) as PendingGenerate;
  } catch {
    return null;
  }
}
