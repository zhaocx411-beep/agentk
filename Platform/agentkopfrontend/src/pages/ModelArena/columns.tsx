import { Button, Space, Tag, Tooltip } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { Link } from 'react-router-dom';
import type { ArenaBattleRecord } from './types';

const STATUS_META: Record<
  ArenaBattleRecord['status'],
  { label: string; color: string }
> = {
  draft: { label: '草稿', color: 'default' },
  running: { label: '进行中', color: 'processing' },
  voting: { label: '投票中', color: 'blue' },
  completed: { label: '已完成', color: 'green' },
  invalid: { label: '已失效', color: 'red' },
};

interface ArenaManageColumnOptions {
  onViewDetail: (record: ArenaBattleRecord) => void;
}

export function getArenaManageColumns({
  onViewDetail,
}: ArenaManageColumnOptions): ColumnsType<ArenaBattleRecord> {
  return [
    {
      title: '对战 ID',
      dataIndex: 'id',
      width: 140,
      ellipsis: { showTitle: false },
      render: (text: string) => (
        <Tooltip title={text}>
          <span>{text}</span>
        </Tooltip>
      ),
    },
    {
      title: '标题',
      dataIndex: 'title',
      width: 220,
      ellipsis: { showTitle: false },
      render: (text: string) => (
        <Tooltip title={text}>
          <span>{text}</span>
        </Tooltip>
      ),
    },
    {
      title: '模型 A',
      dataIndex: 'modelA',
      width: 160,
      ellipsis: { showTitle: false },
    },
    {
      title: '模型 B',
      dataIndex: 'modelB',
      width: 160,
      ellipsis: { showTitle: false },
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 100,
      render: (status: ArenaBattleRecord['status']) => {
        const meta = STATUS_META[status];
        return <Tag color={meta.color}>{meta.label}</Tag>;
      },
    },
    {
      title: '投票',
      width: 110,
      render: (_: unknown, record: ArenaBattleRecord) => `${record.votesA} : ${record.votesB}`,
    },
    {
      title: '胜者',
      dataIndex: 'winner',
      width: 90,
      render: (winner: ArenaBattleRecord['winner']) => {
        if (!winner) return '--';
        if (winner === 'tie') return '平局';
        return winner === 'A' ? '模型 A' : '模型 B';
      },
    },
    { title: '创建人', dataIndex: 'creator', width: 110 },
    { title: '创建时间', dataIndex: 'createdAt', width: 170 },
    {
      title: '完成时间',
      dataIndex: 'completedAt',
      width: 170,
      render: (value?: string) => value || '--',
    },
    {
      title: '操作',
      key: 'actions',
      width: 160,
      fixed: 'right',
      render: (_: unknown, record: ArenaBattleRecord) => (
        <Space size="small">
          <Link to={`/model-arena?battle=${encodeURIComponent(record.id)}`}>进入对战</Link>
          <Button type="link" size="small" onClick={() => onViewDetail(record)}>
            详情
          </Button>
        </Space>
      ),
    },
  ];
}
