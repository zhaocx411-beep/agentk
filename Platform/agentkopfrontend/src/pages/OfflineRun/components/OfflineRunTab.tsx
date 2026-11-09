/**
 * 离线跑数 Tab 组件
 * 包含：任务列表 + 提交任务 + 用例执行详情 + Trace 回看
 */
import { useState, useCallback, useEffect, useRef, type UIEvent } from 'react';
import {
  Button,
  Table,
  Tag,
  Space,
  Modal,
  Form,
  Select,
  Descriptions,
  Spin,
  Empty,
  Typography,
} from 'antd';
import { message } from '@/utils/feedback';
import { PlayCircleOutlined, ReloadOutlined } from '@ant-design/icons';
import DataTable from '@/components/DataTable';
import type {
  OfflineJob,
  OfflineJobSubmitParams,
  JobCell,
  TraceDetail,
  TraceItem,
  TraceEvent,
  AgentConfigAgent,
  AgentConfigSessionMode,
  Dataset,
} from '../types';
import {
  queryOfflineJobs,
  submitOfflineJob,
  deleteOfflineJob,
  downloadOfflineJobExport,
  queryJobCells,
  queryCellTrace,
  getAgentConfig,
  queryDatasets,
  getOfflineJobDetail,
} from '../api';
import { getJobColumns, getCellColumns, traceEventColumns } from '../columns';
import { buildTraceItems, normalizeJobCells } from '../utils';
import { TraceMediaList } from './TraceMediaList';
import styles from './OfflineRunTab.module.less';

const AGENT_SOURCE_LABEL: Record<string, string> = {
  official: '官方',
  mine: '我的',
};

/** 提交弹窗数据集下拉每页条数 */
const DATASET_PAGE_SIZE = 20;

function formatSessionMode(mode: string, options: AgentConfigSessionMode[]): string {
  const option = options.find((item) => item.code === mode);
  return option ? `${option.label}（${option.code}）` : mode;
}

function formatAgentOptionLabel(agent: AgentConfigAgent): string {
  const source = agent.source ? AGENT_SOURCE_LABEL[agent.source] || agent.source : undefined;
  const version = agent.agentVersionId != null ? `vId ${agent.agentVersionId}` : '未发布版本';
  return source ? `${agent.name}（${source} · ${version}）` : `${agent.name}（${version}）`;
}

function filterEvalDatasets(records: Dataset[]): Dataset[] {
  return records.filter((ds) => !ds.usage || ds.usage === 'EVAL');
}

function mergeDatasetsById(prev: Dataset[], next: Dataset[]): Dataset[] {
  if (next.length === 0) return prev;
  const seen = new Set(prev.map((ds) => ds.id));
  const merged = [...prev];
  for (const ds of next) {
    if (seen.has(ds.id)) continue;
    seen.add(ds.id);
    merged.push(ds);
  }
  return merged;
}

