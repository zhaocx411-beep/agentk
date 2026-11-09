import { Button, Input, Select } from 'antd';
import { ReloadOutlined, SearchOutlined } from '@ant-design/icons';
import type { OssToolCategory, OssToolFilters as Filters, OssToolForm } from '../types';
import styles from '../OssToolLibrary.module.less';

interface OssToolFiltersProps {
  filters: Filters;
  categories: OssToolCategory[];
  forms: OssToolForm[];
  licenses: string[];
  onChange: (filters: Filters) => void;
  onReset: () => void;
}

export function OssToolFilters({
  filters,
  categories,
  forms,
  licenses,
  onChange,
  onReset,
}: OssToolFiltersProps) {
  return (
    <div className={styles.filters} aria-label="工具筛选">
      <Input
        allowClear
        prefix={<SearchOutlined className={styles.searchIcon} />}
        placeholder="搜索名称、能力或标签"
        value={filters.keyword}
        onChange={(event) => onChange({ ...filters, keyword: event.target.value })}
        className={styles.search}
      />
      <Select
        allowClear
        placeholder="全部分类"
        value={filters.category}
        options={categories.map((value) => ({ value, label: value }))}
        onChange={(category) => onChange({ ...filters, category })}
        className={styles.filterSelect}
      />
      <Select
        allowClear
        placeholder="全部形态"
        value={filters.form}
        options={forms.map((value) => ({ value, label: value }))}
        onChange={(form) => onChange({ ...filters, form })}
        className={styles.filterSelect}
      />
      <Select
        allowClear
        placeholder="全部许可证"
        value={filters.license}
        options={licenses.map((value) => ({ value, label: value }))}
        onChange={(license) => onChange({ ...filters, license })}
        className={styles.filterSelect}
      />
      <Button icon={<ReloadOutlined />} onClick={onReset}>
        重置
      </Button>
    </div>
  );
}
