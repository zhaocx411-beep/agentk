/**
 * Agent-loop 生成过程：展示助手流式输出与已完成的回复正文
 */
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ComponentPropsWithoutRef,
  type MutableRefObject,
} from 'react';
import {
  ArrowDownOutlined,
  ArrowUpOutlined,
  CopyOutlined,
  DownOutlined,
  FileOutlined,
  ThunderboltOutlined,
  ToolOutlined,
  UserOutlined,
} from '@ant-design/icons';
import { Button, Image, Spin, Tooltip } from 'antd';
import { AgentMarkIcon } from '@/components';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import AskUserCard from '@/pages/AgentPlayground/components/chat/AskUserCard';
import type {
  AskUserAnswer,
  AskUserCardStatus,
  AskUserQuestion,
  MessageItem,
  MessagePart,
  ToolCardState,
} from '@/pages/AgentPlayground/types';
import { CHAT_LOAD_MORE_THRESHOLD_PX } from '@/pages/AgentPlayground/constants';
import {
  collectMediaUrls,
  resolveMessagePartMediaKind,
  resolveUserMessageDisplay,
  type CollectedMediaUrls,
} from '@/pages/AgentPlayground/hooks/sse/helpers';
import type { SSEState } from '@/pages/AgentPlayground/hooks/useSSE';
import { copyTextToClipboard } from '@/utils/clipboard';
import { message } from '@/utils/feedback';
import { restoreLegacyHeadingIcons, safeMarkdownUrl } from '@/utils/assistantMarkdown';
import styles from './OfficialLoopStreamPanel.module.less';

const NEAR_EDGE_PX = 96;
/** 首屏不足一屏时最多主动补几页更早对话 */
const AUTO_FILL_MAX_PAGES = 3;

type ScrollNavAction = 'top' | 'bottom';

function CopyAction({ text, label }: { text: string; label: string }) {
  const handleCopy = async () => {
    try {
      await copyTextToClipboard(text);
      message.success('已复制');
    } catch {
      message.error('复制失败');
    }
  };
  return (
    <Button
      type="text"
      size="small"
      className={styles.streamCopyButton}
      icon={<CopyOutlined />}
      aria-label={label}
      onClick={(event) => {
        event.preventDefault();
        event.stopPropagation();
        void handleCopy();
      }}
    >
      复制
    </Button>
  );
}

function createMarkdownComponents(onMediaLoad: () => void) {
  return {
    p: ({ children }: ComponentPropsWithoutRef<'p'>) => (
      <div className={styles.markdownParagraph}>{children}</div>
    ),
    a: ({ href, children, ...props }: ComponentPropsWithoutRef<'a'>) => (
      <a href={href} target="_blank" rel="noreferrer" {...props}>
        {children}
      </a>
    ),
    img: (props: ComponentPropsWithoutRef<'img'>) => (
      <Image
        src={props.src}
        alt={props.alt || ''}
        style={{ maxWidth: 400, maxHeight: 300, objectFit: 'contain' }}
        preview={{ mask: false }}
        onLoad={onMediaLoad}
      />
    ),
    table: ({ children, ...props }: ComponentPropsWithoutRef<'table'>) => (
      <div className={styles.markdownTableWrap}>
        <table {...props}>{children}</table>
      </div>
    ),
  };
}

function MediaList({ source, onMediaLoad }: { source: unknown; onMediaLoad: () => void }) {
  const media = useMemo(() => collectMediaUrls(source), [source]);
  return <MediaTiles media={media} onMediaLoad={onMediaLoad} />;
}

function MediaTiles({
  media,
  onMediaLoad,
}: {
  media: CollectedMediaUrls;
  onMediaLoad: () => void;
}) {
  if (media.videos.length === 0 && media.audios.length === 0 && media.images.length === 0) {
    return null;
  }
  return (
    <div className={styles.mediaList}>
      {media.videos.map((url) => (
        <video
          key={url}
          className={styles.mediaVideo}
          controls
          preload="metadata"
          src={url}
          onLoadedMetadata={onMediaLoad}
        />
      ))}
      {media.audios.map((url) => (
        <div key={url} className={styles.mediaAudioWrap}>
          <audio className={styles.mediaAudio} controls preload="metadata" src={url} />
        </div>
      ))}
      {media.images.map((url) => (
        <Image
          key={url}
          className={styles.mediaImg}
          src={url}
          alt=""
          preview={{ mask: false }}
          onLoad={onMediaLoad}
        />
      ))}
    </div>
  );
}

