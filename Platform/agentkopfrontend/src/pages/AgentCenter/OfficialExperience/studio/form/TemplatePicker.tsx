/**
 * 模版选择区：创作模版来自 datasets/tasks/query?usage=TEMPLATE&pipelineName=
 * 下拉支持滚动加载更多
 */
import type { UIEvent } from 'react';
import { Empty, Select, Spin } from 'antd';
import type { CreationStudioModel } from '../hooks/useCreationStudio';
import { formatTemplateCatalogMeta } from '../lib/templatePickerMeta';
import styles from '../CreationStudio.module.less';

interface TemplatePickerProps {
  model: CreationStudioModel;
}

function TemplatePicker({ model }: TemplatePickerProps) {
  const {
    template,
    templates,
    templatesTotal,
    templatesLoading,
    templatesLoadingMore,
    templatesHasMore,
    templatesError,
    hasTemplate,
    sample,
    loadMoreTemplates,
  } = model;
  const handlePopupScroll = (event: UIEvent<HTMLDivElement>) => {
    const target = event.target as HTMLDivElement;
    if (!templatesHasMore || templatesLoadingMore) return;
    if (target.scrollTop + target.clientHeight >= target.scrollHeight - 32) {
      loadMoreTemplates();
    }
  };

  return (
    <section className={`${styles.card} ${styles.formCard}`}>
      <div className={styles.formBody}>
        <div className={styles.field}>
          <div className={styles.fieldHead}>
            <span className={styles.fieldLabel}>创作模版</span>
            <span className={styles.fieldMeta}>
              {formatTemplateCatalogMeta({
                loading: templatesLoading,
                error: templatesError,
                loadedCount: templates.length,
                total: templatesTotal,
              })}
            </span>
          </div>
          <Spin spinning={templatesLoading}>
            {templatesLoading || templates.length > 0 ? (
              <Select
                showSearch
                allowClear
                size="large"
                value={hasTemplate ? template.id : undefined}
                onChange={(id) => {
                  if (id) model.selectTemplate(id);
                }}
                onClear={() => model.clearTemplate()}
                placeholder={templatesLoading ? '加载中…' : '选择创作模版（可选）'}
                optionFilterProp="title"
                optionLabelProp="name"
                className={`${styles.fullWidth} ${styles.selectInput}`}
                notFoundContent="暂无创作模版"
                listHeight={280}
                onPopupScroll={handlePopupScroll}
                dropdownRender={(menu) => (
                  <>
                    {menu}
                    {templatesLoadingMore ? (
                      <div className={styles.templateLoadingMore}>加载更多…</div>
                    ) : null}
                  </>
                )}
                options={templates.map((item) => ({
                  value: item.id,
                  name: item.name,
                  title: `${item.name} ${item.tags.join(' ')}`,
                  label: (
                    <div className={styles.templateOption}>
                      <span className={styles.templateOptionName}>{item.name}</span>
                      <span className={styles.templateOptionDesc}>{item.description}</span>
                    </div>
                  ),
                }))}
              />
            ) : (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description={templatesError ? '创作模版加载失败' : '暂无创作模版'}
              />
            )}
          </Spin>
          {hasTemplate && sample ? (
            <details className={styles.sampleDetails}>
              <summary className={styles.sampleSummary}>
                {sample.coverUrls[0] && (
                  <img className={styles.sampleSummaryCover} src={sample.coverUrls[0]} alt="" />
                )}
                <span className={styles.sampleSummaryText}>
                  {sample.category}
                  {sample.price ? ` · ¥${sample.price}` : ''}
                </span>
                <span className={styles.sampleSummaryAction}>详情</span>
              </summary>
              <div className={styles.sampleCard}>
                <dl className={styles.sampleMeta}>
                  <div className={styles.sampleRow}>
                    <dt>主图/详情</dt>
                    <dd>
                      {sample.coverUrls.length} 张主图 / {sample.detailImages.length} 张详情图
                    </dd>
                  </div>
                  <div className={styles.sampleRow}>
                    <dt>价格</dt>
                    <dd>{sample.price || '-'}</dd>
                  </div>
                  <div className={styles.sampleRow}>
                    <dt>属性</dt>
                    <dd className={styles.sampleAttrs}>{sample.attributes || '-'}</dd>
                  </div>
                  <div className={styles.sampleRow}>
                    <dt>链接</dt>
                    <dd className={styles.sampleLink}>{sample.link || '-'}</dd>
                  </div>
                </dl>
              </div>
            </details>
          ) : null}
        </div>
      </div>
    </section>
  );
}

export default TemplatePicker;
