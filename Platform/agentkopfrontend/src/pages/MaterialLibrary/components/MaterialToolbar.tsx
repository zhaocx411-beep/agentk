import { Button, Select, Tooltip } from 'antd';
import { ReloadOutlined } from '@ant-design/icons';
import { materialCategoryOptionsForKind } from '../constants';
import type { MaterialCategoryFilter, MaterialDramaFilter } from '../types';
import { MaterialDramaSelect } from './MaterialDramaSelect';
import styles from '../MaterialLibrary.module.less';

interface MaterialToolbarProps {
  category: MaterialCategoryFilter;
  drama: MaterialDramaFilter;
  dramas: readonly string[];
  unlabeledDrama: boolean;
  dramaLoading?: boolean;
  searching: boolean;
  onCategoryChange: (category: MaterialCategoryFilter) => void;
  onDramaChange: (drama: MaterialDramaFilter) => void;
  onRefresh: () => void;
}

/** 按 category 筛选素材；短剧/漫剧可再按剧名筛 */
export function MaterialToolbar({
  category,
  drama,
  dramas,
  unlabeledDrama,
  dramaLoading = false,
  searching,
  onCategoryChange,
  onDramaChange,
  onRefresh,
}: MaterialToolbarProps) {
  return (
    <div className={styles.toolbar}>
      <Select<MaterialCategoryFilter>
        value={category}
        options={materialCategoryOptionsForKind('all')}
        onChange={onCategoryChange}
        className={styles.categorySelect}
      />
      {category === 'short_drama' ? (
        <MaterialDramaSelect
          value={drama}
          dramas={dramas}
          unlabeled={unlabeledDrama}
          loading={dramaLoading}
          className={styles.dramaSelect}
          onChange={onDramaChange}
        />
      ) : null}
      <Tooltip title="刷新素材">
        <span className={styles.toolbarRefresh}>
          <Button
            icon={<ReloadOutlined />}
            loading={searching}
            onClick={onRefresh}
            className={styles.filterBtn}
          />
        </span>
      </Tooltip>
    </div>
  );
}