type UserMediaPart = Extract<MessagePart, { type: 'image' | 'file' }>;

type StreamBlock =
  | { key: string; kind: 'user'; text: string; media: UserMediaPart[] }
  | { key: string; kind: 'text'; text: string }
  | { key: string; kind: 'reason'; text: string }
  | {
      key: string;
      kind: 'ask';
      toolCallId: string;
      questions: AskUserQuestion[];
      answers?: AskUserAnswer[];
      status: AskUserCardStatus;
    }
  | {
      key: string;
      kind: 'tool';
      toolCallId: string;
      toolName: string;
      input: unknown;
      payload?: unknown;
    };

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function parseToolInput(input: unknown): unknown {
  if (typeof input !== 'string') return input;
  try {
    return JSON.parse(input) as unknown;
  } catch {
    return input;
  }
}

function formatModelValues(value: unknown): string[] {
  if (typeof value === 'string' && value.trim()) return [value.trim()];
  if (Array.isArray(value)) return value.flatMap(formatModelValues);
  return Object.entries(asRecord(value)).flatMap(([key, item]) =>
    formatModelValues(item).map((model) => `${key}: ${model}`),
  );
}

function toolModels(input: unknown): string[] {
  const models = new Set<string>();
  for (const [key, value] of Object.entries(asRecord(parseToolInput(input)))) {
    if (key === 'model' || key === 'models' || /Model$/.test(key)) {
      for (const model of formatModelValues(value)) models.add(model);
    }
  }
  return [...models];
}

function formatToolPayload(payload: unknown): string {
  const parsed = parseToolInput(payload);
  if (typeof parsed === 'string') return parsed;
  try {
    return JSON.stringify(parsed ?? {}, null, 2);
  } catch {
    return String(parsed ?? '');
  }
}

function streamBlocks(messages: MessageItem[]): StreamBlock[] {
  const blocks: StreamBlock[] = [];
  for (const [messageIndex, message] of messages.entries()) {
    if (message.role === 'user') {
      const { text, media } = resolveUserMessageDisplay(message);
      if (text || media.length > 0) {
        blocks.push({ key: `user-${messageIndex}`, kind: 'user', text, media });
      }
      continue;
    }
    if (message.role !== 'assistant') continue;
    for (const [partIndex, part] of message.parts.entries()) {
      if (part.type === 'text' && part.text.trim()) {
        blocks.push({ key: `text-${messageIndex}-${partIndex}`, kind: 'text', text: part.text });
      } else if (part.type === 'reasoning' && part.text.trim()) {
        blocks.push({
          key: `reason-${messageIndex}-${partIndex}`,
          kind: 'reason',
          text: part.text,
        });
      } else if (part.type === 'ask-user') {
        blocks.push({
          key: `ask-${messageIndex}-${partIndex}`,
          kind: 'ask',
          toolCallId: part.toolCallId,
          questions: part.questions,
          answers: part.answers,
          status: part.status,
        });
      } else if (part.type === 'tool-call' && part.toolName !== 'ask_user') {
        blocks.push({
          key: `tool-${messageIndex}-${partIndex}`,
          kind: 'tool',
          toolCallId: part.toolCallId,
          toolName: part.toolName || 'tool',
          input: part.input,
          payload: part.payload,
        });
      }
    }
  }
  return blocks;
}

function UserMediaPreview({
  part,
  index,
  onMediaLoad,
}: {
  part: UserMediaPart;
  index: number;
  onMediaLoad: () => void;
}) {
  if (part.type === 'image') {
    return (
      <Image
        key={`user-media-${index}`}
        className={styles.userAttachImage}
        src={part.url}
        alt={part.name || '附件图片'}
        preview={{ mask: false }}
        onLoad={onMediaLoad}
      />
    );
  }
  const mediaKind = resolveMessagePartMediaKind(part);
  if (mediaKind === 'video') {
    return (
      <video
        key={`user-media-${index}`}
        className={styles.userAttachVideo}
        src={part.url}
        controls
        preload="metadata"
        onLoadedMetadata={onMediaLoad}
      />
    );
  }
  if (mediaKind === 'audio') {
    return (
      <audio
        key={`user-media-${index}`}
        className={styles.userAttachAudio}
        src={part.url}
        controls
        preload="metadata"
        aria-label={part.name || '附件音频'}
      />
    );
  }
  return (
    <a
      key={`user-media-${index}`}
      href={part.url}
      target="_blank"
      rel="noreferrer"
      className={styles.userAttachFile}
    >
      <FileOutlined />
      <span>{part.name || '附件'}</span>
    </a>
  );
}

