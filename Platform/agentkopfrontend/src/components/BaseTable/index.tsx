import { Table as AntTable, type TableProps as AntTableProps } from 'antd';
import styles from './BaseTable.module.less';

interface BaseTableProps<T> extends Omit<AntTableProps<T>, 'pagination'> {
  /** 继承 antd Table 全量能力，内部分页器默认关闭 */
}

/**
 * BaseTable - 纯表格组件
 * 在 antd Table 基础上提供统一样式（滚动条、边框），不处理分页器
 * 配合 BasePagination / DataTable 使用
 *
 * @example
 * <BaseTable columns={columns} dataSource={data} loading={loading} />
 */
function BaseTable<T extends object>(props: BaseTableProps<T>) {
  return (
    <div className={styles.tableWrapper}>
      <AntTable<T> rowKey="id" size="middle" pagination={false} {...props} />
    </div>
  );
}

export default BaseTable;
