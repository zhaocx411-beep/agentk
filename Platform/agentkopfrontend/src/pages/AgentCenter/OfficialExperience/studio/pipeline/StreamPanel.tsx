/**
 * 实时输出：agent-loop 模式下把 SSE 的思考、正文、工具调用与后台任务铺开
 *
 * 后台任务看板固定在滚动区之外；ask_user 问卷作为最新一条对话内容，
 * 追加到消息流末尾并随对话区域滚动。
 */
import { useEffect, useRef, useState } from 'react';
import { Button, Empty, Input, Tooltip } from 'antd';
import { ArrowUpOutlined } from '@ant-design/icons';
import { HIDDEN_TOOL_CARDS } from '@/pages/AgentPlayground/constants';
import AskUserCard from '@/pages/AgentPlayground/components/chat/AskUserCard';
import type { JobEvent, MessageItem, MessagePart } from '@/pages/AgentPlayground/types';
import type { SSEActions, SSEState } from '@/pages/AgentPlayground/hooks/useSSE';
import { getToolMeta } from '../../../AgentDetail/lib/toolMeta';
import styles from '../CreationStudio.module.less';

const STREAM_ROLE_LABEL: Record<string, string> = {
  user: '我',
  assistant: 'Agent',
};

const TOOL_STATUS_DOT: Record<string, string> = {
  ok: styles.statusDone,
  err: styles.statusFailed,
};

const JOB_STATUS_TEXT: Record<JobEvent['status'], string> = {
  submitted: '进行中',
  completed: '已完成',
  failed: '失败',
  cancelled: '已停止',
};

const JOB_STATUS_CLASS: Record<JobEvent['status'], string> = {
  submitted: styles.statusRunning,
  completed: styles.statusDone,
  failed: styles.statusFailed,
  cancelled: styles.statusCancelled,
};

/** 已终态的任务不再展示远端轮询文案，避免取消后仍显示 processing / 进行中 */
function jobStatusLabel(job: JobEvent): string {
  if (job.status !== 'submitted') return JOB_STATUS_TEXT[job.status];
  return job.remoteStatus || JOB_STATUS_TEXT.submitted;
}

function elapsedText(ms?: number): string {
  if (!ms || ms < 0) return '';
  const seconds = Math.round(ms / 1000);
  if (seconds < 60) return `已等待 ${seconds} 秒`;
  return `已等待 ${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`;
}

/** 一条消息里可读的文本片段，隐藏轮询类工具产生的噪音 */
function readableParts(msg: MessageItem) {
  const texts: string[] = [];
  const reasons: string[] = [];
  const tools: { toolCallId: string; toolName: string }[] = [];
  const asks: Extract<MessagePart, { type: 'ask-user' }>[] = [];
  for (const part of msg.parts) {
    if (part.type === 'text' && part.text.trim()) texts.push(part.text);
    else if (part.type === 'reasoning' && part.text.trim()) reasons.push(part.text);
    else if (part.type === 'ask-user') asks.push(part);
    else if (
      part.type === 'tool-call' &&
      part.toolName !== 'ask_user' &&
      !HIDDEN_TOOL_CARDS.has(part.toolName)
    ) {
      tools.push({ toolCallId: part.toolCallId, toolName: part.toolName });
    }
  }
  return { texts, reasons, tools, asks };
}

interface StreamPanelProps {
  sessionId: string;
  state: SSEState;
  actions: SSEActions;
  /** 选中记录关联的 runId；有值时只看这一轮的输出 */
  runId?: string;
}