interface OfficialLoopStreamPanelProps {
  sseState: SSEState;
  onAnswer: (toolCallId: string, answers: AskUserAnswer[]) => Promise<boolean>;
  variant?: 'default' | 'stage';
  /** 绑定滚动容器，供会话 events 上滚翻页 */
  scrollRef?: MutableRefObject<HTMLDivElement | null>;
  onHistoryScroll?: () => void;
  isLoadingHistory?: boolean;
  /** 切换会话时重置自动补页额度 */
  historyScrollToken?: string | number | null;
}

function StreamPanelEmpty() {
  return (
    <div className={styles.emptyState}>
      <span className={styles.emptyIcon} aria-hidden>
        <AgentMarkIcon size={28} />
      </span>
      <h3 className={styles.emptyTitle}>等待开始生成</h3>
      <p className={styles.emptyDesc}>发送消息后，你的输入与助手的思考、回复会实时显示在这里</p>
      <ul className={styles.emptyHints}>
        <li>你的输入与附件</li>
        <li>思考过程与正文回复</li>
        <li>工具调用与执行进度</li>
        <li>成片产出可在「当前产物」查看</li>
      </ul>
    </div>
  );
}

function StreamPanelLoading() {
  return (
    <div className={styles.emptyState}>
      <Spin size="small" />
      <p className={styles.loadingText}>正在生成，请稍候…</p>
    </div>
  );
}

type ToolStreamBlock = Extract<StreamBlock, { kind: 'tool' }>;

const TOOL_STATUS_LABEL = {
  running: '执行中',
  ok: '已完成',
  err: '失败',
} as const;

const TOOL_STATUS_CLASS = {
  running: styles.toolCallStatusRunning,
  ok: styles.toolCallStatusOk,
  err: styles.toolCallStatusErr,
} as const;

