/**
 * 新建 Agent 弹窗
 * 创建一个全新的 AgentLoop 并把当前调试台配置存为其首个草稿版本。
 * 创建成功后通过 onCreated 回调（父级会把它设为当前会话配置）
 */
import { useEffect, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { Modal, Input } from 'antd';
import { message } from '@/utils/feedback';
import { AGENT_LOOPS_QUERY_KEY, createAgentLoop, saveVersionDraft } from '@/services/agentLoop';
import type { AgentLoop, AgentLoopVersion } from '@/types/agentLoop';
import { profileYamlToSessionProfile, tabsToSpaces } from '@/utils/profileYaml';
import styles from '../../AgentPlayground.module.less';

const { TextArea } = Input;

interface NewAgentModalProps {
  open: boolean;
  /** 打开时预填的 YAML（当前调试台生效的配置） */
  defaultYaml: string;
  onClose: () => void;
  /** 创建成功：回传新建的 Agent 与首个草稿版本 */
  onCreated: (loop: AgentLoop, version: AgentLoopVersion) => void;
}

function NewAgentModal({ open, defaultYaml, onClose, onCreated }: NewAgentModalProps) {
  const queryClient = useQueryClient();
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [sourceLabel, setSourceLabel] = useState('');
  const [yaml, setYaml] = useState('');
  const [saving, setSaving] = useState(false);

  // 每次打开时重置
  useEffect(() => {
    if (!open) return;
    setYaml(defaultYaml);
    setName('');
    setDescription('');
    setSourceLabel('');
  }, [open, defaultYaml]);

  const handleOk = async () => {
    if (!name.trim() || !description.trim()) {
      message.warning('请填写 Agent 的名称和描述');
      return;
    }
    if (!yaml.trim()) {
      message.warning('profileYaml 不能为空');
      return;
    }

    setSaving(true);
    try {
      // 先建逻辑实体，再把当前配置存为首个草稿版本
      const loop = await createAgentLoop({ name: name.trim(), description: description.trim() });
      const version = await saveVersionDraft(loop.id, {
        // 接口字段名沿用 profileYaml，但值必须是 Agent-k Profile JSON object。
        profileYaml: profileYamlToSessionProfile(tabsToSpaces(yaml)),
        sourceLabel: sourceLabel.trim() || undefined,
      });
      message.success(`Agent「${loop.name}」已创建，草稿 v${version.version} 已保存`);
      queryClient.invalidateQueries({ queryKey: AGENT_LOOPS_QUERY_KEY });
      onCreated(loop, version);
      onClose();
    } catch {
      // 错误提示由 request 拦截器统一处理
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      title="新建 Agent"
      open={open}
      onOk={handleOk}
      onCancel={onClose}
      okText="创建并使用"
      cancelText="取消"
      confirmLoading={saving}
      width={860}
      destroyOnHidden
    >
      <div className={styles.saveAgentForm}>
        <div className={styles.saveAgentField}>
          <span className={`${styles.saveAgentLabel} ${styles.required}`}>Agent 名称</span>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="profile 的名字 / agent 名字"
            maxLength={64}
          />
        </div>
        <div className={styles.saveAgentField}>
          <span className={`${styles.saveAgentLabel} ${styles.required}`}>Agent 描述</span>
          <Input
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="profile 的描述"
            maxLength={256}
          />
        </div>
        <div className={styles.saveAgentField}>
          <span className={styles.saveAgentLabel}>来源标识（sourceLabel，可选）</span>
          <Input
            value={sourceLabel}
            onChange={(e) => setSourceLabel(e.target.value)}
            placeholder="如 playground-debug，标记这份配置的来源"
            maxLength={128}
          />
        </div>
        <div className={styles.saveAgentField}>
          <span className={`${styles.saveAgentLabel} ${styles.required}`}>
            Profile 配置（YAML 编辑）
          </span>
          <TextArea
            value={yaml}
            onChange={(e) => setYaml(e.target.value)}
            autoSize={{ minRows: 10, maxRows: 18 }}
            className={styles.overrideEditor}
          />
        </div>
      </div>
    </Modal>
  );
}

export default NewAgentModal;
