/**
 * 数据集用例弹窗
 * 承载某个数据集下的用例列表与增删改，自带分页与表单状态，
 * 用例数变化后通过 onTasksChanged 通知外层刷新数据集列表的「用例数」列
 */
import { useCallback, useEffect, useState } from 'react';
import { Button, Form, Input, Modal, Table, Typography } from 'antd';
import { message } from '@/utils/feedback';
import { PlusOutlined } from '@ant-design/icons';
import { createDatasetTask, deleteDatasetTask, queryDatasetTasks, updateDatasetTask } from '../api';
import { getTaskColumns } from '../columns';
import type { Dataset, DatasetTask } from '../types';
import styles from '../OfflineRun.module.less';

interface DatasetTaskModalProps {
  /** 当前查看用例的数据集，为 null 时弹窗关闭 */
  dataset: Dataset | null;
  onClose: () => void;
  /** 用例增删后回调，用于同步外层数据集列表 */
  onTasksChanged: () => void;
}

interface TaskFormValues {
  taskKey?: string;
  prompt: string;
  linkUrl?: string;
}

const DEFAULT_PAGE_SIZE = 10;

function formatExtraFields(extra?: Record<string, unknown>): string {
  if (!extra || Object.keys(extra).length === 0) return '';
  try {
    return JSON.stringify(extra, null, 2);
  } catch {
    return '';
  }
}

