/**
 * ConfigPanel - PipelineRunner 左侧配置面板
 * 选择 Pipeline + 参数配置 + 运行按钮 + 运行历史
 */
import { Button, Select, Input, InputNumber, Tag, Divider, Segmented } from 'antd';
import {
  ApartmentOutlined,
  PlayCircleOutlined,
  HistoryOutlined,
  MessageOutlined,
  SaveOutlined,
  BranchesOutlined,
  PlusOutlined,
} from '@ant-design/icons';
import { RunStatusTag, SessionHistoryRow, SessionTitleRenameButton, useSessionTitleRename, WorkflowParamSelect } from '@/components';
import type { Session, WorkflowDefinition, WorkflowRun } from '@/pages/AgentPlayground/types';
import styles from '../WorkflowRunner.module.less';

interface ConfigPanelProps {
  sessionId: string | null;
  sessionReady: boolean;
  sessions: Session[];
  isLoadingMoreSessions: boolean;
  historyListRef: React.RefObject<HTMLDivElement>;
  onHistoryScroll: () => void;
  onSwitchSession: (sid: string) => void;
  onSessionTitleUpdated?: (sessionId: string, title: string) => void;
  /** 历史会话过滤提示（如「ops-assistant」），非空表示已按 Pipeline 隔离 */
  sessionFilterLabel?: string | null;
  /** 清除历史会话过滤 */
  onClearSessionFilter?: () => void;
  availableWorkflows: WorkflowDefinition[];
  isLoadingWorkflows: boolean;
  selectedWorkflow: WorkflowDefinition | null;
  workflowParams: Record<string, string | number>;
  setWorkflowParams: React.Dispatch<React.SetStateAction<Record<string, string | number>>>;
  /** 参数输入模式：form = schema 表单；json = 整体原始 args（对齐 agent-k demoui） */
  paramMode: 'form' | 'json';
  setParamMode: (mode: 'form' | 'json') => void;
  /** JSON 模式下的 args 原文 */
  rawArgsJson: string;
  setRawArgsJson: (text: string) => void;
  onSelectWorkflow: (wfId: string) => void;
  onRunWorkflow: () => void;
  isRunning: boolean;
  hasRunningWorkflow: boolean;
  workflowRuns: WorkflowRun[];
  activeRunId: string | null;
  onViewRun: (runId: string) => void;
  /** 打开「存为草稿」弹窗 */
  onOpenSaveDraft: () => void;
  /** 打开「版本管理」抽屉 */
  onOpenVersionManager: () => void;
  /** 打开「新建 Pipeline」弹窗 */
  onOpenNewWorkflow: () => void;
}

