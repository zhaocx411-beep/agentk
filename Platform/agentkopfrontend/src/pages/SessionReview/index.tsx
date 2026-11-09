import { useState, useCallback, useEffect, useMemo, useRef } from 'react';
import { Form, Input, InputNumber, DatePicker, Button, Space, Tag, Modal, Alert } from 'antd';
import dayjs from 'dayjs';
import { message } from '@/utils/feedback';
import { SearchOutlined, ReloadOutlined } from '@ant-design/icons';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { RiHistoryLine } from 'react-icons/ri';
import PageContainer from '@/components/PageContainer';
import DataTable from '@/components/DataTable';
import { queryAdminSessions } from './api';
import type { SessionReviewRecord, SessionReviewQueryParams } from './types';
import { getSessionColumns } from './columns';
import { TIME_FORMAT } from './constants';
import { parseSessionReviewSearch, serializeSessionReviewSearch } from './listSearchParams';
import styles from './SessionReview.module.less';

const { RangePicker } = DatePicker;

/**
 * 数据回看页面（管理员视角）
 * 检索全量 Agent Session，查看会话配置快照、运行状态与对话内容
 */
function SessionReview() {
  const navigate = useNavigate();
  const location = useLocation();
  const [, setSearchParams] = useSearchParams();
  const [form] = Form.useForm();
  const initialQueryRef = useRef(parseSessionReviewSearch(location.search));
  const initialQuery = initialQueryRef.current;
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<SessionReviewRecord[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(initialQuery.page);
  const [pageSize, setPageSize] = useState(initialQuery.pageSize);
  const [profileModalRecord, setProfileModalRecord] = useState<SessionReviewRecord | null>(null);

  /** 由表单值组装查询参数 */
  const buildParams = useCallback(
    (targetPage: number, targetPageSize: number): SessionReviewQueryParams => {
      const values = form.getFieldsValue();
      const [createdFrom, createdTo] = values.timeRange || [];
      return {
        page: targetPage,
        pageSize: targetPageSize,
        userId: values.userId?.trim() || undefined,
        status: values.status?.trim() || undefined,
        agentLoopId: values.agentLoopId ?? undefined,
        agentLoopVersionId: values.agentLoopVersionId ?? undefined,
        workflowId: values.workflowId ?? undefined,
        workflowVersionId: values.workflowVersionId ?? undefined,
        createdFrom: createdFrom?.format(TIME_FORMAT),
        createdTo: createdTo?.format(TIME_FORMAT),
      };
    },
    [form],
  );

  const doSearch = useCallback(
    async (targetPage: number, targetPageSize: number) => {
      const params = buildParams(targetPage, targetPageSize);
      setSearchParams(serializeSessionReviewSearch(params), { replace: true });
      try {
        setLoading(true);
        const res = await queryAdminSessions(params);
        setData(res?.records || []);
        setTotal(res?.total || 0);
        setPage(res?.page || targetPage);
        setPageSize(res?.pageSize || targetPageSize);
      } catch {
        message.error('查询失败');
      } finally {
        setLoading(false);
      }
    },
    [buildParams, setSearchParams],
  );

  // 首次挂载自动查询（ref 防 StrictMode double-mount）
  const initRef = useRef(false);
  useEffect(() => {
    if (initRef.current) return;
    initRef.current = true;
    doSearch(initialQuery.page, initialQuery.pageSize);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleSearch = useCallback(() => doSearch(1, pageSize), [doSearch, pageSize]);

  const handleReset = useCallback(() => {
    form.setFieldsValue({
      timeRange: undefined,
      userId: undefined,
      status: undefined,
      agentLoopId: undefined,
      agentLoopVersionId: undefined,
      workflowId: undefined,
      workflowVersionId: undefined,
    });
    doSearch(1, pageSize);
  }, [form, doSearch, pageSize]);

  const handlePageChange = useCallback(
    (newPage: number, newPageSize?: number) => {
      doSearch(newPage, newPageSize || pageSize);
    },
    [doSearch, pageSize],
  );

  const columns = useMemo(
    () =>
      getSessionColumns(setProfileModalRecord, (record) => {
        if (!record.agentkSessionId) return;
        navigate(`/session-review/${encodeURIComponent(record.agentkSessionId)}`, {
          state: {
            record,
            returnTo: `${location.pathname}${location.search}`,
          },
        });
      }),
    [location.pathname, location.search, navigate],
  );

  /** 配置快照展示文本：兼容 YAML 字符串与 JSON object 两种返回形态 */
  const profileSnapshotText = useMemo(() => {
    const raw = profileModalRecord?.profileYaml;
    if (!raw) return '暂无配置快照';
    if (typeof raw === 'string') return raw;
    return JSON.stringify(raw, null, 2);
  }, [profileModalRecord]);

  return (
    <PageContainer
      title="数据回看"
      extra={
        <Button icon={<ReloadOutlined />} onClick={handleSearch} loading={loading}>
          刷新
        </Button>
      }
    >
      {/* 页面描述 */}
      <div className={styles.subtitle}>
        <RiHistoryLine className={styles.subtitleIcon} />
        检索全量 Agent 会话，回看配置快照、运行状态与对话内容
      </div>

      {/* 搜索筛选区 */}
      <div className={styles.searchSection}>
        <Form
          form={form}
          layout="inline"
          className={styles.searchForm}
          initialValues={{
            timeRange:
              initialQuery.createdFrom || initialQuery.createdTo
                ? [
                    initialQuery.createdFrom ? dayjs(initialQuery.createdFrom) : null,
                    initialQuery.createdTo ? dayjs(initialQuery.createdTo) : null,
                  ]
                : undefined,
            userId: initialQuery.userId,
            status: initialQuery.status,
            agentLoopId: initialQuery.agentLoopId,
            agentLoopVersionId: initialQuery.agentLoopVersionId,
            workflowId: initialQuery.workflowId,
            workflowVersionId: initialQuery.workflowVersionId,
          }}
        >
          <Form.Item name="timeRange" className={styles.formItemWide}>
            <RangePicker
              showTime
              format={TIME_FORMAT}
              placeholder={['创建时间起', '创建时间止']}
              className={styles.picker}
            />
          </Form.Item>
          <Form.Item name="userId">
            <Input placeholder="用户 ID" allowClear className={styles.input} />
          </Form.Item>
          <Form.Item name="status">
            <Input placeholder="状态（如 active / failed）" allowClear className={styles.input} />
          </Form.Item>
          <Form.Item name="agentLoopId">
            <InputNumber placeholder="AgentLoop ID" min={1} className={styles.numberInput} />
          </Form.Item>
          <Form.Item name="agentLoopVersionId">
            <InputNumber placeholder="AgentLoop 版本 ID" min={1} className={styles.numberInput} />
          </Form.Item>
          <Form.Item name="workflowId">
            <InputNumber placeholder="Workflow ID" min={1} className={styles.numberInput} />
          </Form.Item>
          <Form.Item name="workflowVersionId">
            <InputNumber placeholder="Workflow 版本 ID" min={1} className={styles.numberInput} />
          </Form.Item>
          <Form.Item>
            <Space>
              <Button
                type="primary"
                icon={<SearchOutlined />}
                onClick={handleSearch}
                loading={loading}
              >
                搜索
              </Button>
              <Button icon={<ReloadOutlined />} onClick={handleReset}>
                清空
              </Button>
            </Space>
          </Form.Item>
        </Form>
      </div>

      {/* 结果头部 */}
      <div className={styles.resultsHeader}>
        <Space>
          <span className={styles.resultsTitle}>查询结果</span>
          <Tag color={total > 0 ? 'green' : 'default'}>
            {loading ? '查询中...' : `${total} 条记录`}
          </Tag>
        </Space>
      </div>

      {/* Session 列表 */}
      <DataTable<SessionReviewRecord>
        rowKey="id"
        columns={columns}
        dataSource={data}
        loading={loading}
        pagination={{
          current: page,
          pageSize,
          total,
          onChange: handlePageChange,
        }}
      />

      {/* Profile 配置快照弹窗 */}
      <Modal
        title={
          profileModalRecord
            ? `会话配置 - ${profileModalRecord.title || profileModalRecord.publicId}`
            : '会话配置'
        }
        open={!!profileModalRecord}
        onCancel={() => setProfileModalRecord(null)}
        footer={null}
        width={800}
      >
        {profileModalRecord?.rejectReason && (
          <Alert
            type="error"
            showIcon
            message="会话被拒绝"
            description={profileModalRecord.rejectReason}
            className={styles.profileRejectAlert}
          />
        )}
        <pre className={styles.profileYaml}>{profileSnapshotText}</pre>
      </Modal>
    </PageContainer>
  );
}

export default SessionReview;
