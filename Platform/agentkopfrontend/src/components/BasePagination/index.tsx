import { Pagination, type PaginationProps } from 'antd';
import styles from './BasePagination.module.less';

/**
 * BasePagination - 分页器组件
 * 在 antd Pagination 基础上提供统一样式和默认配置
 *
 * @example
 * <BasePagination current={1} pageSize={20} total={100} onChange={(page, size) => {}} />
 */
function BasePagination(props: PaginationProps) {
  return (
    <div className={styles.paginationWrapper}>
      <Pagination
        showSizeChanger
        showQuickJumper
        showTotal={(total) => `共 ${total} 条`}
        {...props}
      />
    </div>
  );
}

export default BasePagination;