export default function OfflineRunTab() {
  // ==================== 任务列表 ====================
  const [jobs, setJobs] = useState<OfflineJob[]>([]);
  const [jobsLoading, setJobsLoading] = useState(false);
  const [jobsTotal, setJobsTotal] = useState(0);
  const [jobsPage, setJobsPage] = useState(1);
  const [jobsPageSize, setJobsPageSize] = useState(10);
  const [exportingBizId, setExportingBizId] = useState<string | null>(null);

  // ==================== 提交任务弹窗 ====================
  const [submitVisible, setSubmitVisible] = useState(false);
  const [submitForm] = Form.useForm();
  const sessionMode = Form.useWatch<string>('sessionMode', submitForm);
  const [submitting, setSubmitting] = useState(false);
  const [agentOptions, setAgentOptions] = useState<AgentConfigAgent[]>([]);
  const [sessionModeOptions, setSessionModeOptions] = useState<AgentConfigSessionMode[]>([]);
  const [configLoading, setConfigLoading] = useState(false);
  const [datasetOptions, setDatasetOptions] = useState<Dataset[]>([]);
  const [datasetPage, setDatasetPage] = useState(1);
  const [datasetTotal, setDatasetTotal] = useState(0);
  const [datasetHasMore, setDatasetHasMore] = useState(false);
  const [datasetsLoadingMore, setDatasetsLoadingMore] = useState(false);
  const datasetsLoadingMoreRef = useRef(false);

  // ==================== 用例详情弹窗 ====================
  const [cellsVisible, setCellsVisible] = useState(false);
  const [cellsTitle, setCellsTitle] = useState('');
  const [cells, setCells] = useState<JobCell[]>([]);
  const [cellsLoading, setCellsLoading] = useState(false);
  const currentJobRef = useRef<OfflineJob | null>(null);

  // ==================== 任务详情弹窗 ====================
  const [detailVisible, setDetailVisible] = useState(false);
  const [detail, setDetail] = useState<OfflineJob | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);

  // ==================== Trace 弹窗 ====================
  const [traceVisible, setTraceVisible] = useState(false);
  const [traceTitle, setTraceTitle] = useState('');
  const [trace, setTrace] = useState<TraceDetail | null>(null);
  const [traceLoading, setTraceLoading] = useState(false);

  // ==================== 加载任务列表 ====================
  const loadJobs = useCallback(
    async (current = jobsPage, size = jobsPageSize) => {
      try {
        setJobsLoading(true);
        const res = await queryOfflineJobs({ current, pageSize: size });
        setJobs(res?.records || []);
        setJobsTotal(res?.total || 0);
        setJobsPage(res?.current || 1);
      } catch {
        message.error('加载任务列表失败');
      } finally {
        setJobsLoading(false);
      }
    },
    [jobsPage, jobsPageSize],
  );

  // 初始加载（用 ref 防止 StrictMode double-mount 导致重复调用）
  const hasInitLoadedRef = useRef(false);
  useEffect(() => {
    if (hasInitLoadedRef.current) return;
    hasInitLoadedRef.current = true;
    loadJobs(1, 10);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 分页
  const handleJobPageChange = useCallback(
    (newPage: number, newPageSize?: number) => {
      const size = newPageSize || jobsPageSize;
      setJobsPage(newPage);
      setJobsPageSize(size);
      loadJobs(newPage, size);
    },
    [jobsPageSize, loadJobs],
  );

  // ==================== 提交任务 ====================
  const resetSubmitDatasets = useCallback(() => {
    setDatasetOptions([]);
    setDatasetPage(1);
    setDatasetTotal(0);
    setDatasetHasMore(false);
    setDatasetsLoadingMore(false);
    datasetsLoadingMoreRef.current = false;
  }, []);

  const loadDatasetPage = useCallback(async (page: number, append: boolean) => {
    const res = await queryDatasets({
      current: page,
      pageSize: DATASET_PAGE_SIZE,
      usage: 'EVAL',
    });
    const rawRecords = res?.records || [];
    const records = filterEvalDatasets(rawRecords);
    const total = res?.total || 0;
    setDatasetTotal(total);
    setDatasetPage(res?.current || page);
    setDatasetOptions((prev) => (append ? mergeDatasetsById(prev, records) : records));
    // total 优先；缺失时用本页是否满页判断是否还有下一页
    setDatasetHasMore(
      total > 0 ? page * DATASET_PAGE_SIZE < total : rawRecords.length >= DATASET_PAGE_SIZE,
    );
  }, []);

  const loadMoreDatasets = useCallback(async () => {
    if (datasetsLoadingMoreRef.current || !datasetHasMore) return;
    datasetsLoadingMoreRef.current = true;
    setDatasetsLoadingMore(true);
    try {
      await loadDatasetPage(datasetPage + 1, true);
    } catch {
      message.error('加载更多数据集失败');
    } finally {
      datasetsLoadingMoreRef.current = false;
      setDatasetsLoadingMore(false);
    }
  }, [datasetHasMore, datasetPage, loadDatasetPage]);

  const handleDatasetPopupScroll = useCallback(
    (event: UIEvent<HTMLDivElement>) => {
      const target = event.target as HTMLDivElement;
      if (!datasetHasMore || datasetsLoadingMoreRef.current) return;
      if (target.scrollTop + target.clientHeight >= target.scrollHeight - 32) {
        void loadMoreDatasets();
      }
    },
    [datasetHasMore, loadMoreDatasets],
  );

  const openSubmitModal = useCallback(async () => {
    setSubmitVisible(true);
    setConfigLoading(true);
    setSessionModeOptions([]);
    resetSubmitDatasets();
    // agents → agent-config；数据集 → usage=EVAL（首屏分页，滚动再加载）
    try {
      const [config] = await Promise.all([getAgentConfig(), loadDatasetPage(1, false)]);
      setAgentOptions(config.agents || []);
      const sessionModes = config.sessionModes || [];
      setSessionModeOptions(sessionModes);
      const defaultSessionMode = config.defaultSessionMode || sessionModes[0]?.code;
      if (defaultSessionMode) {
        submitForm.setFieldValue('sessionMode', defaultSessionMode);
      }
    } catch {
      message.error('加载配置失败');
    } finally {
      setConfigLoading(false);
    }
  }, [loadDatasetPage, resetSubmitDatasets, submitForm]);

  const closeSubmitModal = useCallback(() => {
    setSubmitVisible(false);
    submitForm.resetFields();
    resetSubmitDatasets();
  }, [resetSubmitDatasets, submitForm]);

  const handleSubmit = useCallback(async () => {
    try {
      const values = await submitForm.validateFields();
      const payload: OfflineJobSubmitParams = {
        datasetId: values.datasetId as number,
        agentId: values.agentId as number,
        sessionMode: values.sessionMode as string,
      };
      setSubmitting(true);
      await submitOfflineJob(payload);
      message.success('任务提交成功');
      closeSubmitModal();
      loadJobs(1, jobsPageSize);
    } catch (err: unknown) {
      if (err instanceof Error) {
        message.error(err.message);
      }
    } finally {
      setSubmitting(false);
    }
  }, [closeSubmitModal, submitForm, jobsPageSize, loadJobs]);

  // ==================== 任务详情 ====================
  const openDetailModal = useCallback(async (job: OfflineJob) => {
    try {
      setDetailLoading(true);
      setDetailVisible(true);
      const res = await getOfflineJobDetail(job.bizId);
      setDetail(res);
    } catch {
      message.error('加载任务详情失败');
      setDetail(null);
    } finally {
      setDetailLoading(false);
    }
  }, []);

  // ==================== 用例详情 ====================
  const openCellsModal = useCallback(async (job: OfflineJob) => {
    try {
      setCellsLoading(true);
      setCellsTitle(`任务 ${job.bizId} - 用例执行结果`);
      setCellsVisible(true);
      currentJobRef.current = job;
      const res = await queryJobCells(job.bizId);
      setCells(normalizeJobCells(res));
    } catch {
      message.error('加载用例结果失败');
      setCells([]);
    } finally {
      setCellsLoading(false);
    }
  }, []);

  // ==================== Trace 详情 ====================
  const openTraceModal = useCallback(async (cell: JobCell) => {
    const job = currentJobRef.current;
    if (!job) {
      message.warning('未找到当前任务信息');
      return;
    }
    // Loop 或 Pipeline 任一通道有执行凭证即可查 Trace
    if (!cell.agentkSessionId && !cell.pipelineExecId) {
      message.warning('该用例尚未执行成功，无法查看 Trace');
      return;
    }
    try {
      setTraceLoading(true);
      setTraceTitle(`${cell.taskKey} - Trace 详情`);
      setTraceVisible(true);
      const res = await queryCellTrace(job.bizId, cell.taskKey);
      setTrace(res);
    } catch {
      // 拦截器已提示具体原因（如 412 trace 尚未生成）
      setTrace(null);
    } finally {
      setTraceLoading(false);
    }
  }, []);

  // ==================== 下载产出 ====================
  const handleDownloadJob = useCallback(async (job: OfflineJob) => {
    try {
      setExportingBizId(job.bizId);
      const filename = await downloadOfflineJobExport(job.bizId);
      message.success(`已下载 ${filename}`);
    } catch (err) {
      // HTTP 错误由拦截器提示；JSON 业务错误在此提示
      if (err instanceof Error && !('isAxiosError' in err)) {
        message.error(err.message);
      }
    } finally {
      setExportingBizId(null);
    }
  }, []);

  // ==================== 删除任务 ====================
  const handleDeleteJob = useCallback(
    async (job: OfflineJob) => {
      try {
        await deleteOfflineJob(job.bizId);
        message.success('任务已删除');
        loadJobs(jobsPage, jobsPageSize);
      } catch {
        // 拦截器已提示具体原因（如 412 任务不存在）
      }
    },
    [jobsPage, jobsPageSize, loadJobs],
  );

  // ==================== 列定义 ====================
  const jobColumns = getJobColumns({
    onViewDetail: openDetailModal,
    onDownload: handleDownloadJob,
    onDelete: handleDeleteJob,
    exportingBizId,
  });
  const cellColumns = getCellColumns({
    onViewTrace: openTraceModal,
  });

  // ==================== Trace 渲染 ====================
  const renderTraceItems = (items: TraceItem[]) => (
    <div className={styles.traceItems}>
      {items.map((item) => {
        const isUser = item.role === 'user';
        return (
          <div
            key={item.seq}
            className={`${styles.traceItem} ${isUser ? styles.traceItemUser : styles.traceItemAssistant}`}
          >
            <div className={styles.traceItemHeader}>
              <Tag color={isUser ? 'blue' : 'green'}>{item.role}</Tag>
              <span className={styles.traceItemSeq}>#{item.seq}</span>
            </div>
            <div className={styles.traceItemParts}>
              {item.parts.map((part, idx) => {
                if (part.type === 'text') {
                  return (
                    <div key={idx} className={styles.tracePart}>
                      {part.text ? <div className={styles.traceBubble}>{part.text}</div> : null}
                      <TraceMediaList
                        source={part.mediaSource ?? part.text}
                        align={isUser ? 'right' : 'left'}
                      />
                    </div>
                  );
                }
                if (part.type === 'reasoning') {
                  return (
                    <div key={idx} className={styles.tracePart}>
                      {part.text ? (
                        <div className={`${styles.traceBubble} ${styles.traceBubbleReasoning}`}>
                          {part.text}
                        </div>
                      ) : null}
                      <TraceMediaList
                        source={part.mediaSource ?? part.text}
                        align={isUser ? 'right' : 'left'}
                      />
                    </div>
                  );
                }
                if (part.type === 'tool-call') {
                  return (
                    <div key={idx} className={styles.tracePart}>
                      <div className={`${styles.traceBubble} ${styles.traceBubbleToolCall}`}>
                        <span className={styles.traceToolLabel}>tool-call: {part.toolName}</span>
                        {part.toolArgs && (
                          <pre className={styles.traceToolArgs}>{part.toolArgs}</pre>
                        )}
                      </div>
                    </div>
                  );
                }
                if (part.type === 'tool-result') {
                  return (
                    <div key={idx} className={styles.tracePart}>
                      <div className={`${styles.traceBubble} ${styles.traceBubbleToolResult}`}>
                        <span className={styles.traceToolLabel}>tool-result</span>
                        {part.toolOutput && (
                          <pre className={styles.traceToolArgs}>{part.toolOutput}</pre>
                        )}
                      </div>
                      <TraceMediaList
                        source={part.mediaSource ?? part.toolOutput}
                        align={isUser ? 'right' : 'left'}
                      />
                    </div>
                  );
                }
                return (
                  <div key={idx} className={styles.tracePart}>
                    <div className={styles.traceBubble}>
                      <Tag>{part.type}</Tag>
                      {part.text}
                    </div>
                    <TraceMediaList
                      source={part.mediaSource ?? part.text}
                      align={isUser ? 'right' : 'left'}
                    />
                  </div>
                );
              })}
            </div>
          </div>
        );
      })}
    </div>
  );

  const renderTraceEvents = (events: TraceEvent[]) => (
    <Table
      rowKey="seq"
      dataSource={events}
      pagination={false}
      size="small"
      scroll={{ y: 300 }}
      columns={traceEventColumns}
    />
  );

  return (
    <div className={styles.wrapper}>
      {/* 操作栏 */}
      <div className={styles.actionBar}>
        <Space>
          <Button type="primary" icon={<PlayCircleOutlined />} onClick={openSubmitModal}>
            提交评测
          </Button>
          <Button
            icon={<ReloadOutlined />}
            onClick={() => loadJobs(jobsPage, jobsPageSize)}
            loading={jobsLoading}
          >
            刷新
          </Button>
        </Space>
      </div>

      {/* 任务列表 */}
      <DataTable<OfflineJob>
        className={styles.jobsTable}
        rowKey="bizId"
        columns={jobColumns}
        dataSource={jobs}
        loading={jobsLoading}
        scroll={{ x: 1620 }}
        pagination={{
          current: jobsPage,
          pageSize: jobsPageSize,
          total: jobsTotal,
          onChange: handleJobPageChange,
        }}
      />

      {/* 提交评测弹窗 */}
      <Modal
        title="提交离线跑数"
        open={submitVisible}
        onOk={handleSubmit}
        onCancel={closeSubmitModal}
        confirmLoading={submitting}
        okText="提交"
        cancelText="取消"
        width={560}
      >
        <Form form={submitForm} layout="vertical">
          <Form.Item
            name="datasetId"
            label="数据集"
            extra={
              datasetTotal > 0
                ? `仅展示 usage=EVAL；已加载 ${datasetOptions.length}/${datasetTotal}，滚动可继续加载`
                : '仅展示 usage=EVAL 的数据集；TEMPLATE 不可提交离线跑数'
            }
            rules={[{ required: true, message: '请选择数据集' }]}
          >
            <Select
              placeholder="选择 EVAL 数据集"
              showSearch
              loading={configLoading}
              optionFilterProp="label"
              listHeight={280}
              onPopupScroll={handleDatasetPopupScroll}
              dropdownRender={(menu) => (
                <>
                  {menu}
                  {datasetsLoadingMore ? (
                    <div className={styles.datasetLoadingMore}>加载更多…</div>
                  ) : null}
                </>
              )}
              options={datasetOptions.map((ds) => ({
                label: `${ds.name} (${ds.version}) - ${ds.description || '无描述'}`,
                value: ds.id,
              }))}
            />
          </Form.Item>
          <Form.Item
            name="agentId"
            label="Agent"
            extra="来自 agent-config.agents；将使用该 Agent 已发布版本的配置快照"
            rules={[{ required: true, message: '请选择 Agent' }]}
          >
            <Select
              placeholder="选择 Agent"
              showSearch
              loading={configLoading}
              optionFilterProp="label"
              notFoundContent={configLoading ? '加载中…' : '暂无可用 Agent'}
              options={agentOptions.map((agent) => ({
                label: formatAgentOptionLabel(agent),
                value: agent.agentId,
              }))}
            />
          </Form.Item>
          <Form.Item
            name="sessionMode"
            label="会话策略"
            extra={
              sessionModeOptions.find((option) => option.code === sessionMode)?.description ??
              (configLoading ? '正在加载会话策略…' : '暂无可用会话策略')
            }
            rules={[{ required: true, message: '请选择会话策略' }]}
          >
            <Select
              aria-label="会话策略"
              loading={configLoading}
              placeholder="选择会话策略"
              notFoundContent={configLoading ? '加载中…' : '暂无可用会话策略'}
              options={sessionModeOptions.map((option) => ({
                label: `${option.label}（${option.code}）`,
                value: option.code,
              }))}
            />
          </Form.Item>
        </Form>
      </Modal>

      {/* 用例执行结果弹窗 */}
      <Modal
        title={cellsTitle}
        open={cellsVisible}
        onCancel={() => setCellsVisible(false)}
        footer={null}
        width={1080}
      >
        <Table
          rowKey="taskKey"
          columns={cellColumns}
          dataSource={cells}
          loading={cellsLoading}
          pagination={false}
          size="small"
          scroll={{ x: 1280 }}
        />
      </Modal>

      {/* 任务详情弹窗 */}
      <Modal
        title="任务详情"
        open={detailVisible}
        onCancel={() => {
          setDetailVisible(false);
          setDetail(null);
        }}
        footer={null}
        width={700}
        classNames={{ body: styles.detailModalBody }}
      >
        {detailLoading ? (
          <Spin />
        ) : detail ? (
          <>
            <Descriptions size="small" bordered column={2}>
              <Descriptions.Item label="BizID">{detail.bizId}</Descriptions.Item>
              <Descriptions.Item label="数据集 ID">{detail.datasetId}</Descriptions.Item>
              <Descriptions.Item label="Agent">
                {detail.agentName || '--'}
                {detail.agentId != null ? ` (#${detail.agentId})` : ''}
              </Descriptions.Item>
              <Descriptions.Item label="版本 ID">{detail.agentVersionId ?? '--'}</Descriptions.Item>
              <Descriptions.Item label="会话策略">
                {detail.sessionMode ? (
                  <Tag color="blue">
                    {formatSessionMode(detail.sessionMode, sessionModeOptions)}
                  </Tag>
                ) : (
                  '--'
                )}
              </Descriptions.Item>
              <Descriptions.Item label="模型">{detail.model}</Descriptions.Item>
              <Descriptions.Item label="Pipeline">
                {detail.pipeline ? <Tag color="purple">{detail.pipeline}</Tag> : '--'}
              </Descriptions.Item>
              <Descriptions.Item label="状态">
                <Tag
                  color={
                    detail.status === 'SUCCEEDED'
                      ? 'green'
                      : detail.status === 'FAILED'
                        ? 'red'
                        : 'blue'
                  }
                >
                  {detail.status}
                </Tag>
              </Descriptions.Item>
              <Descriptions.Item label="用例数">{detail.total}</Descriptions.Item>
              <Descriptions.Item label="错误数">{detail.errors}</Descriptions.Item>
              <Descriptions.Item label="创建人">{detail.creator}</Descriptions.Item>
              <Descriptions.Item label="创建时间">{detail.createdAt}</Descriptions.Item>
              <Descriptions.Item label="开始时间">{detail.startedAt}</Descriptions.Item>
              <Descriptions.Item label="完成时间">{detail.finishedAt || '--'}</Descriptions.Item>
            </Descriptions>
            <Descriptions size="small" bordered column={1} style={{ marginTop: 16 }}>
              <Descriptions.Item label="系统提示词">
                <pre className={styles.systemPrompt}>{detail.systemPrompt}</pre>
              </Descriptions.Item>
              <Descriptions.Item label="工具">
                {detail.tools?.length ? detail.tools.map((t) => <Tag key={t}>{t}</Tag>) : '--'}
              </Descriptions.Item>
            </Descriptions>
            <div style={{ marginTop: 16, textAlign: 'right' }}>
              <Button type="primary" onClick={() => openCellsModal(detail)}>
                查看用例执行结果
              </Button>
            </div>
          </>
        ) : (
          <Empty description="任务详情为空" />
        )}
      </Modal>

      {/* Trace 弹窗 */}
      <Modal
        title={traceTitle}
        open={traceVisible}
        onCancel={() => {
          setTraceVisible(false);
          setTrace(null);
        }}
        footer={null}
        width={900}
        styles={{
          body: { maxHeight: '70vh', overflow: 'auto', padding: 16 },
        }}
      >
        {traceLoading ? (
          <Spin />
        ) : trace ? (
          <>
            <Descriptions size="small" bordered column={2} className={styles.traceMeta}>
              <Descriptions.Item label="Session ID">{trace.sessionId || '--'}</Descriptions.Item>
              <Descriptions.Item label="状态">
                <Tag color={trace.status === 'completed' ? 'green' : 'red'}>{trace.status}</Tag>
              </Descriptions.Item>
              <Descriptions.Item label="最后轮次">{trace.lastRoundId || '--'}</Descriptions.Item>
              <Descriptions.Item label="开始">{trace.createdAt || '--'}</Descriptions.Item>
              <Descriptions.Item label="完成">{trace.finishedAt || '--'}</Descriptions.Item>
              {trace.pipeline ? (
                <Descriptions.Item label="Pipeline" span={2}>
                  <Tag color="purple">{trace.pipeline.pipeline}</Tag>
                  <span style={{ marginLeft: 8 }}>execId: {trace.pipeline.execId}</span>
                  <Tag style={{ marginLeft: 8 }}>{trace.pipeline.status}</Tag>
                </Descriptions.Item>
              ) : null}
              {trace.error && (
                <Descriptions.Item label="失败原因（合并）" span={2}>
                  <Typography.Text type="danger">{trace.error}</Typography.Text>
                </Descriptions.Item>
              )}
              {trace.loopError && (
                <Descriptions.Item label="Loop 错误" span={2}>
                  <Typography.Text type="danger">{trace.loopError}</Typography.Text>
                </Descriptions.Item>
              )}
              {trace.pipelineError && (
                <Descriptions.Item label="Pipeline 错误" span={2}>
                  <Typography.Text type="danger">{trace.pipelineError}</Typography.Text>
                </Descriptions.Item>
              )}
            </Descriptions>
            {trace.pipeline?.snapshot ? (
              <div style={{ marginTop: 16 }}>
                <h4>Pipeline Snapshot</h4>
                <pre className={styles.eventPayloadThin}>
                  {JSON.stringify(trace.pipeline.snapshot, null, 2)}
                </pre>
              </div>
            ) : null}
            <div style={{ marginTop: 16 }}>
              <h4>对话消息</h4>
              {(() => {
                const items = buildTraceItems(trace.events);
                return items.length > 0 ? renderTraceItems(items) : <Empty description="无消息" />;
              })()}
            </div>
            <div style={{ marginTop: 16 }}>
              <h4>执行事件</h4>
              {trace.events?.length > 0 ? (
                renderTraceEvents(trace.events)
              ) : (
                <Empty description="无事件" />
              )}
            </div>
          </>
        ) : (
          <Empty description="Trace 数据为空" />
        )}
      </Modal>
    </div>
  );
}
