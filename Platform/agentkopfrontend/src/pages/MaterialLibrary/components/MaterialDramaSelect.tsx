import { Select } from 'antd';
import type { MaterialDramaFilter } from '../types';
import { materialDramaSelectOptions } from '../utils';

interface MaterialDramaSelectProps {
  value: MaterialDramaFilter;
  dramas: readonly string[];
  unlabeled: boolean;
  loading?: boolean;
  className?: string;
  onChange: (value: MaterialDramaFilter) => void;
  onOpenChange?: (open: boolean) => void;
}

/** 短剧/漫剧下按 extraFields.drama 再筛一层 */
export function MaterialDramaSelect({
  value,
  dramas,
  unlabeled,
  loading = false,
  className,
  onChange,
  onOpenChange,
}: MaterialDramaSelectProps) {
  return (
    <Select<MaterialDramaFilter>
      value={value}
      options={materialDramaSelectOptions(dramas, unlabeled)}
      onChange={onChange}
      onOpenChange={onOpenChange}
      loading={loading}
      showSearch
      optionFilterProp="label"
      popupMatchSelectWidth={false}
      placeholder="剧名"
      className={className}
    />
  );
}
