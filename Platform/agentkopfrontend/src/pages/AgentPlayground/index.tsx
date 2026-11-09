/**
 * Agent Playground - 内容区页面
 * 侧边栏与整体布局复用 MainLayout，不在页面内重复实现
 * Pipeline 运行已拆分为独立页面 @/pages/WorkflowRunner
 */
import { useState, useRef, useCallback, useEffect, useMemo } from 'react';
import { useSearchParams } from 'react-router-dom';

import { useSSE } from './hooks/useSSE';
import { useCatalog } from './hooks/useCatalog';
import { useSessions } from './hooks/useSessions';
import { getSessions } from './api';
import Sidebar from './components/sidebar/Sidebar';
import ChatArea from './components/chat/ChatArea';
import RunInspector from './components/inspector/RunInspector';
import OverrideModal, { type OverrideApplyResult } from './components/modals/OverrideModal';
import SaveAgentModal from './components/modals/SaveAgentModal';
import NewAgentModal from './components/modals/NewAgentModal';
import VersionManagerDrawer from './components/version/VersionManagerDrawer';
import SessionConfigModal, {
  type AgentConfigMeta,
  type SessionConfigChoice,
  type SessionConfigSource,
} from './components/modals/SessionConfigModal';
import {
  buildProfileYaml,
  parseProfileYaml,
  sessionProfileToYaml,
  type ProfileYamlData,
} from '@/utils/profileYaml';
import { toToolNames } from '@/utils/profile';
import type { AgentLoop, AgentLoopVersion } from '@/types/agentLoop';
import type { BaseProfile, MessageAttachment, Session } from './types';
import { parseJsonStringArray, parseSpecTools } from './utils';
import { structuredAttachmentTypesForModel } from '@/utils/models';
import styles from './AgentPlayground.module.less';

