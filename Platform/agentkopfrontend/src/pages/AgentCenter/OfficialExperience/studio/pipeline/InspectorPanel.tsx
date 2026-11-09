/**
 * 执行详情：意图 / 提交入参 / 选中步骤的输入输出 / 运行日志
 */
import { useEffect, useState } from 'react';
import { Badge, Empty, Tabs } from 'antd';
import type { LedgerEvent } from '@/pages/AgentPlayground/types';
import type { SSEActions, SSEState } from '@/pages/AgentPlayground/hooks/useSSE';
import StreamPanel from './StreamPanel';
import type { PipelineNode } from '../lib/types';
import type { CreationStudioModel } from '../hooks/useCreationStudio';
import styles from '../CreationStudio.module.less';

interface InspectorPanelProps {
  model: CreationStudioModel;
  activeNode: PipelineNode | null;
  sessionId: string;
  sseState: SSEState;
  sseActions: SSEActions;
  ledger: LedgerEvent[];
  error: string | null;
}

function json(value: unknown): string {
  if (value == null) return '暂无数据';
  if (typeof value === 'string') return value;
  return JSON.stringify(value, null, 2);
}

function InspectorPanel({
  model,
  activeNode,
  sessionId,
  sseState,
  sseActions,
  ledger,
  error,
}: InspectorPanelProps) {
  const { spec, selectedRecord } = model;
  const isAgentLoop = spec.mode === 'agent-loop';
  const [activeTab, setActiveTab] = useState(isAgentLoop ? 'stream' : 'intent');
  const waitingAnswer = !!sseState.pendingAskUser;

  // 模型发起提问后不回答会话就停住，强制把用户带到能回答的地方
  useEffect(() => {
    if (waitingAnswer) setActiveTab('stream');
  }, [waitingAnswer]);

  // 选中历史记录时看当时留档的那份，没选中就跟着左栏表单实时变
  const intentText = selectedRecord?.intentPreview ?? model.intentPreview;
  const payloadText = selectedRecord?.payloadPreview ?? model.payloadPreview;

  return (
    <section className={styles.inspectorPane}>
      <div className={styles.columnHead}>
        <span className={styles.columnEyebrow}>INSPECTOR</span>
        <h2 className={styles.columnTitle}>执行详情</h2>
      </div>
      <Tabs
        size="small"
        className={styles.inspectorTabs}
        activeKey={activeTab}
        onChange={setActiveTab}
        items={[
          ...(isAgentLoop
            ? [
                {
                  key: 'stream',
                  label: (
                    <Badge dot offset={[4, 0]} count={waitingAnswer ? 1 : 0}>
                      实时输出
                    </Badge>
                  ),
                  children: (
                    <StreamPanel
                      sessionId={sessionId}
                      state={sseState}
                      actions={sseActions}
                      runId={selectedRecord?.runId}
                    />
                  ),
                },
              ]
            : []),
          {
            key: 'intent',
            label: '意图',
            children: <pre className={styles.pre}>{intentText}</pre>,
          },
          {
            key: 'payload',
            label: spec.mode === 'pipeline' ? '入参' : '对话文本',
            children: (
              <>
                {!selectedRecord && model.payloadError && (
                  <p className={styles.logError}>入参未就绪：{model.payloadError}</p>
                )}
                <pre className={styles.pre}>{payloadText}</pre>
              </>
            ),
          },
          {
            key: 'step',
            label: '步骤',
            children: activeNode ? (
              <div className={styles.stepDetail}>
                <h3 className={styles.stepTitle}>
                  {activeNode.index} {activeNode.title}
                </h3>
                <p className={styles.stepLabel}>输入</p>
                <pre className={styles.pre}>{json(activeNode.input)}</pre>
                <p className={styles.stepLabel}>输出</p>
                <pre className={styles.pre}>{json(activeNode.output)}</pre>
              </div>
            ) : (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description="运行后展示当前步骤的输入输出"
              />
            ),
          },
          {
            key: 'logs',
            label: '日志',
            children: (
              <div className={styles.logs}>
                {error && <p className={styles.logError}>{error}</p>}
                {ledger.length === 0 && !error ? (
                  <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无运行日志" />
                ) : (
                  ledger.map((event) => (
                    <p key={event.seq} className={styles.logLine}>
                      <span className={styles.logKind}>{event.kind}</span>
                      {event.summary}
                    </p>
                  ))
                )}
              </div>
            ),
          },
        ]}
      />
    </section>
  );
}

export default InspectorPanel;
