// 左栏 AI 面板。已接通：发送消息（POST /messages）+ 会话事件消费。
// 工具调用卡片 / ask_user 表单 / @引用素材是后续独立任务。
//
// 事件消费路径（对齐 cut-apiserver 的 agent-k 桥接层 platform/api/agentk.go）：
//   挂载 → listEvents 分页拉历史（可靠通道，新到旧分页，倒序回放）
//   POST message → 保证有一条为它服务的流（204 = Execution 未激活，短暂重试；
//     旧 Execution 的流还开着则在其关闭后重连）
//   → 消费 delta 预览与持久事件（按 eventId 去重）
//   → stream.end / 断开 / 重试耗尽 → listEvents 最终校准（翻页直到遇到已见事件）
//
// 降级：项目没有 sessionId（cut-apiserver 未配 AGENTK_BASE_URL）时，
// 后端对话接口一律 503，这里显示"未启用"提示而不是报错。
import {
  Check,
  Copy,
  Film,
  Info,
  Megaphone,
  Mic2,
  Package,
  Paperclip,
  Presentation,
  SendHorizontal,
  Sparkles,
  X,
} from "lucide-react";
import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { ApiError, api, unwrapAgentUserMessage } from "@/remote";
import type { AgentProfile, DeltaFrame, SessionEvent } from "@/remote";
import {
  beginAgentTurn,
  cancelAgentTurn,
  finishAgentTurn,
  onSessionEvent,
  resetAgentSync,
} from "@/store/agentSync";
import {
  snapshotEditorContext,
  useEditorContextStore,
  type EditorContextSnapshot,
} from "@/store/editorContextStore";
import { useProjectStore } from "@/store/projectStore";
import { useTimelineStore } from "@/store/timelineStore";

import { AskUserForm } from "./AskUserForm";
import type { PendingAsk } from "./AskUserForm";
import { ToolCallCard } from "./ToolCallCard";
import type { ToolRun } from "./ToolCallCard";

const MarkdownMessage = lazy(() => import("./MarkdownMessage"));

async function copyText(text: string): Promise<void> {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(text);
    return;
  }
  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  document.body.append(textarea);
  textarea.select();
  const copied = document.execCommand("copy");
  textarea.remove();
  if (!copied) throw new Error("clipboard copy was rejected");
}

interface ChatMessage {
  id: string;
  role: "user" | "assistant";
  text: string;
  /** 乐观/delta 预览中的消息（未定稿，收到持久事件后替换）。 */
  pending?: boolean;
}

type AgentRunPhase = "idle" | "waiting" | "thinking" | "working";

/** assistant.text / user.input 事件的 data 字段（agentk-docs 事件信封）。 */
interface TextEventData {
  text?: string;
}

interface ExecutionFinishedData {
  status?: string;
  error?: string;
}

interface ToolCallData {
  toolCallId?: string;
  toolName?: string;
  args?: unknown;
}

interface ToolResultData {
  toolCallId?: string;
  result?: unknown;
  error?: string;
}

/** /stream 连 204 时的重试参数（run 可能在消息提交后稍晚激活）。 */
const STREAM_RETRY_MAX = 6;
const STREAM_RETRY_DELAY_MS = 800;

const DEFAULT_PROFILE: AgentProfile = {
  profileId: "agentcut-default",
  name: "AgentCut",
  description: "通用视频剪辑 Agent",
  isDefault: true,
};

