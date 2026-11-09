/**
 * 左栏 Sidebar：能力配置 + 会话配置（配置来源卡片）+ 历史会话
 * Pipeline 配置面板已拆分为独立页面 @/pages/WorkflowRunner
 */
import { Button, Tag, Divider, Tooltip } from 'antd';
import {
  SettingOutlined,
  PlayCircleOutlined,
  HistoryOutlined,
  MessageOutlined,
  ReloadOutlined,
  SaveOutlined,
  BranchesOutlined,
  PlusOutlined,
  SwapOutlined,
} from '@ant-design/icons';
import { AgentMarkIcon, RunStatusTag, SessionHistoryRow, SessionTitleRenameButton, useSessionTitleRename } from '@/components';
import type { Session } from '../../types';
import type { SessionConfigSource } from '../modals/SessionConfigModal';
import styles from '../../AgentPlayground.module.less';

interface SidebarProps {
  /** 当前生效的会话配置来源 */
  configSource: SessionConfigSource;
  /** 自由配置摘要文案（如「5 个工具」） */
  freeSummary: string;
  /** 当前生效的工具列表 */
  selectedTools: string[];
  sessionId: string | null;
  sessions: Session[];
  isLoadingMoreSessions: boolean;
  historyListRef: React.RefObject<HTMLDivElement>;
  onHistoryScroll: () => void;
  onNewSession: () => void;
  onSwitchSession: (sid: string) => void;
  onSessionTitleUpdated?: (sessionId: string, title: string) => void;
  /** 打开「会话配置」弹窗更换配置 */
  onChangeConfig: () => void;
  /** 打开「新建 Agent」弹窗 */
  onOpenNewAgent: () => void;
  /** 打开「存为草稿」弹窗 */
  onOpenSaveAgent: () => void;
  /** 打开「版本管理」抽屉 */
  onOpenVersionManager: () => void;
}

function Sidebar({
  configSource,
  freeSummary,
  selectedTools,
  sessionId,
  sessions,
  isLoadingMoreSessions,
  historyListRef,
  onHistoryScroll,
  onNewSession,
  onSwitchSession,
  onSessionTitleUpdated,
  onChangeConfig,
  onOpenNewAgent,
  onOpenSaveAgent,
  onOpenVersionManager,
}: SidebarProps) {
  const { openRename, renameModal } = useSessionTitleRename(onSessionTitleUpdated);

  return (
    <aside className={styles.sidebar}>
      {/* 标题区 */}
      <div className={styles.sidebarHeader}>
        <h1 className={styles.sidebarTitle}>agent-k playground</h1>
      </div>

      {/* 当前能力配置：展示当前生效的模型与工具，响应式跟随配置更换 */}
      <div className={styles.sidebarSection}>
        <div className={styles.sidebarSectionTitle}>
          <SettingOutlined className={styles.sidebarIcon} />
          当前能力配置
          <Tag color="blue" className={styles.profileTag}>
            {configSource.type === 'free'
              ? 'builtin'
              : `${configSource.agentName} v${configSource.version}`}
          </Tag>
        </div>
        <p className={styles.sidebarDesc}>当前生效的工具</p>

        <div className={styles.sidebarSubSection}>
          <div className={styles.sidebarLabel}>可用工具</div>
          <div className={styles.toolTags}>
            {selectedTools.length === 0 && <span className={styles.toolEmpty}>未启用工具</span>}
            {selectedTools.map((t) => (
              <Tooltip key={t} title={t} placement="right">
                <Tag className={styles.toolTag}>
                  <span className={styles.toolTagName}>{t}</span>
                </Tag>
              </Tooltip>
            ))}
          </div>
        </div>
      </div>

      <Divider className={styles.sidebarDivider} />

      {/* 会话配置：展示当前生效的配置来源，可更换 / 新建 Agent */}
      <div className={styles.sidebarSection}>
        <div className={styles.sidebarSectionTitle}>
          <PlayCircleOutlined className={styles.sidebarIcon} />
          会话配置
        </div>
        <p className={styles.sidebarDesc}>下一段对话生效的配置</p>

        <div className={styles.configSourceCard}>
          {configSource.type === 'free' ? (
            <>
              <div className={styles.configSourceName}>
                <SettingOutlined /> 自由配置
              </div>
              <div className={styles.configSourceMeta}>{freeSummary}</div>
            </>
          ) : (
            <>
              <div className={styles.configSourceName}>
                <AgentMarkIcon size={14} /> {configSource.agentName}
              </div>
              <div className={styles.configSourceMeta}>
                v{configSource.version} ·{' '}
                {configSource.status === 'published' ? (
                  <Tag color="success">已发布</Tag>
                ) : (
                  <Tag>草稿</Tag>
                )}
              </div>
            </>
          )}
        </div>

        <div className={styles.persistBtns}>
          <Button size="small" icon={<SwapOutlined />} onClick={onChangeConfig}>
            更换配置
          </Button>
          <Button size="small" icon={<SaveOutlined />} onClick={onOpenSaveAgent}>
            存为草稿
          </Button>
          <Button size="small" icon={<BranchesOutlined />} onClick={onOpenVersionManager}>
            版本管理
          </Button>
        </div>

        <Button
          block
          icon={<PlusOutlined />}
          onClick={onOpenNewAgent}
          className={styles.newAgentBtn}
        >
          新建 Agent
        </Button>

        <Button
          type="primary"
          block
          icon={<ReloadOutlined />}
          onClick={onNewSession}
          className={styles.newSessionBtn}
        >
          开始新会话
        </Button>
      </div>

      <Divider className={styles.sidebarDivider} />

      {/* 当前会话信息 */}
      {sessionId && (
        <div className={styles.sidebarSection}>
          <div className={styles.sidebarSectionTitle}>
            <MessageOutlined className={styles.sidebarIcon} />
            当前会话
          </div>
          <div className={styles.sessionInfo}>
            <span className={styles.sessionId}>{sessionId}</span>
          </div>
        </div>
      )}

      {/* 历史会话 */}
      <div className={styles.sidebarSection}>
        <div className={styles.sidebarSectionTitle}>
          <HistoryOutlined className={styles.sidebarIcon} />
          历史会话
        </div>
        <div ref={historyListRef} className={styles.historyList} onScroll={onHistoryScroll}>
          {sessions.length === 0 && <div className={styles.historyEmpty}>暂无历史会话</div>}
          {sessions.map((s) => (
            <SessionHistoryRow
              key={s.id}
              className={`${styles.historyItem} ${s.id === sessionId ? styles.historyActive : ''}`}
            >
              <div
                className={styles.historyItemBody}
                title={s.title || s.id}
                onClick={() => onSwitchSession(s.id)}
                role="button"
                tabIndex={0}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') onSwitchSession(s.id);
                }}
              >
                <div className={styles.historyTitle}>{s.title}</div>
                <RunStatusTag status={s.activeRunStatus} />
              </div>
              {onSessionTitleUpdated ? (
                <SessionTitleRenameButton session={s} onRename={openRename} />
              ) : null}
            </SessionHistoryRow>
          ))}
          {isLoadingMoreSessions && <div className={styles.historyLoading}>加载中...</div>}
        </div>
      </div>
      {renameModal}
    </aside>
  );
}

export default Sidebar;
