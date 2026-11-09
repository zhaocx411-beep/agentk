/**
 * 数据集管理页（二级菜单「批量跑数 / 数据集管理」）
 * 原「离线跑数」页内的数据集 Tab，拆为独立路由 /offline-run/datasets
 * 负责数据集的上传、筛选、编辑元信息与删除，用例明细交给 DatasetTaskModal
 */
import { useState, useCallback, useEffect, useRef } from 'react';
import { Input, Button, Upload, Modal, Select, Form, Space } from 'antd';
import { message } from '@/utils/feedback';
import { UploadOutlined, InboxOutlined, FileAddOutlined } from '@ant-design/icons';
import { RiFlaskLine } from 'react-icons/ri';
import PageContainer from '@/components/PageContainer';
import DataTable from '@/components/DataTable';
import { useUserStore, isSuperAdminUser } from '@/store/user';
import { queryDatasets, uploadDataset, updateDataset, deleteDataset, downloadDataset } from './api';
import { DATASET_TYPE_OPTIONS } from './types';
import type { Dataset, DatasetExportFormat, DatasetType } from './types';
import { getDatasetColumns } from './columns';
import DatasetTaskModal from './components/DatasetTaskModal';
import DatasetTemplateUploadModal from './components/DatasetTemplateUploadModal';
import styles from './OfflineRun.module.less';

interface DatasetFilters {
  name?: string;
  datasetType?: DatasetType;
  creator?: string;
}

interface DatasetUploadFormValues {
  name: string;
  version?: string;
  datasetType: DatasetType;
  taskInstruction?: string;
  description?: string;
}

const DATASET_FILE_PATTERN = /\.(ya?ml|json|xlsx)$/i;

function isXlsxFile(file: File | null): boolean {
  if (!file) return false;
  return file.name.toLowerCase().endsWith('.xlsx');
}

function validateDatasetFile(file: File): string | undefined {
  if (!DATASET_FILE_PATTERN.test(file.name)) {
    return '仅支持 .yaml、.yml、.json 和 .xlsx 文件';
  }
  return undefined;
}

