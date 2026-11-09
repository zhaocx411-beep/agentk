/**
 * 创作表单：当前执行方式声明的入参 + 参考图 + 生成
 * 字段结构完全来自 executionSpecs 的声明，默认状态下用户什么都不用填
 * 模型类字段：schema enum 用下拉，否则手填，不请求 /agentk/models
 */
import { useState } from 'react';
import { Alert, Button, Collapse, Input } from 'antd';
import { LinkOutlined, PlusOutlined, ThunderboltOutlined } from '@ant-design/icons';
import { MediaUpload, type MediaAsset } from '@/components';
import ArgField from './ArgField';
import IntentTrace from './IntentTrace';
import type { CreationStudioModel } from '../hooks/useCreationStudio';
import type { ReferenceImage } from '../lib/types';
import styles from '../CreationStudio.module.less';

interface CreationFormProps {
  model: CreationStudioModel;
}

function toMediaAssets(refs: ReferenceImage[]): MediaAsset[] {
  return refs.map((ref) => ({
    id: ref.uid,
    url: ref.url,
    name: ref.name,
    mediaType: 'image',
    source: 'local',
  }));
}

function CreationForm({ model }: CreationFormProps) {
  const { spec, template, refImages, fieldValues, payloadError, isBusy } = model;
  const [refUrl, setRefUrl] = useState('');

  const isPipeline = spec.mode === 'pipeline';
  const fields = Array.isArray(spec.fields) ? spec.fields.filter((field) => !!field?.key) : [];
  const mainFields = fields.filter((field) => !field.advanced);
  const advancedFields = fields.filter((field) => field.advanced);
  const busyLabel = model.waitingJobs ? '后台任务处理中…' : '生成中…';
  // schema 声明了图片字段时，即使模版未给 limit 也要露出上传区
  const refImageLimit = Math.max(template?.refImageLimit ?? 0, spec.usesImages ? 8 : 0);
  const imageFieldLabel = spec.imageFieldLabel || (isPipeline ? '商品图片' : '参考图');
  const displayImageFieldLabel = imageFieldLabel.replace(/\s*URL\s*列表/gi, '') || imageFieldLabel;
  /** capabilities 无字段时顶部已有空态，底部不再重复打 payloadError */
  const schemaEmpty = isPipeline && mainFields.length === 0 && !spec.usesImages;

  return (
    <section className={`${styles.card} ${styles.formCard}`}>
      <div className={styles.cardHead}>
        <div>
          <h2 className={styles.cardTitle}>{isPipeline ? '创作参数' : '创作内容'}</h2>
        </div>
        {model.isFormDirty && (
          <Button size="small" type="link" onClick={model.resetFields}>
            恢复默认
          </Button>
        )}
      </div>

      {/* <div className={styles.formTip}>
        <InfoCircleFilled className={styles.formTipIcon} />
        <p>
          {isPipeline
            ? '详细填写以下信息，有助于生成更稳定的结果，并减少后续反复调整。'
            : '选模板即可开始，Agent 会自己拆解步骤并调用所需工具。'}
        </p>
      </div> */}

      <div className={styles.formBody}>
        {schemaEmpty ? (
          <Alert
            type="warning"
            showIcon
            message="暂无创作参数"
            description={
              spec.description ||
              'capabilities 未返回 fields，无法渲染配置项。请检查 workflowKey 是否与官配 Pipeline 一致。'
            }
          />
        ) : null}

        {mainFields.map((field) => {
          return (
            <ArgField
              key={field.key}
              field={field}
              showKey={false}
              value={fieldValues[field.key]}
              onChange={(value) => model.setFieldValue(field.key, value)}
              selectOptions={field.options ?? []}
            />
          );
        })}

        {spec.usesImages && refImageLimit > 0 && (
          <div className={styles.field}>
            <div className={styles.fieldHead}>
              <span className={styles.fieldLabel}>
                {displayImageFieldLabel}
                {spec.imagesRequired || isPipeline ? (
                  <span className={styles.requiredMark}>*</span>
                ) : null}
              </span>
              <span className={styles.fieldMeta}>
                已选 {refImages.length}/{refImageLimit}
              </span>
            </div>
            <p className={styles.fieldTip}>
              支持 PNG/JPG/WEBP/GIF；可从历史创作或素材库选择，也可粘贴图片 URL
            </p>
            <MediaUpload
              value={toMediaAssets(refImages)}
              onChange={(items) =>
                model.replaceRefImages(
                  items.map((item) => ({
                    uid: item.id,
                    name: item.name ?? item.url,
                    url: item.url,
                  })),
                )
              }
              onUploadingChange={model.setRefUploading}
              accept="image"
              maxCount={refImageLimit}
              title={`添加${displayImageFieldLabel}`}
              disabled={isBusy}
            />
            <div className={styles.refRow}>
              <Button
                icon={<LinkOutlined />}
                disabled={!model.sample || refImages.length >= refImageLimit}
                className={styles.mediaAction}
                onClick={model.useSampleImages}
              >
                用商品图
              </Button>
            </div>
            <div className={styles.refRow}>
              <Input
                value={refUrl}
                allowClear
                onChange={(e) => setRefUrl(e.target.value)}
                onPressEnter={() => {
                  model.addRefUrl(refUrl);
                  setRefUrl('');
                }}
                placeholder="或粘贴图片 URL"
                size="large"
                className={styles.refUrlInput}
              />
              <Button
                type="primary"
                size="large"
                icon={<PlusOutlined />}
                aria-label="添加图片链接"
                title="添加图片链接"
                disabled={!refUrl.trim() || refImages.length >= refImageLimit}
                className={styles.refAddButton}
                onClick={() => {
                  model.addRefUrl(refUrl);
                  setRefUrl('');
                }}
              />
            </div>
          </div>
        )}
      </div>

      {advancedFields.length > 0 ? (
        <Collapse
          ghost
          className={styles.collapse}
          items={[
            {
              key: 'settings',
              label: '更多参数',
              children: (
                <div className={styles.settingsGrid}>
                  {advancedFields.map((field) => {
                    return (
                      <ArgField
                        key={field.key}
                        field={field}
                        showKey={false}
                        value={fieldValues[field.key]}
                        onChange={(value) => model.setFieldValue(field.key, value)}
                        selectOptions={field.options ?? []}
                      />
                    );
                  })}
                </div>
              ),
            },
          ]}
        />
      ) : null}

      <Collapse
        ghost
        className={styles.collapse}
        items={[
          {
            key: 'trace',
            label: '查看模版处理详情',
            children: <IntentTrace model={model} />,
          },
        ]}
      />

      {payloadError && !schemaEmpty ? (
        <Alert type="warning" showIcon message={payloadError} />
      ) : null}

      <div className={styles.formFooter}>
        <p className={styles.formFooterHint}>
          {isBusy ? '正在生成，成品在页面下方' : '生成结果在页面下方'}
        </p>
        <div className={styles.formFooterActions}>
          <Button
            size="large"
            disabled={isBusy}
            onClick={model.resetFields}
            className={styles.formFooterGhost}
          >
            重置
          </Button>
          <Button
            size="large"
            type="primary"
            loading={isBusy}
            disabled={(!isBusy && !!payloadError) || model.refUploading}
            className={styles.generateBtn}
            icon={isBusy ? undefined : <ThunderboltOutlined />}
            onClick={isBusy ? undefined : () => void model.generate()}
          >
            {isBusy ? busyLabel : '生成视频'}
          </Button>
        </div>
      </div>
    </section>
  );
}

export default CreationForm;