function AgentPlayground() {
  const [searchParams, setSearchParams] = useSearchParams();
  /** 详情页深链打开的会话，消费一次后清空，避免 StrictMode / 刷新重复切换 */
  const deepLinkSessionConsumedRef = useRef(false);

  // ===== 能力目录 + 模型目录 =====
  const {
    catalog,
    models,
    selectedModel,
    setSelectedModel,
    selectedToolModels,
    setSelectedToolModels,
    systemPrompt,
    setSystemPrompt,
    selectedTools,
    setSelectedTools,
    overrideYaml,
    setOverrideYaml,
    spacesToTabs,
  } = useCatalog();

  // ===== SSE =====
  const structuredAttachmentTypes = useMemo(
    () => structuredAttachmentTypesForModel(models, selectedModel),
    [models, selectedModel],
  );
  const [sseState, sseActions] = useSSE({ structuredAttachmentTypes });

  // ===== 会话配置来源（自由配置 / 已有 Agent+版本） =====
  const [configSource, setConfigSource] = useState<SessionConfigSource>({ type: 'free' });
  /** 首次发送前暂存的消息：配置确认建会话后再发出 */
  const pendingMessageRef = useRef<{ text: string; attachments?: MessageAttachment[] } | null>(
    null,
  );

  // ===== Sessions =====
  const {
    sessionId,
    setSessionId,
    sessions,
    isLoadingMoreSessions,
    historyListRef,
    isLoadingMoreItems,
    handleHistoryScroll,
    handleNewSession,
    handleSwitchSession,
    updateSessionTitle,
    handleChatScroll,
  } = useSessions({
    selectedModel,
    selectedTools,
    selectedToolModels,
    catalog,
    systemPrompt,
    overrideYaml,
    sseActions,
    isChatBusy: sseState.isRunning,
  });

  // ===== 本地 UI 状态 =====
  const [inputText, setInputText] = useState('');
  const chatRef = useRef<HTMLDivElement>(null);
  const [rightPanelWidth, setRightPanelWidth] = useState(320);
  const [isDragging, setIsDragging] = useState(false);
  const dragStartXRef = useRef(0);
  const dragStartWidthRef = useRef(280);

  // ===== Modal 状态 =====
  const [overrideModalOpen, setOverrideModalOpen] = useState(false);
  const [saveAgentOpen, setSaveAgentOpen] = useState(false);
  const [newAgentOpen, setNewAgentOpen] = useState(false);
  const [versionManagerOpen, setVersionManagerOpen] = useState(false);
  const [sessionConfigOpen, setSessionConfigOpen] = useState(false);

  // ===== 发送消息 =====
  const handleSend = useCallback(
    async (attachments?: MessageAttachment[]): Promise<boolean> => {
      const text = inputText.trim();
      const files = attachments?.filter((a) => a.url.trim()) ?? [];
      if (!text && files.length === 0) return false;
      // 生成中不再拦截：sseActions.send 会先打断上一轮再开新的
      // 首次对话：先弹「会话配置」确认窗；此时不清附件，等真正发送成功再清
      if (!sessionId) {
        pendingMessageRef.current = {
          text: inputText,
          attachments: files.length > 0 ? files : undefined,
        };
        setInputText('');
        setSessionConfigOpen(true);
        return false;
      }
      const ok = await sseActions.send(sessionId, text, files.length > 0 ? files : undefined);
      return ok;
    },
    [inputText, sessionId, sseActions],
  );

  /** 当前调试台生效的 Profile 文档：优先用已编辑的 override，否则现场构建 */
  const getCurrentYaml = useCallback(() => {
    if (overrideYaml.trim()) return overrideYaml;
    const built = buildProfileYaml(
      { systemPrompt, tools: selectedTools, defaultModel: selectedModel },
      catalog,
    );
    return spacesToTabs(built);
  }, [overrideYaml, systemPrompt, selectedTools, selectedModel, catalog, spacesToTabs]);

  // ===== Override 弹窗 =====
  const handleOpenOverride = useCallback(() => {
    // 若已有 overrideYaml（来自 Pipeline 导入或之前编辑），则直接复用；
    // 否则基于 selectedTools + systemPrompt 现场构建一份
    if (!overrideYaml.trim()) {
      setOverrideYaml(getCurrentYaml());
    }
    setOverrideModalOpen(true);
  }, [overrideYaml, getCurrentYaml, setOverrideYaml]);

  /** 应用 override：只同步勾选项与配置来源，不创建会话——
   *  建会话统一由「会话配置」弹窗的「开始使用」触发（handleNewSession 会带上 overrideYaml） */
  const handleApplyOverride = useCallback(
    (result: OverrideApplyResult): void => {
      // undefined 表示文档未写 tools 字段（保持现状）；空数组表示显式清空勾选
      if (result.tools !== undefined) {
        setSelectedTools(result.tools);
      }
      if (result.systemPrompt !== undefined) {
        setSystemPrompt(result.systemPrompt);
      }
      // override 属于自由配置
      setConfigSource({ type: 'free' });
    },
    [setSelectedTools, setSystemPrompt],
  );

  /** 把某个版本的 Profile 应用为当前调试配置（不建会话）：
   *  1. 写入 overrideYaml（定制配置弹窗内容与新会话请求都用它）
   *  2. 解析文档同步左侧选项：工具勾选 / systemPrompt
   *  3. 有 meta 时把配置来源标记为该 Agent 版本 */
  const applyVersionConfig = useCallback(
    (profileValue: BaseProfile | string, meta?: AgentConfigMeta) => {
      const yaml =
        typeof profileValue === 'string' ? profileValue : sessionProfileToYaml(profileValue);
      setOverrideYaml(spacesToTabs(yaml));
      const parsed = parseProfileYaml(yaml);
      if (parsed.tools !== undefined) {
        setSelectedTools(parsed.tools);
      }
      if (parsed.systemPrompt !== undefined) {
        setSystemPrompt(parsed.systemPrompt);
      }
      if (parsed.defaultModel) {
        setSelectedModel(parsed.defaultModel);
      }
      if (meta) {
        setConfigSource({ type: 'agent', ...meta });
      }
    },
    [setOverrideYaml, spacesToTabs, setSelectedTools, setSystemPrompt, setSelectedModel],
  );

  /** 版本管理「加载调试」：只应用版本配置，不再自动创建会话。
   *  - 有历史会话：优先展示，自动切换到最近一条（历史接口只传 page/pageSize/sessionKind，
   *    前端按 agentVersionId 匹配该版本的会话）
   *  - 无历史会话：清空当前会话上下文并弹出会话配置窗（回显该 Agent 版本），
   *    由用户手动「开始使用」创建；关闭弹窗后直接输入会再次弹窗（handleSend 首次发送逻辑），
   *    让用户明确感知自己在开启新对话 */
  const handleLoadVersionYaml = useCallback(
    async (version: AgentLoopVersion, meta?: AgentConfigMeta): Promise<'switched' | 'modal'> => {
      applyVersionConfig(version.profileYaml, meta);
      // 清空输入框，避免残留内容误入切换后的会话
      setInputText('');
      if (meta) {
        try {
          const res = await getSessions({ page: 1, pageSize: 20, sessionKind: 'agent_loop' });
          // 后端按 updatedAt 降序返回，取第一条属于该版本的会话
          const latest = res.sessions.find((s) => s.agentVersionId === version.id);
          if (latest) {
            // 与当前会话相同时 handleSwitchSession 为空操作，保持现状即可
            await handleSwitchSession(latest.id);
            return 'switched';
          }
        } catch {
          // 历史查询失败按无历史处理，走手动确认流程
        }
      }
      // 无历史：清掉旧会话上下文，避免新配置生效后消息误入旧会话
      setSessionId(null);
      sseActions.reset();
      setSessionConfigOpen(true);
      return 'modal';
    },
    [applyVersionConfig, handleSwitchSession, setSessionId, sseActions],
  );

  /** 历史会话配置回显：会话记录携带创建时的配置快照，切换时反向写回配置项。
   *  - profileYaml（JSON object / 旧 YAML 字符串）→ overrideYaml，并解析同步勾选
   *  - 工具优先级：effectiveToolsJson > specJson.tools > profile 快照 tools
   *  快照只带 agentVersionId，无法还原 Agent 名称等元信息，统一标记为自由配置 */
  const applySessionSnapshot = useCallback(
    (sess: Session) => {
      let parsed: ProfileYamlData = {};
      const snap = sess.profileYaml;
      if (typeof snap === 'string' && snap.trim()) {
        setOverrideYaml(spacesToTabs(snap));
        parsed = parseProfileYaml(snap);
      } else if (snap && typeof snap === 'object') {
        const yaml = sessionProfileToYaml(snap as BaseProfile);
        setOverrideYaml(spacesToTabs(yaml));
        parsed = parseProfileYaml(yaml);
      }

      const tools =
        parseJsonStringArray(sess.effectiveToolsJson) ??
        parseSpecTools(sess.specJson) ??
        parsed.tools;
      if (tools) setSelectedTools(tools);

      if (parsed.systemPrompt !== undefined) {
        setSystemPrompt(parsed.systemPrompt);
      }
      const model = sess.effectiveModel || parsed.defaultModel;
      if (model) setSelectedModel(model);
      setConfigSource({ type: 'free' });
    },
    [setOverrideYaml, spacesToTabs, setSelectedTools, setSystemPrompt, setSelectedModel],
  );

  // ===== 会话配置弹窗 =====
  /** 切换历史会话：回显该会话的配置快照，并清空输入框避免残留内容误入 */
  const handleSwitchSessionWithReset = useCallback(
    (sid: string) => {
      setInputText('');
      const target = sessions.find((s) => s.id === sid);
      if (target) applySessionSnapshot(target);
      void handleSwitchSession(sid);
    },
    [sessions, applySessionSnapshot, handleSwitchSession],
  );

  /** Agent 详情页「历史会话」深链：?sessionId=xxx → 打开对应会话并回显快照 */
  useEffect(() => {
    if (deepLinkSessionConsumedRef.current) return;
    const sid = searchParams.get('sessionId');
    if (!sid) return;
    deepLinkSessionConsumedRef.current = true;

    const next = new URLSearchParams(searchParams);
    next.delete('sessionId');
    setSearchParams(next, { replace: true });

    void (async () => {
      setInputText('');
      let target = sessions.find((s) => s.id === sid);
      if (!target) {
        try {
          const res = await getSessions({ page: 1, pageSize: 50, sessionKind: 'agent_loop' });
          target = res.sessions.find((s) => s.id === sid);
        } catch {
          // 查不到快照仍可打开会话账本
        }
      }
      if (target) applySessionSnapshot(target);
      await handleSwitchSession(sid);
    })();
  }, [searchParams, setSearchParams, sessions, applySessionSnapshot, handleSwitchSession]);

  /** 确认配置：按选择创建新会话；有暂存的首条消息则创建后立即发出 */
  const handleConfirmSessionConfig = useCallback(
    async (choice: SessionConfigChoice): Promise<boolean> => {
      let sid: string | null = null;
      if (choice.type === 'agent') {
        applyVersionConfig(choice.version.profileYaml, {
          agentId: choice.agentId,
          agentName: choice.agentName,
          versionId: choice.version.id,
          version: choice.version.version,
          status: choice.version.status,
        });
        // 草稿不可 pin：直接传 profile；已发布版本才 pin agentLoopVersionId
        sid =
          choice.version.status === 'draft'
            ? await handleNewSession({
                profile:
                  choice.version.profileYaml && typeof choice.version.profileYaml === 'object'
                    ? choice.version.profileYaml
                    : undefined,
                tools: toToolNames(choice.version.profileYaml?.tools),
              })
            : await handleNewSession({ agentLoopVersionId: choice.version.id });
      } else {
        setConfigSource({ type: 'free' });
        sid = await handleNewSession();
      }
      if (sid && pendingMessageRef.current) {
        const pending = pendingMessageRef.current;
        pendingMessageRef.current = null;
        const ok = await sseActions.send(sid, pending.text, pending.attachments);
        if (!ok) {
          setInputText(pending.text);
        }
        return ok;
      }
      return sid !== null;
    },
    [applyVersionConfig, handleNewSession, sseActions],
  );

  /** 取消配置弹窗：若是首次发送触发的，把暂存的消息还原回输入框；附件仍留在输入区 */
  const handleCloseSessionConfig = useCallback(() => {
    setSessionConfigOpen(false);
    if (pendingMessageRef.current) {
      setInputText(pendingMessageRef.current.text);
      pendingMessageRef.current = null;
    }
  }, []);

  /** 新建 Agent 成功：把首个草稿版本设为当前会话配置（不立即建会话，等用户发起） */
  const handleAgentCreated = useCallback(
    (loop: AgentLoop, version: AgentLoopVersion) => {
      applyVersionConfig(version.profileYaml, {
        agentId: loop.id,
        agentName: loop.name,
        versionId: version.id,
        version: version.version,
        status: version.status,
      });
    },
    [applyVersionConfig],
  );

  // 配置来源展示文案（sidebar 配置卡片）
  const freeSummary = `${selectedModel || '默认模型'} · ${selectedTools.length} 个工具`;

  // ===== 拖动面板 =====
  const handleDragStart = useCallback(
    (e: React.MouseEvent) => {
      setIsDragging(true);
      dragStartXRef.current = e.clientX;
      dragStartWidthRef.current = rightPanelWidth;
      e.preventDefault();
    },
    [rightPanelWidth],
  );

  useEffect(() => {
    if (!isDragging) return;
    const handleMouseMove = (e: MouseEvent) => {
      const delta = dragStartXRef.current - e.clientX;
      setRightPanelWidth(Math.max(280, Math.min(520, dragStartWidthRef.current + delta)));
    };
    const handleMouseUp = () => setIsDragging(false);
    document.addEventListener('mousemove', handleMouseMove);
    document.addEventListener('mouseup', handleMouseUp);
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
    return () => {
      document.removeEventListener('mousemove', handleMouseMove);
      document.removeEventListener('mouseup', handleMouseUp);
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
    };
  }, [isDragging]);

  return (
    <div className={styles.page}>
      <div className={styles.mainGrid}>
        <Sidebar
          configSource={configSource}
          freeSummary={freeSummary}
          selectedTools={selectedTools}
          sessionId={sessionId}
          sessions={sessions}
          isLoadingMoreSessions={isLoadingMoreSessions}
          historyListRef={historyListRef}
          onHistoryScroll={handleHistoryScroll}
          onNewSession={() => setSessionConfigOpen(true)}
          onSwitchSession={handleSwitchSessionWithReset}
          onSessionTitleUpdated={updateSessionTitle}
          onChangeConfig={() => setSessionConfigOpen(true)}
          onOpenNewAgent={() => setNewAgentOpen(true)}
          onOpenSaveAgent={() => setSaveAgentOpen(true)}
          onOpenVersionManager={() => setVersionManagerOpen(true)}
        />

        <ChatArea
          sseState={sseState}
          sseActions={sseActions}
          inputText={inputText}
          setInputText={setInputText}
          onSend={(attachments) => handleSend(attachments)}
          chatRef={chatRef}
          onChatScroll={() => handleChatScroll(chatRef)}
          isLoadingMoreItems={isLoadingMoreItems}
          stickBottomToken={sessionId}
          enableAttachments
        />

        {/* 拖动条 */}
        <div
          className={`${styles.dragHandle} ${isDragging ? styles.dragging : ''}`}
          onMouseDown={handleDragStart}
          title="拖动调整宽度"
        />

        <RunInspector sseState={sseState} width={rightPanelWidth} />
      </div>

      <OverrideModal
        open={overrideModalOpen}
        onClose={() => setOverrideModalOpen(false)}
        overrideYaml={overrideYaml}
        setOverrideYaml={setOverrideYaml}
        onApply={handleApplyOverride}
      />

      {/* 持久化：存为草稿 / 新建 Agent / 版本管理 */}
      <SaveAgentModal
        open={saveAgentOpen}
        defaultYaml={getCurrentYaml()}
        onClose={() => setSaveAgentOpen(false)}
      />
      <NewAgentModal
        open={newAgentOpen}
        defaultYaml={getCurrentYaml()}
        onClose={() => setNewAgentOpen(false)}
        onCreated={handleAgentCreated}
      />
      <VersionManagerDrawer
        open={versionManagerOpen}
        onClose={() => setVersionManagerOpen(false)}
        onLoadYaml={handleLoadVersionYaml}
      />

      {/* 会话配置：首次发送 / 开始新会话 / 点配置 chip 时弹出 */}
      <SessionConfigModal
        open={sessionConfigOpen}
        configSource={configSource}
        onClose={handleCloseSessionConfig}
        onConfirm={handleConfirmSessionConfig}
        catalog={catalog}
        models={models}
        selectedModel={selectedModel}
        setSelectedModel={setSelectedModel}
        selectedTools={selectedTools}
        setSelectedTools={setSelectedTools}
        selectedToolModels={selectedToolModels}
        setSelectedToolModels={setSelectedToolModels}
        onOpenOverride={handleOpenOverride}
      />
    </div>
  );
}

export default AgentPlayground;
