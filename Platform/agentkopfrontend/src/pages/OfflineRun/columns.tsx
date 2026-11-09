/**
 * 离线跑数页面 - 表格列定义
 * 纯静态列直接导出，带交互的列通过工厂函数生成
 */
import { Button, Tag, Space, Popconfirm, Typography, Tooltip, Dropdown } from 'antd';
import styles from './components/OfflineRunTab.module.less';
import {
  EyeOutlined,
  DownloadOutlined,
  DeleteOutlined,
  CodeOutlined,
  EditOutlined,
  DownOutlined,
} from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import type {
  Dataset,
  DatasetExportFormat,
  DatasetTask,
  OfflineJob,
  JobCell,
  TraceEvent,
} from './types';
import { CELL_STATUS_COLOR, JOB_STATUS_COLOR } from './constants';
import { canExportOfflineJob, parseMetrics } from './utils';
import { OutputUrisCell } from './components/OutputUrisCell';

const { Text, Link } = Typography;

// ==================== 数据集列表列 ====================

export function getDatasetColumns(opts: {
  onViewTasks: (record: Dataset) => void;
  onEdit: (record: Dataset) => void;
  onDownload: (record: Dataset, format: DatasetExportFormat) => void;
  onDelete: (id: number, name: string) => void;
}): ColumnsType<Dataset> {
  return [
    { title: 'ID', dataIndex: 'id', width: 80 },
    { title: '名称', dataIndex: 'name', width: 160 },
    { title: '版本', dataIndex: 'version', width: 100 },
    {
      title: '类型',
      dataIndex: 'datasetType',
      width: 90,
      render: (type: string) => (type ? <Tag color="geekblue">{type}</Tag> : '--'),
    },
    { title: '用例数', dataIndex: 'total', width: 80 },
    {
      title: '引用任务数',
      dataIndex: 'jobCount',
      width: 100,
      render: (count: number) => count ?? '--',
    },
    {
      title: '描述',
      dataIndex: 'description',
      ellipsis: { showTitle: false },
      // 不设宽度作为弹性列，吸收容器剩余空间，保证表格始终填满宽度
      render: (text: string) => (
        <Tooltip title={text}>
          <span>{text || '--'}</span>
        </Tooltip>
      ),
    },
    { title: '创建人', dataIndex: 'creator', width: 120 },
    { title: '创建时间', dataIndex: 'createdAt', width: 180 },
    {
      title: '操作',
      // 四个链接按钮加间距约 300px；列宽不足时“删除”会溢出 td，导致 hover 背景在右侧断开。
      width: 340,
      fixed: 'right',
      render: (_: unknown, record: Dataset) => (
        <Space>
          <Button
            type="link"
            size="small"
            icon={<EyeOutlined />}
            onClick={() => opts.onViewTasks(record)}
          >
            用例
          </Button>
          <Button
            type="link"
            size="small"
            icon={<EditOutlined />}
            onClick={() => opts.onEdit(record)}
          >
            编辑
          </Button>
          <Dropdown
            menu={{
              items: [
                { key: 'yaml', label: 'YAML' },
                { key: 'json', label: 'JSON' },
                { key: 'xlsx', label: '表格 (XLSX)' },
              ],
              onClick: ({ key }) => opts.onDownload(record, key as DatasetExportFormat),
            }}
          >
            <Button type="link" size="small" icon={<DownloadOutlined />}>
              下载 <DownOutlined style={{ fontSize: 10 }} />
            </Button>
          </Dropdown>
          <Popconfirm
            title="确认删除"
            description={`确定要删除数据集 "${record.name}" 吗？`}
            onConfirm={() => opts.onDelete(record.id, record.name)}
            okText="删除"
            cancelText="取消"
            okButtonProps={{ danger: true }}
          >
            <Button type="link" size="small" danger icon={<DeleteOutlined />}>
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];
}

// ==================== 数据集用例列表列 ====================

export function getTaskColumns(opts: {
  onEdit: (record: DatasetTask) => void;
  onDelete: (record: DatasetTask) => void;
}): ColumnsType<DatasetTask> {
  return [
    { title: 'ID', dataIndex: 'id', width: 64 },
    {
      title: '用例标识',
      dataIndex: 'taskKey',
      width: 128,
      ellipsis: { showTitle: false },
      render: (text: string) => (
        <Tooltip title={text}>
          <span className={styles.tableCellEllipsis}>{text || '--'}</span>
        </Tooltip>
      ),
    },
    {
      title: 'Prompt',
      dataIndex: 'prompt',
      width: 320,
      ellipsis: { showTitle: false },
      render: (text: string) => (
        <Tooltip title={text}>
          <span className={styles.tableCellEllipsis}>{text || '--'}</span>
        </Tooltip>
      ),
    },
    {
      title: '外链',
      dataIndex: 'linkUrl',
      width: 160,
      ellipsis: { showTitle: false },
      render: (url: string) =>
        url ? (
          <Link
            className={styles.tableCellEllipsis}
            href={url}
            target="_blank"
            rel="noopener noreferrer"
          >
            {url}
          </Link>
        ) : (
          '--'
        ),
    },
    {
      title: '参考图',
      dataIndex: 'referImages',
      width: 168,
      render: (imgs: string[] | undefined) => <OutputUrisCell uris={imgs} />,
    },
    {
      title: '扩展字段',
      dataIndex: 'extraFields',
      width: 120,
      ellipsis: { showTitle: false },
      render: (fields: Record<string, unknown> | undefined) => {
        if (!fields || Object.keys(fields).length === 0) return '--';
        const text = JSON.stringify(fields);
        return (
          <Tooltip title={<pre style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{text}</pre>}>
            <span className={styles.tableCellEllipsis}>{text}</span>
          </Tooltip>
        );
      },
    },
    { title: '创建时间', dataIndex: 'createdAt', width: 168 },
    {
      title: '操作',
      width: 140,
      render: (_: unknown, record: DatasetTask) => (
        <Space>
          <Button
            type="link"
            size="small"
            icon={<EditOutlined />}
            onClick={() => opts.onEdit(record)}
          >
            编辑
          </Button>
          <Popconfirm
            title="确认删除"
            description={`确定要删除用例 "${record.taskKey}" 吗？`}
            onConfirm={() => opts.onDelete(record)}
            okText="删除"
            cancelText="取消"
            okButtonProps={{ danger: true }}
          >
            <Button type="link" size="small" danger icon={<DeleteOutlined />}>
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];
}

// ==================== 离线跑数任务列表列 ====================

export function getJobColumns(opts: {
  onViewDetail: (record: OfflineJob) => void;
  onDownload: (record: OfflineJob) => void;
  onDelete: (record: OfflineJob) => void;
  exportingBizId?: string | null;
}): ColumnsType<OfflineJob> {
  return [
    {
      title: 'BizID',
      dataIndex: 'bizId',
      width: 220,
      ellipsis: { showTitle: false },
      render: (text: string) => (
        <Tooltip title={text}>
          <Text copyable={{ text, tooltips: ['复制', '已复制'] as [string, string] }}>{text}</Text>
        </Tooltip>
      ),
    },
    { title: '数据集', dataIndex: 'datasetId', width: 80 },
    {
      title: 'Agent',
      dataIndex: 'agentName',
      width: 160,
      ellipsis: { showTitle: false },
      render: (name: string | undefined, record: OfflineJob) => {
        const label = name || (record.agentId != null ? `Agent #${record.agentId}` : '--');
        return (
          <Tooltip title={record.agentId != null ? `agentId: ${record.agentId}` : undefined}>
            <span>{label}</span>
          </Tooltip>
        );
      },
    },
    { title: '模型', dataIndex: 'model', width: 140 },
    {
      title: '状态',
      dataIndex: 'status',
      width: 100,
      render: (status: string) => <Tag color={JOB_STATUS_COLOR[status] || 'default'}>{status}</Tag>,
    },
    { title: '用例数', dataIndex: 'total', width: 80 },
    {
      title: '错误数',
      dataIndex: 'errors',
      width: 80,
      render: (errors: number) =>
        errors > 0 ? <Text type="danger">{errors}</Text> : <span>{errors}</span>,
    },
    {
      title: '工具',
      dataIndex: 'tools',
      width: 180,
      ellipsis: { showTitle: false },
      render: (tools: string[] | undefined) => {
        if (!tools?.length) return '--';
        const maxVisible = 2;
        const visible = tools.slice(0, maxVisible);
        const rest = tools.length - visible.length;
        const content = (
          <span className={styles.toolsCell}>
            {visible.map((t) => (
              <Tag key={t}>{t}</Tag>
            ))}
            {rest > 0 ? <Tag>+{rest}</Tag> : null}
          </span>
        );
        if (tools.length <= maxVisible) return content;
        return (
          <Tooltip
            title={
              <div className={styles.toolsTooltip}>
                {tools.map((t) => (
                  <div key={t}>{t}</div>
                ))}
              </div>
            }
          >
            {content}
          </Tooltip>
        );
      },
    },
    {
      title: 'Pipeline',
      dataIndex: 'pipeline',
      width: 160,
      ellipsis: { showTitle: false },
      render: (pipeline: string | null | undefined) =>
        pipeline ? (
          <Tooltip title={pipeline}>
            <Tag color="purple">{pipeline}</Tag>
          </Tooltip>
        ) : (
          '--'
        ),
    },
    { title: '创建时间', dataIndex: 'createdAt', width: 180 },
    {
      title: '操作',
      width: 240,
      fixed: 'right',
      render: (_: unknown, record: OfflineJob) => {
        const exportable = canExportOfflineJob(record.status);
        return (
          <Space>
            <Button
              type="link"
              size="small"
              icon={<EyeOutlined />}
              onClick={() => opts.onViewDetail(record)}
            >
              详情
            </Button>
            <Tooltip title={exportable ? undefined : '任务未完成，暂不可下载'}>
              <span>
                <Button
                  type="link"
                  size="small"
                  icon={<DownloadOutlined />}
                  loading={opts.exportingBizId === record.bizId}
                  disabled={!exportable}
                  onClick={() => opts.onDownload(record)}
                >
                  下载
                </Button>
              </span>
            </Tooltip>
            <Popconfirm
              title="确认删除"
              description={`删除任务 ${record.bizId} 及其全部用例？`}
              onConfirm={() => opts.onDelete(record)}
              okText="删除"
              cancelText="取消"
            >
              <Button type="link" size="small" danger icon={<DeleteOutlined />}>
                删除
              </Button>
            </Popconfirm>
          </Space>
        );
      },
    },
  ];
}

// ==================== 用例执行结果列 ====================

export function getCellColumns(opts: {
  onViewTrace: (record: JobCell) => void;
}): ColumnsType<JobCell> {
  return [
    { title: '用例标识', dataIndex: 'taskKey', width: 140 },
    {
      title: 'Prompt',
      dataIndex: 'prompt',
      width: 220,
      ellipsis: { showTitle: false },
      render: (text: string | undefined) =>
        text ? (
          <Tooltip
            placement="topLeft"
            title={<div className={styles.promptTooltip}>{text}</div>}
          >
            <span className={styles.tableCellEllipsis}>{text}</span>
          </Tooltip>
        ) : (
          '--'
        ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 80,
      render: (status: string) => (
        <Tag color={CELL_STATUS_COLOR[status] || 'default'}>{status}</Tag>
      ),
    },
    {
      title: '产出',
      dataIndex: 'outputUris',
      width: 168,
      render: (uris: string[] | undefined) => <OutputUrisCell uris={uris} />,
    },
    {
      title: '耗时',
      dataIndex: 'metricsJson',
      width: 100,
      render: (json: string) => {
        const m = parseMetrics(json);
        return m ? `${m.durationMillis}ms` : '--';
      },
    },
    {
      title: '输入 Token',
      dataIndex: 'metricsJson',
      width: 100,
      render: (json: string) => parseMetrics(json)?.inputTokens ?? '--',
    },
    {
      title: '输出 Token',
      dataIndex: 'metricsJson',
      width: 100,
      render: (json: string) => parseMetrics(json)?.outputTokens ?? '--',
    },
    {
      title: '工具调用',
      dataIndex: 'metricsJson',
      width: 100,
      render: (json: string) => {
        const m = parseMetrics(json);
        return m ? `${m.toolCalls} / ${m.toolErrors} err` : '--';
      },
    },
    {
      title: 'Pipeline Exec',
      dataIndex: 'pipelineExecId',
      width: 140,
      ellipsis: { showTitle: false },
      render: (id: string | undefined) =>
        id ? (
          <Tooltip title={id}>
            <Text copyable={{ text: id, tooltips: ['复制', '已复制'] as [string, string] }}>
              {id}
            </Text>
          </Tooltip>
        ) : (
          '--'
        ),
    },
    {
      title: '错误',
      dataIndex: 'error',
      width: 160,
      ellipsis: { showTitle: false },
      render: (err: string) =>
        err ? (
          <Tooltip title={err}>
            <Text type="danger" className={styles.tableCellEllipsis}>
              {err}
            </Text>
          </Tooltip>
        ) : (
          '--'
        ),
    },
    {
      title: '操作',
      width: 96,
      fixed: 'right',
      render: (_: unknown, record: JobCell) => (
        <Button
          type="link"
          size="small"
          icon={<CodeOutlined />}
          disabled={!record.agentkSessionId && !record.pipelineExecId}
          onClick={() => opts.onViewTrace(record)}
        >
          Trace
        </Button>
      ),
    },
  ];
}

// ==================== Trace 事件列 ====================

export const traceEventColumns: ColumnsType<TraceEvent> = [
  { title: 'Seq', dataIndex: 'seq', width: 60 },
  {
    title: '事件',
    dataIndex: 'kind',
    width: 160,
    render: (kind: string) => <Tag color="geekblue">{kind}</Tag>,
  },
  {
    title: 'Round',
    dataIndex: 'roundId',
    width: 140,
    ellipsis: { showTitle: false },
    render: (roundId: string) =>
      roundId ? (
        <Tooltip title={roundId}>
          <span className={styles.tableCellEllipsis}>{roundId}</span>
        </Tooltip>
      ) : (
        '--'
      ),
  },
  {
    title: 'Data',
    dataIndex: 'data',
    render: (data: Record<string, unknown>) => (
      <pre className={styles.eventPayloadThin}>{JSON.stringify(data, null, 2)}</pre>
    ),
  },
  {
    title: '时间',
    dataIndex: 'createdAt',
    width: 200,
    ellipsis: { showTitle: false },
    render: (text: string) => (
      <Tooltip title={text}>
        <span>{text}</span>
      </Tooltip>
    ),
  },
];
