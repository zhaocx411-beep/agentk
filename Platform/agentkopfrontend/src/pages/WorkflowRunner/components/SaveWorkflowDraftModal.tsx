/**
 * 保存 Pipeline 版本草稿弹窗
 * 把 definitionJson 存到已有 Pipeline 下（version 自增，status=draft）
 */
import { useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Modal, Select, Input } from 'antd';
import { message } from '@/utils/feedback';
import {
  WORKFLOW_ENTITIES_QUERY_KEY,
  workflowVersionsKey,
  getWorkflowEntities,
  getWorkflowVersions,
  saveWorkflowVersionDraft,
} from '@/services/workflowPersistence';
import { prettyDefinitionJson } from '@/utils/workflowDefinition';
import styles from '../WorkflowRunner.module.less';

const { TextArea } = Input;

interface SaveWorkflowDraftModalProps {
  open: boolean;
  /** 打开时预填的定义 JSON（由当前选中的 Pipeline 生成骨架） */
  defaultDefinitionJson: string;
  onClose: () => void;
}

function SaveWorkflowDraftModal({
  open,
  defaultDefinitionJson,
  onClose,
}: SaveWorkflowDraftModalProps) {
  const queryClient = useQueryClient();
  const [workflowId, setWorkflowId] = useState<number | null>(null);
  const [definitionJson, setDefinitionJson] = useState('');
  const [saving, setSaving] = useState(false);

  const { data: entities = [] } = useQuery({
    queryKey: WORKFLOW_ENTITIES_QUERY_KEY,
    queryFn: getWorkflowEntities,
    // 弹窗常驻挂载，未打开时不发请求
    enabled: open,
  });

  // 选中 Pipeline 的版本列表：以其最新版本 definitionJson 为底稿预填
  const { data: versions = [] } = useQuery({
    queryKey: workflowVersionsKey(workflowId ?? -1),
    queryFn: () => getWorkflowVersions(workflowId as number),
    enabled: open && workflowId !== null,
  });

  /** 最近一次应用的默认内容：默认定义晚到（profile 异步拉取）时，仅在用户未编辑过时替换 */
  const lastDefaultRef = useRef('');

  // 每次打开时重置：默认选中第一个 Pipeline，先用默认定义占位（版本底稿加载后会被覆盖）
  useEffect(() => {
    if (!open) return;
    setDefinitionJson(defaultDefinitionJson);
    lastDefaultRef.current = defaultDefinitionJson;
    if (entities.length > 0) {
      setWorkflowId((prev) => prev ?? entities[0].id);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, entities]);

  // 默认定义晚到且用户未改动过时替换（已被版本底稿覆盖的不会误伤：内容不再等于旧默认值）
  useEffect(() => {
    if (!open) return;
    setDefinitionJson((prev) => (prev === lastDefaultRef.current ? defaultDefinitionJson : prev));
    lastDefaultRef.current = defaultDefinitionJson;
  }, [open, defaultDefinitionJson]);

  // 版本列表加载完成（或切换 Pipeline）后：以最新版本为底稿，在其上修改出新一版
  useEffect(() => {
    if (!open || versions.length === 0) return;
    setDefinitionJson(prettyDefinitionJson(versions[0].definitionJson));
  }, [open, workflowId, versions]);

  const handleOk = async () => {
    if (workflowId === null) {
      message.warning('请选择要保存到的 Pipeline');
      return;
    }
    try {
      JSON.parse(definitionJson);
    } catch {
      message.warning('definitionJson 不是合法 JSON，请检查格式');
      return;
    }

    setSaving(true);
    try {
      const version = await saveWorkflowVersionDraft(workflowId, { definitionJson });
      message.success(`草稿 v${version.version} 已保存，可在「版本管理」中发布`);
      queryClient.invalidateQueries({ queryKey: WORKFLOW_ENTITIES_QUERY_KEY });
      queryClient.invalidateQueries({ queryKey: workflowVersionsKey(workflowId) });
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
      <div className={styles.formCol}>
        <div className={styles.formField}>
          <span className={`${styles.formLabel} ${styles.required}`}>保存到 Pipeline</span>
          <Select
            value={workflowId}
            onChange={setWorkflowId}
            placeholder="选择要挂载版本的 Pipeline"
            popupMatchSelectWidth={false}
            options={entities.map((e) => ({
              label: `${e.name}${e.latestVersion > 0 ? `（线上 v${e.latestVersion}）` : '（未发布）'}`,
              value: e.id,
            }))}
            className={styles.select}
          />
          {versions.length > 0 && (
            <span className={styles.formHint}>
              已按最新版本 v{versions[0].version} 预填，在此基础上修改
            </span>
          )}
        </div>
        <div className={styles.formField}>
          <span className={`${styles.formLabel} ${styles.required}`}>definitionJson</span>
          <TextArea
            value={definitionJson}
            onChange={(e) => setDefinitionJson(e.target.value)}
            autoSize={{ minRows: 12, maxRows: 20 }}
            className={styles.jsonEditor}
          />
        </div>
      </div>
    </Modal>
  );
}

export default SaveWorkflowDraftModal;
