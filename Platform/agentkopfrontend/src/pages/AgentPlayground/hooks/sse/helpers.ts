/**
 * SSE 纯工具函数（无 React 依赖，可独立测试）
 */
import {
  attachmentsFromUserInput,
  pickUploadMediaUrl,
  splitComposedUserMessage,
} from '@/services/media';
import type {
  DeltaPayload,
  MessageAttachment,
  MessageItem,
  MessagePart,
  SessionLedgerEvent,
  ToolCardState,
} from '../../types';

/** 将 unknown 错误转为可读字符串 */
export function asErrorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return '发送失败';
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value);
}

function textsFromParts(parts: unknown): string {
  if (!Array.isArray(parts)) return '';
  const texts: string[] = [];
  for (const raw of parts) {
    if (!isRecord(raw)) continue;
    const type = typeof raw.type === 'string' ? raw.type : '';
    // text / input_text / output_text；无 type 时也认 text 字段
    if (
      (!type || type === 'text' || type === 'input_text' || type === 'output_text') &&
      typeof raw.text === 'string' &&
      raw.text.trim()
    ) {
      texts.push(raw.text.trim());
      continue;
    }
    if (typeof raw.content === 'string' && raw.content.trim()) {
      texts.push(raw.content.trim());
    }
  }
  return texts.join('\n').trim();
}

function textsFromContent(content: unknown): string {
  if (typeof content === 'string') return content.trim();
  if (!Array.isArray(content)) return '';
  const texts: string[] = [];
  for (const raw of content) {
    if (typeof raw === 'string' && raw.trim()) {
      texts.push(raw.trim());
      continue;
    }
    if (!isRecord(raw)) continue;
    if (typeof raw.text === 'string' && raw.text.trim()) {
      texts.push(raw.text.trim());
      continue;
    }
    if (typeof raw.content === 'string' && raw.content.trim()) {
      texts.push(raw.content.trim());
    }
  }
  return texts.join('\n').trim();
}

function attachmentFromUnknown(raw: unknown): MessageAttachment | null {
  if (typeof raw === 'string' && raw.trim()) {
    return { url: raw.trim() };
  }
  if (!isRecord(raw)) return null;
  const cdnUrl = typeof raw.cdnUrl === 'string' ? raw.cdnUrl.trim() : '';
  const fileUrl = typeof raw.fileUrl === 'string' ? raw.fileUrl.trim() : '';
  const blobUrl = typeof raw.blobUrl === 'string' ? raw.blobUrl.trim() : '';
  const legacyUrl =
    (typeof raw.url === 'string' && raw.url.trim()) ||
    (typeof raw.uri === 'string' && raw.uri.trim()) ||
    (typeof raw.mediaUrl === 'string' && raw.mediaUrl.trim()) ||
    '';
  const url = pickUploadMediaUrl({ blobUrl, cdnUrl, fileUrl, url: legacyUrl });
  if (!url) return null;
  return {
    url,
    cdnUrl: cdnUrl || undefined,
    fileUrl: fileUrl || undefined,
    blobUrl: blobUrl || undefined,
    fileId: typeof raw.fileId === 'string' ? raw.fileId : undefined,
    mediaType:
      typeof raw.mediaType === 'string'
        ? raw.mediaType
        : typeof raw.type === 'string'
          ? raw.type
          : undefined,
    contentType:
      typeof raw.contentType === 'string'
        ? raw.contentType
        : typeof raw.mimeType === 'string'
          ? raw.mimeType
          : typeof raw.mime_type === 'string'
            ? raw.mime_type
            : undefined,
    name:
      typeof raw.originalFilename === 'string'
        ? raw.originalFilename
        : typeof raw.name === 'string'
          ? raw.name
          : typeof raw.fileName === 'string'
            ? raw.fileName
            : undefined,
  };
}

