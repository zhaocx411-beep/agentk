/**
 * 中栏：对话消息列表 + 输入框
 */
import {
  Fragment,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ComponentPropsWithoutRef,
  type ComponentRef,
} from 'react';
import { Button, Input, Tag, Image, Tooltip } from 'antd';
import {
  ArrowUpOutlined,
  ThunderboltOutlined,
  DownOutlined,
  UserOutlined,
  StopOutlined,
  FileOutlined,
  ArrowDownOutlined,
} from '@ant-design/icons';
import { AgentMarkIcon, MediaSourceMenu } from '@/components';
import { formatElapsed } from '@/utils/format';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import type { PlatformMediaType } from '@/services/mediaTypes';
import type { MessageAttachment, MessageItem, MessagePart, JobEvent } from '../../types';
import { HIDDEN_TOOL_CARDS } from '../../constants';
import { collectMediaUrls, resolveMessagePartMediaKind } from '../../hooks/sse/helpers';
import type { SSEState, SSEActions } from '../../hooks/useSSE';
import AskUserCard from './AskUserCard';
import ChatAttachmentBar, { type ChatAttachmentBarHandle } from './ChatAttachmentBar';
import ChatEmptyState, {
  DEFAULT_SUGGESTION_SKELETON_COUNT,
  type ChatSuggestion,
} from './ChatEmptyState';
import styles from '../../AgentPlayground.module.less';

const { TextArea } = Input;
const NEAR_BOTTOM_PX = 96;
/** 首屏不足一屏时最多主动补几页更早对话 */
const AUTO_FILL_MAX_PAGES = 3;

const MSG_ROLE_NAME: Record<string, string> = {
  user: '你',
  assistant: 'agent-k',
};

function MessageRoleMark({ role }: { role: string }) {
  if (role === 'user') return <UserOutlined style={{ fontSize: 13 }} />;
  if (role === 'assistant') return <AgentMarkIcon size={13} />;
  return 'T';
}

function sendAriaLabel(showStop: boolean, isBusy: boolean) {
  if (showStop) return '停止生成';
  if (isBusy) return '打断并发送';
  return '发送消息';
}

type JobDisplayStatus = JobEvent['status'];

const JOB_STATUS_META: Record<JobDisplayStatus, { className: string; label: string }> = {
  completed: { className: styles.toolStatusOk, label: '已完成' },
  failed: { className: styles.toolStatusErr, label: '失败' },
  cancelled: { className: styles.toolStatusErr, label: '已停止' },
  submitted: { className: styles.toolStatusPending, label: '已提交，后台处理中' },
};

type ToolCardDisplayStatus = 'ok' | 'err' | 'pending';

const TOOL_CARD_STATUS_CLASS: Record<ToolCardDisplayStatus, string> = {
  ok: styles.toolStatusOk,
  err: styles.toolStatusErr,
  pending: styles.toolStatusPending,
};

function resolveToolCardDisplayStatus(status?: string): ToolCardDisplayStatus {
  if (status === 'ok') return 'ok';
  if (status === 'err') return 'err';
  return 'pending';
}

function formatToolCardStatusLabel(
  status: ToolCardDisplayStatus,
  durationMs?: number | null,
): string {
  if (status === 'ok') {
    return durationMs != null ? `完成 · ${durationMs}ms` : '完成';
  }
  if (status === 'err') return '失败';
  return '执行中…';
}

function composerPlaceholderText(
  isBusy: boolean,
  sendPending: boolean,
  inputPlaceholder: string | undefined,
  variant: 'quiet' | 'default',
): string {
  if (sendPending) return '正在发送，请稍候…';
  if (isBusy) return '生成中，Enter 打断并发送新消息';
  if (inputPlaceholder) return inputPlaceholder;
  return variant === 'quiet' ? '今天帮你做些什么？' : '输入消息或上传附件，Enter 发送';
}

function composerSendTooltipText(options: {
  showStop: boolean;
  isCancelling: boolean;
  canSend: boolean;
  isBusy: boolean;
  sendPending: boolean;
}): string {
  if (options.sendPending) return '正在发送';
  if (options.showStop) {
    return options.isCancelling ? '正在停止' : '停止生成';
  }
  if (!options.canSend) return '请输入内容';
  if (options.isBusy) return '打断当前生成并发送';
  return '发送';
}

/** Markdown 渲染组件：用 Ant Design Image 替代 img，支持点击预览；成片由下方 MediaList 内联 */
const markdownComponents = {
  p: ({ children }: ComponentPropsWithoutRef<'p'>) => (
    <div className={styles.markdownParagraph}>{children}</div>
  ),
  img: (props: ComponentPropsWithoutRef<'img'>) => (
    <Image
      src={props.src}
      alt={props.alt || ''}
      style={{ maxWidth: 400, maxHeight: 300, objectFit: 'contain' }}
      preview={{ mask: false }}
    />
  ),
};

