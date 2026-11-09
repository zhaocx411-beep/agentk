import { Select } from 'antd';
import { MaterialDramaSelect } from '@/pages/MaterialLibrary/components/MaterialDramaSelect';
import { materialCategoryOptionsForKinds } from '@/pages/MaterialLibrary/constants';
import type { MaterialCategoryFilter, MaterialDramaFilter } from '@/pages/MaterialLibrary/types';
import type { MediaKind, MediaPickerTab, MediaSortOrder } from './types';
import { kindLabel } from './utils';
import styles from './MediaUpload.module.less';

interface MediaPickerToolbarProps {
  tab: MediaPickerTab;
  kinds: MediaKind[];
  filterKind: MediaKind | 'all';
  sort: MediaSortOrder;
  materialCategory: MaterialCategoryFilter;
  materialDrama: MaterialDramaFilter;
  dramas: readonly string[];
  unlabeledDrama: boolean;
  dramaLoading: boolean;
  categoryLocked: boolean;
  onFilterKindChange: (value: MediaKind | 'all') => void;
  onSortChange: (value: MediaSortOrder) => void;
  onCategoryChange: (value: MaterialCategoryFilter) => void;
  onDramaChange: (value: MaterialDramaFilter) => void;
}

export function MediaPickerToolbar({
  tab,
  kinds,
  filterKind,
  sort,
  materialCategory,
  materialDrama,
  dramas,
  unlabeledDrama,
  dramaLoading,
  categoryLocked,
  onFilterKindChange,
  onSortChange,
  onCategoryChange,
  onDramaChange,
}: MediaPickerToolbarProps) {
  return (
    <div className={styles.toolbar}>
      <span className={styles.scopeLabel}>
        {tab === 'material' ? '素材分类' : '所有作品'}
      </span>
      <div className={styles.toolbarRight}>
        {tab === 'material' ? (
          <>
            <Select<MaterialCategoryFilter>
              value={materialCategory}
              onChange={onCategoryChange}
              options={materialCategoryOptionsForKinds(kinds)}
              disabled={categoryLocked}
              className={styles.categorySelect}
            />
            {materialCategory === 'short_drama' ? (
              <MaterialDramaSelect
                value={materialDrama}
                dramas={dramas}
                unlabeled={unlabeledDrama}
                loading={dramaLoading}
                className={styles.dramaSelect}
                onChange={onDramaChange}
              />
            ) : null}
          </>
        ) : (
          <>
            {kinds.length > 1 ? (
              <Select
                value={filterKind}
                onChange={onFilterKindChange}
                options={[
                  { value: 'all', label: '全部' },
                  ...kinds.map((kind) => ({ value: kind, label: kindLabel(kind) })),
                ]}
                className={styles.filterSelect}
              />
            ) : (
              <span className={styles.kindHint}>{kindLabel(kinds[0])}</span>
            )}
            <Select
              value={sort}
              onChange={onSortChange}
              options={[
                { value: 'time-desc', label: '时间降序' },
                { value: 'time-asc', label: '时间升序' },
              ]}
              className={styles.filterSelect}
            />
          </>
        )}
      </div>
    </div>
  );
}
