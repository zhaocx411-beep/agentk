/**
 * 上传数据集素材/模版（usage=TEMPLATE，仅 .json）
 */
import { useCallback, useState } from 'react';
import { Form, Input, Modal, Select, Upload } from 'antd';
import { InboxOutlined } from '@ant-design/icons';
import { message } from '@/utils/feedback';
import { uploadDatasetTemplate } from '../api';
import { DATASET_TYPE_OPTIONS, type DatasetType } from '../types';
import styles from '../OfflineRun.module.less';

function validateTemplateFile(file: File): string | undefined {
  if (!/\.json$/i.test(file.name)) {
    return '模版文件仅支持 .json';
  }
  return undefined;
}

interface TemplateUploadFormValues {
  name: string;
  pipelineName: string;
  datasetType: DatasetType;
  version?: string;
  description?: string;
}

interface DatasetTemplateUploadModalProps {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
}

function DatasetTemplateUploadModal({ open, onClose, onSuccess }: DatasetTemplateUploadModalProps) {
  const [form] = Form.useForm<TemplateUploadFormValues>();
  const [uploadFile, setUploadFile] = useState<File | null>(null);
  const [uploading, setUploading] = useState(false);

  const resetForm = useCallback(() => {
    setUploadFile(null);
    form.resetFields();
  }, [form]);

  const handleClose = useCallback(() => {
    onClose();
    resetForm();
  }, [onClose, resetForm]);

  const handleUpload = useCallback(async () => {
    if (!uploadFile) {
      message.warning('请选择模版 JSON 文件');
      return;
    }
    const validationError = validateTemplateFile(uploadFile);
    if (validationError) {
      message.error(validationError);
      return;
    }
    try {
      const values = await form.validateFields();
      setUploading(true);
      await uploadDatasetTemplate({
        file: uploadFile,
        datasetType: values.datasetType,
        name: values.name.trim(),
        pipelineName: values.pipelineName.trim(),
        description: values.description?.trim() || undefined,
        version: values.version?.trim() || undefined,
      });
      message.success('模版上传成功');
      handleClose();
      onSuccess();
    } catch (err: unknown) {
      if (err instanceof Error) {
        message.error(err.message || '上传失败');
      }
    } finally {
      setUploading(false);
    }
  }, [uploadFile, form, handleClose, onSuccess]);

  return (
    <Modal
      title="上传数据集素材/模版"
      open={open}
      onOk={() => void handleUpload()}
      onCancel={handleClose}
      confirmLoading={uploading}
      okText="上传"
      cancelText="取消"
      width={560}
      centered
      destroyOnHidden
      className={styles.uploadModal}
    >
      <Form form={form} layout="vertical" preserve={false}>
        <Form.Item label="模版文件" required>
          <div className={styles.uploadArea}>
            <Upload.Dragger
              accept=".json"
              maxCount={1}
              beforeUpload={(file) => {
                const validationError = validateTemplateFile(file);
                if (validationError) {
                  message.error(validationError);
                  return false;
                }
                setUploadFile(file);
                return false;
              }}
              onRemove={() => setUploadFile(null)}
              fileList={uploadFile ? [{ uid: '1', name: uploadFile.name, status: 'done' }] : []}
            >
              <p className="ant-upload-drag-icon">
                <InboxOutlined />
              </p>
              <p className="ant-upload-text">点击或拖拽 JSON 模版文件到此处</p>
              <p className="ant-upload-hint">仅支持 .json</p>
            </Upload.Dragger>
          </div>
        </Form.Item>
        <Form.Item
          name="name"
          label="数据集名称"
          extra="与 version 组成唯一键"
          rules={[{ required: true, message: '请填写数据集名称' }]}
        >
          <Input placeholder="请输入数据集名称" maxLength={128} />
        </Form.Item>
        <Form.Item
          name="pipelineName"
          label="Pipeline 名称"
          rules={[{ required: true, message: '请填写 Pipeline 名称' }]}
        >
          <Input placeholder="如 generate_brand_ad" maxLength={128} />
        </Form.Item>
        <Form.Item
          name="datasetType"
          label="数据集类型"
          rules={[{ required: true, message: '请选择数据集类型' }]}
        >
          <Select placeholder="请选择数据集类型" options={DATASET_TYPE_OPTIONS} />
        </Form.Item>
        <Form.Item name="version" label="version" extra="留空则从 JSON 内提取">
          <Input placeholder="可选" maxLength={64} />
        </Form.Item>
        <Form.Item name="description" label="描述">
          <Input.TextArea
            placeholder="可选，最多 512 字"
            rows={3}
            maxLength={512}
            showCount={{ formatter: ({ count, maxLength }) => `${count} / ${maxLength}` }}
            className={styles.uploadTextarea}
          />
        </Form.Item>
      </Form>
    </Modal>
  );
}

export default DatasetTemplateUploadModal;