function ConfigPanel({
  sessionId,
  sessionReady,
  sessions,
  isLoadingMoreSessions,
  historyListRef,
  onHistoryScroll,
  onSwitchSession,
  onSessionTitleUpdated,
  sessionFilterLabel,
  onClearSessionFilter,
  availableWorkflows,
  isLoadingWorkflows,
  selectedWorkflow,
  workflowParams,
  setWorkflowParams,
  paramMode,
  setParamMode,
  rawArgsJson,
  setRawArgsJson,
  onSelectWorkflow,
  onRunWorkflow,
  isRunning,
  hasRunningWorkflow,
  workflowRuns,
  activeRunId,
  onViewRun,
  onOpenSaveDraft,
  onOpenVersionManager,
  onOpenNewWorkflow,
}: ConfigPanelProps) {
  const { openRename, renameModal } = useSessionTitleRename(onSessionTitleUpdated);

  return (
    <aside className={styles.panel}>
      {/* 标题区 */}
      <div className={styles.panelHeader}>
        <h1 className={styles.panelTitle}>Pipeline 运行</h1>
      </div>

      {/* Pipeline 选择 */}
      <div className={styles.section}>
        <div className={styles.sectionTitle}>
          <ApartmentOutlined className={styles.sectionIcon} />
          Pipeline
        </div>
        <p className={styles.sectionDesc}>选择并配置 Pipeline，一键运行</p>

        <div className={styles.subSection}>
          <div className={styles.label}>选择 Pipeline</div>
          <Select
            value={selectedWorkflow?.id}
            onChange={onSelectWorkflow}
            options={availableWorkflows.map((wf) => ({
              label: `${wf.name} — ${wf.description}`,
              value: wf.id,
            }))}
            loading={isLoadingWorkflows}
            allowClear
            placeholder={sessionReady ? '选择 Pipeline' : '会话初始化中...'}
            disabled={!sessionReady}
            className={styles.select}
          />
          {selectedWorkflow?.description && (
            <div className={styles.wfDescription} title={selectedWorkflow.description}>
              {selectedWorkflow.description}
            </div>
          )}
        </div>

        {/* 参数配置：schema 表单 / 整体原始 JSON args 两种模式；无 schema 时只用 JSON */}
        {selectedWorkflow && (
          <div className={styles.subSection}>
            <div className={styles.labelRow}>
              <div className={styles.label}>参数配置</div>
              {selectedWorkflow.params.length > 0 && (
                <Segmented
                  size="small"
                  value={paramMode}
                  onChange={(v) => setParamMode(v as 'form' | 'json')}
                  options={[
                    { label: '表单', value: 'form' },
                    { label: 'JSON', value: 'json' },
                  ]}
                />
              )}
            </div>
            {paramMode === 'json' ? (
              <Input.TextArea
                value={rawArgsJson}
                onChange={(e) => setRawArgsJson(e.target.value)}
                placeholder='{"key": "value"}'
                autoSize={{ minRows: 3, maxRows: 10 }}
                className={styles.wfParamInput}
              />
            ) : (
              <div className={styles.wfParams}>
                {selectedWorkflow.params.map((p) => (
                  <div key={p.name} className={styles.wfParamItem}>
                    <label className={styles.wfParamLabel}>
                      {p.label}
                      {p.required && <span className={styles.wfParamRequired}>*</span>}
                    </label>
                    {p.type === 'array' || p.type === 'object' ? (
                      // 数组/对象参数：JSON 文本输入，提交时解析为对应类型
                      <Input.TextArea
                        value={workflowParams[p.name] as string}
                        onChange={(e) =>
                          setWorkflowParams((prev) => ({ ...prev, [p.name]: e.target.value }))
                        }
                        placeholder={
                          p.type === 'array'
                            ? `${p.placeholder || p.label}，JSON 数组，如 ["https://...", "https://..."]`
                            : `${p.placeholder || p.label}，JSON 对象`
                        }
                        autoSize={{ minRows: 2, maxRows: 6 }}
                        className={styles.wfParamInput}
                      />
                    ) : p.type === 'number' ? (
                      <InputNumber
                        value={workflowParams[p.name] as number}
                        onChange={(v) =>
                          setWorkflowParams((prev) => ({ ...prev, [p.name]: v ?? 0 }))
                        }
                        min={p.min}
                        max={p.max}
                        placeholder={p.placeholder}
                        className={styles.wfParamInput}
                      />
                    ) : p.type === 'select' && (p.optionsFrom || p.options) ? (
                      <WorkflowParamSelect
                        param={p}
                        value={workflowParams[p.name] as string}
                        onChange={(v) => setWorkflowParams((prev) => ({ ...prev, [p.name]: v }))}
                        className={styles.select}
                      />
                    ) : (
                      <Input
                        value={workflowParams[p.name] as string}
                        onChange={(e) =>
                          setWorkflowParams((prev) => ({ ...prev, [p.name]: e.target.value }))
                        }
                        placeholder={p.placeholder}
                        className={styles.wfParamInput}
                      />
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>
        )}

        <Button
          type="primary"
          block
          icon={<PlayCircleOutlined />}
          onClick={onRunWorkflow}
          disabled={!selectedWorkflow || isRunning || hasRunningWorkflow}
          loading={isRunning}
          className={styles.runBtn}
        >
          运行 Pipeline
        </Button>

        {/* 持久化入口：存为草稿 / 版本管理 / 新建 Pipeline */}
        <div className={styles.persistBtns}>
          <Button size="small" icon={<SaveOutlined />} onClick={onOpenSaveDraft}>
            存为草稿
          </Button>
          <Button size="small" icon={<BranchesOutlined />} onClick={onOpenVersionManager}>
            版本管理
          </Button>
        </div>
        <Button
          block
          icon={<PlusOutlined />}
          onClick={onOpenNewWorkflow}
          className={styles.newWorkflowBtn}
        >
          新建 Pipeline
        </Button>
      </div>

      <Divider className={styles.divider} />

      {/* 运行历史 */}
      <div className={styles.section}>
        <div className={styles.sectionTitle}>
          <HistoryOutlined className={styles.sectionIcon} />
          运行历史
        </div>
        <div className={styles.historyList}>
          {workflowRuns.length === 0 && <div className={styles.historyEmpty}>暂无运行记录</div>}
          {[...workflowRuns].reverse().map((run) => (
            <div
              key={run.runId}
              className={`${styles.historyItem} ${
                run.runId === activeRunId ? styles.historyItemActive : ''
              }`}
              title={run.runId}
              onClick={() => onViewRun(run.runId)}
              role="button"
              tabIndex={0}
              onKeyDown={(e) => {
                if (e.key === 'Enter') onViewRun(run.runId);
              }}
            >
              <div className={styles.historyHead}>
                <span className={styles.historyName}>{run.workflowName}</span>
                <Tag
                  color={
                    run.status === 'completed'
                      ? 'success'
                      : run.status === 'failed'
                        ? 'error'
                        : 'processing'
                  }
                  className={styles.historyTag}
                >
                  {run.status === 'running'
                    ? '运行中'
                    : run.status === 'completed'
                      ? '完成'
                      : '失败'}
                </Tag>
              </div>
              <div className={styles.historyRunId}>ID: {run.runId.slice(-8)}</div>
            </div>
          ))}
        </div>
      </div>

      <Divider className={styles.divider} />

      {/* 历史会话：切换到其他会话，查看其下的 Pipeline 与运行记录 */}
      <div className={styles.section}>
        <div className={styles.sectionTitle}>
          <MessageOutlined className={styles.sectionIcon} />
          历史会话
          {sessionFilterLabel && (
            <Tag closable onClose={onClearSessionFilter} color="blue">
              仅看「{sessionFilterLabel}」
            </Tag>
          )}
        </div>
        <div ref={historyListRef} className={styles.sessionList} onScroll={onHistoryScroll}>
          {sessions.length === 0 && <div className={styles.historyEmpty}>暂无历史会话</div>}
          {sessions.map((s) => (
            <SessionHistoryRow
              key={s.id}
              className={`${styles.historyItem} ${s.id === sessionId ? styles.historyItemActive : ''}`}
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
                <div className={styles.historyName}>{s.title}</div>
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

export default ConfigPanel;
