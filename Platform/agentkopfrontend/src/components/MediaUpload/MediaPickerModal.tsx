import { useEffect, useMemo, useState } from 'react';
import { Button, Modal, Tabs } from 'antd';
import { message } from '@/utils/feedback';
import {
  defaultMaterialCategoryForKinds,
  materialCategoryOptionsForKinds,
} from '@/pages/MaterialLibrary/constants';
import type { MaterialCategoryFilter, MaterialDramaFilter } from '@/pages/MaterialLibrary/types';
import { MATERIAL_DRAMA_ALL } from '@/pages/MaterialLibrary/types';
import { MediaPickerGrid } from './MediaPickerGrid';
import { MediaPickerToolbar } from './MediaPickerToolbar';
import type { MediaAsset, MediaKind, MediaPickerTab, MediaSortOrder } from './types';
import { isKnownMediaOversize, mediaOversizeMessage } from '@/services/mediaSizeGuard';
import { pickerTitle } from './utils';
import { useMediaPickerList } from './useMediaPickerList';
import styles from './MediaUpload.module.less';

interface MediaPickerModalProps {
  open: boolean;
  kinds: MediaKind[];
  /** 不传或 null 表示不限可选数量 */
  maxCount?: number | null;
  existing: MediaAsset[];
  initialTab?: MediaPickerTab;
  /** 素材库默认分类（如商品图/音频）；单类型选择时会自动落到对应分类 */
  materialCategoryDefault?: MaterialCategoryFilter;
  onCancel: () => void;
  onConfirm: (picked: MediaAsset[]) => void;
}

const TAB_ITEMS = [
  { key: 'portfolio', label: '历史创作' },
  { key: 'uploads', label: '历史上传' },
  { key: 'material', label: '素材库' },
] as const;

export function MediaPickerModal({
  open,
  kinds,
  maxCount,
  existing,
  initialTab = 'portfolio',
  materialCategoryDefault,
  onCancel,
  onConfirm,
}: MediaPickerModalProps) {
  const unlimited = maxCount == null;
  const remaining = unlimited ? Number.POSITIVE_INFINITY : Math.max(maxCount - existing.length, 0);
  const [tab, setTab] = useState<MediaPickerTab>('portfolio');
  const [filterKind, setFilterKind] = useState<MediaKind | 'all'>(
    kinds.length === 1 ? kinds[0] : 'all',
  );
  const [sort, setSort] = useState<MediaSortOrder>('time-desc');
  const [materialCategory, setMaterialCategory] = useState<MaterialCategoryFilter>('all');
  const [materialDrama, setMaterialDrama] = useState<MaterialDramaFilter>(MATERIAL_DRAMA_ALL);
  const [page, setPage] = useState(1);
  const [picked, setPicked] = useState<MediaAsset[]>([]);
  const kindsKey = kinds.join(',');

  const categoryLocked =
    kinds.length === 1 && materialCategoryOptionsForKinds(kinds).length <= 1;

  useEffect(() => {
    if (!open) return;
    const nextKinds = kindsKey.split(',').filter(Boolean) as MediaKind[];
    setTab(initialTab);
    setFilterKind(nextKinds.length === 1 ? nextKinds[0] : 'all');
    setSort('time-desc');
    setMaterialCategory(
      materialCategoryDefault ?? defaultMaterialCategoryForKinds(nextKinds),
    );
    setMaterialDrama(MATERIAL_DRAMA_ALL);
    setPage(1);
    setPicked([]);
  }, [initialTab, kindsKey, materialCategoryDefault, open]);

  const list = useMediaPickerList({
    open,
    tab,
    kinds,
    filterKind,
    sort,
    page,
    materialCategory,
    materialDrama,
  });

  const selectedIds = useMemo(() => new Set(picked.map((item) => item.id)), [picked]);
  const existingUrls = useMemo(() => new Set(existing.map((item) => item.url)), [existing]);

  const handleToggle = (item: MediaAsset) => {
    if (existingUrls.has(item.url)) {
      message.info('该素材已添加');
      return;
    }
    setPicked((prev) => {
      if (prev.some((row) => row.id === item.id)) {
        return prev.filter((row) => row.id !== item.id);
      }
      if (isKnownMediaOversize(item.size)) {
        message.error(mediaOversizeMessage(item.name));
        return prev;
      }
      if (!unlimited && prev.length >= remaining) {
        message.warning(`最多再选择 ${remaining} 个`);
        return prev;
      }
      return [...prev, item];
    });
  };

  const handleConfirm = () => {
    const allowed = picked.filter((item) => !isKnownMediaOversize(item.size));
    const blocked = picked.filter((item) => isKnownMediaOversize(item.size));
    if (blocked.length > 0) {
      message.error(
        blocked.length === 1
          ? mediaOversizeMessage(blocked[0].name)
          : `以下素材超过上限：${blocked.map((item) => item.name || '素材').join('、')}`,
      );
    }
    if (allowed.length === 0) return;
    onConfirm(allowed);
  };

  const count = picked.length;
  const title = unlimited
    ? `${pickerTitle(kinds)} (已选 ${count})`
    : `${pickerTitle(kinds)} (${count}/${remaining})`;

  return (
    <Modal
      title={title}
      open={open}
      onCancel={onCancel}
      width={720}
      centered
      destroyOnHidden
      className={styles.pickerModal}
      wrapClassName={styles.pickerModalWrap}
      footer={
        <div className={styles.modalFooter}>
          <Button onClick={onCancel}>取消</Button>
          <Button type="primary" disabled={count === 0} onClick={handleConfirm}>
            确定
          </Button>
        </div>
      }
    >
      <div className={styles.pickerMain}>
        <Tabs
          className={styles.pickerTabs}
          activeKey={tab}
          onChange={(key) => {
            setTab(key as MediaPickerTab);
            setMaterialCategory(
              materialCategoryDefault ?? defaultMaterialCategoryForKinds(kinds),
            );
            setMaterialDrama(MATERIAL_DRAMA_ALL);
            setPage(1);
            setPicked([]);
          }}
          items={TAB_ITEMS.map((item) => ({ key: item.key, label: item.label }))}
        />
        <MediaPickerToolbar
          tab={tab}
          kinds={kinds}
          filterKind={filterKind}
          sort={sort}
          materialCategory={materialCategory}
          materialDrama={materialDrama}
          dramas={list.dramas}
          unlabeledDrama={list.unlabeledDrama}
          dramaLoading={list.dramaLoading}
          categoryLocked={categoryLocked}
          onFilterKindChange={(value) => {
            setFilterKind(value);
            setPage(1);
          }}
          onSortChange={(value) => {
            setSort(value);
            setPage(1);
          }}
          onCategoryChange={(value) => {
            setMaterialCategory(value);
            setMaterialDrama(MATERIAL_DRAMA_ALL);
            setPage(1);
          }}
          onDramaChange={(value) => {
            setMaterialDrama(value);
            setPage(1);
          }}
        />
        <MediaPickerGrid
          items={list.items}
          selectedIds={selectedIds}
          existingUrls={existingUrls}
          remaining={remaining}
          loading={list.isLoading}
          fetching={list.isFetching}
          error={list.isError}
          page={page}
          pageSize={list.pageSize}
          total={list.total}
          onRetry={() => void list.refetch()}
          onToggle={handleToggle}
          onPageChange={setPage}
        />
      </div>
    </Modal>
  );
}
