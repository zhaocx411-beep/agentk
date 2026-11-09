import { Alert, Button, Input, Select } from 'antd';
import { ThunderboltOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import { MediaUpload, type MediaAsset, type MediaKind } from '@/components';
import {
  CLOTHING_DUPE_VOICE_PACKS_QUERY_KEY,
  getClothingDupeVoicePacks,
} from '@/services/clothingDupe';
import {
  CLOTHING_DUPE_ASPECT_RATIO_OPTIONS,
  KEFFECTS_RESOLUTION_OPTIONS,
} from '../keffects/constants';
import type { ClothingDupeMedia, ClothingDupeModel } from './useClothingDupeTask';
import type { StudioTemplatesModel } from '../form/useStudioTemplates';
import { MEDIA_LIMIT_BY_FIELD } from './mediaFields';
import StudioTemplatePicker from '../form/StudioTemplatePicker';
import studio from '../CreationStudio.module.less';

interface ClothingDupeFormProps {
  model: ClothingDupeModel;
  templates: StudioTemplatesModel;
}

function toMediaAssets(items: ClothingDupeMedia[], mediaType: MediaKind): MediaAsset[] {
  return items.map((item) => ({
    id: item.id,
    url: item.url,
    name: item.name,
    mediaType,
    source: 'local',
  }));
}

function fromMediaAssets(items: MediaAsset[]): ClothingDupeMedia[] {
  return items.map((item) => ({
    id: item.id,
    name: item.name || item.url,
    url: item.url,
  }));
}

function ClothingDupeForm({ model, templates }: ClothingDupeFormProps) {
  const blocked = !model.canGenerate && !model.isUploading && !model.bgmUploading;
  const { data: voicePacks, isLoading: voiceLoading } = useQuery({
    queryKey: CLOTHING_DUPE_VOICE_PACKS_QUERY_KEY,
    queryFn: getClothingDupeVoicePacks,
    staleTime: 5 * 60 * 1000,
  });
  const voiceOptions = (Array.isArray(voicePacks) ? voicePacks : [])
    .filter((item) => item?.voiceId && item.displayName)
    .map((item) => ({ label: item.displayName, value: item.voiceId }));

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
                源视频
                <span className={studio.requiredMark}>*</span>
              </span>
              <span className={studio.fieldMeta}>
                已选 {model.sourceVideo.length}/{MEDIA_LIMIT_BY_FIELD.sourceVideo}
              </span>
            </div>
            <p className={studio.fieldTip}>必须且只能上传 1 个视频，作为换装的动作来源</p>
            <MediaUpload
              value={toMediaAssets(model.sourceVideo, 'video')}
              onChange={(items) => model.replaceMedia('sourceVideo', fromMediaAssets(items))}
              accept="video"
              maxCount={MEDIA_LIMIT_BY_FIELD.sourceVideo}
              title="选择源视频"
              disabled={model.isBusy}
            />
          </div>

          <div className={studio.field}>
            <div className={studio.fieldHead}>
              <span className={studio.fieldLabel}>
                商品图
                <span className={studio.requiredMark}>*</span>
              </span>
              <span className={studio.fieldMeta}>
                已选 {model.productImages.length}/{MEDIA_LIMIT_BY_FIELD.productImages}
              </span>
            </div>
            <p className={studio.fieldTip}>必须上传 1–5 张商品图片，建议清晰正面图</p>
            <MediaUpload
              value={toMediaAssets(model.productImages, 'image')}
              onChange={(items) => model.replaceMedia('productImages', fromMediaAssets(items))}
              accept="image"
              maxCount={MEDIA_LIMIT_BY_FIELD.productImages}
              title="选择商品图"
              materialCategoryDefault="product_image"
              disabled={model.isBusy}
            />
          </div>

          <div className={studio.field}>
            <div className={studio.fieldHead}>
              <span className={studio.fieldLabel}>模特图</span>
              <span className={studio.fieldMeta}>
                已选 {model.modelImages.length}/{MEDIA_LIMIT_BY_FIELD.modelImages}
              </span>
            </div>
            <p className={studio.fieldTip}>可选，最多 5 张</p>
            <MediaUpload
              value={toMediaAssets(model.modelImages, 'image')}
              onChange={(items) => model.replaceMedia('modelImages', fromMediaAssets(items))}
              accept="image"
              maxCount={MEDIA_LIMIT_BY_FIELD.modelImages}
              title="选择模特图"
              materialCategoryDefault="model_image"
              disabled={model.isBusy}
            />
          </div>

          <div className={studio.field}>
            <div className={studio.fieldHead}>
              <span className={studio.fieldLabel}>背景音乐</span>
              <span className={studio.fieldMeta}>已选 {model.bgm.length}/1</span>
            </div>
            <p className={studio.fieldTip}>可选，最多 1 个；可从历史创作或素材库选择，也可本地上传</p>
            <MediaUpload
              value={toMediaAssets(model.bgm, 'audio')}
              onChange={(items) => model.replaceMedia('bgm', fromMediaAssets(items))}
              onUploadingChange={model.setBgmUploading}
              accept="audio"
              maxCount={MEDIA_LIMIT_BY_FIELD.bgm}
              title="选择背景音乐"
              materialCategoryDefault="audio"
              disabled={model.isBusy}
            />
          </div>

          <div className={studio.field}>
            <div className={studio.fieldHead}>
              <span className={studio.fieldLabel}>创作提示词</span>
            </div>
            <div className={studio.textareaWrap}>
              <Input.TextArea
                className={studio.textareaInput}
                value={model.prompt}
                maxLength={500}
                allowClear
                autoSize={{ minRows: 3, maxRows: 6 }}
                placeholder="例如：红色连衣裙，自然走秀"
                onChange={(event) => model.setPrompt(event.target.value)}
              />
            </div>
          </div>

          <div className={studio.field}>
            <div className={studio.fieldHead}>
              <span className={studio.fieldLabel}>音色</span>
            </div>
            <Select
              className={studio.fullWidth}
              showSearch
              allowClear
              loading={voiceLoading}
              value={model.voiceId || undefined}
              options={voiceOptions}
              optionFilterProp="label"
              placeholder={voiceLoading ? '音色列表加载中…' : '请选择音色'}
              notFoundContent={voiceLoading ? '加载中…' : '暂无音色'}
              onChange={(value) => model.setVoiceId(value ?? '')}
            />
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
              options={CLOTHING_DUPE_ASPECT_RATIO_OPTIONS}
              onChange={model.setAspectRatio}
            />
          </div>
        </div>

        {blocked ? (
          <Alert type="warning" showIcon message="请先上传 1 个源视频和至少 1 张商品图" />
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

export default ClothingDupeForm;
