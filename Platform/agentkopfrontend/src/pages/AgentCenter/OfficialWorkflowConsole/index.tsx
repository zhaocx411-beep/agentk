/**
 * OfficialWorkflowConsole - 工作流生成记录控制台
 * 表格看任务状态；弹窗预览视频/图片；再次生成同 session 开填参弹窗
 * 「立即使用」先带占位 session 进页，再在弹窗提交时创建 quick session
 */
import { useEffect, useMemo, useState } from 'react';
import { Button, Empty, Spin, Table, Tag } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ArrowLeftOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import { useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { OFFICIAL_DEFAULTS_QUERY_KEY, getOfficialDefaults } from '../api';
import {
  buildOfficialConsolePath,
  isOfficialPendingSessionId,
  resolveOfficialCard,
  toOfficialLoopCard,
  toOfficialWorkflowCard,
} from '../utils';
import type { OfficialCatalogCardData } from '@/types/officialCatalog';
import type { ArtifactItem } from '../OfficialExperience/studio/lib/types';
import WorkflowLaunchModal from '../WorkflowLaunchModal';
import ArtifactPreviewModal from './ArtifactPreviewModal';
import { useConsoleRuns, type ConsoleRunRow } from './useConsoleRuns';
import styles from './OfficialWorkflowConsole.module.less';

const STATUS_META: Record<string, { color: string; text: string }> = {
  running: { color: 'processing', text: '运行中' },
  completed: { color: 'success', text: '已完成' },
  failed: { color: 'error', text: '失败' },
};

function summarizeInputs(inputs?: Record<string, unknown>): string {
  if (!inputs || typeof inputs !== 'object') return '—';
  const args = (inputs.args ?? inputs) as Record<string, unknown>;
  try {
    const text = JSON.stringify(args);
    return text.length > 80 ? `${text.slice(0, 80)}…` : text;
  } catch {
    return '—';
  }
}

function formatTime(value?: string): string {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function OfficialWorkflowConsole() {
  const { sessionId = '' } = useParams<{ sessionId: string }>();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const pending = isOfficialPendingSessionId(sessionId);
  const [launchOpen, setLaunchOpen] = useState(pending);
  const [preview, setPreview] = useState<{ title: string; artifacts: ArtifactItem[] } | null>(
    null,
  );

  const workflowId = Number(searchParams.get('workflowId') || '') || undefined;
  const workflowKey = searchParams.get('workflowKey') || undefined;
  const cardKey = searchParams.get('cardKey') || undefined;

  const { data, isLoading: catalogLoading } = useQuery({
    queryKey: OFFICIAL_DEFAULTS_QUERY_KEY,
    queryFn: getOfficialDefaults,
  });

  const item = useMemo(() => {
    const loops = (data?.agentLoops ?? []).map(toOfficialLoopCard);
    const workflows = (data?.workflowAgents ?? []).map(toOfficialWorkflowCard);
    const resolved = resolveOfficialCard(
      'workflow',
      undefined,
      workflowId,
      workflowKey,
      loops,
      workflows,
    );
    if (resolved) return resolved;
    if (cardKey) {
      const byKey = workflows.find((card) => card.key === cardKey);
      if (byKey) return byKey;
    }
    // 目录未命中时仍允许再次生成（走内置 / JSON 回退）
    if (workflowKey) {
      return {
        key: cardKey || `workflow:${workflowKey}`,
        name: workflowKey,
        description: '',
        subtitle: '工作流',
        skills: [],
        variant: 'workflow' as const,
        workflowId,
        workflowKey,
      };
    }
    return null;
  }, [data, workflowId, workflowKey, cardKey]);

  // 立即使用进页：目录就绪后自动打开填参弹窗（创建会话在提交时发生）
  useEffect(() => {
    if (pending && item) setLaunchOpen(true);
  }, [pending, item]);

  const { runs, loading, reload } = useConsoleRuns(sessionId);

  const columns: ColumnsType<ConsoleRunRow> = [
    {
      title: '任务摘要',
      key: 'summary',
      ellipsis: true,
      render: (_, row) => (
        <span className={styles.summary} title={summarizeInputs(row.inputs)}>
          {row.name || workflowKey || 'pipeline'} · {summarizeInputs(row.inputs)}
        </span>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 110,
      render: (status: string) => {
        const meta = STATUS_META[status] ?? { color: 'default', text: status || '未知' };
        return <Tag color={meta.color}>{meta.text}</Tag>;
      },
    },
    {
      title: '时间',
      key: 'time',
      width: 180,
      render: (_, row) => formatTime(row.finishedAt || row.createdAt),
    },
    {
      title: '操作',
      key: 'actions',
      width: 120,
      render: (_, row) => (
        <Button
          type="link"
          size="small"
          disabled={row.artifacts.length === 0}
          onClick={() =>
            setPreview({
              title: `${row.name || '产物'} · ${STATUS_META[row.status]?.text || row.status}`,
              artifacts: row.artifacts,
            })
          }
        >
          查看产物
        </Button>
      ),
    },
  ];

  if (!sessionId) {
    return (
      <div className={styles.page}>
        <Empty description="缺少会话 ID">
          <Button type="link" onClick={() => navigate('/agents/official')}>
            返回可灵Agent中后台
          </Button>
        </Empty>
      </div>
    );
  }

  const handleSubmitted = (sid: string, submitted: OfficialCatalogCardData) => {
    setLaunchOpen(false);
    if (pending || sid !== sessionId) {
      navigate(buildOfficialConsolePath(sid, submitted), { replace: true });
      return;
    }
    void reload();
  };

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div className={styles.headerMain}>
          <button type="button" className={styles.back} onClick={() => navigate('/agents/official')}>
            <ArrowLeftOutlined />
            返回可灵Agent中后台
          </button>
          <h1 className={styles.title}>
            {catalogLoading && !item ? <Spin size="small" /> : item?.name || workflowKey || '工作流控制台'}
          </h1>
          <p className={styles.subtitle}>
            {pending ? '准备开始生成' : `生成记录 · Session ${sessionId}`}
          </p>
        </div>
        <div className={styles.actions}>
          <Button
            icon={<ReloadOutlined />}
            onClick={() => void reload()}
            loading={loading}
            disabled={pending}
          >
            刷新
          </Button>
          <Button
            type="primary"
            icon={<PlusOutlined />}
            disabled={!item && !workflowKey}
            onClick={() => setLaunchOpen(true)}
          >
            {pending ? '开始生成' : '再次生成'}
          </Button>
        </div>
      </header>

      <section className={styles.panel}>
        <div className={styles.panelHead}>
          <h2 className={styles.panelTitle}>生成记录</h2>
        </div>
        <Table<ConsoleRunRow>
          rowKey="id"
          size="middle"
          loading={loading}
          columns={columns}
          dataSource={runs}
          pagination={false}
          locale={{
            emptyText: (
              <Empty
                description={
                  pending ? '请先填写参数开始生成' : '暂无生成记录，点击「再次生成」提交任务'
                }
              />
            ),
          }}
        />
      </section>

      <ArtifactPreviewModal
        open={!!preview}
        artifacts={preview?.artifacts ?? []}
        title={preview?.title}
        onClose={() => setPreview(null)}
      />

      <WorkflowLaunchModal
        open={launchOpen}
        item={item}
        sessionId={pending ? undefined : sessionId}
        onClose={() => setLaunchOpen(false)}
        onSubmitted={handleSubmitted}
      />
    </div>
  );
}

export default OfficialWorkflowConsole;