function StreamPanel({ sessionId, state, actions, runId }: StreamPanelProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const [reply, setReply] = useState('');

  const messages = runId ? state.messages.filter((msg) => msg.runId === runId) : state.messages;
  // 已停止的任务不再展示；没有进行中/已完成/失败的条目时整块看板隐藏
  const jobs = state.jobs.filter((job) => job.status !== 'cancelled');
  const pendingJobs = jobs.filter((job) => job.status === 'submitted').length;
  const showJobBoard = jobs.length > 0;
  const hasStream = messages.length > 0 || !!state.streamingText || !!state.streamingReason;
  const hasPendingAskMessage = messages.some((messageItem) =>
    messageItem.parts.some(
      (part) => part.type === 'ask-user' && part.toolCallId === state.pendingAskUser?.toolCallId,
    ),
  );

  // 只滚输出区自身，不惊动外层整页滚动
  useEffect(() => {
    if (!state.isRunning && !state.pendingAskUser) return;
    const node = scrollRef.current;
    if (node) node.scrollTop = node.scrollHeight;
  }, [state.isRunning, state.streamingText, messages.length, state.pendingAskUser]);

  const sendReply = async () => {
    const text = reply.trim();
    if (!text) return;
    setReply('');
    await actions.send(sessionId, text);
  };

  return (
    <div className={styles.streamWrap}>
      {showJobBoard && (
        <section className={styles.jobBoard}>
          <p className={styles.jobBoardHead}>
            后台任务 {jobs.length} 个
            {pendingJobs > 0 ? ` · ${pendingJobs} 个进行中` : ' · 已全部结束'}
          </p>
          <ul className={styles.jobList}>
            {jobs.map((job) => (
              <li key={job.jobId} className={styles.jobItem}>
                <span className={`${styles.statusDot} ${JOB_STATUS_CLASS[job.status]}`} />
                <span className={styles.jobKind}>{job.kind}</span>
                <span className={styles.jobStatus}>{jobStatusLabel(job)}</span>
                {job.status === 'submitted' && (
                  <span className={styles.jobElapsed}>{elapsedText(job.elapsedMs)}</span>
                )}
                {job.error && <span className={styles.jobError}>{job.error}</span>}
              </li>
            ))}
          </ul>
        </section>
      )}

      {!hasStream && !showJobBoard && !state.pendingAskUser ? (
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description="还没有输出，点「生成视频」后这里会实时显示 Agent 的思考与调用"
        />
      ) : (
        <div className={styles.stream} ref={scrollRef}>
          {messages.map((msg, idx) => {
            const { texts, reasons, tools, asks } = readableParts(msg);
            if (
              texts.length === 0 &&
              reasons.length === 0 &&
              tools.length === 0 &&
              asks.length === 0
            ) {
              return null;
            }
            return (
              <article key={msg.id ?? `msg-${idx}`} className={styles.streamMsg}>
                <span className={styles.streamRole}>{STREAM_ROLE_LABEL[msg.role] ?? '工具'}</span>
                {reasons.map((textValue, i) => (
                  <p key={`r-${i}`} className={styles.streamReason}>
                    {textValue}
                  </p>
                ))}
                {texts.map((textValue, i) => (
                  <p key={`t-${i}`} className={styles.streamText}>
                    {textValue}
                  </p>
                ))}
                {tools.length > 0 && (
                  <ul className={styles.streamTools}>
                    {tools.map((tool) => {
                      const card = state.toolCards.find((c) => c.toolCallId === tool.toolCallId);
                      const status = card?.status ?? 'running';
                      return (
                        <li key={tool.toolCallId} className={styles.streamTool}>
                          <span
                            className={`${styles.statusDot} ${TOOL_STATUS_DOT[status] ?? styles.statusRunning}`}
                          />
                          {getToolMeta(tool.toolName).title}
                        </li>
                      );
                    })}
                  </ul>
                )}
                {asks.map((ask) => {
                  const isPending = state.pendingAskUser?.toolCallId === ask.toolCallId;
                  return (
                    <AskUserCard
                      key={ask.toolCallId}
                      questions={ask.questions}
                      answers={ask.answers}
                      status={
                        isPending ? 'pending' : ask.status === 'pending' ? 'dismissed' : ask.status
                      }
                      onSubmit={
                        isPending ? (answers) => actions.answer(ask.toolCallId, answers) : undefined
                      }
                    />
                  );
                })}
              </article>
            );
          })}

          {(state.streamingReason || state.streamingText) && (
            <article className={styles.streamMsg}>
              <span className={styles.streamRole}>Agent</span>
              {state.streamingReason && (
                <p className={styles.streamReason}>{state.streamingReason}</p>
              )}
              {state.streamingText && <p className={styles.streamText}>{state.streamingText}</p>}
            </article>
          )}

          {state.pendingAskUser && (
            <div className={styles.askDock}>
              {!hasPendingAskMessage && (
                <AskUserCard
                  questions={state.pendingAskUser.questions}
                  onSubmit={(answers) => actions.answer(state.pendingAskUser!.toolCallId, answers)}
                />
              )}
              <div className={styles.replyShell}>
                <Input.TextArea
                  value={reply}
                  onChange={(e) => setReply(e.target.value)}
                  autoSize={{ minRows: 1, maxRows: 4 }}
                  variant="borderless"
                  className={styles.replyInput}
                  placeholder="选项都不合适？直接回复接管对话"
                />
                <div className={styles.replyToolbar}>
                  <span className={styles.replyToolbarGrow} />
                  <Tooltip title={reply.trim() ? '发送' : '请输入内容'}>
                    <Button
                      type="primary"
                      shape="circle"
                      icon={<ArrowUpOutlined />}
                      disabled={!reply.trim()}
                      className={styles.replySendBtn}
                      aria-label="发送消息"
                      onClick={() => void sendReply()}
                    />
                  </Tooltip>
                </div>
              </div>
            </div>
          )}
        </div>
      )}

      {state.error && <p className={styles.logError}>{state.error}</p>}
    </div>
  );
}

export default StreamPanel;