/** 从结果 JSON 中提取图片/视频/音频链接并内联渲染（对齐 agent-k demoui appendMedia） */
function MediaList({ source }: { source: unknown }) {
  const media = useMemo(() => collectMediaUrls(source), [source]);
  if (media.videos.length === 0 && media.audios.length === 0 && media.images.length === 0) {
    return null;
  }
  return (
    <div className={styles.mediaList}>
      {media.videos.map((url) => (
        <video key={url} className={styles.mediaVideo} controls preload="metadata" src={url} />
      ))}
      {media.audios.map((url) => (
        <audio key={url} className={styles.mediaAudio} controls preload="metadata" src={url} />
      ))}
      {media.images.map((url) => (
        <Image key={url} className={styles.mediaImg} src={url} alt="" preview={{ mask: false }} />
      ))}
    </div>
  );
}

/** 任务状态徽标（job 卡片与 data-job-result 卡片共用）
 *  等待中可携带远端状态与已等待时长（对齐 demoui “生成中 · xxx · 已等待 xx”） */
function JobStatusTag({
  status,
  remoteStatus,
  elapsedMs,
}: {
  status: JobEvent['status'];
  remoteStatus?: string;
  elapsedMs?: number;
}) {
  const meta = JOB_STATUS_META[status];
  let label = meta.label;
  if (status === 'submitted' && (remoteStatus || elapsedMs != null)) {
    label =
      '生成中' +
      (remoteStatus ? ` · ${remoteStatus}` : '') +
      (elapsedMs != null ? ` · 已等待 ${formatElapsed(elapsedMs)}` : '');
  }
  return (
    <span className={`${styles.toolStatus} ${meta.className}`}>
      <span
        className={`${styles.toolStatusDot} ${status === 'submitted' ? styles.toolStatusDotPending : ''}`}
      />
      {label}
    </span>
  );
}

/** 后台任务状态卡片（实时 job 事件驱动，对齐 demoui addJobCard 提交/等待卡片）。
 *  默认收起只露头部与状态条；媒体产物在折叠区外保持可见 */
function JobCard({ job }: { job: JobEvent }) {
  return (
    <>
      <details className={styles.toolCard}>
        <summary className={styles.toolHead}>
          <Tag className={styles.toolBadge}>后台任务</Tag>
          <span className={styles.toolName}>
            {job.kind} · {job.jobId}
          </span>
          <JobStatusTag
            status={job.status}
            remoteStatus={job.remoteStatus}
            elapsedMs={job.elapsedMs}
          />
          <DownOutlined className={styles.toolChevron} />
        </summary>
        <div className={styles.toolBody}>
          {job.result != null && (
            <details className={styles.toolDetails}>
              <summary className={styles.toolDetailsSummary}>
                <DownOutlined className={styles.toolDetailsIcon} />
                任务结果
              </summary>
              <pre className={styles.toolPre}>{JSON.stringify(job.result, null, 2)}</pre>
            </details>
          )}
          {job.error && <pre className={styles.toolPre}>{job.error}</pre>}
        </div>
      </details>
      <MediaList source={job.result} />
    </>
  );
}

interface ChatAreaProps {
  sseState: SSEState;
  sseActions: SSEActions;
  inputText: string;
  setInputText: (t: string) => void;
  /** 发送；attachments 仅 enableAttachments 时带上。发送后保留输入框与附件。 */
  onSend: (attachments?: MessageAttachment[]) => boolean | void | Promise<boolean | void>;
  chatRef: React.RefObject<HTMLDivElement>;
  onChatScroll: () => void;
  isLoadingMoreItems: boolean;
  /** 变化时重新贴底（如切换会话：优先展示最新对话） */
  stickBottomToken?: string | number | null;
  /** 空态展示的 Agent 名称 */
  emptyAgentName?: string;
  /** 空态快捷提问；不传则用 Playground 默认文案 */
  suggestions?: ChatSuggestion[];
  /** 空态精选卡片下方的扩展区 */
  emptyPromptExtra?: React.ReactNode;
  /** 输入框工具栏左侧扩展 */
  composerToolbarExtra?: React.ReactNode;
  /** 输入框上方横滚快捷灵感（空对话时展示） */
  composerPromptStrip?: React.ReactNode;
  /** 精选模版加载中（展示骨架，避免 fallback 闪动） */
  suggestionsLoading?: boolean;
  suggestionSkeletonCount?: number;
  /** 是否展示空态 Agent 图标 */
  showEmptyIcon?: boolean;
  /** 页面可覆盖通用空态标题与说明 */
  emptyTitle?: string;
  emptyDescription?: string;
  /** 自主规划（agent-loop）开启附件上传；工作流勿开 */
  enableAttachments?: boolean;
  /** 附件栏就绪回调（供灵感模版预填 referImages） */
  onAttachmentBarReady?: (handle: ChatAttachmentBarHandle | null) => void;
  /** quiet 用于正式对话页：更克制的气泡与紧凑输入区 */
  variant?: 'default' | 'quiet';
  /** 嵌入窄栏（官方 loop 左栏），去掉 840px 居中限制 */
  embedded?: boolean;
  /** composer：仅保留输入区，隐藏对话时间线 */
  surface?: 'chat' | 'composer';
  /** 覆盖输入框 placeholder（如官方 loop 引导文案） */
  inputPlaceholder?: string;
  /** composer 模式下撑满父容器高度（官方 loop 全屏输入） */
  fillHeight?: boolean;
  /** studio 输入区顶部扩展（如 TTS 引擎/音色） */
  composerPreInputExtra?: React.ReactNode;
  /** 创建会话 / 首包发送前的等待态（详情页预览等场景） */
  sendPending?: boolean;
  /** 附件上传 accept（默认全平台媒体类型） */
  uploadAccept?: string;
  /** 附件上传允许的类型（默认 image / video / audio / doc） */
  uploadAllowedTypes?: readonly PlatformMediaType[];
  /** 添加附件按钮文案（studio 模式） */
  uploadAddLabel?: string;
}