/** 从 user.input payload 取出附件列表 */
export function extractUserAttachments(payload: unknown): MessageAttachment[] {
  if (!isRecord(payload)) return [];
  const out: MessageAttachment[] = [];
  const seen = new Set<string>();

  const push = (item: MessageAttachment | null) => {
    if (!item || seen.has(item.url)) return;
    seen.add(item.url);
    out.push(item);
  };

  for (const key of ['attachments', 'files', 'media'] as const) {
    const list = payload[key];
    if (!Array.isArray(list)) continue;
    for (const raw of list) push(attachmentFromUnknown(raw));
  }

  if (Array.isArray(payload.images)) {
    for (const raw of payload.images) push(attachmentFromUnknown(raw));
  }

  if (isRecord(payload.data) && payload.data !== payload) {
    for (const item of extractUserAttachments(payload.data)) push(item);
  }

  if (isRecord(payload.userInput)) {
    for (const item of attachmentsFromUserInput(payload.userInput)) push(item);
  }

  return out;
}

/** 附件 → 消息 parts（图片 / 视频按 image|file 区分，视频在 UI 用 video 标签） */
export function attachmentsToMessageParts(attachments: MessageAttachment[]): MessagePart[] {
  const parts: MessagePart[] = [];
  for (const file of attachments) {
    const isImage =
      file.mediaType === 'image' ||
      (file.mediaType?.startsWith('image/') ?? false) ||
      /\.(png|jpe?g|webp|gif|bmp|svg)(\?|$)/i.test(file.url);
    if (isImage) {
      parts.push({ type: 'image', url: file.url, name: file.name });
    } else {
      parts.push({
        type: 'file',
        url: file.url,
        name: file.name,
        mediaType: file.mediaType,
        contentType: file.contentType,
      });
    }
  }
  return parts;
}