function DatasetTaskModal({ dataset, onClose, onTasksChanged }: DatasetTaskModalProps) {
  const [tasks, setTasks] = useState<DatasetTask[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE);
  const [loading, setLoading] = useState(false);

  const [editVisible, setEditVisible] = useState(false);
  const [editingTask, setEditingTask] = useState<DatasetTask | null>(null);
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm<TaskFormValues>();

  const datasetId = dataset?.id ?? null;

  const loadTasks = useCallback(
    async (current: number, size: number) => {
      if (datasetId == null) return;
      try {
        setLoading(true);
        const res = await queryDatasetTasks({ datasetId, current, pageSize: size });
        setTasks(res?.records || []);
        setTotal(res?.total || 0);
        setPage(res?.current || current);
        setPageSize(res?.pageSize || size);
      } catch {
        message.error('加载用例列表失败');
        setTasks([]);
        setTotal(0);
      } finally {
        setLoading(false);
      }
    },
    [datasetId],
  );

  // 打开或切换数据集时回到第一页重新拉取
  useEffect(() => {
    if (datasetId == null) return;
    loadTasks(1, DEFAULT_PAGE_SIZE);
  }, [datasetId, loadTasks]);

  const handlePageChange = useCallback(
    (nextPage: number, nextPageSize?: number) => {
      loadTasks(nextPage, nextPageSize || pageSize);
    },
    [loadTasks, pageSize],
  );

  const handleAdd = useCallback(() => {
    setEditingTask(null);
    setEditVisible(true);
  }, []);

  const handleEdit = useCallback((task: DatasetTask) => {
    setEditingTask(task);
    setEditVisible(true);
  }, []);

  const handleSubmit = useCallback(async () => {
    if (datasetId == null) return;
    let values: TaskFormValues;
    try {
      values = await form.validateFields();
    } catch {
      // 表单校验失败由 antd 自行提示
      return;
    }
    try {
      setSaving(true);
      if (editingTask) {
        // 编辑：prompt 与 linkUrl 全量提交，linkUrl 传空字符串表示清空
        await updateDatasetTask(editingTask.id, {
          prompt: values.prompt.trim(),
          linkUrl: values.linkUrl?.trim() || '',
        });
        message.success('用例已更新');
      } else {
        await createDatasetTask(datasetId, {
          taskKey: values.taskKey!.trim(),
          prompt: values.prompt.trim(),
          ...(values.linkUrl?.trim() ? { linkUrl: values.linkUrl.trim() } : {}),
        });
        message.success('用例已添加');
      }
      setEditVisible(false);
      setEditingTask(null);
      loadTasks(page, pageSize);
      onTasksChanged();
    } catch {
      // 接口错误已由请求拦截器统一提示
    } finally {
      setSaving(false);
    }
  }, [datasetId, editingTask, form, loadTasks, page, pageSize, onTasksChanged]);

  const handleDelete = useCallback(
    async (task: DatasetTask) => {
      if (datasetId == null) return;
      try {
        await deleteDatasetTask(task.id);
        message.success('用例已删除');
        // 删除后当前页若已空则回退一页
        const nextPage = tasks.length <= 1 && page > 1 ? page - 1 : page;
        loadTasks(nextPage, pageSize);
        onTasksChanged();
      } catch {
        // 仅剩最后一条时后端返回 412，拦截器已提示原因
      }
    },
    [datasetId, tasks.length, page, pageSize, loadTasks, onTasksChanged],
  );

  const columns = getTaskColumns({ onEdit: handleEdit, onDelete: handleDelete });
  const referImages = editingTask?.referImages?.filter(Boolean) ?? [];
  const extraFieldsText = formatExtraFields(editingTask?.extraFields);
  // Form 用 key + initialValues 挂载时即回填；避免 destroyOnHidden 下 setFieldsValue 打空
  const formKey = editingTask ? `edit-${editingTask.id}` : 'create';
  const formInitialValues: Partial<TaskFormValues> | undefined = editingTask
    ? {
        prompt: editingTask.prompt ?? '',
        linkUrl: editingTask.linkUrl || undefined,
      }
    : undefined;

  return (
    <>
      <Modal
        className={styles.taskModal}
        title={dataset ? `${dataset.name} (${dataset.version}) - 用例列表` : ''}
        open={dataset !== null}
        onCancel={onClose}
        footer={null}
        width={1100}
      >
        <div className={styles.taskModalToolbar}>
          <Button type="primary" icon={<PlusOutlined />} onClick={handleAdd}>
            新增用例
          </Button>
        </div>
        <Table
          rowKey="id"
          columns={columns}
          dataSource={tasks}
          loading={loading}
          pagination={{ current: page, pageSize, total, onChange: handlePageChange }}
          size="small"
          scroll={{ x: 1400 }}
          tableLayout="fixed"
        />
      </Modal>

      {/* 用例新增 / 编辑弹窗 */}
      <Modal
        classNames={{ body: styles.taskEditModalBody }}
        title={editingTask ? `编辑用例 - ${editingTask.taskKey}` : '新增用例'}
        open={editVisible}
        onOk={handleSubmit}
        onCancel={() => {
          setEditVisible(false);
          setEditingTask(null);
        }}
        afterOpenChange={(open) => {
          if (!open) return;
          // 动画结束后再写一次，兜底 Form 晚挂载导致的空表
          if (editingTask) {
            form.setFieldsValue({
              prompt: editingTask.prompt ?? '',
              linkUrl: editingTask.linkUrl || undefined,
            });
          } else {
            form.resetFields();
          }
        }}
        confirmLoading={saving}
        okText="保存"
        cancelText="取消"
        destroyOnHidden
        width={640}
      >
        <Form
          key={formKey}
          form={form}
          layout="vertical"
          preserve={false}
          initialValues={formInitialValues}
        >
          {!editingTask && (
            <Form.Item
              name="taskKey"
              label="用例标识"
              extra="同数据集内唯一，创建后不可修改"
              rules={[{ required: true, message: '请输入用例标识' }]}
            >
              <Input placeholder="如 hello / time-query" maxLength={128} />
            </Form.Item>
          )}
          <Form.Item
            name="prompt"
            label="Prompt"
            rules={[{ required: true, message: '请输入 Prompt' }]}
          >
            <Input.TextArea rows={6} placeholder="请输入用户输入 prompt" />
          </Form.Item>
          <Form.Item name="linkUrl" label="外链地址（可选）">
            <Input placeholder="https://..." maxLength={1024} allowClear />
          </Form.Item>
          {editingTask ? (
            <>
              <Form.Item label="参考图 referImages" extra="由数据集文件解析，编辑接口暂不支持修改">
                {referImages.length > 0 ? (
                  <Input.TextArea
                    rows={Math.min(4, referImages.length + 1)}
                    value={referImages.join('\n')}
                    readOnly
                  />
                ) : (
                  <Typography.Text type="secondary">暂无</Typography.Text>
                )}
              </Form.Item>
              <Form.Item label="扩展字段 extraFields" extra="由数据集文件解析，编辑接口暂不支持修改">
                {extraFieldsText ? (
                  <Input.TextArea rows={6} value={extraFieldsText} readOnly />
                ) : (
                  <Typography.Text type="secondary">暂无</Typography.Text>
                )}
              </Form.Item>
            </>
          ) : null}
        </Form>
      </Modal>
    </>
  );
}

export default DatasetTaskModal;