function ToolCallBlock({
  block,
  toolCard,
  onMediaLoad,
}: {
  block: ToolStreamBlock;
  toolCard?: ToolCardState;
  onMediaLoad: () => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const status = toolCard?.status ?? 'running';
  const models = toolModels(block.input);
  const callPayload = block.payload ?? {
    toolName: block.toolName,
    toolCallId: block.toolCallId,
    args: parseToolInput(block.input),
  };
  const copyPayload = {
    call: callPayload,
    ...(toolCard?.output !== undefined ? { execution: toolCard.output } : {}),
  };
  return (
    <details
      className={styles.toolCall}
      onToggle={(event) => setExpanded(event.currentTarget.open)}
    >
      <summary className={styles.toolCallSummary}>
        <ToolOutlined className={styles.toolCallIcon} />
        <span className={styles.toolCallLabel}>工具调用</span>
        <span className={styles.toolCallName}>{block.toolName}</span>
        {models.map((model) => (
          <span key={model} className={styles.toolCallModel}>
            {model}
          </span>
        ))}
        <CopyAction
          text={formatToolPayload(copyPayload)}
          label={`复制工具调用 ${block.toolName}`}
        />
        <span className={`${styles.toolCallStatus} ${TOOL_STATUS_CLASS[status]}`}>
          {TOOL_STATUS_LABEL[status]}
        </span>
        <DownOutlined className={styles.toolCallChevron} />
      </summary>
      {expanded ? (
        <div className={styles.toolCallDetails}>
          <section>
            <h4 className={styles.toolCallSectionTitle}>完整调用参数</h4>
            <pre className={styles.toolCallInput}>{formatToolPayload(callPayload)}</pre>
          </section>
          {toolCard?.output !== undefined ? (
            <section>
              <h4 className={styles.toolCallSectionTitle}>
                {status === 'err' ? '完整失败信息' : '完整执行结果'}
              </h4>
              <pre className={styles.toolCallInput}>{formatToolPayload(toolCard.output)}</pre>
              <MediaList source={toolCard.output} onMediaLoad={onMediaLoad} />
            </section>
          ) : null}
        </div>
      ) : null}
    </details>
  );
}

function StreamScrollBody({
  sseState,
  blocks,
  onAnswer,
  scrollRef,
  onHistoryScroll,
  isLoadingHistory = false,
  historyScrollToken = null,
}: {
  sseState: SSEState;
  blocks: StreamBlock[];
  onAnswer: (toolCallId: string, answers: AskUserAnswer[]) => Promise<boolean>;
  scrollRef?: MutableRefObject<HTMLDivElement | null>;
  onHistoryScroll?: () => void;
  isLoadingHistory?: boolean;
  historyScrollToken?: string | number | null;
}) {
  const localScrollRef = useRef<HTMLDivElement | null>(
    null,
  ) as MutableRefObject<HTMLDivElement | null>;
  const scrollElRef = scrollRef ?? localScrollRef;
  const contentRef = useRef<HTMLDivElement>(null);
  const followOutputRef = useRef(true);
  /** 只有明确的向上滚动意图才停止跟随，媒体撑高不能被误判为用户上翻。 */
  const userDetachedRef = useRef(false);
  const wasRunningRef = useRef(false);
  const autoFillLeftRef = useRef(AUTO_FILL_MAX_PAGES);
  const pendingSessionBottomRef = useRef(true);
  const currentBlocksRef = useRef(blocks);
  currentBlocksRef.current = blocks;
  const lastCommittedBlocksRef = useRef(blocks);
  const sessionSwitchSourceBlocksRef = useRef<StreamBlock[] | null>(null);
  const sessionTokenInitializedRef = useRef(false);
  const [scrollNavAction, setScrollNavAction] = useState<ScrollNavAction | null>(null);

  const scrollToBottom = useCallback(
    (behavior: ScrollBehavior = 'auto') => {
      const node = scrollElRef.current;
      if (!node) return;
      node.scrollTo({ top: node.scrollHeight, behavior });
    },
    [scrollElRef],
  );

  const syncScrollNav = useCallback(() => {
    const node = scrollElRef.current;
    if (!node) return;
    const { scrollTop, scrollHeight, clientHeight } = node;
    const nearBottom = scrollHeight - scrollTop - clientHeight <= NEAR_EDGE_PX;
    const scrollable = scrollHeight - clientHeight > NEAR_EDGE_PX;
    if (!scrollable) {
      setScrollNavAction(null);
      return;
    }
    setScrollNavAction(nearBottom ? 'top' : 'bottom');
  }, [scrollElRef]);

  const handleScroll = useCallback(() => {
    const node = scrollElRef.current;
    if (!node) return;
    const nearBottom = node.scrollHeight - node.scrollTop - node.clientHeight <= NEAR_EDGE_PX;
    if (nearBottom) {
      followOutputRef.current = true;
      userDetachedRef.current = false;
    } else if (userDetachedRef.current) {
      followOutputRef.current = false;
    }
    syncScrollNav();
    if (onHistoryScroll && node.scrollTop <= CHAT_LOAD_MORE_THRESHOLD_PX) {
      onHistoryScroll();
    }
  }, [onHistoryScroll, scrollElRef, syncScrollNav]);

  const handleWheel = useCallback((event: { deltaY: number }) => {
    if (event.deltaY >= 0) return;
    userDetachedRef.current = true;
    followOutputRef.current = false;
  }, []);

  const handlePointerDown = useCallback(
    (event: { clientX: number }) => {
      const node = scrollElRef.current;
      if (!node) return;
      const rightEdge = node.getBoundingClientRect().right;
      if (event.clientX < rightEdge - 16) return;
      userDetachedRef.current = true;
      followOutputRef.current = false;
    },
    [scrollElRef],
  );

  const handleTouchMove = useCallback(() => {
    userDetachedRef.current = true;
    followOutputRef.current = false;
  }, []);

  const jumpToTop = useCallback(() => {
    const node = scrollElRef.current;
    if (!node) return;
    userDetachedRef.current = true;
    followOutputRef.current = false;
    node.scrollTo({ top: 0, behavior: 'smooth' });
  }, [scrollElRef]);

  const jumpToBottom = useCallback(() => {
    userDetachedRef.current = false;
    followOutputRef.current = true;
    scrollToBottom('smooth');
  }, [scrollToBottom]);

  useEffect(() => {
    if (sseState.isRunning && !wasRunningRef.current) {
      userDetachedRef.current = false;
      followOutputRef.current = true;
    }
    wasRunningRef.current = sseState.isRunning;
  }, [sseState.isRunning]);

  useEffect(() => {
    if (followOutputRef.current) {
      scrollToBottom('auto');
    }
    syncScrollNav();
  }, [
    blocks,
    sseState.pendingAskUser,
    sseState.streamingText,
    sseState.streamingReason,
    scrollToBottom,
    syncScrollNav,
  ]);

  useEffect(() => {
    if (!sessionTokenInitializedRef.current) {
      sessionTokenInitializedRef.current = true;
      pendingSessionBottomRef.current = false;
      sessionSwitchSourceBlocksRef.current = null;
      return;
    }
    // token 可能先于新会话内容变化，不能在这里滚动仍留在 DOM 中的旧会话。
    const renderedBlocks = currentBlocksRef.current;
    const contentAlreadyChanged = lastCommittedBlocksRef.current !== renderedBlocks;
    userDetachedRef.current = false;
    followOutputRef.current = true;
    pendingSessionBottomRef.current = true;
    sessionSwitchSourceBlocksRef.current = contentAlreadyChanged ? null : renderedBlocks;
    autoFillLeftRef.current = AUTO_FILL_MAX_PAGES;
    setScrollNavAction(null);
  }, [historyScrollToken]);

  useEffect(() => {
    if (isLoadingHistory || !pendingSessionBottomRef.current) return;
    // 等 blocks 脱离切换前的引用，确认新会话内容已经提交到 DOM 后再贴底。
    if (sessionSwitchSourceBlocksRef.current === blocks) return;
    // 等新会话历史完成渲染后再最终贴底，避免旧内容高度或分页锚点造成中间位置。
    const frame = requestAnimationFrame(() => {
      followOutputRef.current = true;
      scrollToBottom('auto');
      syncScrollNav();
      pendingSessionBottomRef.current = false;
      sessionSwitchSourceBlocksRef.current = null;
    });
    return () => cancelAnimationFrame(frame);
  }, [blocks, historyScrollToken, isLoadingHistory, scrollToBottom, syncScrollNav]);

  useEffect(() => {
    lastCommittedBlocksRef.current = blocks;
  }, [blocks]);

  const wasLoadingHistoryRef = useRef(false);
  useEffect(() => {
    if (wasLoadingHistoryRef.current && !isLoadingHistory && onHistoryScroll) {
      const el = scrollElRef.current;
      if (el && el.scrollTop <= CHAT_LOAD_MORE_THRESHOLD_PX) {
        requestAnimationFrame(() => onHistoryScroll());
      }
    }
    wasLoadingHistoryRef.current = isLoadingHistory;
  }, [isLoadingHistory, onHistoryScroll, scrollElRef]);

  useEffect(() => {
    const el = scrollElRef.current;
    if (!el || isLoadingHistory || blocks.length === 0 || !onHistoryScroll) return;
    if (autoFillLeftRef.current <= 0) return;
    if (el.clientHeight === 0 || el.scrollHeight > el.clientHeight) return;
    autoFillLeftRef.current -= 1;
    onHistoryScroll();
  }, [blocks, historyScrollToken, isLoadingHistory, onHistoryScroll, scrollElRef]);

  useEffect(() => {
    const content = contentRef.current;
    if (!content) return;

    const observer = new ResizeObserver(() => {
      if (userDetachedRef.current) return;
      if (
        pendingSessionBottomRef.current &&
        sessionSwitchSourceBlocksRef.current === currentBlocksRef.current
      ) {
        return;
      }
      followOutputRef.current = true;
      scrollToBottom('auto');
      syncScrollNav();
    });
    observer.observe(content);
    return () => observer.disconnect();
  }, [scrollToBottom, syncScrollNav]);

  const handleMediaLoad = useCallback(() => {
    if (userDetachedRef.current) return;
    if (
      pendingSessionBottomRef.current &&
      sessionSwitchSourceBlocksRef.current === currentBlocksRef.current
    ) {
      return;
    }
    followOutputRef.current = true;
    requestAnimationFrame(() => {
      scrollToBottom('auto');
      syncScrollNav();
    });
  }, [scrollToBottom, syncScrollNav]);

  const markdownComponents = useMemo(
    () => createMarkdownComponents(handleMediaLoad),
    [handleMediaLoad],
  );

  const assignScrollRef = useCallback(
    (node: HTMLDivElement | null) => {
      localScrollRef.current = node;
      if (scrollRef) scrollRef.current = node;
    },
    [scrollRef],
  );

  return (
    <div className={styles.scrollWrap}>
      <div
        ref={assignScrollRef}
        className={styles.body}
        onScroll={handleScroll}
        onWheel={handleWheel}
        onPointerDown={handlePointerDown}
        onTouchMove={handleTouchMove}
      >
        <div ref={contentRef} className={styles.bodyContent}>
          {isLoadingHistory ? (
            <div className={styles.historyLoadMore} aria-live="polite">
              加载更早消息
              <span className={styles.historyLoadMoreDots} aria-hidden />
            </div>
          ) : null}
          {blocks.map((block) =>
            block.kind === 'user' ? (
              <div key={block.key} className={styles.userMsg}>
                <div className={styles.userMsgHeader}>
                  <CopyAction
                    text={[block.text, ...block.media.map((item) => item.url)]
                      .filter(Boolean)
                      .join('\n')}
                    label="复制用户消息"
                  />
                  <div className={styles.userMsgAvatar}>
                    <UserOutlined />
                  </div>
                  <span className={styles.userMsgName}>你</span>
                </div>
                <div className={styles.userMsgContent}>
                  <div className={styles.userBubbleStack}>
                    {block.media.length > 0 ? (
                      <div className={styles.userAttachments}>
                        {block.media.map((part, index) => (
                          <UserMediaPreview
                            key={`${block.key}-media-${index}`}
                            part={part}
                            index={index}
                            onMediaLoad={handleMediaLoad}
                          />
                        ))}
                      </div>
                    ) : null}
                    {block.text ? <div className={styles.userMsgText}>{block.text}</div> : null}
                  </div>
                </div>
              </div>
            ) : block.kind === 'reason' ? (
              <details key={block.key} className={styles.reason} open={sseState.isRunning}>
                <summary className={styles.reasonSummary}>
                  <ThunderboltOutlined className={styles.reasonIcon} />
                  <span>思考</span>
                  <CopyAction text={block.text} label="复制思考过程" />
                  <DownOutlined className={styles.reasonChevron} />
                </summary>
                <div className={styles.reasonText}>{block.text}</div>
              </details>
            ) : block.kind === 'ask' ? (
              <div key={block.key} className={styles.askUserBlock}>
                <AskUserCard
                  questions={block.questions}
                  answers={block.answers}
                  status={
                    sseState.pendingAskUser?.toolCallId === block.toolCallId
                      ? 'pending'
                      : block.status === 'pending'
                        ? 'dismissed'
                        : block.status
                  }
                  onSubmit={
                    sseState.pendingAskUser?.toolCallId === block.toolCallId
                      ? (answers) => onAnswer(block.toolCallId, answers)
                      : undefined
                  }
                />
              </div>
            ) : block.kind === 'tool' ? (
              <ToolCallBlock
                key={block.key}
                block={block}
                toolCard={sseState.toolCards.find((card) => card.toolCallId === block.toolCallId)}
                onMediaLoad={handleMediaLoad}
              />
            ) : (
              <div key={block.key} className={`${styles.textBlock} ${styles.textBlockCopyable}`}>
                <CopyAction text={restoreLegacyHeadingIcons(block.text)} label="复制助手回复" />
                <ReactMarkdown
                  remarkPlugins={[remarkGfm]}
                  components={markdownComponents}
                  urlTransform={safeMarkdownUrl}
                >
                  {restoreLegacyHeadingIcons(block.text)}
                </ReactMarkdown>
                <MediaList source={block.text} onMediaLoad={handleMediaLoad} />
              </div>
            ),
          )}
          {sseState.streamingReason ? (
            <details className={styles.reason} open>
              <summary className={styles.reasonSummary}>
                <ThunderboltOutlined className={styles.reasonIcon} />
                <span>思考</span>
                <CopyAction text={sseState.streamingReason} label="复制当前思考" />
                <DownOutlined className={styles.reasonChevron} />
              </summary>
              <div className={styles.reasonText}>{sseState.streamingReason}</div>
            </details>
          ) : null}
          {sseState.streamingText ? (
            <div
              className={`${styles.textBlock} ${styles.textStreaming} ${styles.textBlockCopyable}`}
            >
              <CopyAction
                text={restoreLegacyHeadingIcons(sseState.streamingText)}
                label="复制当前回复"
              />
              <ReactMarkdown
                remarkPlugins={[remarkGfm]}
                components={markdownComponents}
                urlTransform={safeMarkdownUrl}
              >
                {restoreLegacyHeadingIcons(sseState.streamingText)}
              </ReactMarkdown>
              <span className={styles.cursor} aria-hidden />
              <MediaList source={sseState.streamingText} onMediaLoad={handleMediaLoad} />
            </div>
          ) : null}
          {sseState.pendingAskUser &&
          !blocks.some(
            (block) =>
              block.kind === 'ask' && block.toolCallId === sseState.pendingAskUser?.toolCallId,
          ) ? (
            <div className={styles.askUserBlock}>
              <AskUserCard
                questions={sseState.pendingAskUser.questions}
                onSubmit={(answers) => onAnswer(sseState.pendingAskUser!.toolCallId, answers)}
              />
            </div>
          ) : null}
        </div>
      </div>
      {scrollNavAction ? (
        <div className={styles.scrollNav}>
          {scrollNavAction === 'top' ? (
            <Tooltip title="到顶">
              <Button
                type="default"
                shape="circle"
                className={styles.scrollNavBtn}
                icon={<ArrowUpOutlined />}
                aria-label="滚动到顶部"
                onClick={jumpToTop}
              />
            </Tooltip>
          ) : (
            <Tooltip title="到底">
              <Button
                type="default"
                shape="circle"
                className={styles.scrollNavBtn}
                icon={<ArrowDownOutlined />}
                aria-label="滚动到底部"
                onClick={jumpToBottom}
              />
            </Tooltip>
          )}
        </div>
      ) : null}
    </div>
  );
}

function OfficialLoopStreamPanel({
  sseState,
  onAnswer,
  variant = 'default',
  scrollRef,
  onHistoryScroll,
  isLoadingHistory,
  historyScrollToken,
}: OfficialLoopStreamPanelProps) {
  const blocks = useMemo(() => streamBlocks(sseState.messages), [sseState.messages]);
  const hasStream = Boolean(sseState.streamingText || sseState.streamingReason);
  const hasContent = blocks.length > 0 || hasStream || Boolean(sseState.pendingAskUser);

  if (!hasContent && !sseState.isRunning) {
    if (variant === 'stage') {
      return (
        <section className={`${styles.panel} ${styles.panelStage}`} aria-label="生成过程">
          <div className={`${styles.body} ${styles.bodyEmpty}`}>
            <StreamPanelEmpty />
          </div>
        </section>
      );
    }
    return null;
  }

  if (!hasContent && sseState.isRunning) {
    return (
      <section
        className={`${styles.panel} ${variant === 'stage' ? styles.panelStage : ''}`}
        aria-label="生成过程"
      >
        <div className={`${styles.body} ${styles.bodyEmpty}`}>
          <StreamPanelLoading />
        </div>
      </section>
    );
  }

  return (
    <section
      className={`${styles.panel} ${variant === 'stage' ? styles.panelStage : ''}`}
      aria-label="生成过程"
    >
      <StreamScrollBody
        sseState={sseState}
        blocks={blocks}
        onAnswer={onAnswer}
        scrollRef={scrollRef}
        onHistoryScroll={onHistoryScroll}
        isLoadingHistory={isLoadingHistory}
        historyScrollToken={historyScrollToken}
      />
    </section>
  );
}

export default OfficialLoopStreamPanel;
