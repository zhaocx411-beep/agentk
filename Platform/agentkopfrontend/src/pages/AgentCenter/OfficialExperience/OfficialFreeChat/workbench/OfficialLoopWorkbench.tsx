/**
 * Agent-loop 工作台：全高输入区（流式输出在右侧产出栏）
 */
import { useMemo, type ReactNode } from 'react';
import ChatArea from '@/pages/AgentPlayground/components/chat/ChatArea';
import type { ChatAttachmentBarHandle } from '@/pages/AgentPlayground/components/chat/ChatAttachmentBar';
import type { useOfficialExperienceChat } from '../../useOfficialExperienceChat';
import { TtsComposerControls } from '../tts/TtsComposerControls';
import { VirtualTryOnComposerControls } from '../virtualTryOn/VirtualTryOnComposerControls';
import { DOCUMENT_UPLOAD_ACCEPT } from '@/services/media';
import styles from '../OfficialFreeChat.module.less';

type ChatModel = ReturnType<typeof useOfficialExperienceChat>;

interface OfficialLoopWorkbenchProps {
  chat: ChatModel;
  sessionId: string;
  emptyDescription: string;
  composerPromptStrip?: ReactNode;
  composerToolbarExtra?: ReactNode;
  onAttachmentBarReady?: (handle: ChatAttachmentBarHandle | null) => void;
}

function OfficialLoopWorkbench({
  chat,
  sessionId,
  emptyDescription,
  composerPromptStrip,
  composerToolbarExtra,
  onAttachmentBarReady,
}: OfficialLoopWorkbenchProps) {
  const { sseState, sseActions, needsTts, needsDoc, needsVirtualTryOn, ttsComposer, virtualTryOnComposer, isSending } = chat;

  const composerPreInputExtra = useMemo(() => {
    if (needsVirtualTryOn) {
      return (
        <VirtualTryOnComposerControls
          model={virtualTryOnComposer}
          disabled={sseState.isRunning || isSending}
        />
      );
    }
    if (needsTts) {
      return (
        <TtsComposerControls
          model={ttsComposer}
          disabled={sseState.isRunning || isSending}
        />
      );
    }
    return undefined;
  }, [
    isSending,
    needsTts,
    needsVirtualTryOn,
    sseState.isRunning,
    ttsComposer,
    virtualTryOnComposer,
  ]);

  return (
    <div className={styles.workColumn}>
      <ChatArea
        sseState={sseState}
        sseActions={sseActions}
        inputText={chat.inputText}
        setInputText={chat.setInputText}
        onSend={(attachments) => chat.handleSend(attachments)}
        chatRef={chat.chatRef}
        onChatScroll={() => chat.handleChatScroll(chat.chatRef)}
        isLoadingMoreItems={chat.isLoadingHistory}
        stickBottomToken={sessionId}
        composerPromptStrip={composerPromptStrip}
        composerToolbarExtra={composerToolbarExtra}
        composerPreInputExtra={composerPreInputExtra}
        enableAttachments={!needsVirtualTryOn}
        variant="quiet"
        surface="composer"
        fillHeight
        inputPlaceholder={
          needsVirtualTryOn ? '描述试穿场景，如模特姿态、画幅比例等' : emptyDescription
        }
        onAttachmentBarReady={onAttachmentBarReady}
        sendPending={isSending}
        uploadAccept={needsDoc ? DOCUMENT_UPLOAD_ACCEPT : undefined}
        uploadAllowedTypes={needsDoc ? (['doc'] as const) : undefined}
        uploadAddLabel={needsDoc ? '添加文档' : undefined}
      />
    </div>
  );
}

export default OfficialLoopWorkbench;
