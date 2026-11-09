/**
 * 数据回看 - 表格列定义
 */
import { Button, Space, Tag, Typography, Tooltip } from 'antd';
import { CodeOutlined, MessageOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import type { SessionReviewRecord } from './types';
import { RUN_STATUS_COLOR, SESSION_STATUS_COLOR } from './constants';

const { Text } = Typography;

export function getSessionColumns(
  onViewProfile: (record: SessionReviewRecord) => void,
  onViewConversation: (record: SessionReviewRecord) => void,
): ColumnsType<SessionReviewRecord> {
  return [
    { title: 'ID', dataIndex: 'id', width: 70 },
    {
      title: 'Public ID',
      dataIndex: 'publicId',
      width: 220,
      ellipsis: { showTitle: false },
      render: (text: string) => (
        <Tooltip title={text}>
          <Text copyable={{ text, tooltips: ['复制', '已复制'] as [string, string] }}>{text}</Text>
        </Tooltip>
      ),
    },
    {
      title: '标题',
      dataIndex: 'title',
      width: 160,
      ellipsis: { showTitle: false },
      render: (text: string) => (
        <Tooltip title={text}>
          <span>{text || '--'}</span>
        </Tooltip>
      ),
    },
    { title: '用户', dataIndex: 'userId', width: 110, render: (v: string) => v || '--' },
    {
      title: '状态',
      dataIndex: 'status',
      width: 90,
      render: (status: string) => (
        <Tag color={SESSION_STATUS_COLOR[status] || 'default'}>{status || '--'}</Tag>
      ),
    },
    {
      title: '运行状态',
      dataIndex: 'activeRunStatus',
      width: 90,
      render: (status: string) => (
        <Tag color={RUN_STATUS_COLOR[status] || 'default'}>{status || '--'}</Tag>
      ),
    },
    {
      title: 'Profile 版本',
      dataIndex: 'profileVersion',
      width: 110,
      render: (v: string) => v || '--',
    },
    {
      title: '配置来源',
      dataIndex: 'configSource',
      width: 120,
      render: (v: string) => v || '--',
    },
    {
      title: '生效模型',
      dataIndex: 'effectiveModel',
      width: 160,
      ellipsis: { showTitle: false },
      render: (text: string) => (
        <Tooltip title={text}>
          <span>{text || '--'}</span>
        </Tooltip>
      ),
    },
    { title: '事件序号', dataIndex: 'lastEventSeq', width: 90 },
    { title: '创建时间', dataIndex: 'createdAt', width: 165 },
    {
      title: '操作',
      width: 150,
      fixed: 'right',
      render: (_: unknown, record) => {
        const canViewChat = Boolean(record.agentkSessionId);
        const isWorkflow = record.sessionKind === 'workflow';
        return (
          <Space size={0}>
            <Tooltip
              title={
                canViewChat
                  ? isWorkflow
                    ? '打开 Pipeline 独立回看页面'
                    : '打开 Agent 对话独立回看页面'
                  : '无 agentkSessionId，无法回看对话（多为创建失败）'
              }
            >
              <Button
                type="link"
                size="small"
                icon={<MessageOutlined />}
                disabled={!canViewChat}
                onClick={() => onViewConversation(record)}
              >
                对话
              </Button>
            </Tooltip>
            <Button
              type="link"
              size="small"
              icon={<CodeOutlined />}
              onClick={() => onViewProfile(record)}
            >
              配置
            </Button>
          </Space>
        );
      },
    },
  ];
}
