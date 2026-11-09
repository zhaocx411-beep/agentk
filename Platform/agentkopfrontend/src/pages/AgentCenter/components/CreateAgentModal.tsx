/**
 * CreateAgentModal - 创建 Agent-Loop 弹窗
 * lockDismiss：自定义 Agent 入口的命名闸门，不能关掉弹窗，只能返回
 */
import { Modal, Form, Input } from 'antd';
import { message } from '@/utils/feedback';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { AGENT_LOOPS_QUERY_KEY, createAgentLoop } from '../api';
import { CREATE_AGENT_MODAL_TEXT } from '../mock';
import styles from './CreateAgentModal.module.less';

interface CreateAgentModalProps {
  open: boolean;
  onClose: () => void;
  /** 固定弹窗：禁止遮罩 / ESC / 关闭按钮，取消改为返回上一页 */
  lockDismiss?: boolean;
}

interface FormValues {
  name: string;
  description?: string;
}

function CreateAgentModal({ open, onClose, lockDismiss = false }: CreateAgentModalProps) {
  const [form] = Form.useForm<FormValues>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const copy = CREATE_AGENT_MODAL_TEXT;

  const planningMutation = useMutation({
    mutationFn: createAgentLoop,
    onSuccess: (agent) => {
      message.success(`Agent「${agent.name}」创建成功`);
      queryClient.invalidateQueries({ queryKey: AGENT_LOOPS_QUERY_KEY });
      form.resetFields();
      if (!lockDismiss) onClose();
      navigate(`/agents/${agent.id}`, { replace: lockDismiss });
    },
    onError: () => message.error('创建失败，请稍后重试'),
  });

  const handleOk = async () => {
    const values = await form.validateFields();
    planningMutation.mutate({
      name: values.name.trim(),
      description: values.description?.trim() ?? '',
    });
  };

  const handleClose = () => {
    form.resetFields();
    onClose();
  };

  return (
    <Modal
      title={copy.title}
      open={open}
      onOk={() => void handleOk()}
      onCancel={handleClose}
      confirmLoading={planningMutation.isPending}
      okText={copy.ok}
      cancelText={lockDismiss ? copy.back : copy.cancel}
      maskClosable={!lockDismiss}
      keyboard={!lockDismiss}
      closable={!lockDismiss}
      centered={lockDismiss}
      destroyOnHidden
      width={560}
    >
      <p className={styles.typeHint}>{lockDismiss ? copy.lockHint : copy.hint}</p>
      <Form form={form} layout="vertical" preserve={false}>
        <Form.Item
          name="name"
          label={copy.nameLabel}
          rules={[
            { required: true, message: copy.nameRequired },
            { whitespace: true, message: copy.nameWhitespace },
            { max: 50, message: copy.nameMax },
          ]}
        >
          <Input placeholder={copy.namePlaceholder} maxLength={50} showCount />
        </Form.Item>
        <Form.Item name="description" label={copy.descLabel} rules={[{ max: 200, message: copy.descMax }]}>
          <Input.TextArea
            placeholder={copy.descPlaceholder}
            rows={3}
            maxLength={200}
            showCount
          />
        </Form.Item>
      </Form>
    </Modal>
  );
}

export default CreateAgentModal;