const SCENE_TEMPLATES: {
  id: string;
  title: string;
  description: string;
  prompt: string;
  icon: ReactNode;
}[] = [
  {
    id: "talking-head",
    title: "口播剪辑",
    description: "去停顿、加字幕、突出重点",
    prompt: "请把我上传的口播素材剪成节奏紧凑的短视频，删除明显停顿和重复表达，生成字幕并突出重点。",
    icon: <Mic2 className="size-4" />,
  },
  {
    id: "product",
    title: "产品宣传",
    description: "卖点梳理与宣传成片",
    prompt: "请根据上传的产品素材制作一条宣传视频，提炼核心卖点，安排清晰的开场、功能展示和行动引导。",
    icon: <Package className="size-4" />,
  },
  {
    id: "app",
    title: "App 介绍",
    description: "功能演示与使用流程",
    prompt: "请把这些录屏和素材剪成 App 功能介绍视频，按使用流程组织画面，并为关键操作添加简短说明。",
    icon: <Megaphone className="size-4" />,
  },
  {
    id: "ai-short",
    title: "AI 短片",
    description: "快速组织氛围感短片",
    prompt: "请用上传素材制作一条有氛围感的 AI 短片，挑选最有表现力的镜头，控制节奏并给出合适的字幕建议。",
    icon: <Sparkles className="size-4" />,
  },
  {
    id: "explainer",
    title: "讲解视频",
    description: "结构化信息与章节重点",
    prompt: "请把素材整理成结构清晰的讲解视频，按主题分段，保留关键信息，并为每一部分添加准确字幕。",
    icon: <Presentation className="size-4" />,
  },
  {
    id: "highlights",
    title: "精彩集锦",
    description: "提取高光镜头快速成片",
    prompt: "请从上传素材中挑选高光片段制作一条精彩集锦，减少冗余内容，让开头尽快进入亮点。",
    icon: <Film className="size-4" />,
  },
];

