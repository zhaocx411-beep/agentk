/**
 * SSE 相关类型定义
 */
import type {
  MessageAttachment,
  MessageItem,
  RunStats,
  LedgerEvent,
  ToolCardState,
  JobEvent,
  SessionLedgerEvent,
  PendingAskUser,
  AskUserAnswer,
  SendMessageOptions,
} from '../../types';

/** SSE 连接状态 */
export interface SSEState {
  messages: MessageItem[];
  stats: RunStats;
  ledger: LedgerEvent[];
  toolCards: ToolCardState[];
  jobs: JobEvent[];
  isRunning: boolean;
  isCancelling: boolean;
  currentRunId: string | null;
  error: string | null;
  /** run.started 事件携带的配置快照 */
  runSnapshot: {
    profileVersion?: string;
    promptSha256?: string;
    workerId?: string;
  } | null;
  /** 流式输出中当前累积的文本（用于逐字渲染） */
  streamingText: string;
  /** 流式输出中当前累积的 reasoning 文本 */
  streamingReason: string;
  /** 待回答的 ask_user 问卷（ask_user.requested 到达后等待用户选择） */
  pendingAskUser: PendingAskUser | null;
}

/** 建立 / 切换会话时的可选行为 */
export interface StartSessionOptions {
  /** 切换历史会话时，用 sessions/query 的 executionId 续订实时流 */
  resumeExecutionId?: string;
}

/** SSE 对外暴露的操作方法 */
export interface SSEActions {
  /** @returns 是否发送成功；失败时调用方应保留输入区附件 */
  send: (
    sessionId: string,
    text: string,
    attachments?: MessageAttachment[],
    options?: SendMessageOptions,
  ) => Promise<boolean>;
  /** @returns 是否成功取消；失败时调用方应回滚乐观 UI */
  cancel: () => Promise<boolean>;
  /** 建立会话流；传入账本时先回放历史，可选续订进行中的 execution */
  startSession: (sessionId: string, history?: SessionLedgerEvent[], options?: StartSessionOptions) => void;
  /** 历史已回放后，仅续订实时流（sessions 列表晚到时补订） */
  resumeStream: (sessionId: string, executionId: string) => void;
  /** 续回放后续账本页，不清空已渲染的消息 */
  appendReplayEvents: (history: SessionLedgerEvent[]) => void;
  /** 回放更早的账本页：合并 store 后全量重放（上滚加载历史）；生成中返回 false */
  prependReplayEvents: (history: SessionLedgerEvent[]) => boolean;
  /** 生成中暂存更早页到 ledger，待空闲后再 prepend 回放 */
  stageLedgerEvents: (history: SessionLedgerEvent[]) => void;
  reset: () => void;
  /** 批量加载历史消息（切换会话时使用） */
  loadHistory: (messages: MessageItem[]) => void;
  /** 向前追加更早的历史消息（向上滚动加载更多） */
  prependHistory: (messages: MessageItem[]) => void;
  /** 提交 ask_user 问卷答案（成功后会话继续执行），返回是否成功 */
  answer: (toolCallId: string, answers: AskUserAnswer[]) => Promise<boolean>;
}

/** SSE 初始状态 */
export const INITIAL_SSE_STATE: SSEState = {
  messages: [],
  stats: { steps: 0, inTok: 0, outTok: 0, modelMs: 0, tools: 0 },
  ledger: [],
  toolCards: [],
  jobs: [],
  isRunning: false,
  isCancelling: false,
  currentRunId: null,
  error: null,
  runSnapshot: null,
  streamingText: '',
  streamingReason: '',
  pendingAskUser: null,
};
