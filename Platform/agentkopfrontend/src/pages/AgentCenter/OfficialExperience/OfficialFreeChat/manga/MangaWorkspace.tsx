import { useState, type ReactNode } from 'react';
import type { ChatAttachmentBarHandle } from '@/pages/AgentPlayground/components/chat/ChatAttachmentBar';
import type { OfficialCatalogCardData } from '@/types/officialCatalog';
import type { useOfficialExperienceChat } from '../../useOfficialExperienceChat';
import OfficialLoopWorkbench from '../workbench/OfficialLoopWorkbench';
import { MangaCreationCanvas } from './MangaCreationCanvas';
import styles from '../OfficialFreeChat.module.less';

type ChatModel = ReturnType<typeof useOfficialExperienceChat>;
type WorkspaceTab = 'canvas' | 'output';

interface MangaWorkspaceProps {
  item: OfficialCatalogCardData | null;
  chat: ChatModel;
  composerPromptStrip?: ReactNode;
  composerToolbarExtra?: ReactNode;
  emptyDescription: string;
  sessionId: string;
  onAttachmentBarReady?: (handle: ChatAttachmentBarHandle | null) => void;
}

export function MangaWorkspace({
  item: _item,
  chat,
  composerPromptStrip,
  composerToolbarExtra,
  emptyDescription,
  sessionId,
  onAttachmentBarReady,
}: MangaWorkspaceProps) {
  const [tab, setTab] = useState<WorkspaceTab>('output');
  const waitingAsk = Boolean(chat.sseState.pendingAskUser);
  const alertOutput = waitingAsk && tab !== 'output';

  return (
    <div className={styles.workspace}>
      <div className={styles.modeTabs} role="tablist" aria-label="漫剧视图">
        <button
          type="button"
          className={`${styles.modeTab} ${tab === 'output' ? styles.modeTabActive : ''} ${
            alertOutput ? styles.modeTabAlert : ''
          }`}
          aria-label={alertOutput ? 'Agent 输出，待确认' : 'Agent 输出'}
          onClick={() => setTab('output')}
        >
          Agent 输出
          {alertOutput ? <span className={styles.modeTabBadge}>待确认</span> : null}
        </button>
        <button
          type="button"
          className={`${styles.modeTab} ${tab === 'canvas' ? styles.modeTabActive : ''}`}
          onClick={() => setTab('canvas')}
        >
          创作画布
        </button>
      </div>
      {alertOutput ? (
        <button type="button" className={styles.askBanner} onClick={() => setTab('output')}>
          Agent 正在等你确认，点击查看问卷
        </button>
      ) : null}
      <div className={tab === 'output' ? styles.workspacePane : styles.workspacePaneHidden}>
        <OfficialLoopWorkbench
          chat={chat}
          sessionId={sessionId}
          emptyDescription={emptyDescription}
          composerPromptStrip={composerPromptStrip}
          composerToolbarExtra={composerToolbarExtra}
          onAttachmentBarReady={onAttachmentBarReady}
        />
      </div>
      {tab === 'canvas' ? (
        <div className={styles.workspacePane}>
          <MangaCreationCanvas sessionId={sessionId} sseState={chat.sseState} />
        </div>
      ) : null}
    </div>
  );
}
