/**
 * Pipeline 版本管理抽屉
 * 查看 / 发布 / 查看详情（definitionJson 可视化）
 */
import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Drawer, Empty, Popconfirm, Select, Space, Table, Tag } from 'antd';
import { message } from '@/utils/feedback';
import type { ColumnsType } from 'antd/es/table';
import {
  WORKFLOW_ENTITIES_QUERY_KEY,
  workflowVersionsKey,
  getWorkflowEntities,
  getWorkflowVersions,
  publishWorkflowVersion,
} from '@/services/workflowPersistence';
import type { WorkflowVersion } from '@/types/workflowPersistence';
import WorkflowVersionDetailModal from './WorkflowVersionDetailModal';
import styles from '../WorkflowRunner.module.less';

interface WorkflowVersionDrawerProps {
  open: boolean;
  onClose: () => void;
  /** 用某个版本替换会话里的 Pipeline 定义并切到新会话运行 */
  onRunVersion: (version: WorkflowVersion, entityName: string) => void;
}

function WorkflowVersionDrawer({ open, onClose, onRunVersion }: WorkflowVersionDrawerProps) {
  const queryClient = useQueryClient();
  const [workflowId, setWorkflowId] = useState<number | null>(null);
  /** 详情弹窗当前查看的版本 */
  const [detailVersion, setDetailVersion] = useState<WorkflowVersion | null>(null);

  const { data: entities = [] } = useQuery({
    queryKey: WORKFLOW_ENTITIES_QUERY_KEY,
    queryFn: getWorkflowEntities,
  });

  // 打开时默认选中第一个 Pipeline
  useEffect(() => {
    if (open && workflowId === null && entities.length > 0) {
      setWorkflowId(entities[0].id);
    }
  }, [open, workflowId, entities]);

  const selectedEntity = entities.find((e) => e.id === workflowId) ?? null;

  const { data: versions = [], isLoading } = useQuery({
    queryKey: workflowVersionsKey(workflowId ?? -1),
    queryFn: () => getWorkflowVersions(workflowId as number),
    enabled: open && workflowId !== null,
  });

  const publishMutation = useMutation({
    mutationFn: publishWorkflowVersion,
    onSuccess: (version) => {
      message.success(`v${version.version} 已发布`);
      queryClient.invalidateQueries({ queryKey: WORKFLOW_ENTITIES_QUERY_KEY });
      if (workflowId !== null) {
        queryClient.invalidateQueries({ queryKey: workflowVersionsKey(workflowId) });
      }
    },
  });

  const columns: ColumnsType<WorkflowVersion> = [
    { title: '版本', dataIndex: 'version', width: 64, render: (v: number) => `v${v}` },
    {
      title: '状态',
      dataIndex: 'status',
      width: 80,
      render: (s: string) =>
        s === 'published' ? <Tag color="success">已发布</Tag> : <Tag>草稿</Tag>,
    },
    { title: '创建人', dataIndex: 'createdBy', width: 90, render: (v: string) => v || '-' },
    { title: '创建时间', dataIndex: 'createdAt', width: 150 },
    {
      title: '操作',
      width: 190,
      render: (_, r) => (
        <Space size={0}>
          <Button type="link" size="small" onClick={() => setDetailVersion(r)}>
            详情
          </Button>
          <Button
            type="link"
            size="small"
            onClick={() => {
              onRunVersion(r, selectedEntity?.name ?? '');
              onClose();
            }}
          >
            运行
          </Button>
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
    <Drawer title="Pipeline 版本管理" open={open} onClose={onClose} width={720}>
      <div className={styles.versionManagerHeader}>
        <Select
          value={workflowId}
          onChange={setWorkflowId}
          placeholder="选择 Pipeline"
          popupMatchSelectWidth={false}
          className={styles.versionManagerSelect}
          options={entities.map((e) => ({
            label: `${e.name}${e.latestVersion > 0 ? `（线上 v${e.latestVersion}）` : '（未发布）'}`,
            value: e.id,
          }))}
        />
        {selectedEntity && (
          <span className={styles.versionManagerDesc}>{selectedEntity.description}</span>
        )}
      </div>

      {entities.length === 0 ? (
        <Empty description="暂无 Pipeline，可先在左栏「新建 Pipeline」创建一个" />
      ) : (
        <Table<WorkflowVersion>
          rowKey="id"
          columns={columns}
          dataSource={versions}
          loading={isLoading}
          pagination={false}
          size="small"
        />
      )}

      <WorkflowVersionDetailModal version={detailVersion} onClose={() => setDetailVersion(null)} />
    </Drawer>
  );
}

export default WorkflowVersionDrawer;
