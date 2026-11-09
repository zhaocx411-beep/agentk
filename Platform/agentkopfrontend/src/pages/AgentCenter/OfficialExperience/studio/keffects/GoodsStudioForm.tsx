import { Alert, Button, Input, Select } from 'antd';
import { ThunderboltOutlined } from '@ant-design/icons';
import { MediaUpload } from '@/components';
import {
  GOODS_STUDIO_ASPECT_RATIO_OPTIONS,
  GOODS_STUDIO_DURATION_OPTIONS,
  KEFFECTS_RESOLUTION_OPTIONS,
} from './constants';
import type { GoodsStudioModel } from './useGoodsStudioTask';
import type { StudioTemplatesModel } from '../form/useStudioTemplates';
import StudioTemplatePicker from '../form/StudioTemplatePicker';
import studio from '../CreationStudio.module.less';

interface GoodsStudioFormProps {
  model: GoodsStudioModel;
  templates: StudioTemplatesModel;
}

function GoodsStudioForm({ model, templates }: GoodsStudioFormProps) {
  const blocked = !model.canGenerate && !model.uploading;

  return (
    <div className={studio.formColumn}>
      <StudioTemplatePicker catalog={templates} disabled={model.isBusy} />
      <section className={`${studio.card} ${studio.formCard}`}>
        <div className={studio.cardHead}>
          <h2 className={studio.cardTitle}>创作参数</h2>
        </div>

        <div className={studio.formBody}>
          <div className={studio.field}>
            <div className={studio.fieldHead}>
              <span className={studio.fieldLabel}>
                商品参考图
                <span className={studio.requiredMark}>*</span>
              </span>
              <span className={studio.fieldMeta}>
                已选 {model.refImages.length}/{model.maxRefImages}
              </span>
            </div>
            <p className={studio.fieldTip}>最多 5 张，建议清晰正面图；可本地上传或从历史创作选择</p>
            <MediaUpload
              value={model.refImages}
              onChange={model.setRefImages}
              onUploadingChange={model.setUploading}
              accept="image"
              maxCount={model.maxRefImages}
              title="添加商品图"
              disabled={model.isBusy}
            />
          </div>

          <div className={studio.field}>
            <div className={studio.fieldHead}>
              <span className={studio.fieldLabel}>
                商品标题
                <span className={studio.requiredMark}>*</span>
              </span>
            </div>
            <Input
              size="large"
              className={studio.textInput}
              maxLength={500}
              allowClear
              value={model.title}
              placeholder="例如：YSL 圣罗兰黑管唇釉"
              onChange={(event) => model.setTitle(event.target.value)}
            />
          </div>

          <div className={studio.field}>
            <div className={studio.fieldHead}>
              <span className={studio.fieldLabel}>商品描述</span>
            </div>
            <div className={studio.textareaWrap}>
              <Input.TextArea
                className={studio.textareaInput}
                maxLength={2000}
                allowClear
                autoSize={{ minRows: 3, maxRows: 6 }}
                value={model.description}
                placeholder="卖点、材质、适用场景等，选填"
                onChange={(event) => model.setDescription(event.target.value)}
              />
            </div>
          </div>

          <div className={studio.field}>
            <div className={studio.fieldHead}>
              <span className={studio.fieldLabel}>分辨率</span>
            </div>
            <Select
              className={studio.fullWidth}
              value={model.resolution}
              options={KEFFECTS_RESOLUTION_OPTIONS}
              onChange={model.setResolution}
            />
          </div>

          <div className={studio.field}>
            <div className={studio.fieldHead}>
              <span className={studio.fieldLabel}>画幅比例</span>
            </div>
            <Select
              className={studio.fullWidth}
              value={model.aspectRatio}
              options={GOODS_STUDIO_ASPECT_RATIO_OPTIONS}
              onChange={model.setAspectRatio}
            />
          </div>

          <div className={studio.field}>
            <div className={studio.fieldHead}>
              <span className={studio.fieldLabel}>成片时长</span>
            </div>
            <Select
              className={studio.fullWidth}
              value={model.duration}
              options={GOODS_STUDIO_DURATION_OPTIONS}
              onChange={model.setDuration}
            />
          </div>
        </div>

        {blocked ? (
          <Alert type="warning" showIcon message="请先上传至少 1 张商品图，并填写商品标题" />
        ) : null}

        <div className={studio.formFooter}>
          <p className={studio.formFooterHint}>
            {model.isBusy ? '正在生成，成品在页面下方' : '生成结果在页面下方'}
          </p>
          <div className={studio.formFooterActions}>
            <Button
              size="large"
              type="primary"
              loading={model.isBusy}
              disabled={!model.canGenerate || model.isBusy}
              className={studio.generateBtn}
              icon={model.isBusy ? undefined : <ThunderboltOutlined />}
              onClick={() => void model.generate()}
            >
              {model.isBusy ? '生成中…' : '生成视频'}
            </Button>
          </div>
        </div>
      </section>
    </div>
  );
}

export default GoodsStudioForm;
