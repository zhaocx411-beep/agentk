/**
 * Override 配置弹窗
 * 编辑会话级 Profile 文档，应用时解析并同步：
 * - tools → selectedTools
 * - system → systemPrompt
 * 注意：本弹窗只负责「应用配置」，不创建会话——建会话统一由「会话配置」弹窗的「开始使用」触发
 */
import { Modal, Input } from 'antd';
import { message } from '@/utils/feedback';
import { parseProfileYaml } from '@/utils/profileYaml';
import styles from '../../AgentPlayground.module.less';

const { TextArea } = Input;

export interface OverrideApplyResult {
  /** 解析出的 tools 列表；undefined 表示文档未写 tools 字段（保持现状），空数组表示显式清空勾选 */
  tools?: string[];
  /** 解析出的 system prompt（来自文档的 system 字段） */
  systemPrompt?: string;
}

interface OverrideModalProps {
  open: boolean;
  onClose: () => void;
  overrideYaml: string;
  setOverrideYaml: (yaml: string) => void;
  /** 应用配置（同步勾选项与 overrideYaml），不创建会话 */
  onApply: (result: OverrideApplyResult) => void;
}

function OverrideModal({
  open,
  onClose,
  overrideYaml,
  setOverrideYaml,
  onApply,
}: OverrideModalProps) {
  const handleApply = () => {
    const parsed = parseProfileYaml(overrideYaml);
    onApply({ tools: parsed.tools, systemPrompt: parsed.systemPrompt });
    message.success('已应用定制配置，将在下一段会话生效');
    onClose();
  };

  return (
    <Modal
      title="本会话能力配置（override）"
      open={open}
      onCancel={onClose}
      onOk={handleApply}
      okText="应用配置"
      cancelText="取消"
      width={860}
    >
      <div className={styles.overrideModalContent}>
        <p className={styles.overrideDesc}>
          这份 Profile 文档只作用于之后开始的新会话：随会话请求发送，不改动实例配置。可写 system /
          tools / toolDefaults / httpTools / pipelines，服务端整份校验，出现文档之外的字段会被拒绝。
          清空并应用即恢复默认配置。应用后勾选项会同步到「会话配置」弹窗，点「开始使用」时生效。
        </p>
        <TextArea
          value={overrideYaml}
          onChange={(e) => setOverrideYaml(e.target.value)}
          autoSize={{ minRows: 10, maxRows: 20 }}
          className={styles.overrideEditor}
        />
      </div>
    </Modal>
  );
}

export default OverrideModal;
