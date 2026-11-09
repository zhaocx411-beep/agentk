import { Alert, Button, Input } from 'antd';
import { ThunderboltOutlined } from '@ant-design/icons';
import type { GoodsScraperModel } from './useGoodsScraperTask';
import type { StudioTemplatesModel } from '../form/useStudioTemplates';
import StudioTemplatePicker from '../form/StudioTemplatePicker';
import studio from '../CreationStudio.module.less';

interface GoodsScraperFormProps {
  model: GoodsScraperModel;
  templates: StudioTemplatesModel;
}

function GoodsScraperForm({ model, templates }: GoodsScraperFormProps) {
  const blocked = !model.canGenerate;

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
                商品详情页 URL
                <span className={studio.requiredMark}>*</span>
              </span>
            </div>
            <p className={studio.fieldTip}>目前支持淘宝 / 天猫 / 京东公网商品链接，可带 skuId</p>
            <Input
              size="large"
              className={studio.textInput}
              allowClear
              value={model.url}
              placeholder="https://item.taobao.com/item.htm?id=…"
              onChange={(event) => model.setUrl(event.target.value)}
            />
          </div>
        </div>

        {blocked ? (
          <Alert type="warning" showIcon message="请填写以 http:// 或 https:// 开头的商品详情页链接" />
        ) : null}

        <div className={studio.formFooter}>
          <p className={studio.formFooterHint}>
            {model.isBusy ? '正在抓取，结果在页面下方' : '抓取结果在页面下方'}
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
              {model.isBusy ? '抓取中…' : '开始抓取'}
            </Button>
          </div>
        </div>
      </section>
    </div>
  );
}

export default GoodsScraperForm;
