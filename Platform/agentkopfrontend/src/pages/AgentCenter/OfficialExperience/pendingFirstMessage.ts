/**
 * 占位 session 下首次发消息：先建 ses_ 再跳转，跳转后需等账本就绪再 send
 */
import type { MessageAttachment, SendMessageTtsOptions, VirtualTryOnUserInput } from '@/pages/AgentPlayground/types';

export interface PendingFirstMessage {
  text: string;
  attachments?: MessageAttachment[];
  tts?: SendMessageTtsOptions;
  userInput?: VirtualTryOnUserInput;
}

const keyOf = (sessionId: string) => `official-pending-first-message:${sessionId}`;

export function savePendingFirstMessage(
  sessionId: string,
  text: string,
  attachments?: MessageAttachment[],
  tts?: SendMessageTtsOptions,
  userInput?: VirtualTryOnUserInput,
) {
  const payload: PendingFirstMessage = {
    text,
    ...(attachments && attachments.length > 0 ? { attachments } : {}),
    ...(tts ? { tts } : {}),
    ...(userInput ? { userInput } : {}),
  };
  sessionStorage.setItem(keyOf(sessionId), JSON.stringify(payload));
}

export function takePendingFirstMessage(sessionId: string): PendingFirstMessage | null {
  const key = keyOf(sessionId);
  const raw = sessionStorage.getItem(key);
  if (raw == null) return null;
  sessionStorage.removeItem(key);

  // 兼容旧版纯字符串
  if (!raw.startsWith('{')) {
    return { text: raw };
  }
  try {
    const parsed = JSON.parse(raw) as PendingFirstMessage;
    if (typeof parsed?.text !== 'string') return null;
    const tts =
      parsed.tts &&
      typeof parsed.tts.engine === 'string' &&
      typeof parsed.tts.voiceId === 'string'
        ? parsed.tts
        : undefined;
    const productImageUrls = Array.isArray(parsed.userInput?.productImageUrls)
      ? parsed.userInput.productImageUrls.filter((url): url is string => typeof url === 'string')
      : undefined;
    const modelImageUrl =
      typeof parsed.userInput?.modelImageUrl === 'string'
        ? parsed.userInput.modelImageUrl
        : undefined;
    const userInput =
      productImageUrls && productImageUrls.length > 0
        ? {
            productImageUrls,
            ...(modelImageUrl ? { modelImageUrl } : {}),
          }
        : undefined;
    return {
      text: parsed.text,
      attachments: Array.isArray(parsed.attachments) ? parsed.attachments : undefined,
      tts,
      userInput,
    };
  } catch {
    return { text: raw };
  }
}
