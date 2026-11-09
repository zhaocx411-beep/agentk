/**
 * 基于官方模版首次保存：填写 Agent 名称后再创建实体
 */
import { useEffect } from 'react';
import { Form, Input, Modal } from 'antd';
import styles from '../AgentDetail.module.less';

interface SaveOfficialAgentNameModalProps {
  open: boolean;
  confirming: boolean;
  defaultName: string;
  defaultDescription?: string;
  onCancel: () => void;
  onConfirm: (name: string, description: string) => void;
}

interface FormValues {
  name: string;
  description?: string;
}

function SaveOfficialAgentNameModal({
  open,
  confirming,
  defaultName,
  defaultDescription = '',
  onCancel,
  onConfirm,
}: SaveOfficialAgentNameModalProps) {
  const [form] = Form.useForm<FormValues>();

  useEffect(() => {
    if (!open) return;
    // 首次保存强制用户确认/填写名称：默认留空，官方名仅作占位提示
    form.setFieldsValue({
      name: '',
      description: defaultDescription,
    });
  }, [open, defaultDescription, form]);

  return (
    <Modal
      open={open}
      title="保存为我的 Agent"
      okText="保存草稿"
      cancelText="取消"
      confirmLoading={confirming}
      onCancel={onCancel}
      onOk={() => {
        void form.validateFields().then((values) => {
          onConfirm(values.name.trim(), values.description?.trim() || '');
        });
      }}
      destroyOnHidden
    >
      <p className={styles.saveNameHint}>
        请先填写 Agent 名称，确认后才会创建我的 Agent 并写入草稿。
      </p>
      <Form form={form} layout="vertical" requiredMark={false}>
        <Form.Item
          name="name"
          label="Agent 名称"
          rules={[
            { required: true, whitespace: true, message: '请填写 Agent 名称' },
            { max: 64, message: '名称不超过 64 字' },
          ]}
        >
          <Input
            placeholder={defaultName ? `例如：${defaultName}` : '请输入名称'}
            maxLength={64}
            allowClear
          />
        </Form.Item>
        <Form.Item
          name="description"
          label="描述（可选）"
          rules={[{ max: 200, message: '描述不超过 200 字' }]}
        >
          <Input.TextArea placeholder="可选，补充说明" maxLength={200} rows={3} allowClear />
        </Form.Item>
      </Form>
    </Modal>
  );
}

export { SaveOfficialAgentNameModal };
