/**
 * 版本管理抽屉
 * 在调试台内直接管理 Agent 的 profile 版本：查看 / 发布 / 加载到调试台继续调
 */
import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Drawer, Empty, Popconfirm, Select, Space, Table, Tag } from 'antd';
import { message } from '@/utils/feedback';
import type { ColumnsType } from 'antd/es/table';
import {
  AGENT_LOOPS_QUERY_KEY,
  agentLoopVersionsKey,
  getAgentLoops,
  getAgentLoopVersions,
  publishAgentLoopVersion,
} from '@/services/agentLoop';
import type { AgentLoopVersion } from '@/types/agentLoop';
import VersionDetailModal from './VersionDetailModal';
import type { AgentConfigMeta } from '../modals/SessionConfigModal';
import styles from '../../AgentPlayground.module.less';

interface VersionManagerDrawerProps {
  open: boolean;
  onClose: () => void;
  /** 把某个版本加载回调试台并创建新会话，返回新会话 id */
  /** 加载调试：应用版本配置后不自动建会话；返回 'switched'（已切到历史会话）或 'modal'（待用户确认新建） */
  onLoadYaml: (version: AgentLoopVersion, meta?: AgentConfigMeta) => Promise<'switched' | 'modal'>;
}

function VersionManagerDrawer({ open, onClose, onLoadYaml }: VersionManagerDrawerProps) {
  const queryClient = useQueryClient();
  const [agentId, setAgentId] = useState<number | null>(null);
  /** 详情弹窗当前查看的版本 */
  const [detailVersion, setDetailVersion] = useState<AgentLoopVersion | null>(null);

  const { data: loops = [] } = useQuery({
    queryKey: AGENT_LOOPS_QUERY_KEY,
    queryFn: getAgentLoops,
  });

  // 打开时默认选中第一个 Agent
  useEffect(() => {
    if (open && agentId === null && loops.length > 0) {
      setAgentId(loops[0].id);
    }
  }, [open, agentId, loops]);

  const selectedLoop = loops.find((l) => l.id === agentId) ?? null;

  const { data: versions = [], isLoading } = useQuery({
    queryKey: agentLoopVersionsKey(agentId ?? -1),
    queryFn: () => getAgentLoopVersions(agentId as number),
    enabled: open && agentId !== null,
  });

  const publishMutation = useMutation({
    mutationFn: publishAgentLoopVersion,
    onSuccess: (version) => {
      message.success(`v${version.version} 已发布`);
      queryClient.invalidateQueries({ queryKey: AGENT_LOOPS_QUERY_KEY });
      if (agentId !== null) {
        queryClient.invalidateQueries({ queryKey: agentLoopVersionsKey(agentId) });
      }
    },
  });

  const [loadingVersionId, setLoadingVersionId] = useState<number | null>(null);

  const handleLoad = async (version: AgentLoopVersion) => {
    setLoadingVersionId(version.id);
    try {
      const result = await onLoadYaml(version, {
        agentId: selectedLoop?.id ?? -1,
        agentName: selectedLoop?.name ?? '',
        versionId: version.id,
        version: version.version,
        status: version.status,
      });
      if (result === 'switched') {
        message.success(`已加载 v${version.version} 配置，已切换到最近的历史会话`);
      } else {
        message.success(`已加载 v${version.version} 配置，请在会话配置弹窗中确认后开始新对话`);
      }
      onClose();
    } finally {
      setLoadingVersionId(null);
    }
  };

  const columns: ColumnsType<AgentLoopVersion> = [
    { title: '版本', dataIndex: 'version', width: 64, render: (v: number) => `v${v}` },
    {
      title: '状态',
      dataIndex: 'status',
      width: 80,
      render: (s: string) =>
        s === 'published' ? <Tag color="success">已发布</Tag> : <Tag>草稿</Tag>,
    },
    {
      title: '来源',
      dataIndex: 'sourceLabel',
      ellipsis: true,
      render: (v: string) => v || '-',
    },
    { title: '创建时间', dataIndex: 'createdAt', width: 150 },
    {
      title: '操作',
      width: 240,
      render: (_, r) => (
        <Space size={0}>
          <Button type="link" size="small" onClick={() => setDetailVersion(r)}>
            详情
          </Button>
          {/* 草稿态不展示加载调试：草稿配置可能未补全，先发布再调试 */}
          {r.status !== 'draft' && (
            <Button
              type="link"
              size="small"
              loading={loadingVersionId === r.id}
              onClick={() => handleLoad(r)}
            >
              加载调试
            </Button>
          )}
          {r.status === 'draft' && (
            <Popconfirm
              title={`发布版本 v${r.version}？`}
              description="发布后将成为线上生效版本"
              onConfirm={() => publishMutation.mutate(r.id)}
            >
              <Button type="link" size="small" loading={publishMutation.isPending}>
                发布
              </Button>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ];

  return (
    <Drawer title="版本管理" open={open} onClose={onClose} width={760}>
      <div className={styles.versionManagerHeader}>
        <Select
          value={agentId}
          onChange={setAgentId}
          placeholder="选择 Agent"
          popupMatchSelectWidth={false}
          className={styles.versionManagerSelect}
          options={loops.map((l) => ({
            label: `${l.name}${l.latestVersion > 0 ? `（线上 v${l.latestVersion}）` : '（未发布）'}`,
            value: l.id,
          }))}
        />
        {selectedLoop && (
          <span className={styles.versionManagerDesc}>{selectedLoop.description}</span>
        )}
      </div>

      {loops.length === 0 ? (
        <Empty description="暂无 Agent，可先在调试台「存为草稿」创建一个" />
      ) : (
        <Table<AgentLoopVersion>
          rowKey="id"
          columns={columns}
          dataSource={versions}
          loading={isLoading}
          pagination={false}
          size="small"
        />
      )}

      <VersionDetailModal version={detailVersion} onClose={() => setDetailVersion(null)} />
    </Drawer>
  );
}

export default VersionManagerDrawer;
