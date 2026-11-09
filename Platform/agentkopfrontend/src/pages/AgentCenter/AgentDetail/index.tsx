/**
 * AgentDetail - Agent 配置详情页
 * 左侧配置面板 + 右侧嵌入式对话（Playground ChatArea）
 */
import { Empty, Spin } from 'antd';
import { useNavigate, useParams } from 'react-router-dom';
import { WorkbenchLayout, WorkbenchPane } from '@/components';
import { AgentConfigHeader, AgentIdentityHeader } from './components/DetailHeader';
import { CapabilityConfigContent, RolePromptContent } from './components/ConfigPanel';
import PreviewPanel from './components/PreviewPanel';
import { SaveOfficialAgentNameModal } from './components/SaveOfficialAgentNameModal';
import { useAgentDetailModel } from './hooks/useAgentDetailModel';
import { usePreviewChat } from './hooks/usePreviewChat';
import styles from './AgentDetail.module.less';

function AgentDetail() {
  const { agentId = '' } = useParams();
  const navigate = useNavigate();
  const model = useAgentDetailModel(agentId);
  // 用路由参数稳定 agentId，避免详情加载前传 undefined → 加载后再传数字，
  // 导致历史会话 query 的 resetKey 从 'none' 切到真实 id、重复打两次 sessions/query
  const numericAgentId = Number(agentId);
  const previewAgentId =
    model.isBuiltin || model.isPendingUnsaved || !Number.isFinite(numericAgentId)
      ? undefined
      : numericAgentId;

  const previewChat = usePreviewChat({
    agentId: previewAgentId,
    versionId: model.detail?.versionId,
    isDraftVersion: model.detail?.status === 'draft',
    isBuiltin: model.isBuiltin,
    system: model.system,
    defaultModel: model.defaultModel,
    modelCatalog: model.modelCatalog,
    tools: model.tools,
    officialTools: model.officialTools,
    catalog: model.catalog,
    toolDefaults: model.toolDefaults,
    rawProfile: model.detail?.rawProfile ?? {},
    dirty: model.dirty,
    onAutoSaveDraft: () => model.persistDraft(true),
  });

  if (model.loading && !model.detail) {
    return (
      <div className={styles.loading}>
        <Spin size="large" />
      </div>
    );
  }
  if (!model.detail) {
    return (
      <div className={styles.loading}>
        <Empty
          description={
            model.isFromOfficial ? '官方模版配置已失效，请返回重新「基于此创建」' : '未找到该 Agent'
          }
        >
          <a onClick={() => navigate(model.isFromOfficial ? '/agents/official' : '/agents/mine')}>
            返回列表
          </a>
        </Empty>
      </div>
    );
  }

  const detail = model.detail;

  return (
    <WorkbenchLayout variant="configuration" className={styles.page}>
      <WorkbenchPane
        ariaLabel="角色指令"
        header={
          <AgentIdentityHeader
            detail={detail}
            autoSavedAt={model.autoSavedAt}
            onBack={(goBack) => model.confirmLeave(goBack)}
          />
        }
      >
        <RolePromptContent system={model.system} onSystemChange={model.setSystem} />
      </WorkbenchPane>
      <WorkbenchPane
        ariaLabel="Agent 配置"
        header={
          <AgentConfigHeader
            detail={detail}
            versions={model.versions}
            selectedVersionId={model.selectedVersion?.id}
            dirty={model.dirty}
            canSaveDraft={model.canSaveDraft}
            saving={model.saveMutation.isPending || model.creatingFromOfficial}
            publishing={model.publishMutation.isPending}
            publishDisabled={model.isPendingUnsaved}
            onVersionChange={model.handleVersionChange}
            onSaveDraft={() => model.saveMutation.mutate()}
            onPublish={model.handlePublish}
          />
        }
      >
        <CapabilityConfigContent
          detail={detail}
          defaultModel={model.defaultModel}
          chatModels={model.chatModels}
          modelsLoading={model.modelsLoading}
          tools={model.tools}
          toolDefaults={model.toolDefaults}
          officialTools={model.officialTools}
          onDefaultModelChange={model.setDefaultModel}
          onToolsChange={model.setToolsConfig}
        />
      </WorkbenchPane>
      <PreviewPanel
        detail={detail}
        versionMap={model.versionMap}
        sseState={previewChat.sseState}
        sseActions={previewChat.sseActions}
        inputText={previewChat.inputText}
        setInputText={previewChat.setInputText}
        onSend={(attachments) => previewChat.handleSend(attachments)}
        sendPending={previewChat.isSending}
        chatRef={previewChat.chatRef}
        isLoadingHistory={previewChat.isLoadingHistory}
        onChatScroll={() => previewChat.handleChatScroll(previewChat.chatRef)}
        sessionId={previewChat.sessionId}
        sessions={previewChat.sessions}
        sessionsLoading={previewChat.sessionsLoading}
        isLoadingMoreSessions={previewChat.isLoadingMoreSessions}
        historyListRef={previewChat.historyListRef}
        onHistoryScroll={previewChat.handleHistoryScroll}
        onSwitchSession={previewChat.switchSession}
        onSessionTitleUpdated={previewChat.updateSessionTitle}
        isFromOfficial={model.isFromOfficial}
        officialAgentLoopId={model.officialAgentLoopId}
      />

      <SaveOfficialAgentNameModal
        open={model.nameModalOpen}
        confirming={model.creatingFromOfficial}
        defaultName={model.pendingSuggestedName}
        defaultDescription={model.pendingDescription}
        onCancel={() => model.setNameModalOpen(false)}
        onConfirm={model.confirmCreateFromOfficial}
      />
    </WorkbenchLayout>
  );
}

export default AgentDetail;
