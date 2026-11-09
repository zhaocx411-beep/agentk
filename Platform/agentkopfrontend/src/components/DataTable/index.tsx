import { useEffect } from 'react';
import type { TableProps as AntTableProps } from 'antd';
import BaseTable from '@/components/BaseTable';
import BasePagination from '@/components/BasePagination';
import { useTableScrollY } from '@/hooks';
import styles from './DataTable.module.less';

interface DataTablePagination {
  current: number;
  pageSize: number;
  total: number;
  onChange: (page: number, pageSize?: number) => void;
}

interface DataTableProps<T> extends Omit<AntTableProps<T>, 'pagination' | 'scroll'> {
  /** 分页配置，传 false 关闭分页 */
  pagination: DataTablePagination | false;
  /** 自动计算滚动高度，默认 true */
  autoScrollY?: boolean;
  /** 自定义滚动高度（autoScrollY 为 false 时生效） */
  scroll?: AntTableProps<T>['scroll'];
}

/**
 * DataTable - 数据表格组件
 * 封装 BaseTable + BasePagination + 自动高度计算
 * 业务页面直接使用，无需手动管理 scrollY 和分页器布局
 *
 * @example
 * <DataTable
 *   columns={columns}
 *   dataSource={data}
 *   loading={loading}
 *   pagination={{ current: 1, pageSize: 20, total: 100, onChange }}
 * />
 */
function DataTable<T extends object>(props: DataTableProps<T>) {
  const { pagination, autoScrollY = true, scroll, ...restProps } = props;
  const { ref, scrollY, recalculate } = useTableScrollY<HTMLDivElement>();

  // 数据变化时重新计算高度
  useEffect(() => {
    if (autoScrollY) {
      recalculate();
    }
  }, [autoScrollY, restProps.dataSource, recalculate]);

  const finalScroll = autoScrollY ? { ...scroll, y: scrollY } : scroll;

  return (
    <div className={styles.dataTableWrapper} ref={ref}>
      <BaseTable<T> {...restProps} scroll={finalScroll} />
      {pagination !== false && (
        <BasePagination
          current={pagination.current}
          pageSize={pagination.pageSize}
          total={pagination.total}
          onChange={pagination.onChange}
          onShowSizeChange={pagination.onChange}
        />
      )}
    </div>
  );
}

export default DataTable;
