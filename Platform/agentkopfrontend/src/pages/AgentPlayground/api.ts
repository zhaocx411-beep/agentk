/**
 * Agent Playground - API 接口
 * 对接真实 agent-k 后端 /agent/operation/backend/agentk/*
 * Workflow 相关接口见 @/services/workflow（WorkflowRunner / WorkflowEditor 使用）
 */
import { get, patch, post } from '@/utils/request';
import {
  createSession as _createSession,
  getCapabilities as _getCapabilities,
  getModels as _getModels,
} from '@/services/workflow';
import type {
  RunMessageResponse,
  SendMessageOptions,
  Session,
  SessionListResult,
  SessionEventsResponse,
  SessionLedgerEvent,
  GetSessionsParams,
  GetSessionEventsParams,
  PlatformChatSessionEventRecord,
  SubmitAnswersRequest,
  SubmitAnswersResponse,
  TtsEngineOption,
} from './types';
import type { SessionReviewRecord, SessionReviewResponse } from '@/pages/SessionReview/types';
import type { PlatformPageResult } from '@/types/api';

/** 获取实例能力目录（代理到共享层） */
export const getCapabilities = _getCapabilities;

/** 获取模型目录（代理到共享层；全平台选模型唯一来源） */
export const getModels = _getModels;

/** 创建新会话（代理到共享层） */
export const createSession = _createSession;

/**
 * 发送消息并异步跑。
 * V3 通过 attachments 传结构化媒体；text 同时保留 URL 清单兼容旧 Agent。
 * 语音合成场景额外传 tts.engine / tts.voiceId。
 * 响应含 executionId，后续 SSE 订流必填。
 */
export function sendMessage(sessionId: string, text: string, options?: SendMessageOptions) {
  return post<RunMessageResponse>(
    `/agent/operation/backend/agentk/sessions/${sessionId}/messages`,
    {
      text,
      ...(options?.tts ? { tts: options.tts } : {}),
      ...(options?.attachments?.length ? { attachments: options.attachments } : {}),
      ...(options?.userInput ? { userInput: options.userInput } : {}),
    },
  );
}

/** 语音合成引擎与音色目录 */
export const TTS_VOICES_QUERY_KEY = ['agentk-tts-voices'] as const;

export function getTtsVoices() {
  return get<{ engines: TtsEngineOption[] }>(
    '/agent/operation/backend/agentk/tts/voices',
    undefined,
    { headers: { 'X-No-Loading': '1' } },
  );
}

/** 打断当前会话正在执行的 round */
export function cancelSession(sessionId: string, reason?: string) {
  return post<void>(`/agent/operation/backend/agentk/sessions/${sessionId}/cancel`, {
    reason,
  });
}

/** 平台会话记录 → 前端 Session。
 *  会话标识优先取 agentkSessionId（ses_ 前缀，agentk 系列接口的路径参数）；
 *  缺失时（raw_profile 异常会话）回退 publicId。
 *  同时透传创建时的配置快照（profileYaml/effectiveModel 等），切换历史会话时用于回显。 */
function mapSessionRecord(r: SessionReviewRecord): Session {
  return {
    id: r.agentkSessionId || r.publicId,
    tenantId: r.tenantId || undefined,
    title: r.title,
    createdAt: r.createdAt,
    status: r.status,
    activeRunStatus: r.activeRunStatus,
    lastExecutionId: r.lastExecutionId,
    lastAgentkRunId: r.lastAgentkRunId,
    agentVersionId: r.agentVersionId,
    configSource: r.configSource,
    profileYaml: r.profileYaml,
    effectiveModel: r.effectiveModel,
    effectiveToolsJson: r.effectiveToolsJson,
    specJson: r.specJson,
  };
}

/** 并发相同参数的 sessions/query 共用一个 Promise（开发态 StrictMode 双挂载会打两次） */
const sessionsInflight = new Map<string, Promise<SessionListResult>>();

/**
 * 获取历史会话列表（用户个人）
 * 对齐 POST /platform/chat/sessions/query：不传过滤条件即返回该用户全部 session；
 * agentLoopXxx / workflowXxx 参数由 WorkflowRunner 用于按 Pipeline 隔离。
 * 返回 { page, pageSize, total, records } 分页结构。
 */
export function getSessions(params: GetSessionsParams): Promise<SessionListResult> {
  const key = JSON.stringify(params);
  const pending = sessionsInflight.get(key);
  if (pending) return pending;

  const promise = (async (): Promise<SessionListResult> => {
    const res = await post<SessionReviewResponse>(
      '/agent/operation/backend/platform/chat/sessions/query',
      params,
    );
    const records = res?.records || [];
    const total = res?.total ?? 0;
    const page = res?.page || params.page;
    const pageSize = res?.pageSize || params.pageSize;
    return {
      sessions: records.map(mapSessionRecord),
      hasMore: page * pageSize < total,
      page,
      pageSize,
    };
  })().finally(() => {
    sessionsInflight.delete(key);
  });

  sessionsInflight.set(key, promise);
  return promise;
}

function parsePayloadJson(raw?: string): unknown {
  if (!raw?.trim()) return undefined;
  try {
    return JSON.parse(raw) as unknown;
  } catch {
    return raw;
  }
}

/** 平台账本记录 → 会话回放/catch-up 共用的 SessionLedgerEvent */
function mapPlatformEventRecord(record: PlatformChatSessionEventRecord): SessionLedgerEvent {
  const data = parsePayloadJson(record.payloadJson);
  return {
    seq: record.seq,
    roundId: record.roundId || undefined,
    executionId: record.agentkRunId || undefined,
    eventId: record.agentkEventId || undefined,
    kind: record.kind,
    data,
    payload: data,
    payloadJson: record.payloadJson,
    createdAt: record.agentkCreatedAt || record.createdAt,
  };
}

/**
 * 获取会话历史账本（按 seq 倒序分页）
 * GET /platform/admin/chat/sessions/by-agentk/{agentkSessionId}/events
 */
export async function getSessionEvents(
  sessionId: string,
  params: GetSessionEventsParams,
): Promise<SessionEventsResponse> {
  const res = await get<PlatformPageResult<PlatformChatSessionEventRecord>>(
    `/agent/operation/backend/platform/admin/chat/sessions/by-agentk/${encodeURIComponent(sessionId)}/events`,
    params,
  );
  const page = res?.page ?? params.page;
  const pageSize = res?.pageSize ?? params.pageSize;
  const total = res?.total ?? 0;
  const records = res?.records ?? [];
  return {
    events: records.map(mapPlatformEventRecord),
    hasMore: page * pageSize < total,
    page,
    pageSize,
    total,
  };
}

/** 提交 ask_user 问卷答案
 *  POST /agent/operation/backend/agentk/sessions/{sessionId}/answers
 */
export function submitAnswers(sessionId: string, body: SubmitAnswersRequest) {
  return post<SubmitAnswersResponse>(
    `/agent/operation/backend/agentk/sessions/${sessionId}/answers`,
    body,
  );
}

/**
 * 修改会话标题
 * PATCH /platform/chat/sessions/by-agentk/{agentkSessionId}/title
 */
export function updateSessionTitle(sessionId: string, title: string): Promise<Session> {
  return patch<SessionReviewRecord>(
    `/agent/operation/backend/platform/chat/sessions/by-agentk/${encodeURIComponent(sessionId)}/title`,
    { title },
    { headers: { 'X-No-Loading': '1' } },
  ).then(mapSessionRecord);
}
