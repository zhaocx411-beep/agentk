/**
 * WorkflowLaunchModal - 官方工作流填参弹窗
 * 提交：无 session 时 quick session + pipeline-runs；有 session（再次生成）只跑 pipeline
 */
import { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Alert, Button, Modal, Spin } from 'antd';
import { message } from '@/utils/feedback';
import { pickOfficialUiFields } from '@/utils/officialUiFields';
import type { OfficialCatalogCardData } from '@/types/officialCatalog';
import {
  createOfficialWorkflowQuickSession,
  getOfficialWorkflowUiSchema,
  officialWorkflowSchemaKey,
} from '../api';
import { runOfficialPipeline } from '@/services/workflow';
import ArgField from '../OfficialExperience/studio/form/ArgField';
import type { FieldValues } from '../OfficialExperience/studio/lib/executionSpecs';
import { mergeComicDramaUiFields } from '../OfficialExperience/studio/lib/comicDrama';
import {
  buildSchemaDrivenSpec,
  emptyPipelineSpec,
} from '../OfficialExperience/studio/lib/schemaFields';
import ImageUrlListField from './ImageUrlListField';
import styles from './WorkflowLaunchModal.module.less';

const EMPTY_SETTINGS = { scene: '效果广告', project: '官方体验', tag: '' };

interface WorkflowLaunchModalProps {
  open: boolean;
  item: OfficialCatalogCardData | null;
  /** 已有 session 时再次生成，不再新建 session */
  sessionId?: string;
  onClose: () => void;
  onSubmitted: (sessionId: string, item: OfficialCatalogCardData) => void;
}

function WorkflowLaunchModal({
  open,
  item,
  sessionId,
  onClose,
  onSubmitted,
}: WorkflowLaunchModalProps) {
  const workflowKey = item?.workflowKey?.trim() || '';
  const [fieldValues, setFieldValues] = useState<FieldValues>({});
  const [imageUrls, setImageUrls] = useState<string[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  const { data: schemaData, isLoading: schemaLoading } = useQuery({
    queryKey: officialWorkflowSchemaKey(workflowKey),
    queryFn: () => getOfficialWorkflowUiSchema(workflowKey),
    enabled: open && !!workflowKey,
    staleTime: 5 * 60 * 1000,
  });

  const schemaFields = useMemo(() => pickOfficialUiFields(schemaData), [schemaData]);

  const catalogFields = item?.fields;
  const effectiveSchema =
    schemaFields.length > 0
      ? schemaFields
      : catalogFields && catalogFields.length > 0
        ? catalogFields
        : undefined;

  const spec = useMemo(() => {
    if (!workflowKey) {
      return emptyPipelineSpec('', '缺少 workflowKey，无法加载创作参数');
    }
    const pipelineKey = item?.pipelineName || workflowKey;
    const merged = mergeComicDramaUiFields(pipelineKey, effectiveSchema ?? []);
    if (merged.length > 0) return buildSchemaDrivenSpec(workflowKey, merged);
    if (schemaLoading) {
      return emptyPipelineSpec(workflowKey, '正在加载创作参数…');
    }
    return emptyPipelineSpec(
      workflowKey,
      '未从 capabilities 获取到配置字段，请确认 workflowKey 对应官配 Pipeline 是否已发布',
    );
  }, [workflowKey, effectiveSchema, schemaLoading, item?.pipelineName]);

  const specFingerprint = useMemo(
    () =>
      [
        spec.id,
        spec.usesImages ? 'img' : '',
        spec.imageFieldKey ?? '',
        ...spec.fields.map((field) => field.key),
      ].join('|'),
    [spec],
  );

  // 打开、切换工作流，或 capabilities 字段到位后重置表单
  useEffect(() => {
    if (!open || !item) return;
    const defaults = spec.defaults({ filledPrompt: '', sample: null });
    setFieldValues(defaults);
    setImageUrls([]);
    setFormError(null);
    // 用 fingerprint 捕获 schema 驱动字段变化；避免 defaults 引用抖动清空输入
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, item?.key, specFingerprint]);

  const fields = spec.fields.filter((field) => !!field?.key && !field.advanced);
  const imageFieldKey = spec.imageFieldKey ?? 'images';

  const handleSubmit = async () => {
    if (!item || !workflowKey || submitting) return;
    setFormError(null);
    const built = spec.build(fieldValues, { imageUrls, settings: EMPTY_SETTINGS });
    if (!built.ok) {
      setFormError(built.error);
      return;
    }
    if (built.payload.mode !== 'pipeline') {
      setFormError('当前仅支持工作流 Pipeline 提交');
      return;
    }

    setSubmitting(true);
    try {
      let sid = sessionId?.trim() || '';
      if (!sid) {
        const session = await createOfficialWorkflowQuickSession(workflowKey);
        sid = session.id;
      }
      await runOfficialPipeline(sid, {
        workflow: built.payload.workflow,
        // pipeline 认 official/defaults 的 pipelineName，workflowKey 的全名不在 session profile 里
        pipeline: item?.pipelineName?.trim() || built.payload.workflow,
        args: built.payload.args as Record<string, unknown>,
      });
      message.success(sessionId ? '已提交再次生成' : '任务已提交');
      onSubmitted(sid, item);
    } catch (error) {
      message.error(error instanceof Error ? error.message : '提交失败，请稍后重试');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal
      open={open}
      title={item ? `配置参数 · ${item.name}` : '配置参数'}
      onCancel={onClose}
      destroyOnHidden
      footer={
        <div className={styles.footer}>
          <Button onClick={onClose} disabled={submitting}>
            取消
          </Button>
          <Button type="primary" loading={submitting} onClick={() => void handleSubmit()}>
            {sessionId ? '再次生成' : '开始生成'}
          </Button>
        </div>
      }
      width={560}
    >
      {schemaLoading && !effectiveSchema ? (
        <div className={styles.form}>
          <Spin />
        </div>
      ) : (
        <div className={styles.form}>
          {formError ? <Alert type="error" showIcon message={formError} /> : null}
          {fields.map((field) => {
            return (
              <ArgField
                key={field.key}
                field={field}
                showKey
                value={fieldValues[field.key]}
                onChange={(value) =>
                  setFieldValues((prev) => ({
                    ...prev,
                    [field.key]: value,
                  }))
                }
                selectOptions={field.options ?? []}
              />
            );
          })}
          {spec.usesImages ? (
            <ImageUrlListField
              label={
                effectiveSchema?.find((field) => field.widget === 'image-url-list')?.label ||
                '商品图片'
              }
              fieldKey={imageFieldKey}
              value={imageUrls}
              onChange={setImageUrls}
              required={!!spec.imagesRequired}
            />
          ) : null}
          {schemaData?.description || spec.description ? (
            <p className={styles.fieldTip}>{schemaData?.description || spec.description}</p>
          ) : null}
        </div>
      )}
    </Modal>
  );
}

export default WorkflowLaunchModal;
