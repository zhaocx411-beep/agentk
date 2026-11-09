/**
 * 保存为版本草稿弹窗
 * 把当前调试台这份 profile YAML 存到已有 Agent 下（新建 Agent 已拆分为独立入口 NewAgentModal）
 */
import { useEffect, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Modal, Select, Input } from 'antd';
import { message } from '@/utils/feedback';
import {
  AGENT_LOOPS_QUERY_KEY,
  agentLoopVersionsKey,
  getAgentLoops,
  saveVersionDraft,
} from '@/services/agentLoop';
import { profileYamlToSessionProfile, tabsToSpaces } from '@/utils/profileYaml';
import styles from '../../AgentPlayground.module.less';

const { TextArea } = Input;

interface SaveAgentModalProps {
  open: boolean;
  /** 打开时预填的 YAML（当前调试台生效的配置） */
  defaultYaml: string;
  onClose: () => void;
}

function SaveAgentModal({ open, defaultYaml, onClose }: SaveAgentModalProps) {
  const queryClient = useQueryClient();
  const [agentId, setAgentId] = useState<number | null>(null);
  const [yaml, setYaml] = useState('');
  const [sourceLabel, setSourceLabel] = useState('');
  const [saving, setSaving] = useState(false);

  const { data: loops = [] } = useQuery({
    queryKey: AGENT_LOOPS_QUERY_KEY,
    queryFn: getAgentLoops,
  });

  // 每次打开时重置状态：默认选中第一个已有 Agent
  useEffect(() => {
    if (!open) return;
    setYaml(defaultYaml);
    setSourceLabel('');
    if (loops.length > 0) {
      setAgentId((prev) => prev ?? loops[0].id);
    }
  }, [open, defaultYaml, loops]);

  const handleOk = async () => {
    if (!yaml.trim()) {
      message.warning('profileYaml 不能为空');
      return;
    }
    if (agentId === null) {
      message.warning('请选择要保存到的 Agent');
      return;
    }

    setSaving(true);
    try {
      const version = await saveVersionDraft(agentId, {
        // 接口字段名沿用 profileYaml，但值必须是 Agent-k Profile JSON object。
        profileYaml: profileYamlToSessionProfile(tabsToSpaces(yaml)),
        sourceLabel: sourceLabel.trim() || undefined,
      });
      message.success(`草稿 v${version.version} 已保存，可在「版本管理」中发布`);
      queryClient.invalidateQueries({ queryKey: AGENT_LOOPS_QUERY_KEY });
      queryClient.invalidateQueries({ queryKey: agentLoopVersionsKey(agentId) });
      onClose();
    } catch {
      // 错误提示由 request 拦截器统一处理
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      title="保存为版本草稿"
      open={open}
      onOk={handleOk}
      onCancel={onClose}
      okText="保存草稿"
      cancelText="取消"
      confirmLoading={saving}
      width={860}
      destroyOnHidden
    >
      <div className={styles.saveAgentForm}>
        <div className={styles.saveAgentField}>
          <span className={`${styles.saveAgentLabel} ${styles.required}`}>保存到 Agent</span>
          <Select
            value={agentId}
            onChange={setAgentId}
            placeholder="选择要挂载版本的 Agent"
            popupMatchSelectWidth={false}
            options={loops.map((l) => ({
              label: `${l.name}${l.latestVersion > 0 ? `（线上 v${l.latestVersion}）` : '（未发布）'}`,
              value: l.id,
            }))}
            className={styles.select}
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

export default SaveAgentModal;