/** 消息附件展示类型：优先结构化媒体类型，URL 扩展名仅作历史数据兜底。 */
export function resolveMessagePartMediaKind(
  part: Extract<MessagePart, { type: 'image' | 'file' }>,
): 'image' | 'video' | 'audio' | 'file' {
  if (part.type === 'image') return 'image';
  const declared = part.mediaType?.trim().toLowerCase();
  const contentType = part.contentType?.trim().toLowerCase();
  if (declared === 'video' || declared?.startsWith('video/') || contentType?.startsWith('video/')) {
    return 'video';
  }
  if (declared === 'audio' || declared?.startsWith('audio/') || contentType?.startsWith('audio/')) {
    return 'audio';
  }
  if (/\.(mp4|webm|mov|m4v|avi|mkv)(\?|#|$)/i.test(part.url)) return 'video';
  if (/\.(mp3|wav|aac|m4a|ogg|flac)(\?|#|$)/i.test(part.url)) return 'audio';
  return 'file';
}

/**
 * 规范化用户消息 parts：去掉拼进 text 的附件清单，改为独立 image/file parts。
 * 气泡展示 = 缩略图行 + 纯文案气泡。
 */
export function normalizeUserMessageParts(
  rawText: string,
  explicitAttachments: MessageAttachment[] = [],
): MessagePart[] {
  const split = splitComposedUserMessage(rawText);
  const byUrl = new Map<string, MessageAttachment>();
  for (const item of [...explicitAttachments, ...split.attachments]) {
    if (item.url) byUrl.set(item.url, item);
  }
  const parts: MessagePart[] = [];
  if (split.displayText) parts.push({ type: 'text', text: split.displayText });
  parts.push(...attachmentsToMessageParts([...byUrl.values()]));
  return parts;
}

/** 用户气泡展示：从 parts 解析出缩略图 + 纯文案（兼容结构化 user.input 内嵌 URL） */
export function resolveUserMessageDisplay(message: MessageItem): {
  text: string;
  media: Extract<MessagePart, { type: 'image' | 'file' }>[];
} {
  const rawText = message.parts
    .filter((part): part is Extract<MessagePart, { type: 'text' }> => part.type === 'text')
    .map((part) => part.text)
    .join('\n')
    .trim();
  const explicitAttachments: MessageAttachment[] = [];
  for (const part of message.parts) {
    if (part.type === 'image' || part.type === 'file') {
      explicitAttachments.push({
        url: part.url,
        name: part.name,
        mediaType: part.type === 'image' ? 'image' : part.mediaType,
        contentType: part.type === 'file' ? part.contentType : undefined,
      });
    }
  }
  const normalized = normalizeUserMessageParts(rawText, explicitAttachments);
  const text = normalized
    .filter((part): part is Extract<MessagePart, { type: 'text' }> => part.type === 'text')
    .map((part) => part.text)
    .join('\n')
    .trim();
  const media = normalized.filter(
    (part): part is Extract<MessagePart, { type: 'image' | 'file' }> =>
      part.type === 'image' || part.type === 'file',
  );
  return { text, media };
}

function appendInputMediaUrls(source: unknown, urls: Set<string>): void {
  if (typeof source === 'string' && /^https?:\/\//i.test(source.trim())) {
    urls.add(source.trim());
    return;
  }
  if (Array.isArray(source)) {
    for (const item of source) appendInputMediaUrls(item, urls);
    return;
  }
  if (!isRecord(source)) return;
  for (const key of [
    'referenceImages',
    'productImageUrls',
    'modelImageUrl',
    'images',
    'imageUrls',
    'inputImages',
  ] as const) {
    appendInputMediaUrls(source[key], urls);
  }
}

/** 会话内用户上传 / 工具参考图 URL，用于从「当前产物」排除输入素材 */
export function collectSessionInputMediaUrls(options: {
  messages?: MessageItem[];
  toolCards?: ToolCardState[];
  ledgerEvents?: SessionLedgerEvent[];
}): Set<string> {
  const urls = new Set<string>();

  for (const msg of options.messages ?? []) {
    if (msg.role !== 'user') continue;
    const { media } = resolveUserMessageDisplay(msg);
    for (const part of media) {
      const trimmed = part.url.trim();
      if (trimmed) urls.add(trimmed);
    }
  }

  for (const card of options.toolCards ?? []) {
    appendInputMediaUrls(card.input, urls);
  }

  for (const event of options.ledgerEvents ?? []) {
    if (event.kind === 'user.input') {
      const payload = resolveEventPayload(event);
      const text = extractUserInputText(payload);
      const parts = normalizeUserMessageParts(text, extractUserAttachments(payload));
      for (const part of parts) {
        if (part.type === 'image' || part.type === 'file') {
          const trimmed = part.url.trim();
          if (trimmed) urls.add(trimmed);
        }
      }
      continue;
    }
    if (event.kind === 'assistant.tool_call') {
      const payload = resolveEventPayload(event);
      if (!isRecord(payload)) continue;
      appendInputMediaUrls(payload.args ?? payload.input ?? payload.arguments, urls);
    }
  }

  return urls;
}

/** 从 user.input / item.completed(user) 等 payload 里取出用户正文 */
export function extractUserInputText(payload: unknown): string {
  if (typeof payload === 'string') return payload.trim();
  if (!isRecord(payload)) return '';

  for (const key of ['text', 'message', 'prompt', 'query', 'content_text'] as const) {
    const value = payload[key];
    if (typeof value === 'string' && value.trim()) return value.trim();
  }

  // input 可能是字符串，也可能是 { text } 对象
  if (typeof payload.input === 'string' && payload.input.trim()) {
    return payload.input.trim();
  }
  if (isRecord(payload.input)) {
    const nestedInput = extractUserInputText(payload.input);
    if (nestedInput) return nestedInput;
  }

  const fromContent = textsFromContent(payload.content);
  if (fromContent) return fromContent;

  const fromParts = textsFromParts(payload.parts);
  if (fromParts) return fromParts;

  if (isRecord(payload.item)) {
    const nestedItem = extractUserInputText(payload.item);
    if (nestedItem) return nestedItem;
  }

  // 少数接口把正文再包一层 data / payload
  if (isRecord(payload.data) && payload.data !== payload) {
    const nested = extractUserInputText(payload.data);
    if (nested) return nested;
  }
  if (isRecord(payload.payload) && payload.payload !== payload) {
    const nested = extractUserInputText(payload.payload);
    if (nested) return nested;
  }

  return '';
}

/**
 * 统一解析账本 / SSE 事件正文载荷。
 * 兼容 data / payload / payloadJson；data 为空对象时回退到整事件。
 */
export function resolveEventPayload(raw: unknown): unknown {
  if (!isRecord(raw)) return raw;

  let fromJson: unknown;
  if (typeof raw.payloadJson === 'string' && raw.payloadJson.trim()) {
    try {
      fromJson = JSON.parse(raw.payloadJson) as unknown;
    } catch {
      fromJson = raw.payloadJson;
    }
  }

  const candidates: unknown[] = [raw.data, raw.payload, fromJson, raw];
  for (const candidate of candidates) {
    if (candidate == null) continue;
    if (typeof candidate === 'string' && candidate.trim()) return candidate;
    if (!isRecord(candidate)) continue;
    // 空对象不算有效 data，继续回退
    if (candidate !== raw && Object.keys(candidate).length === 0) continue;
    if (extractUserInputText(candidate) || extractUserAttachments(candidate).length > 0) {
      return candidate;
    }
    // 非 user 事件：优先非空 data/payload
    if (candidate !== raw && Object.keys(candidate).length > 0) {
      return candidate;
    }
  }

  return raw.data ?? raw.payload ?? fromJson ?? raw;
}

function messageTextOf(msg: MessageItem): string {
  return msg.parts
    .filter((part): part is Extract<MessagePart, { type: 'text' }> => part.type === 'text')
    .map((part) => part.text)
    .join('\n')
    .trim();
}

function messageMediaUrls(msg: MessageItem): string[] {
  return msg.parts
    .filter(
      (part): part is Extract<MessagePart, { type: 'image' | 'file' }> =>
        part.type === 'image' || part.type === 'file',
    )
    .map((part) => part.url);
}

/** 乐观气泡与账本回放合并：同文、包含关系、或共享附件 URL */
export function shouldMergeUserMessages(prev: MessageItem, next: MessageItem): boolean {
  if (prev.role !== 'user' || next.role !== 'user') return false;
  const prevText = messageTextOf(prev);
  const nextText = messageTextOf(next);
  if (prevText && nextText) {
    if (prevText === nextText) return true;
    if (nextText.includes(prevText) || prevText.includes(nextText)) return true;
  }
  const prevUrls = messageMediaUrls(prev);
  const nextUrls = messageMediaUrls(next);
  if (prevUrls.length > 0 && nextUrls.some((url) => prevUrls.includes(url))) return true;
  // 一侧只有附件、一侧是拼好的含 URL 正文
  if (prevUrls.length > 0 && nextText && prevUrls.some((url) => nextText.includes(url)))
    return true;
  if (nextUrls.length > 0 && prevText && nextUrls.some((url) => prevText.includes(url)))
    return true;
  return false;
}

/** 合并用户气泡：正文去掉附件清单后取更合适的一句；附件 parts 去重合并 */
export function mergeUserMessageParts(prev: MessageItem, next: MessageItem): MessagePart[] {
  const prevSplit = splitComposedUserMessage(messageTextOf(prev));
  const nextSplit = splitComposedUserMessage(messageTextOf(next));
  // 优先保留「干净原文」；两边都有则取更长的一句
  let text = prevSplit.displayText;
  if (nextSplit.displayText) {
    if (!text || nextSplit.displayText.length >= text.length) text = nextSplit.displayText;
  }
  const mediaByUrl = new Map<string, MessagePart>();
  for (const part of [
    ...attachmentsToMessageParts(prevSplit.attachments),
    ...attachmentsToMessageParts(nextSplit.attachments),
    ...prev.parts,
    ...next.parts,
  ]) {
    if (part.type === 'image' || part.type === 'file') {
      mediaByUrl.set(part.url, part);
    }
  }
  const parts: MessagePart[] = [];
  if (text) parts.push({ type: 'text', text });
  parts.push(...mediaByUrl.values());
  if (parts.length > 0) return parts;
  if (next.parts.length > 0) return next.parts;
  return prev.parts;
}

/** 从历史消息中重建 toolCards 状态
 *  - tool-call part：创建/更新 toolCard（status 取自是否有匹配的 tool-result）
 *  - tool-result part：把对应 toolCard 标记为 ok/err，并填充 output
 */
export function buildToolCardsFromMessages(messages: MessageItem[]): ToolCardState[] {
  const cards: ToolCardState[] = [];
  const cardMap = new Map<string, ToolCardState>();

  for (const msg of messages) {
    for (const part of msg.parts) {
      if (part.type === 'tool-call') {
        const extra = part as unknown as Record<string, unknown>;
        let card = cardMap.get(part.toolCallId);
        if (!card) {
          card = {
            toolCallId: part.toolCallId,
            toolName: part.toolName,
            status: 'running',
            input: part.input,
            // 后端未来可能在 tool-call part 返回 durationMillis，防御性读取
            duration: extra.durationMillis as number | undefined,
          };
          cards.push(card);
          cardMap.set(part.toolCallId, card);
        } else {
          card.toolName = part.toolName;
          card.input = part.input;
          if (extra.durationMillis != null) {
            card.duration = extra.durationMillis as number;
          }
        }
      } else if (part.type === 'tool-result') {
        const extra = part as unknown as Record<string, unknown>;
        let card = cardMap.get(part.toolCallId);
        if (!card) {
          card = {
            toolCallId: part.toolCallId,
            toolName: '',
            status: part.isError ? 'err' : 'ok',
            output: part.output,
            // tool-result 同样可能携带 durationMillis
            duration: extra.durationMillis as number | undefined,
          };
          cards.push(card);
          cardMap.set(part.toolCallId, card);
        } else {
          card.output = part.output;
          card.status = part.isError ? 'err' : 'ok';
          if (extra.durationMillis != null) {
            card.duration = extra.durationMillis as number;
          }
        }
      }
    }
  }

  return cards;
}

/** 从一段自由文本里捞出可播放的绝对媒体 URL（含 Markdown 正文里的成片链接） */
const EMBEDDED_MEDIA_URL_RE = /https?:\/\/[^\s"'<>)\]]+/gi;

export interface CollectedMediaUrls {
  videos: string[];
  audios: string[];
  images: string[];
}

function pushMediaUrl(acc: CollectedMediaUrls, raw: string) {
  // 去掉句末标点，避免 Markdown「链接。」把扩展名弄脏
  const url = raw.replace(/[.,;:!?]+$/g, '');
  if (!/^https?:\/\//i.test(url)) return;
  const path = url.split('?')[0].toLowerCase();
  if (path.endsWith('.mp4')) acc.videos.push(url);
  else if (/\.(mp3|wav|ogg|m4a|aac|flac)$/.test(path)) acc.audios.push(url);
  else if (/\.(png|jpe?g|webp|gif)$/.test(path)) acc.images.push(url);
}

function extractEmbeddedMediaUrls(text: string, acc: CollectedMediaUrls) {
  const markdownLinkRe = /\[[^\]]*\]\((https?:\/\/[^)\s]+)\)/gi;
  const backtickUrlRe = /`(https?:\/\/[^`\s]+)`/gi;
  let match: RegExpExecArray | null;
  while ((match = markdownLinkRe.exec(text)) !== null) {
    pushMediaUrl(acc, match[1]);
  }
  while ((match = backtickUrlRe.exec(text)) !== null) {
    pushMediaUrl(acc, match[1]);
  }
  const matches = text.match(EMBEDDED_MEDIA_URL_RE);
  if (matches) {
    for (const item of matches) pushMediaUrl(acc, item);
  }
}

/** 合并多段来源的媒体 URL，去重后返回 */
export function mergeCollectedMedia(...sources: unknown[]): CollectedMediaUrls {
  const acc: CollectedMediaUrls = { videos: [], audios: [], images: [] };
  for (const source of sources) {
    const next = collectMediaUrls(source);
    acc.videos.push(...next.videos);
    acc.audios.push(...next.audios);
    acc.images.push(...next.images);
  }
  return {
    videos: [...new Set(acc.videos)],
    audios: [...new Set(acc.audios)],
    images: [...new Set(acc.images)],
  };
}

/** 从任意 JSON 值里收集媒体链接（工具结果与任务结果通用），按扩展名分类。
 *  仅取绝对 http(s) 地址：对象存储 key（如 "brandstudio/finals/x.mp4"）扩展名一样，
 *  但不是可取的地址，当成相对 src 会按页面 origin 解析出 404。
 *  对齐 agent-k demoui chat.js 的 collectMediaUrls；额外支持从 Markdown/长文本里抽取。 */
export function collectMediaUrls(v: unknown): CollectedMediaUrls {
  const acc: CollectedMediaUrls = { videos: [], audios: [], images: [] };
  const walk = (val: unknown) => {
    if (typeof val === 'string') {
      if (/^https?:\/\//i.test(val)) {
        pushMediaUrl(acc, val);
        return;
      }
      extractEmbeddedMediaUrls(val, acc);
      return;
    }
    if (Array.isArray(val)) {
      for (const x of val) walk(x);
      return;
    }
    if (val && typeof val === 'object') {
      for (const x of Object.values(val)) walk(x);
    }
  };
  // output 可能是序列化成字符串的 JSON，先尝试解析
  let parsed = v;
  if (typeof v === 'string') {
    try {
      parsed = JSON.parse(v);
    } catch {
      /* 保持原字符串 */
    }
  }
  walk(parsed);
  return {
    videos: [...new Set(acc.videos)],
    audios: [...new Set(acc.audios)],
    images: [...new Set(acc.images)],
  };
}

const SESSION_DELTA_SSE_EVENTS = new Set(['delta', 'reasoning.delta', 'text.delta']);

/** 会话流瞬态 delta：兼容旧版 event:delta 与新版 reasoning.delta / text.delta */
export function isSessionDeltaSseEvent(eventName: string): boolean {
  return SESSION_DELTA_SSE_EVENTS.has(eventName);
}

function resolveSessionDeltaKind(
  kindRaw: string,
  sseEventName: string,
): DeltaPayload['kind'] | null {
  if (kindRaw === 'text' || kindRaw === 'text.delta' || sseEventName === 'text.delta') {
    return 'text';
  }
  if (
    kindRaw === 'reasoning' ||
    kindRaw === 'reasoning.delta' ||
    sseEventName === 'reasoning.delta'
  ) {
    return 'reasoning';
  }
  return null;
}

/** 从 SSE data 解析瞬态 delta；解析失败或非 delta 时返回 null */
export function parseSessionDeltaPayload(
  sseEventName: string,
  rawData: string,
  fallbackRoundId?: string,
): DeltaPayload | null {
  if (!rawData.trim()) return null;
  let body: Record<string, unknown>;
  try {
    const parsed = JSON.parse(rawData) as unknown;
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return null;
    body = parsed as Record<string, unknown>;
  } catch {
    return null;
  }

  const envelopeKind = typeof body.kind === 'string' ? body.kind : '';
  const eventLooksLikeDelta =
    isSessionDeltaSseEvent(sseEventName) ||
    envelopeKind === 'reasoning.delta' ||
    envelopeKind === 'text.delta';
  if (!eventLooksLikeDelta) return null;

  const roundId = String(body.roundId ?? fallbackRoundId ?? '').trim();
  if (!roundId) return null;

  const blockRaw = body.block ?? body.gen;
  const block =
    typeof blockRaw === 'number' && Number.isFinite(blockRaw) ? Math.trunc(blockRaw) : 0;

  const kind = resolveSessionDeltaKind(envelopeKind || sseEventName, sseEventName);
  if (!kind) return null;

  const text = String(body.text ?? body.delta ?? body.content ?? '');
  return { roundId, block, kind, text };
}

/** 解析 SSE 文本流，按 \n\n 分割事件块，提取 id / event / data */
export function parseSSEChunk(
  text: string,
  onEvent: (id: string, event: string, data: string) => void,
) {
  const blocks = text.split('\n\n');
  for (const block of blocks) {
    if (!block.trim()) continue;
    let id = '';
    let eventName = 'message';
    let data = '';
    for (const line of block.split('\n')) {
      if (line.startsWith('id:')) {
        id = line.slice(3).trim();
      } else if (line.startsWith('event:')) {
        eventName = line.slice(6).trim();
      } else if (line.startsWith('data:')) {
        data = line.slice(5).trim();
      } else if (line.startsWith(':')) {
        // SSE comment / heartbeat
      }
    }
    if (data) {
      onEvent(id, eventName, data);
    } else if (eventName === 'ping' || eventName === 'heartbeat') {
      onEvent(id, eventName, '');
    }
  }
}