function ChatArea({
  sseState,
  sseActions,
  inputText,
  setInputText,
  onSend,
  chatRef,
  onChatScroll,
  isLoadingMoreItems,
  stickBottomToken = null,
  emptyAgentName,
  suggestions,
  emptyPromptExtra,
  composerToolbarExtra,
  composerPromptStrip,
  suggestionsLoading,
  suggestionSkeletonCount = DEFAULT_SUGGESTION_SKELETON_COUNT,
  showEmptyIcon = true,
  emptyTitle,
  emptyDescription,
  enableAttachments = false,
  onAttachmentBarReady,
  variant = 'default',
  embedded = false,
  surface = 'chat',
  inputPlaceholder,
  fillHeight = false,
  composerPreInputExtra,
  sendPending = false,
  uploadAccept,
  uploadAllowedTypes,
  uploadAddLabel,
}: ChatAreaProps) {
  const composerOnly = surface === 'composer';
  const studioComposer = composerOnly && (enableAttachments || composerPreInputExtra != null);
  const composerFill = composerOnly && fillHeight;
  /** 专用表单 + 描述输入：表单与输入框在同一滚动容器内整体滚动 */
  const composerScrollBody = composerFill && composerPreInputExtra != null && !enableAttachments;
  const attachRef = useRef<ChatAttachmentBarHandle>(null);
  const stickToBottomRef = useRef(true);
  const userDetachedRef = useRef(false);
  const scrollFrameRef = useRef<number | null>(null);
  /** 剩余自动补页次数，防止不可滚动的容器把账本一路翻到底 */
  const autoFillLeftRef = useRef(AUTO_FILL_MAX_PAGES);
  const [attachReady, setAttachReady] = useState<MessageAttachment[]>([]);
  const [attachUploading, setAttachUploading] = useState(false);
  const [showJumpBottom, setShowJumpBottom] = useState(false);
  const composerInputRef = useRef<ComponentRef<typeof TextArea>>(null);
  const prevInputTextRef = useRef(inputText);

  useEffect(() => {
    const prev = prevInputTextRef.current;
    prevInputTextRef.current = inputText;
    const bulkFilled = inputText.length > 120 && inputText.length - prev.length > 80;
    if (!bulkFilled) return;
    requestAnimationFrame(() => {
      const el = composerInputRef.current?.resizableTextArea?.textArea;
      if (!el) return;
      el.scrollTop = 0;
      el.focus();
    });
  }, [inputText]);

  useEffect(() => {
    if (!onAttachmentBarReady) return;
    onAttachmentBarReady(enableAttachments ? attachRef.current : null);
  }, [enableAttachments, onAttachmentBarReady, attachReady, attachUploading]);
  // 同步生成中锁定输入；job_status 轮询、已提交的后台 job 不锁定（可继续提问）
  const submittedToolCallIds = useMemo(() => {
    const ids = new Set<string>();
    for (const job of sseState.jobs) {
      if (job.status === 'submitted' && job.toolCallId) ids.add(job.toolCallId);
    }
    return ids;
  }, [sseState.jobs]);

  const isBusy =
    sseState.isRunning ||
    sseState.toolCards.some(
      (t) =>
        t.status === 'running' &&
        t.toolName !== 'job_status' &&
        !submittedToolCallIds.has(t.toolCallId),
    );

  const handleAttachReady = useCallback((attachments: MessageAttachment[], uploading: boolean) => {
    setAttachReady(attachments);
    setAttachUploading(uploading);
  }, []);

  const canSend = Boolean(inputText.trim() || attachReady.length > 0) && !attachUploading;
  /** 生成中且没写内容才显示停止；写了内容就让位给「打断并发送」 */
  const showStop = isBusy && !canSend;

  // 生成中也允许发送：由 sseActions.send 先打断上一轮，再开新一轮
  const handleComposerSend = useCallback(async () => {
    if (!canSend || sendPending) return;
    const attachments = enableAttachments
      ? (attachRef.current?.getReady() ?? attachReady)
      : undefined;
    try {
      const sent = await onSend(attachments && attachments.length > 0 ? attachments : undefined);
      if (sent !== false) {
        setInputText('');
        attachRef.current?.clear();
        setAttachReady([]);
      }
    } catch {
      // 调用方抛错：保留输入与附件
    }
  }, [canSend, sendPending, enableAttachments, attachReady, onSend, setInputText]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.nativeEvent.isComposing || e.nativeEvent.keyCode === 229) return;
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        void handleComposerSend();
      }
    },
    [handleComposerSend],
  );

  // 消息时间线（Pipeline 卡片已拆分到独立页面，此处仅消息）
  const timeline = useMemo(() => sseState.messages, [sseState.messages]);
  const currentTimelineRef = useRef(timeline);
  currentTimelineRef.current = timeline;
  const lastCommittedTimelineRef = useRef(timeline);
  const sessionSwitchSourceTimelineRef = useRef<MessageItem[] | null>(null);
  const pendingSessionBottomRef = useRef(false);
  const sessionTokenInitializedRef = useRef(false);

  // 所有 tool-call 的 id 集合：用于识别 tool 角色消息里的孤儿结果（无对应调用卡片可回填）
  const toolCallIds = useMemo(() => {
    const ids = new Set<string>();
    for (const m of sseState.messages) {
      for (const p of m.parts) {
        if (p.type === 'tool-call') ids.add(p.toolCallId);
      }
    }
    return ids;
  }, [sseState.messages]);
  const askUserToolCallIds = useMemo(
    () =>
      new Set(
        sseState.messages.flatMap((messageItem) =>
          messageItem.parts.flatMap((part) => (part.type === 'ask-user' ? [part.toolCallId] : [])),
        ),
      ),
    [sseState.messages],
  );

  const syncJumpBottom = useCallback(() => {
    const el = chatRef.current;
    if (!el) return;
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight <= NEAR_BOTTOM_PX;
    if (nearBottom) {
      stickToBottomRef.current = true;
      userDetachedRef.current = false;
      setShowJumpBottom(false);
    } else if (userDetachedRef.current) {
      stickToBottomRef.current = false;
      setShowJumpBottom(true);
    }
  }, [chatRef]);

  const handleListScroll = useCallback(() => {
    syncJumpBottom();
    onChatScroll();
  }, [onChatScroll, syncJumpBottom]);

  const handleListWheel = useCallback((event: React.WheelEvent<HTMLDivElement>) => {
    if (event.deltaY >= 0) return;
    userDetachedRef.current = true;
    stickToBottomRef.current = false;
  }, []);

  const handleListPointerDown = useCallback(
    (event: React.PointerEvent<HTMLDivElement>) => {
      const el = chatRef.current;
      if (!el || event.clientX < el.getBoundingClientRect().right - 16) return;
      userDetachedRef.current = true;
      stickToBottomRef.current = false;
    },
    [chatRef],
  );

  const handleListTouchMove = useCallback(() => {
    userDetachedRef.current = true;
    stickToBottomRef.current = false;
  }, []);

  const jumpToBottom = useCallback(() => {
    const el = chatRef.current;
    if (!el) return;
    userDetachedRef.current = false;
    stickToBottomRef.current = true;
    setShowJumpBottom(false);
    el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' });
  }, [chatRef]);

  const scrollToBottomAfterLayout = useCallback(() => {
    const apply = () => {
      if (!stickToBottomRef.current) return;
      const el = chatRef.current;
      if (el) el.scrollTop = el.scrollHeight;
    };
    apply();
    if (scrollFrameRef.current != null) cancelAnimationFrame(scrollFrameRef.current);
    scrollFrameRef.current = requestAnimationFrame(() => {
      apply();
      scrollFrameRef.current = requestAnimationFrame(() => {
        apply();
        scrollFrameRef.current = null;
      });
    });
  }, [chatRef]);

  useEffect(
    () => () => {
      if (scrollFrameRef.current != null) cancelAnimationFrame(scrollFrameRef.current);
    },
    [],
  );

  const followContentGrowth = useCallback(() => {
    if (userDetachedRef.current) return;
    if (
      pendingSessionBottomRef.current &&
      sessionSwitchSourceTimelineRef.current === currentTimelineRef.current
    ) {
      return;
    }
    stickToBottomRef.current = true;
    scrollToBottomAfterLayout();
  }, [scrollToBottomAfterLayout]);

  // 首次回放后图片、视频等会继续撑高消息；跟随其尺寸变化，避免内容把视口顶离底部。
  useEffect(() => {
    const el = chatRef.current;
    if (!el || typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(followContentGrowth);
    for (const child of el.children) observer.observe(child);
    return () => observer.disconnect();
  }, [
    timeline,
    sseState.isRunning,
    sseState.pendingAskUser,
    chatRef,
    followContentGrowth,
  ]);

  // 切换会话：恢复贴底，并清空待发送附件（输入框由上层 session 状态重置）
  useEffect(() => {
    const renderedTimeline = currentTimelineRef.current;
    stickToBottomRef.current = true;
    userDetachedRef.current = false;
    setShowJumpBottom(false);
    attachRef.current?.clear();
    setAttachReady([]);
    autoFillLeftRef.current = AUTO_FILL_MAX_PAGES;

    if (!sessionTokenInitializedRef.current) {
      sessionTokenInitializedRef.current = true;
      scrollToBottomAfterLayout();
      return;
    }

    pendingSessionBottomRef.current = true;
    sessionSwitchSourceTimelineRef.current =
      lastCommittedTimelineRef.current === renderedTimeline ? renderedTimeline : null;
  }, [stickBottomToken, scrollToBottomAfterLayout]);

  // 时间线或流式内容变化时，仅在贴底时自动滚动（用户上翻阅读时不抢滚动）
  useEffect(() => {
    if (!stickToBottomRef.current) return;
    if (
      pendingSessionBottomRef.current &&
      sessionSwitchSourceTimelineRef.current === timeline
    ) {
      return;
    }
    scrollToBottomAfterLayout();
  }, [
    timeline,
    sseState.streamingText,
    sseState.jobs,
    sseState.pendingAskUser,
    scrollToBottomAfterLayout,
  ]);

  // 会话切换后，等新时间线替换旧内容且历史加载完成，再做最终贴底。
  useEffect(() => {
    if (isLoadingMoreItems || !pendingSessionBottomRef.current) return;
    if (sessionSwitchSourceTimelineRef.current === timeline) return;
    const frame = requestAnimationFrame(() => {
      stickToBottomRef.current = true;
      userDetachedRef.current = false;
      scrollToBottomAfterLayout();
      pendingSessionBottomRef.current = false;
      sessionSwitchSourceTimelineRef.current = null;
    });
    return () => cancelAnimationFrame(frame);
  }, [timeline, stickBottomToken, isLoadingMoreItems, scrollToBottomAfterLayout]);

  useEffect(() => {
    lastCommittedTimelineRef.current = timeline;
  }, [timeline]);

  // 最新一页不足一屏时没有滚动条，用户无从上翻：主动再要一页更早对话。
  // 限额是必须的——容器被隐藏或高度不受限时永远「不足一屏」，会一路把整个账本翻完
  useEffect(() => {
    const el = chatRef.current;
    if (!el || isLoadingMoreItems || timeline.length === 0) return;
    if (autoFillLeftRef.current <= 0) return;
    // clientHeight 为 0：容器未布局或处于隐藏 tab，此时判断不出是否需要补页
    if (el.clientHeight === 0 || el.scrollHeight > el.clientHeight) return;
    autoFillLeftRef.current -= 1;
    onChatScroll();
  }, [timeline, isLoadingMoreItems, onChatScroll, chatRef]);

  // 渲染单条消息
  const renderMessage = (msg: MessageItem, idx: number) => {
    // job 卡片占位消息（data-job part）：无头像 header，动态数据从 sseState.jobs 查
    if (msg.parts.every((p) => p.type === 'data-job')) {
      return (
        <div key={`msg-${idx}`} className={styles.msg}>
          <div className={styles.msgContent}>
            {msg.parts.map((p, pidx) => {
              if (p.type !== 'data-job') return null;
              const job = sseState.jobs.find((j) => j.jobId === p.jobId);
              if (!job) return null;
              return <JobCard key={pidx} job={job} />;
            })}
          </div>
        </div>
      );
    }

    // 对齐 demoui：tool 角色不单独成泡，其结果回填到 assistant 的工具卡片里；
    // 仅当存在找不到对应 tool-call 的孤儿结果时才兜底渲染，避免信息丢失
    if (msg.role === 'tool') {
      const orphans = msg.parts.filter(
        (p) => p.type === 'tool-result' && !toolCallIds.has(p.toolCallId),
      );
      if (orphans.length === 0) return null;
      return (
        <div key={`msg-${idx}`} className={`${styles.msg} ${styles.assistant}`}>
          <div className={styles.msgContent}>
            {orphans.map((p, pidx) =>
              p.type === 'tool-result' ? (
                <div key={pidx} className={styles.toolCard}>
                  <div className={styles.toolBody}>
                    <pre className={styles.toolPre}>{JSON.stringify(p.output, null, 2)}</pre>
                    <MediaList source={p.output} />
                  </div>
                </div>
              ) : null,
            )}
          </div>
        </div>
      );
    }

    // 对齐 demoui renderItemParts：
    // - 气泡（who 头 + 思考/正文）只在有非空文本时创建，空白 reasoning/text 直接跳过
    // - 工具卡片独立成组渲染，不带 who 头（demoui addToolCard 直接 append 到聊天流）
    // - 没有实际产出（无正文、无可展示工具卡片）的思考块不渲染：
    //   它们来自隐藏工具（job_status）的轮询步骤，留下只是重复噪音
    const textParts = msg.parts.filter(
      (p): p is Extract<MessagePart, { type: 'text' }> => p.type === 'text' && p.text.trim() !== '',
    );
    const reasoningParts = msg.parts.filter(
      (p): p is Extract<MessagePart, { type: 'reasoning' }> =>
        p.type === 'reasoning' && p.text.trim() !== '',
    );
    const mediaParts = msg.parts.filter(
      (p): p is Extract<MessagePart, { type: 'image' | 'file' }> =>
        p.type === 'image' || p.type === 'file',
    );
    const toolParts = msg.parts.filter(
      (p): p is Extract<MessagePart, { type: 'tool-call' }> =>
        p.type === 'tool-call' && p.toolName !== 'ask_user' && !HIDDEN_TOOL_CARDS.has(p.toolName),
    );
    const askUserParts = msg.parts.filter(
      (p): p is Extract<MessagePart, { type: 'ask-user' }> => p.type === 'ask-user',
    );
    const hasOutput = textParts.length > 0 || toolParts.length > 0 || mediaParts.length > 0;
    const bubbleParts: Extract<MessagePart, { type: 'text' | 'reasoning' }>[] = hasOutput
      ? [...reasoningParts, ...textParts]
      : textParts;
    // 整条消息没有可展示内容（如仅含隐藏工具的调用与配套思考）时不占位
    if (
      bubbleParts.length === 0 &&
      toolParts.length === 0 &&
      mediaParts.length === 0 &&
      askUserParts.length === 0
    ) {
      return null;
    }

    const showBubble = bubbleParts.length > 0 || mediaParts.length > 0;

    return (
      <div key={`msg-${idx}`} className={`${styles.msg} ${styles[msg.role]}`}>
        {showBubble && (
          <>
            <div className={styles.msgHeader}>
              <div
                className={`${styles.msgAvatar} ${msg.role === 'user' ? styles.msgAvatarUser : ''}`}
              >
                <MessageRoleMark role={msg.role} />
              </div>
              <span className={styles.msgName}>{MSG_ROLE_NAME[msg.role] ?? '工具'}</span>
            </div>

            <div className={styles.msgContent}>
              {msg.role === 'user' ? (
                // 用户附件气泡：上方缩略图横排，下方纯文案（不含拼进 text 的 URL 清单）
                <div className={styles.userBubbleStack}>
                  {mediaParts.length > 0 && (
                    <div className={styles.msgAttachments}>
                      {mediaParts.map((part, pidx) => {
                        if (part.type === 'image') {
                          return (
                            <Image
                              key={`media-${pidx}`}
                              src={part.url}
                              alt={part.name || '附件图片'}
                              className={styles.msgAttachImage}
                              preview={{ mask: false }}
                            />
                          );
                        }
                        const mediaKind = resolveMessagePartMediaKind(part);
                        if (mediaKind === 'video') {
                          return (
                            <video
                              key={`media-${pidx}`}
                              className={styles.msgAttachVideo}
                              src={part.url}
                              controls
                              preload="metadata"
                            />
                          );
                        }
                        if (mediaKind === 'audio') {
                          return (
                            <audio
                              key={`media-${pidx}`}
                              className={styles.mediaAudio}
                              src={part.url}
                              controls
                              preload="metadata"
                              aria-label={part.name || '附件音频'}
                            />
                          );
                        }
                        return (
                          <a
                            key={`media-${pidx}`}
                            href={part.url}
                            target="_blank"
                            rel="noreferrer"
                            className={styles.msgAttachFile}
                          >
                            <FileOutlined />
                            <span>{part.name || '附件'}</span>
                          </a>
                        );
                      })}
                    </div>
                  )}
                  {bubbleParts
                    .filter((p): p is Extract<MessagePart, { type: 'text' }> => p.type === 'text')
                    .map((part, pidx) => (
                      <div key={pidx} className={styles.msgText}>
                        {part.text}
                      </div>
                    ))}
                </div>
              ) : (
                <>
                  {mediaParts.length > 0 && (
                    <div className={styles.msgAttachments}>
                      {mediaParts.map((part, pidx) =>
                        part.type === 'image' ? (
                          <Image
                            key={`media-${pidx}`}
                            src={part.url}
                            alt={part.name || '附件图片'}
                            className={styles.msgAttachImage}
                            preview={{ mask: false }}
                          />
                        ) : (
                          <a
                            key={`media-${pidx}`}
                            href={part.url}
                            target="_blank"
                            rel="noreferrer"
                            className={styles.msgAttachFile}
                          >
                            <FileOutlined />
                            <span>{part.name || '附件'}</span>
                          </a>
                        ),
                      )}
                    </div>
                  )}
                  {bubbleParts.map((part, pidx) => {
                    if (part.type === 'text') {
                      return (
                        <div key={pidx} className={styles.msgText}>
                          <ReactMarkdown
                            remarkPlugins={[remarkGfm]}
                            components={markdownComponents}
                          >
                            {part.text}
                          </ReactMarkdown>
                          <MediaList source={part.text} />
                        </div>
                      );
                    }
                    return (
                      <details key={pidx} className={styles.reason}>
                        <summary className={styles.reasonSummary}>
                          <ThunderboltOutlined className={styles.reasonIcon} />
                          <span>思考</span>
                          <DownOutlined className={styles.reasonChevron} />
                        </summary>
                        <div className={styles.reasonText}>{part.text}</div>
                      </details>
                    );
                  })}
                </>
              )}
            </div>
          </>
        )}

        {askUserParts.length > 0 && (
          <div className={styles.msgContent}>
            {askUserParts.map((part) => {
              const isPending = sseState.pendingAskUser?.toolCallId === part.toolCallId;
              return (
                <AskUserCard
                  key={part.toolCallId}
                  questions={part.questions}
                  answers={part.answers}
                  status={
                    isPending ? 'pending' : part.status === 'pending' ? 'dismissed' : part.status
                  }
                  onSubmit={
                    isPending ? (answers) => sseActions.answer(part.toolCallId, answers) : undefined
                  }
                />
              );
            })}
          </div>
        )}

        {toolParts.length > 0 && (
          <div className={styles.msgContent}>
            {toolParts.map((part, pidx) => {
              const card = sseState.toolCards.find((t) => t.toolCallId === part.toolCallId);
              const toolStatus = resolveToolCardDisplayStatus(card?.status);
              // 工具卡片默认收起只露头部（对齐 demoui）；媒体产物在折叠区外保持可见
              return (
                <Fragment key={pidx}>
                  <details className={styles.toolCard}>
                    <summary className={styles.toolHead}>
                      <Tag className={styles.toolBadge}>工具</Tag>
                      <span className={styles.toolName}>{part.toolName}</span>
                      <span
                        className={`${styles.toolStatus} ${TOOL_CARD_STATUS_CLASS[toolStatus]}`}
                      >
                        <span
                          className={`${styles.toolStatusDot} ${
                            toolStatus === 'pending' ? styles.toolStatusDotPending : ''
                          }`}
                        />
                        {formatToolCardStatusLabel(toolStatus, card?.duration)}
                      </span>
                      <DownOutlined className={styles.toolChevron} />
                    </summary>

                    <div className={styles.toolBody}>
                      {!!part.input && (
                        <details className={styles.toolDetails}>
                          <summary className={styles.toolDetailsSummary}>
                            <DownOutlined className={styles.toolDetailsIcon} />
                            输入参数
                          </summary>
                          <pre className={styles.toolPre}>
                            {JSON.stringify(part.input, null, 2)}
                          </pre>
                        </details>
                      )}

                      {!!card?.output && (
                        <details className={styles.toolDetails}>
                          <summary className={styles.toolDetailsSummary}>
                            <DownOutlined className={styles.toolDetailsIcon} />
                            执行结果
                          </summary>
                          <pre className={styles.toolPre}>
                            {JSON.stringify(card.output, null, 2)}
                          </pre>
                        </details>
                      )}
                    </div>
                  </details>

                  {/* 结果里的图片/视频/音频内联展示（对齐 demoui appendMedia） */}
                  {!!card?.output && <MediaList source={card.output} />}
                </Fragment>
              );
            })}
          </div>
        )}
      </div>
    );
  };

  return (
    <div
      className={`${styles.chatArea} ${variant === 'quiet' ? styles.chatAreaQuiet : ''} ${
        embedded ? styles.chatAreaEmbedded : ''
      } ${composerOnly ? styles.chatAreaComposer : ''} ${
        composerFill ? styles.chatAreaComposerFill : ''
      }`}
    >
      {composerOnly ? (
        <div
          ref={chatRef}
          className={styles.chatScrollSink}
          aria-hidden
          onScroll={handleListScroll}
        />
      ) : (
        <div className={styles.chatWrap}>
          <div
            ref={chatRef}
            className={styles.chat}
            onScroll={handleListScroll}
            onWheel={handleListWheel}
            onPointerDown={handleListPointerDown}
            onTouchMove={handleListTouchMove}
            onLoadCapture={followContentGrowth}
            onLoadedMetadataCapture={followContentGrowth}
          >
            {isLoadingMoreItems && (
              <div className={styles.chatLoadMore}>
                加载更早消息
                <span className={styles.chatLoadMoreDots} aria-hidden />
              </div>
            )}
            {timeline.length === 0 && !isLoadingMoreItems && (
              <ChatEmptyState
                agentName={emptyAgentName}
                suggestions={suggestions}
                onSuggestion={setInputText}
                promptExtra={emptyPromptExtra}
                suggestionsLoading={suggestionsLoading}
                suggestionSkeletonCount={suggestionSkeletonCount}
                variant={variant}
                showIcon={showEmptyIcon}
                title={emptyTitle}
                description={emptyDescription}
              />
            )}

            {/* 消息时间线 */}
            {timeline.map((msg, idx) => renderMessage(msg, idx))}

            {/* 流式输出中的当前消息（独立渲染，避免 ReactMarkdown 逐字重解析） */}
            {sseState.isRunning && (sseState.streamingText || sseState.streamingReason) && (
              <div className={`${styles.msg} ${styles.assistant}`}>
                <div className={styles.msgHeader}>
                  <div className={styles.msgAvatar}>
                    <AgentMarkIcon size={13} />
                  </div>
                  <span className={styles.msgName}>agent-k</span>
                </div>
                <div className={styles.msgContent}>
                  {sseState.streamingReason && (
                    <details className={styles.reason} open>
                      <summary className={styles.reasonSummary}>
                        <ThunderboltOutlined className={styles.reasonIcon} />
                        <span>思考</span>
                        <DownOutlined className={styles.reasonChevron} />
                      </summary>
                      <div className={styles.reasonText}>{sseState.streamingReason}</div>
                    </details>
                  )}
                  {sseState.streamingText && (
                    <div className={`${styles.msgText} ${styles.msgTextStreaming}`}>
                      {sseState.streamingText}
                      <span className={styles.cursor} />
                      <MediaList source={sseState.streamingText} />
                    </div>
                  )}
                </div>
              </div>
            )}

            {/* ask_user 问卷：等待用户选择选项并提交答案 */}
            {sseState.pendingAskUser &&
              !askUserToolCallIds.has(sseState.pendingAskUser.toolCallId) && (
                <div className={`${styles.msg} ${styles.assistant}`}>
                  <div className={styles.msgContent}>
                    <AskUserCard
                      questions={sseState.pendingAskUser.questions}
                      onSubmit={(answers) =>
                        sseActions.answer(sseState.pendingAskUser!.toolCallId, answers)
                      }
                    />
                  </div>
                </div>
              )}

            {sseState.error && <div className={styles.noticeError}>{sseState.error}</div>}
          </div>
          {showJumpBottom ? (
            <div className={styles.jumpBottom}>
              <Tooltip title="回到底部">
                <Button
                  className={styles.jumpBottomBtn}
                  shape="circle"
                  icon={<ArrowDownOutlined />}
                  onClick={jumpToBottom}
                  aria-label="滚动到底部"
                />
              </Tooltip>
            </div>
          ) : null}
        </div>
      )}

      {/* 输入框：统一外壳，附件预览/上传/发送都在壳内（对齐主流对话产品） */}
      <div className={styles.composer}>
        {(composerOnly || (timeline.length === 0 && !isLoadingMoreItems)) && composerPromptStrip ? (
          <div className={styles.composerPromptStrip}>{composerPromptStrip}</div>
        ) : null}
        <div
          className={`${styles.composerShell} ${
            studioComposer ? styles.composerShellStudio : ''
          } ${composerFill ? styles.composerShellFill : ''}`}
        >
          {studioComposer ? (
            <div
              className={`${styles.composerStudioBody} ${
                composerScrollBody ? styles.composerStudioBodyScroll : ''
              }`}
            >
              {composerPreInputExtra}
              {enableAttachments ? (
                <ChatAttachmentBar
                  ref={attachRef}
                  layout="tile"
                  disabled={isBusy || sendPending}
                  onReadyChange={handleAttachReady}
                  uploadAccept={uploadAccept}
                  uploadAllowedTypes={uploadAllowedTypes}
                  uploadAddLabel={uploadAddLabel}
                />
              ) : null}
              <div className={styles.composerInputColumn}>
                <TextArea
                  ref={composerInputRef}
                  value={inputText}
                  onChange={(e) => setInputText(e.target.value)}
                  onKeyDown={handleKeyDown}
                  placeholder={composerPlaceholderText(
                    isBusy,
                    sendPending,
                    inputPlaceholder,
                    variant,
                  )}
                  disabled={sendPending}
                  autoSize={
                    composerFill && !composerScrollBody
                      ? false
                      : composerScrollBody
                        ? false
                        : { minRows: 3, maxRows: 8 }
                  }
                  variant="borderless"
                  className={`${styles.input} ${
                    composerFill && !composerScrollBody ? styles.inputFill : ''
                  } ${composerScrollBody ? styles.inputScrollBodyFill : ''}`}
                />
              </div>
            </div>
          ) : (
            <>
              {enableAttachments && (
                <ChatAttachmentBar
                  ref={attachRef}
                  onReadyChange={handleAttachReady}
                  uploadAccept={uploadAccept}
                  uploadAllowedTypes={uploadAllowedTypes}
                />
              )}
              <TextArea
                ref={composerInputRef}
                value={inputText}
                onChange={(e) => setInputText(e.target.value)}
                onKeyDown={handleKeyDown}
                placeholder={
                  sendPending
                    ? '正在准备对话，请稍候…'
                    : isBusy
                      ? '生成中，Enter 打断并发送新消息'
                      : inputPlaceholder ||
                        (variant === 'quiet'
                          ? '今天帮你做些什么？'
                          : enableAttachments
                            ? '输入消息或上传附件，Enter 发送'
                            : '输入消息，Enter 发送')
                }
                autoSize={
                  variant === 'quiet' ? { minRows: 2, maxRows: 6 } : { minRows: 1, maxRows: 8 }
                }
                variant="borderless"
                className={styles.input}
                disabled={sendPending}
              />
            </>
          )}
          <div className={styles.composerToolbar}>
            {enableAttachments && !studioComposer ? (
              <MediaSourceMenu
                disabled={isBusy || sendPending}
                showLabel={variant === 'quiet'}
                buttonType="text"
                buttonShape={variant === 'quiet' ? undefined : 'circle'}
                className={`${styles.attachBtn} ${
                  variant === 'quiet' ? styles.attachBtnWithLabel : ''
                }`}
                onLocalUpload={() => attachRef.current?.open()}
                onSelectSource={(tab) => attachRef.current?.openPicker(tab)}
              />
            ) : null}
            {composerToolbarExtra}
            <span className={styles.composerToolbarGrow} />
            {/* 生成中有内容时按钮变成「打断并发送」，没内容才是纯停止 */}
            <Tooltip
              title={composerSendTooltipText({
                showStop,
                isCancelling: sseState.isCancelling,
                canSend,
                isBusy,
                sendPending,
              })}
            >
              <Button
                type={showStop ? 'default' : 'primary'}
                danger={showStop}
                shape="circle"
                icon={showStop ? <StopOutlined /> : <ArrowUpOutlined />}
                onClick={
                  showStop ? () => void sseActions.cancel() : () => void handleComposerSend()
                }
                loading={sseState.isCancelling || sendPending}
                disabled={!showStop && (!canSend || sendPending)}
                className={styles.sendBtn}
                aria-label={sendAriaLabel(showStop, isBusy)}
              />
            </Tooltip>
          </div>
        </div>
      </div>
    </div>
  );
}

export default ChatArea;