export function AgentPanel() {
  const project = useProjectStore((s) => s.project);
  const bootstrapError = useProjectStore((s) => s.bootstrapError);

  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [draft, setDraft] = useState("");
  const [sending, setSending] = useState(false);
  const [sendError, setSendError] = useState<string | null>(null);
  const [agentRunPhase, setAgentRunPhase] = useState<AgentRunPhase>("idle");
  const [chatUnavailable, setChatUnavailable] = useState(false);
  const [toolRuns, setToolRuns] = useState<ToolRun[]>([]);
  const [pendingAsk, setPendingAsk] = useState<PendingAsk | null>(null);
  const [answering, setAnswering] = useState(false);
  const [showMediaPicker, setShowMediaPicker] = useState(false);
  const [attachedMediaIds, setAttachedMediaIds] = useState<string[]>([]);
  const [profiles, setProfiles] = useState<AgentProfile[]>([DEFAULT_PROFILE]);
  const [selectedProfileId, setSelectedProfileId] = useState(DEFAULT_PROFILE.profileId);
  const [copiedMessageId, setCopiedMessageId] = useState<string | null>(null);
  const [prefilledEditorContext, setPrefilledEditorContext] =
    useState<EditorContextSnapshot | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const shouldFollowChatRef = useRef(true);
  const selectionInsideChatRef = useRef(false);
  const agentPromptRequest = useEditorContextStore(
    (state) => state.agentPromptRequest,
  );

  // 处理过的事件 ID 集合：持久事件按 eventId 去重（stream 回放 / 校准重叠）。
  // Session 事件量级有限（千级），Set 不做淘汰；项目切换时重建。
  const seenEventsRef = useRef(new Set<string>());
  // delta 预览按 roundId+generation 归组，新一代替换旧一代（模型 Activity 重试语义）。
  const deltaGenRef = useRef<{ roundId: string; gen: string } | null>(null);
  // 当前 stream 的取消函数与 204 重试定时器；组件卸载时都要清。
  const unsubscribeRef = useRef<(() => void) | null>(null);
  const retryTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const copyTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // 消息在上一个 Execution 的流还开着时发出：旧流只属于旧 Execution，
  // 收到 stream.end 关闭后必须为新 Execution 重连。
  const reconnectAfterCloseRef = useRef(false);
  // 最近一次 sendMessage 返回的 executionId：连流时精确指定，
  // 不依赖后端"取最新 Execution"的兜底。
  const latestExecutionRef = useRef<string | undefined>(undefined);
  const pendingAskRef = useRef<PendingAsk | null>(null);
  const disposedRef = useRef(false);

  const chatEnabled = !!project?.sessionId;
  const projectId = project?.projectId;
  // 注意：selector 本身不能在 localDoc 为空时返回新的 [] 字面量——
  // 每次渲染都是新引用，会让 useSyncExternalStore 判定“store 变了”而
  // 无限重渲染。这里只订阅 localDoc（稳定引用），?? [] 挪到渲染体里。
  const localDoc = useTimelineStore((s) => s.localDoc);
  const media = localDoc?.media ?? [];

  useEffect(() => {
    let cancelled = false;
    void api
      .listProfiles()
      .then((available) => {
        if (cancelled || available.length === 0) return;
        setProfiles(available);
        const preferred = available.find((profile) => profile.isDefault) ?? available[0];
        setSelectedProfileId((current) =>
          available.some((profile) => profile.profileId === current)
            ? current
            : preferred.profileId,
        );
      })
      .catch((err) => {
        // 多 Profile 接口尚未部署时保留唯一默认项，不阻断聊天闭环。
        if (!(err instanceof ApiError && [404, 501, 503].includes(err.status))) {
          console.warn("profile list unavailable", err);
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const handlePersisted = useCallback((ev: SessionEvent, expectedProjectId?: string) => {
    const currentProjectId = useProjectStore.getState().project?.projectId;
    if (expectedProjectId && currentProjectId !== expectedProjectId) return;
    if (seenEventsRef.current.has(ev.eventId)) return; // 回放/校准去重
    seenEventsRef.current.add(ev.eventId);
    // Agent 改动感知（设计 4.4）：tool 事件转发给 agentSync 判断是否刷新 Timeline。
    const pid = expectedProjectId ?? currentProjectId;
    if (pid) onSessionEvent(pid, ev);
    if (ev.kind === "execution.started") {
      setAgentRunPhase("waiting");
    } else if (ev.kind === "round.started") {
      setAgentRunPhase("thinking");
    } else if (ev.kind === "execution.finished") {
      const data = ev.data as ExecutionFinishedData;
      setAgentRunPhase("idle");
      if (data.status?.includes("FAILED")) {
        const detail = data.error?.trim();
        setSendError(
          detail
            ? `Agent 运行失败：${detail.length > 360 ? `${detail.slice(0, 360)}…` : detail}`
            : "Agent 运行失败，请联系 Agent-K 服务负责人查看 Execution 日志",
        );
      }
    } else if (ev.kind === "user.input") {
      const text = unwrapAgentUserMessage(
        (ev.data as TextEventData)?.text ?? "",
      );
      setMessages((prev) => {
        // 本地乐观气泡已展示过的归一为持久事件，不重复追加。
        if (prev.some((m) => m.role === "user" && m.pending && m.text === text)) {
          return prev.map((m) =>
            m.role === "user" && m.pending && m.text === text
              ? { ...m, id: `ev_${ev.eventId}`, pending: false }
              : m,
          );
        }
        return [...prev, { id: `ev_${ev.eventId}`, role: "user", text }];
      });
    } else if (ev.kind === "assistant.text") {
      const text = (ev.data as TextEventData)?.text ?? "";
      // 定稿：清掉 delta 预览，追加持久消息。
      deltaGenRef.current = null;
      setMessages((prev) => [
        ...prev.filter((m) => !(m.role === "assistant" && m.pending)),
        { id: `ev_${ev.eventId}`, role: "assistant", text },
      ]);
    } else if (ev.kind === "assistant.tool_call") {
      setAgentRunPhase("working");
      const data = ev.data as ToolCallData;
      if (data.toolCallId && data.toolName?.startsWith("contrib.agentcut.")) {
        setToolRuns((current) => [
          ...current.filter((run) => run.toolCallId !== data.toolCallId),
          {
            toolCallId: data.toolCallId!,
            toolName: data.toolName!,
            args: data.args,
            status: "requested",
          },
        ]);
      }
    } else if (
      ev.kind === "tool.exec_started" ||
      ev.kind === "tool.exec_succeeded" ||
      ev.kind === "tool.exec_failed"
    ) {
      if (ev.kind === "tool.exec_started") setAgentRunPhase("working");
      const data = ev.data as ToolResultData;
      if (data.toolCallId) {
        setToolRuns((current) =>
          current.map((run) =>
            run.toolCallId === data.toolCallId
              ? {
                  ...run,
                  status:
                    ev.kind === "tool.exec_started"
                      ? "running"
                      : ev.kind === "tool.exec_succeeded"
                        ? "succeeded"
                        : "failed",
                  result: data.result,
                  error: data.error,
                }
              : run,
          ),
        );
      }
    } else if (ev.kind === "ask_user.requested") {
      const data = ev.data as PendingAsk;
      if (data.toolCallId && Array.isArray(data.questions)) {
        setAgentRunPhase("idle");
        pendingAskRef.current = data;
        setPendingAsk(data);
      }
    } else if (ev.kind === "ask_user.responded") {
      const data = ev.data as { toolCallId?: string };
      setPendingAsk((current) => {
        const next = current?.toolCallId === data.toolCallId ? null : current;
        pendingAskRef.current = next;
        return next;
      });
    }
  }, []);

  const handleDelta = useCallback((frame: DeltaFrame) => {
    // protojson 的枚举名；REASONING 预览本阶段不渲染。
    if (frame.kind !== "DELTA_BLOCK_KIND_TEXT" || !frame.text) return;
    const roundId = frame.roundId ?? "";
    const gen = frame.generation ?? "";
    const cur = deltaGenRef.current;
    const sameGen = cur && cur.roundId === roundId && cur.gen === gen;
    deltaGenRef.current = { roundId, gen };
    setMessages((prev) => {
      const rest = prev.filter((m) => !(m.role === "assistant" && m.pending));
      const prevPreview = sameGen
        ? (prev.find((m) => m.role === "assistant" && m.pending)?.text ?? "")
        : ""; // 新 gen 替换旧预览，不做两代相加
      return [
        ...rest,
        {
          id: `delta_${roundId}`,
          role: "assistant",
          text: prevPreview + frame.text,
          pending: true,
        },
      ];
    });
  }, []);

  /**
   * 用可靠通道拉平事件。/events 是新到旧分页：从第一页开始收集"没见过"
   * 的事件，一旦整页都是已见事件（或翻到底）就停，再按时间正序回放。
   */
  const catchUp = useCallback(
    async (pid: string) => {
      try {
        const fresh: SessionEvent[] = [];
        let pageToken = "";
        for (;;) {
          if (
            disposedRef.current ||
            useProjectStore.getState().project?.projectId !== pid
          ) {
            return;
          }
          const page = await api.listEvents(pid, pageToken ? { pageToken } : {});
          if (
            disposedRef.current ||
            useProjectStore.getState().project?.projectId !== pid
          ) {
            return;
          }
          let sawKnown = false;
          for (const ev of page.events) {
            if (seenEventsRef.current.has(ev.eventId)) sawKnown = true;
            else fresh.push(ev);
          }
          if (sawKnown || !page.nextPageToken) break;
          pageToken = page.nextPageToken;
        }
        // fresh 是新到旧收集的，倒过来按发生顺序回放。
        for (const ev of fresh.reverse()) handlePersisted(ev, pid);
      } catch (err) {
        // 校准失败不打断 UI；下一次发送/挂载会再拉。503（未启用）静默。
        if (!(err instanceof ApiError && err.status === 503)) {
          console.warn("listEvents catch-up failed", err);
        }
      }
    },
    [handlePersisted],
  );

  /** 连接 /stream；204（run 未激活）按文档短暂重试，结束后校准。 */
  const connectStream = useCallback(
    (pid: string, attempt = 0) => {
      if (disposedRef.current) return;
      if (unsubscribeRef.current) return; // 已有活跃连接（调用方走 ensureStream）

      let unsubscribe: () => void;
      unsubscribe = api.streamEvents(
        pid,
        {
          onEvent: (event) => handlePersisted(event, pid),
          onDelta: (frame) => {
            if (useProjectStore.getState().project?.projectId === pid) {
              handleDelta(frame);
            }
          },
          onClose: (reason) => {
            if (unsubscribeRef.current !== unsubscribe) return;
            unsubscribeRef.current = null;
            if (
              disposedRef.current ||
              useProjectStore.getState().project?.projectId !== pid
            ) {
              return;
            }
            if (reason === "no_active_run" && attempt < STREAM_RETRY_MAX) {
              // Execution 可能在消息提交后稍晚激活：短暂重试。
              retryTimerRef.current = setTimeout(
                () => connectStream(pid, attempt + 1),
                STREAM_RETRY_DELAY_MS,
              );
              return;
            }
            // stream.end / 连接异常 / 重试耗尽：清掉未定稿预览，
            // 回退可靠通道做最终校准（204 重试耗尽说明 Execution 早已结束，
            // 事件可能只进了 Event Log 没进过这条流）。
            deltaGenRef.current = null;
            setMessages((prev) => prev.filter((m) => !(m.role === "assistant" && m.pending)));
            if (!pendingAskRef.current) void finishAgentTurn(pid);
            void catchUp(pid);
            // 这条流开着期间又发过新消息：旧流随旧 Execution 结束，必须
            // 带着全新的重试预算为新 Execution 重连。
            if (reconnectAfterCloseRef.current) {
              reconnectAfterCloseRef.current = false;
              connectStream(pid, 0);
            }
          },
        },
        // 精确指定要 watch 的 Execution；缺省时后端取最新的。
        { executionId: latestExecutionRef.current },
      );
      unsubscribeRef.current = unsubscribe;
    },
    [handlePersisted, handleDelta, catchUp],
  );

  /**
   * 发送新消息后保证有一条为它服务的流：
   * - 没有连接、也没有等待中的重试 → 直接新建连接；
   * - 正处在 204 重试等待期 → 取消旧定时器，立刻按全新预算重连；
   * - 已有活跃流（上一条消息的 run 还在播）→ 标记流关闭后重连。
   */
  const ensureStream = useCallback(
    (pid: string) => {
      if (disposedRef.current) return;
      if (unsubscribeRef.current) {
        reconnectAfterCloseRef.current = true;
        return;
      }
      if (retryTimerRef.current) {
        clearTimeout(retryTimerRef.current);
        retryTimerRef.current = null;
      }
      connectStream(pid, 0);
    },
    [connectStream],
  );

  // 挂载/项目切换：先拉一次历史事件（可靠通道），不主动连 stream——
  // stream 只在 sendMessage 之后按需连接。
  useEffect(() => {
    if (!projectId || !chatEnabled) return;
    disposedRef.current = false;
    seenEventsRef.current = new Set();
    shouldFollowChatRef.current = true;
    setMessages([]);
    setToolRuns([]);
    setPendingAsk(null);
    pendingAskRef.current = null;
    setAgentRunPhase("idle");
    setSendError(null);
    setAttachedMediaIds([]);
    setPrefilledEditorContext(null);
    resetAgentSync(projectId);
    void catchUp(projectId);
    return () => {
      disposedRef.current = true;
      reconnectAfterCloseRef.current = false;
      latestExecutionRef.current = undefined;
      unsubscribeRef.current?.();
      unsubscribeRef.current = null;
      if (retryTimerRef.current) {
        clearTimeout(retryTimerRef.current);
        retryTimerRef.current = null;
      }
      if (copyTimerRef.current) {
        clearTimeout(copyTimerRef.current);
        copyTimerRef.current = null;
      }
      resetAgentSync();
    };
  }, [projectId, chatEnabled, catchUp]);

  useEffect(() => {
    if (!agentPromptRequest) return;
    setDraft(agentPromptRequest.text);
    setPrefilledEditorContext(agentPromptRequest.editorContext);
  }, [agentPromptRequest]);

  useEffect(() => {
    const viewport = scrollRef.current;
    if (!viewport) return;
    const syncFollowState = () => {
      const distanceFromBottom =
        viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight;
      shouldFollowChatRef.current = distanceFromBottom <= 64;
    };
    syncFollowState();
    viewport.addEventListener("scroll", syncFollowState, { passive: true });
    return () => viewport.removeEventListener("scroll", syncFollowState);
  }, [projectId]);

  useEffect(() => {
    const syncSelection = () => {
      const viewport = scrollRef.current;
      const selection = document.getSelection();
      selectionInsideChatRef.current =
        !!viewport &&
        !!selection &&
        !selection.isCollapsed &&
        ((!!selection.anchorNode && viewport.contains(selection.anchorNode)) ||
          (!!selection.focusNode && viewport.contains(selection.focusNode)));
    };
    document.addEventListener("selectionchange", syncSelection);
    return () => document.removeEventListener("selectionchange", syncSelection);
  }, []);

  // 用户仍停留在底部或刚发送消息时跟随新内容；选中文字时绝不抢滚动，
  // 避免流式 delta / 工具状态刷新破坏正在进行的复制选择。
  useEffect(() => {
    const viewport = scrollRef.current;
    if (!viewport) return;
    const selection = document.getSelection();
    const selectionInsideChat =
      !!selection &&
      !selection.isCollapsed &&
      ((selection.anchorNode && viewport.contains(selection.anchorNode)) ||
        (selection.focusNode && viewport.contains(selection.focusNode)));
    if (selectionInsideChat || selectionInsideChatRef.current) return;
    if (!shouldFollowChatRef.current) return;
    viewport.scrollTo({ top: viewport.scrollHeight });
  }, [messages, toolRuns, pendingAsk, agentRunPhase]);

  const placeholder = useMemo(() => {
    if (bootstrapError) return "后端不可用";
    if (!project) return "项目加载中…";
    if (!chatEnabled) return "对话功能未启用";
    return "告诉我你想怎么剪…";
  }, [bootstrapError, project, chatEnabled]);

  async function handleSend(prefilledText?: string) {
    const text = (prefilledText ?? draft).trim();
    if (!text || !project || sending || agentRunPhase !== "idle" || pendingAsk) return;
    const editorContext =
      prefilledText === undefined && prefilledEditorContext
        ? prefilledEditorContext
        : snapshotEditorContext();
    setSendError(null);
    setSending(true);
    setAgentRunPhase("waiting");
    shouldFollowChatRef.current = true;
    beginAgentTurn(project.projectId);
    // 乐观展示自己的消息；后续 user.input 持久事件到达时归一。
    setMessages((prev) => [
      ...prev,
      { id: `local_${Date.now()}`, role: "user", text, pending: true },
    ]);
    setDraft("");
    setPrefilledEditorContext(null);
    try {
      const { executionId } = await api.sendMessage(project.projectId, {
        text,
        profileId: selectedProfileId,
        mediaIds: attachedMediaIds,
        editorContext,
      });
      setAttachedMediaIds([]);
      latestExecutionRef.current = executionId || undefined;
      // 契约路径：消息被 202 接受后先校准一次（拿到 user.input 事件），
      // 再保证有一条为本次消息服务的流（挂载时不连，避免挂空流）。
      void catchUp(project.projectId);
      ensureStream(project.projectId);
    } catch (err) {
      cancelAgentTurn(project.projectId);
      if (err instanceof ApiError && err.status === 503) {
        // AGENTK_BASE_URL 未配置：进入降级态而不是报错。
        setChatUnavailable(true);
      } else if (err instanceof ApiError) {
        setSendError(`发送失败：${err.body.error}`);
      } else {
        setSendError(err instanceof Error ? err.message : "发送失败，请重试");
      }
      setAgentRunPhase("idle");
    } finally {
      setSending(false);
    }
  }

  async function handleAnswer(answers: { questionId: string; selected: string[] }[]) {
    if (!project || !pendingAsk || answering) return;
    setAnswering(true);
    setSendError(null);
    try {
      const { executionId } = await api.answer(project.projectId, {
        toolCallId: pendingAsk.toolCallId,
        answers,
      });
      latestExecutionRef.current = executionId || undefined;
      setPendingAsk(null);
      pendingAskRef.current = null;
      setAgentRunPhase("waiting");
      ensureStream(project.projectId);
    } catch (err) {
      setSendError(
        err instanceof ApiError
          ? `提交回答失败：${err.body.error}`
          : err instanceof Error
            ? err.message
            : "提交回答失败",
      );
      setAgentRunPhase("idle");
    } finally {
      setAnswering(false);
    }
  }

  function attachMedia(mediaId: string) {
    if (!attachedMediaIds.includes(mediaId)) setAttachedMediaIds((ids) => [...ids, mediaId]);
    setDraft((value) => {
      const base = value.endsWith("@") ? value.slice(0, -1) : value;
      return `${base}${base && !base.endsWith(" ") ? " " : ""}[media:${mediaId}] `;
    });
    setShowMediaPicker(false);
  }

  async function copyAssistantMessage(message: ChatMessage) {
    try {
      await copyText(message.text);
      setCopiedMessageId(message.id);
      if (copyTimerRef.current) clearTimeout(copyTimerRef.current);
      copyTimerRef.current = setTimeout(() => {
        setCopiedMessageId((current) =>
          current === message.id ? null : current,
        );
        copyTimerRef.current = null;
      }, 1500);
    } catch {
      setSendError("复制失败，请选中文本后使用系统复制快捷键");
    }
  }

  const showUnavailableBanner = (project && !chatEnabled) || chatUnavailable;

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden bg-[#F2F2F0]">
      {showUnavailableBanner && (
        <div className="flex items-center gap-2 border-b bg-muted px-3 py-2 text-xs text-muted-foreground">
          <Info className="size-3.5 shrink-0" />
          对话功能未启用：当前环境未接入 Agent 服务（cut-apiserver 未配置
          AGENTK_BASE_URL），上传素材、手动剪辑不受影响。
        </div>
      )}
      {sendError && (
        <div className="border-b bg-destructive/10 px-3 py-2 text-xs text-destructive">
          {sendError}
        </div>
      )}
      <ScrollArea
        data-agent-chat
        className="min-h-0 flex-1"
        viewportRef={scrollRef}
      >
        <div
          className={`flex flex-col gap-3 p-4 ${
            messages.length === 0 && chatEnabled
              ? "min-h-full justify-center"
              : ""
          }`}
        >
          {messages.length === 0 && (
            chatEnabled ? (
              <div>
                <div className="mb-3 text-center">
                  <div className="text-base font-semibold text-foreground">今天想创作什么？</div>
                  <div className="mt-1 text-xs text-muted-foreground">
                    选择一个场景，Agent 会立即按预设目标开始处理
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-2">
                  {SCENE_TEMPLATES.map((scene) => (
                    <button
                      key={scene.id}
                      type="button"
                      disabled={sending || agentRunPhase !== "idle"}
                      className="rounded-lg border bg-card p-3 text-left transition-colors hover:border-primary/50 hover:bg-accent disabled:opacity-50"
                      onClick={() => void handleSend(scene.prompt)}
                    >
                      <span className="mb-2 inline-flex rounded-md bg-primary/10 p-1.5 text-primary">
                        {scene.icon}
                      </span>
                      <span className="block text-sm font-medium">{scene.title}</span>
                      <span className="mt-0.5 block text-xs text-muted-foreground">
                        {scene.description}
                      </span>
                    </button>
                  ))}
                </div>
              </div>
            ) : (
              <div className="mt-8 text-center text-sm text-muted-foreground">
                对话功能未启用，可先上传素材、手动编辑时间线。
              </div>
            )
          )}
          {messages.map((m) => (
            <div
              key={m.id}
              className={
                m.role === "user"
                  ? "ml-auto max-w-[80%] rounded-[6px] border border-[#DEE0DC] bg-[#ECEDEA] px-3 py-2 text-sm text-[#303236]"
                  : "group/message relative mr-auto max-w-[92%] cursor-text select-text rounded-lg bg-muted px-3 py-2 pr-9 text-sm" +
                    (m.pending ? " opacity-70" : "")
              }
            >
              {m.role === "assistant" ? (
                <Suspense fallback={<span className="whitespace-pre-wrap">{m.text}</span>}>
                  <MarkdownMessage text={m.text} />
                </Suspense>
              ) : (
                renderText(m.text, media)
              )}
              {m.role === "assistant" && (
                <button
                  type="button"
                  className="absolute right-1 top-1 flex size-7 cursor-pointer select-none items-center justify-center rounded-[4px] text-muted-foreground opacity-0 transition-opacity hover:bg-background/80 hover:text-foreground focus-visible:opacity-100 group-hover/message:opacity-100"
                  aria-label={
                    copiedMessageId === m.id ? "已复制" : "复制回复"
                  }
                  title={copiedMessageId === m.id ? "已复制" : "复制回复"}
                  onClick={() => void copyAssistantMessage(m)}
                >
                  {copiedMessageId === m.id ? (
                    <Check className="size-3.5" />
                  ) : (
                    <Copy className="size-3.5" />
                  )}
                </button>
              )}
            </div>
          ))}
          {toolRuns.map((run) => (
            <ToolCallCard key={run.toolCallId} run={run} />
          ))}
          {pendingAsk && (
            <AskUserForm
              key={pendingAsk.toolCallId}
              ask={pendingAsk}
              submitting={answering}
              onSubmit={(answers) => void handleAnswer(answers)}
            />
          )}
          {agentRunPhase !== "idle" && !pendingAsk && (
            <div
              className="mr-auto flex max-w-[80%] items-center gap-2 rounded-lg bg-muted px-3 py-2 text-sm text-muted-foreground"
              role="status"
              aria-live="polite"
            >
              <span>
                {agentRunPhase === "waiting"
                  ? "正在等待 Agent-K"
                  : agentRunPhase === "thinking"
                    ? "Agent 正在思考"
                    : "Agent 正在执行操作"}
              </span>
              <span className="flex items-center gap-1" aria-hidden="true">
                {[0, 1, 2].map((index) => (
                  <span
                    key={index}
                    className="size-1.5 animate-bounce rounded-full bg-muted-foreground/70"
                    style={{ animationDelay: `${index * 120}ms` }}
                  />
                ))}
              </span>
            </div>
          )}
        </div>
      </ScrollArea>
      <div className="relative border-t p-3">
        {showMediaPicker && (
          <div className="absolute bottom-full left-3 z-20 mb-1 max-h-52 w-72 overflow-auto rounded-md border bg-background p-1 shadow-lg">
            {media.length === 0 ? (
              <div className="p-2 text-xs text-muted-foreground">请先上传素材</div>
            ) : (
              media.map((item) => (
                <button
                  key={item.mediaId}
                  type="button"
                  className="block w-full truncate rounded px-2 py-1.5 text-left text-xs hover:bg-accent"
                  onClick={() => attachMedia(item.mediaId)}
                >
                  @{item.blobKey?.split("/").pop() ?? item.mediaId}
                  <span className="ml-2 text-muted-foreground">{item.kind}</span>
                </button>
              ))
            )}
          </div>
        )}
        {attachedMediaIds.length > 0 && (
          <div className="mb-2 flex flex-wrap gap-1">
            {attachedMediaIds.map((id) => (
              <span
                key={id}
                className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 text-xs"
              >
                @{media.find((item) => item.mediaId === id)?.blobKey?.split("/").pop() ?? id}
                <button
                  type="button"
                  onClick={() => {
                    setAttachedMediaIds((ids) => ids.filter((item) => item !== id));
                    setDraft((value) =>
                      value.replaceAll(`[media:${id}]`, "").replace(/\s+/g, " ").trimStart(),
                    );
                  }}
                >
                  <X className="size-3" />
                </button>
              </span>
            ))}
          </div>
        )}
        <div className="flex items-center gap-2">
        <select
          aria-label="选择 Agent"
          className="h-8 max-w-32 rounded-md border bg-background px-2 text-xs"
          value={selectedProfileId}
          disabled={!chatEnabled || sending || agentRunPhase !== "idle"}
          title={profiles.find((profile) => profile.profileId === selectedProfileId)?.description}
          onChange={(event) => setSelectedProfileId(event.target.value)}
        >
          {profiles.map((profile) => (
            <option key={profile.profileId} value={profile.profileId}>
              {profile.name}
            </option>
          ))}
        </select>
        <Button
          variant={showMediaPicker ? "secondary" : "ghost"}
          size="icon-sm"
          disabled={!chatEnabled}
          title="@引用已上传素材"
          onClick={() => setShowMediaPicker((open) => !open)}
        >
          <Paperclip data-slot="icon" />
        </Button>
        <Input
          placeholder={placeholder}
          value={draft}
          disabled={!chatEnabled || sending || agentRunPhase !== "idle"}
          onChange={(e) => {
            setDraft(e.target.value);
            if (e.target.value.endsWith("@")) setShowMediaPicker(true);
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.nativeEvent.isComposing) void handleSend();
          }}
        />
        <Button
          size="icon-sm"
          disabled={!chatEnabled || sending || agentRunPhase !== "idle" || !draft.trim()}
          title="发送"
          onClick={() => void handleSend()}
        >
          <SendHorizontal data-slot="icon" />
        </Button>
        </div>
      </div>
    </div>
  );
}

function renderText(
  text: string,
  media: { mediaId: string; blobKey?: string }[],
): ReactNode {
  return text.split(/(\[media:[^\]]+\])/g).map((part, index) => {
    const match = part.match(/^\[media:([^\]]+)]$/);
    if (!match) return part;
    const item = media.find((candidate) => candidate.mediaId === match[1]);
    return (
      <span key={`${part}-${index}`} className="mx-0.5 rounded bg-background/20 px-1 py-0.5">
        @{item?.blobKey?.split("/").pop() ?? match[1]}
      </span>
    );
  });
}
