/**
 * 通用创作模版下拉：数据来自 usage=TEMPLATE + pipelineName
 */
import type { UIEvent } from 'react';
import { Empty, Select, Spin } from 'antd';
import type { StudioTemplatesModel } from './useStudioTemplates';
import { formatTemplateCatalogMeta } from '../lib/templatePickerMeta';
import styles from '../CreationStudio.module.less';

interface StudioTemplatePickerProps {
  catalog: StudioTemplatesModel;
  disabled?: boolean;
}

function StudioTemplatePicker({ catalog, disabled }: StudioTemplatePickerProps) {
  const {
    items,
    selectedId,
    total,
    loading,
    loadingMore,
    hasMore,
    error,
    select,
    clear,
    loadMore,
  } = catalog;
  const hasTemplate = !!selectedId;

  const handlePopupScroll = (event: UIEvent<HTMLDivElement>) => {
    const target = event.target as HTMLDivElement;
    if (!hasMore || loadingMore) return;
    if (target.scrollTop + target.clientHeight >= target.scrollHeight - 32) {
      loadMore();
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
                loading,
                error,
                loadedCount: items.length,
                total,
              })}
            </span>
          </div>
          <Spin spinning={loading}>
            {loading || items.length > 0 ? (
              <Select
                showSearch
                allowClear
                size="large"
                disabled={disabled}
                value={hasTemplate ? selectedId : undefined}
                onChange={(id) => {
                  if (id) select(id);
                }}
                onClear={() => clear()}
                placeholder={loading ? '加载中…' : '选择创作模版（可选）'}
                optionFilterProp="title"
                optionLabelProp="name"
                className={`${styles.fullWidth} ${styles.selectInput}`}
                notFoundContent="暂无创作模版"
                listHeight={280}
                onPopupScroll={handlePopupScroll}
                dropdownRender={(menu) => (
                  <>
                    {menu}
                    {loadingMore ? <div className={styles.templateLoadingMore}>加载更多…</div> : null}
                  </>
                )}
                options={items.map((item) => ({
                  value: item.id,
                  name: item.name,
                  title: `${item.name} ${item.description}`,
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
                description={error ? '创作模版加载失败' : '暂无创作模版'}
              />
            )}
          </Spin>
        </div>
      </div>
    </section>
  );
}

export default StudioTemplatePicker;
