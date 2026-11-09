/**
 * 新建 Pipeline 弹窗
 * 创建一个全新的 Pipeline 实体并把 definitionJson 存为其首个草稿版本
 */
import { useEffect, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { Modal, Input, Select } from 'antd';
import { message } from '@/utils/feedback';
import {
  WORKFLOW_ENTITIES_QUERY_KEY,
  createWorkflowEntity,
  saveWorkflowVersionDraft,
} from '@/services/workflowPersistence';
import type { CatalogPipeline } from '@/pages/AgentPlayground/types';
import { getPipelineDescription } from '../utils';
import styles from '../WorkflowRunner.module.less';

const { TextArea } = Input;

interface NewWorkflowModalProps {
  open: boolean;
  /** 打开时预填的定义 JSON（由当前选中的 Pipeline 生成骨架） */
  defaultDefinitionJson: string;
  /** 实例能力目录中的 Pipeline */
  pipelines: CatalogPipeline[];
  onClose: () => void;
}

function NewWorkflowModal({
  open,
  defaultDefinitionJson,
  pipelines,
  onClose,
}: NewWorkflowModalProps) {
  const queryClient = useQueryClient();
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [definitionJson, setDefinitionJson] = useState('');
  const [saving, setSaving] = useState(false);

  /** 最近一次应用的默认内容：默认定义晚到（profile 异步拉取）时，仅在用户未编辑过时替换 */
  const lastDefaultRef = useRef('');

  // 每次打开时重置
  useEffect(() => {
    if (!open) return;
    setName('');
    setDescription('');
    setDefinitionJson(defaultDefinitionJson);
    lastDefaultRef.current = defaultDefinitionJson;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  // 默认定义晚到且用户未改动过时替换
  useEffect(() => {
    if (!open) return;
    setDefinitionJson((prev) => (prev === lastDefaultRef.current ? defaultDefinitionJson : prev));
    lastDefaultRef.current = defaultDefinitionJson;
  }, [open, defaultDefinitionJson]);

  // 目录异步到达后默认选择第一项，并用其元数据补齐创建 Workflow 的必填描述。
  useEffect(() => {
    if (!open || name || pipelines.length === 0) return;
    const first = pipelines[0];
    setName(first.name);
    setDescription(getPipelineDescription(first));
  }, [open, name, pipelines]);

  const handlePipelineChange = (pipelineName: string) => {
    setName(pipelineName);
    const pipeline = pipelines.find((item) => item.name === pipelineName);
    if (pipeline) setDescription(getPipelineDescription(pipeline));
  };

  const handleOk = async () => {
    if (!name.trim() || !description.trim()) {
      message.warning('请填写 Pipeline 的名称和描述');
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
      // 先建逻辑实体，再把定义存为首个草稿版本
      const entity = await createWorkflowEntity({
        name: name.trim(),
        description: description.trim(),
      });
      const version = await saveWorkflowVersionDraft(entity.id, { definitionJson });
      message.success(`Pipeline「${entity.name}」已创建，草稿 v${version.version} 已保存`);
      queryClient.invalidateQueries({ queryKey: WORKFLOW_ENTITIES_QUERY_KEY });
      onClose();
    } catch {
      // 错误提示由 request 拦截器统一处理
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      title="新建 Pipeline"
      open={open}
      onOk={handleOk}
      onCancel={onClose}
      okText="创建"
      cancelText="取消"
      confirmLoading={saving}
      width={860}
      destroyOnHidden
    >
      <div className={styles.formCol}>
        <div className={styles.formField}>
          <span className={`${styles.formLabel} ${styles.required}`}>Pipeline 名称</span>
          <Select
            value={name}
            onChange={handlePipelineChange}
            placeholder="从实例能力目录中选择 Pipeline"
            options={pipelines.map((pipeline) => ({
              value: pipeline.name,
              label: pipeline.name,
            }))}
            showSearch
            optionFilterProp="label"
            notFoundContent="当前实例未上报 Pipeline 能力"
          />
        </div>
        <div className={styles.formField}>
          <span className={`${styles.formLabel} ${styles.required}`}>Pipeline 描述</span>
          <Input
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Pipeline 的描述"
            maxLength={256}
          />
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

export default NewWorkflowModal;