function DatasetManage() {
  const userInfo = useUserStore((state) => state.userInfo);
  const canUploadTemplate = isSuperAdminUser(userInfo);

  const [loading, setLoading] = useState(false);
  const [datasets, setDatasets] = useState<Dataset[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);

  const [filterName, setFilterName] = useState('');
  const [filterType, setFilterType] = useState<DatasetType | undefined>(undefined);
  const [filterCreator, setFilterCreator] = useState('');
  const [appliedFilters, setAppliedFilters] = useState<DatasetFilters>({});

  const [uploadModalVisible, setUploadModalVisible] = useState(false);
  const [templateUploadVisible, setTemplateUploadVisible] = useState(false);
  const [uploadFile, setUploadFile] = useState<File | null>(null);
  const [uploading, setUploading] = useState(false);
  const [uploadForm] = Form.useForm<DatasetUploadFormValues>();

  const [editingDataset, setEditingDataset] = useState<Dataset | null>(null);
  const [editSaving, setEditSaving] = useState(false);
  const [editForm] = Form.useForm<{ datasetType?: DatasetType; description?: string }>();

  const [taskDataset, setTaskDataset] = useState<Dataset | null>(null);

  const loadDatasets = useCallback(
    async (current = page, size = pageSize, filters = appliedFilters) => {
      try {
        setLoading(true);
        const res = await queryDatasets({
          usage: 'EVAL',
          current,
          pageSize: size,
          ...filters,
        });
        setDatasets(res?.records || []);
        setTotal(res?.total || 0);
        setPage(res?.current || 1);
        setPageSize(res?.pageSize || 10);
      } catch {
        message.error('加载数据集列表失败');
      } finally {
        setLoading(false);
      }
    },
    [page, pageSize, appliedFilters],
  );

  const initRef = useRef(false);
  useEffect(() => {
    if (initRef.current) return;
    initRef.current = true;
    loadDatasets(1, pageSize);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleSearch = useCallback(() => {
    const filters: DatasetFilters = {
      name: filterName || undefined,
      datasetType: filterType,
      creator: filterCreator || undefined,
    };
    setAppliedFilters(filters);
    loadDatasets(1, pageSize, filters);
  }, [filterName, filterType, filterCreator, pageSize, loadDatasets]);

  const handleResetFilter = useCallback(() => {
    setFilterName('');
    setFilterType(undefined);
    setFilterCreator('');
    setAppliedFilters({});
    loadDatasets(1, pageSize, {});
  }, [pageSize, loadDatasets]);

  const handlePageChange = useCallback(
    (nextPage: number, nextPageSize?: number) => {
      const size = nextPageSize || pageSize;
      setPage(nextPage);
      setPageSize(size);
      loadDatasets(nextPage, size);
    },
    [pageSize, loadDatasets],
  );

  const resetUploadForm = useCallback(() => {
    setUploadFile(null);
    uploadForm.resetFields();
  }, [uploadForm]);

  const handleUpload = useCallback(async () => {
    if (!uploadFile) {
      message.warning('请选择数据集文件');
      return;
    }
    const validationError = validateDatasetFile(uploadFile);
    if (validationError) {
      message.error(validationError);
      return;
    }
    try {
      const values = await uploadForm.validateFields();
      setUploading(true);
      // 本页只上传离线评测数据集，usage 固定 EVAL，不暴露 pipelineName
      await uploadDataset({
        file: uploadFile,
        datasetType: values.datasetType,
        name: values.name.trim(),
        version: values.version?.trim() || undefined,
        description: values.description?.trim() || undefined,
        usage: 'EVAL',
        taskInstruction: values.taskInstruction?.trim() || undefined,
      });
      message.success('上传成功');
      setUploadModalVisible(false);
      resetUploadForm();
      loadDatasets(1, pageSize);
    } catch (err: unknown) {
      if (err instanceof Error) {
        message.error(err.message || '上传失败');
      }
    } finally {
      setUploading(false);
    }
  }, [uploadFile, uploadForm, pageSize, loadDatasets, resetUploadForm]);

  const handleOpenEdit = useCallback(
    (dataset: Dataset) => {
      setEditingDataset(dataset);
      editForm.setFieldsValue({
        datasetType: dataset.datasetType,
        description: dataset.description || undefined,
      });
    },
    [editForm],
  );

  const handleEditSubmit = useCallback(async () => {
    if (!editingDataset) return;
    try {
      const values = await editForm.validateFields();
      const description = values.description?.trim();
      const params = {
        datasetType: values.datasetType,
        ...(description ? { description } : {}),
      };
      setEditSaving(true);
      const updated = await updateDataset(editingDataset.id, params);
      message.success('更新成功');
      setEditingDataset(null);
      setDatasets((prev) => prev.map((item) => (item.id === updated.id ? updated : item)));
    } catch (err: unknown) {
      if (err instanceof Error) {
        message.error(err.message || '更新失败');
      }
    } finally {
      setEditSaving(false);
    }
  }, [editingDataset, editForm]);

  const handleDelete = useCallback(
    async (id: number) => {
      try {
        await deleteDataset(id);
        message.success('删除成功');
        loadDatasets(page, pageSize);
      } catch {
        // 拦截器已提示具体原因（如数据集被离线任务引用）
      }
    },
    [page, pageSize, loadDatasets],
  );

  const handleDownload = useCallback((dataset: Dataset, format: DatasetExportFormat) => {
    downloadDataset(dataset.id, dataset.name, dataset.version, format);
  }, []);

  const columns = getDatasetColumns({
    onViewTasks: setTaskDataset,
    onEdit: handleOpenEdit,
    onDownload: handleDownload,
    onDelete: (id: number) => handleDelete(id),
  });

  return (
    <PageContainer
      title="数据集管理"
      extra={
        <Space wrap>
          {canUploadTemplate ? (
            <Button icon={<FileAddOutlined />} onClick={() => setTemplateUploadVisible(true)}>
              上传素材/模版
            </Button>
          ) : null}
          <Button
            type="primary"
            icon={<UploadOutlined />}
            onClick={() => setUploadModalVisible(true)}
          >
            上传数据集
          </Button>
        </Space>
      }
    >
      <div className={styles.subtitle}>
        <RiFlaskLine className={styles.subtitleIcon} />
        管理离线评测使用的数据集及其用例
      </div>

      <div className={styles.filterBar}>
        <Input
          placeholder="名称（精确匹配）"
          value={filterName}
          onChange={(e) => setFilterName(e.target.value)}
          onPressEnter={handleSearch}
          allowClear
          className={styles.filterInput}
        />
        <Select
          placeholder="类型"
          options={DATASET_TYPE_OPTIONS}
          value={filterType}
          onChange={(value) => setFilterType(value)}
          allowClear
          className={styles.filterSelect}
        />
        <Input
          placeholder="创建人（精确匹配）"
          value={filterCreator}
          onChange={(e) => setFilterCreator(e.target.value)}
          onPressEnter={handleSearch}
          allowClear
          className={styles.filterInput}
        />
        <Button type="primary" onClick={handleSearch}>
          查询
        </Button>
        <Button onClick={handleResetFilter}>重置</Button>
      </div>

      <DataTable<Dataset>
        className={styles.datasetTable}
        rowKey="id"
        columns={columns}
        dataSource={datasets}
        loading={loading}
        scroll={{ x: 1440 }}
        pagination={{ current: page, pageSize, total, onChange: handlePageChange }}
      />

      <Modal
        title="上传数据集"
        open={uploadModalVisible}
        onOk={() => void handleUpload()}
        onCancel={() => {
          setUploadModalVisible(false);
          resetUploadForm();
        }}
        confirmLoading={uploading}
        okText="上传"
        cancelText="取消"
        width={560}
        centered
        destroyOnHidden
        className={styles.uploadModal}
      >
        <Form form={uploadForm} layout="vertical" preserve={false}>
          <Form.Item label="数据集文件" required>
            <div className={styles.uploadArea}>
              <Upload.Dragger
                accept=".yaml,.yml,.json,.xlsx"
                maxCount={1}
                beforeUpload={(file) => {
                  const validationError = validateDatasetFile(file);
                  if (validationError) {
                    message.error(validationError);
                    return false;
                  }
                  setUploadFile(file);
                  return false;
                }}
                onRemove={() => {
                  setUploadFile(null);
                }}
                fileList={uploadFile ? [{ uid: '1', name: uploadFile.name, status: 'done' }] : []}
              >
                <p className="ant-upload-drag-icon">
                  <InboxOutlined />
                </p>
                <p className="ant-upload-text">点击或拖拽数据集文件到此处上传</p>
                <p className="ant-upload-hint">支持 .yaml / .yml / .json / .xlsx</p>
              </Upload.Dragger>
            </div>
          </Form.Item>
          <Form.Item
            name="name"
            label="数据集名称"
            rules={[{ required: true, message: '请填写数据集名称' }]}
          >
            <Input placeholder="请输入数据集名称" maxLength={128} />
          </Form.Item>
          <Form.Item
            name="version"
            label="版本号"
            extra="可选；YAML / JSON 非空时覆盖文件内版本，XLSX 默认 v1"
          >
            <Input placeholder="请输入版本号" maxLength={64} />
          </Form.Item>
          <Form.Item
            name="datasetType"
            label="数据集类型"
            rules={[{ required: true, message: '请选择数据集类型' }]}
          >
            <Select placeholder="请选择数据集类型" options={DATASET_TYPE_OPTIONS} />
          </Form.Item>
          <Form.Item
            name="taskInstruction"
            label="任务指令"
            extra="XLSX 文件必填，YAML / JSON 文件可选"
            rules={[
              {
                required: isXlsxFile(uploadFile),
                message: '上传 XLSX 时请填写任务指令',
              },
            ]}
          >
            <Input.TextArea
              placeholder="请输入任务指令"
              rows={3}
              className={styles.uploadTextarea}
            />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea
              placeholder="可选，最多 200 字"
              rows={3}
              maxLength={200}
              showCount={{ formatter: ({ count, maxLength }) => `${count} / ${maxLength}` }}
              className={styles.uploadTextarea}
            />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={`编辑数据集 - ${editingDataset ? `${editingDataset.name} (${editingDataset.version})` : ''}`}
        open={editingDataset !== null}
        onOk={handleEditSubmit}
        onCancel={() => setEditingDataset(null)}
        confirmLoading={editSaving}
        okText="保存"
        cancelText="取消"
        destroyOnHidden
      >
        <Form form={editForm} layout="vertical" preserve={false}>
          <Form.Item
            name="datasetType"
            label="数据集类型"
            rules={[{ required: true, message: '请选择数据集类型' }]}
          >
            <Select placeholder="请选择数据集类型" options={DATASET_TYPE_OPTIONS} />
          </Form.Item>
          <Form.Item name="description" label="描述" extra="名称与版本由上传文件决定，不可修改">
            <Input.TextArea
              rows={3}
              placeholder="请输入数据集描述"
              maxLength={200}
              showCount={{ formatter: ({ count, maxLength }) => `${count} / ${maxLength}` }}
            />
          </Form.Item>
        </Form>
      </Modal>

      <DatasetTemplateUploadModal
        open={templateUploadVisible}
        onClose={() => setTemplateUploadVisible(false)}
        onSuccess={() => undefined}
      />

      <DatasetTaskModal
        dataset={taskDataset}
        onClose={() => setTaskDataset(null)}
        onTasksChanged={() => loadDatasets(page, pageSize)}
      />
    </PageContainer>
  );
}

export default DatasetManage;
